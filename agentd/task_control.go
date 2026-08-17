package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// taskControlServer is deliberately narrower than agentd. A sandboxed worker
// may manage schedules for its own main task, but cannot reach task, approval,
// provider, or board control APIs.
type taskControlServer struct {
	current  *task
	path     string
	listener *net.UnixListener
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	handlers sync.WaitGroup
}

func (current *task) taskControlDirectory() string {
	return filepath.Join(current.manager.cfg.TaskControlRoot, current.snapshot().ID)
}

func (current *task) taskControlPath() string {
	return filepath.Join(current.taskControlDirectory(), "c.sock")
}

func (current *task) startTaskControlSocket() (string, error) {
	current.mu.Lock()
	if current.control != nil {
		path := current.control.path
		current.mu.Unlock()
		return path, nil
	}
	current.mu.Unlock()
	path := current.taskControlPath()
	if len(path) > 100 {
		return "", fmt.Errorf("task control socket path is too long")
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := removeTaskControlSocket(path); err != nil {
		return "", err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return "", err
	}
	server := &taskControlServer{current: current, path: path, listener: listener, stop: make(chan struct{}), done: make(chan struct{})}
	current.mu.Lock()
	if current.control != nil {
		current.mu.Unlock()
		_ = listener.Close()
		_ = os.Remove(path)
		return current.control.path, nil
	}
	current.control = server
	current.mu.Unlock()
	go server.serve()
	return path, nil
}

func removeTaskControlSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("task control path is not a socket")
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return fmt.Errorf("task control socket has an unexpected owner")
	}
	return os.Remove(path)
}

func (current *task) stopTaskControlSocket() {
	current.mu.Lock()
	control := current.control
	current.control = nil
	current.mu.Unlock()
	if control != nil {
		control.close()
	}
}

func (server *taskControlServer) close() {
	server.stopOnce.Do(func() {
		close(server.stop)
		_ = server.listener.Close()
		_ = os.Remove(server.path)
		<-server.done
		server.handlers.Wait()
	})
}

func (server *taskControlServer) serve() {
	defer close(server.done)
	for {
		connection, err := server.listener.AcceptUnix()
		if err != nil {
			select {
			case <-server.stop:
				return
			default:
				continue
			}
		}
		server.handlers.Add(1)
		go func() { defer server.handlers.Done(); server.handle(connection) }()
	}
}

func (server *taskControlServer) handle(connection *net.UnixConn) {
	defer connection.Close()
	if err := verifyPeer(connection); err != nil {
		_ = writeJSON(connection, failure("", "forbidden", err))
		return
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := readBoundedLine(connection, maxRequestBytes)
	if err != nil {
		_ = writeJSON(connection, failure("", "invalid_request", err))
		return
	}
	var req request
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF || validateRequest(req) != nil {
		_ = writeJSON(connection, failure(req.ID, "invalid_request", fmt.Errorf("invalid task control request")))
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	server.dispatch(connection, req)
}

func (server *taskControlServer) dispatch(connection *net.UnixConn, req request) {
	server.current.mu.Lock()
	active := server.current.control == server && server.current.command != nil
	server.current.mu.Unlock()
	if !active {
		_ = writeJSON(connection, failure(req.ID, "task_control_closed", fmt.Errorf("task control is no longer active")))
		return
	}
	metadata := server.current.snapshot()
	if !server.current.manager.isScheduleMainTask(metadata) {
		_ = writeJSON(connection, failure(req.ID, "forbidden", fmt.Errorf("only a main Agent timeline can manage schedules")))
		return
	}
	manager := server.current.manager.schedules
	if manager == nil {
		_ = writeJSON(connection, failure(req.ID, "schedules_unavailable", fmt.Errorf("schedules are unavailable")))
		return
	}
	switch req.Method {
	case "schedule.create":
		var params createScheduleParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		if params.TaskID != "" && params.TaskID != metadata.ID {
			_ = writeJSON(connection, failure(req.ID, "forbidden", fmt.Errorf("task control can only schedule its own task")))
			return
		}
		params.TaskID = metadata.ID
		result, err := manager.create(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "schedule_create_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, result))
	case "schedule.list":
		if err := decodeParams(req.Params, &listScheduleParams{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		all := manager.list(true)
		filtered := make([]scheduleRecord, 0, len(all))
		for _, entry := range all {
			if entry.TaskID == metadata.ID {
				filtered = append(filtered, entry)
			}
		}
		_ = writeJSON(connection, success(req.ID, filtered))
	case "schedule.show", "schedule.pause", "schedule.resume", "schedule.delete", "schedule.run", "schedule.run-now":
		var params scheduleIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		entry, err := manager.show(scheduleIDParams{ID: params.ID, Details: req.Method == "schedule.show" && params.Details})
		if err != nil || entry.TaskID != metadata.ID {
			_ = writeJSON(connection, failure(req.ID, "schedule_not_found", fmt.Errorf("schedule does not exist for this task")))
			return
		}
		switch req.Method {
		case "schedule.show":
			_ = writeJSON(connection, success(req.ID, entry))
		case "schedule.pause":
			result, err := manager.pause(params.ID)
			if err != nil {
				_ = writeJSON(connection, failure(req.ID, "schedule_update_failed", err))
				return
			}
			_ = writeJSON(connection, success(req.ID, result))
		case "schedule.resume":
			result, err := manager.resume(params.ID)
			if err != nil {
				_ = writeJSON(connection, failure(req.ID, "schedule_update_failed", err))
				return
			}
			_ = writeJSON(connection, success(req.ID, result))
		case "schedule.run", "schedule.run-now":
			result, err := manager.runNow(params.ID)
			if err != nil {
				_ = writeJSON(connection, failure(req.ID, "schedule_run_failed", err))
				return
			}
			_ = writeJSON(connection, success(req.ID, result))
		case "schedule.delete":
			if err := manager.delete(params.ID); err != nil {
				_ = writeJSON(connection, failure(req.ID, "schedule_delete_failed", err))
				return
			}
			_ = writeJSON(connection, success(req.ID, map[string]bool{"deleted": true}))
		}
	default:
		_ = writeJSON(connection, failure(req.ID, "forbidden", fmt.Errorf("task control method is not allowed")))
	}
}

// taskControlClient deliberately has no daemon fallback. It is selected only
// when the worker inherited the capability-scoped socket from its launch.
type taskControlClient struct{ path string }

func runTaskControlScheduleCLI(args []string) error {
	path := os.Getenv("HOBOT_CODE_TASK_CONTROL_SOCKET")
	taskID := os.Getenv("HOBOT_CODE_TASK_ID")
	if !filepath.IsAbs(path) || !taskIDPattern.MatchString(taskID) {
		return fmt.Errorf("invalid task schedule control environment")
	}
	if len(args) == 0 {
		printScheduleUsage(os.Stderr)
		return fmt.Errorf("a schedule command is required")
	}
	if printRequestedScheduleHelp(args, os.Stdout) {
		return nil
	}
	return runScheduleWithClient(taskControlClient{path: path}, args, taskID)
}

func (client taskControlClient) call(method string, params any) (json.RawMessage, error) {
	connection, err := net.DialTimeout("unix", client.path, time.Second)
	if err != nil {
		return nil, fmt.Errorf("task schedule control is unavailable: %w", err)
	}
	defer connection.Close()
	id := fmt.Sprintf("task-schedule-%d", time.Now().UnixNano())
	req := request{Protocol: protocolVersion, ID: id, Method: method}
	if params != nil {
		if req.Params, err = json.Marshal(params); err != nil {
			return nil, err
		}
	}
	if err := writeJSON(connection, req); err != nil {
		return nil, err
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := readClientLine(connection)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Protocol int             `json:"protocol"`
		ID       string          `json:"id"`
		OK       bool            `json:"ok"`
		Result   json.RawMessage `json:"result"`
		Error    *protocolError  `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, err
	}
	if envelope.Protocol != protocolVersion || envelope.ID != id {
		return nil, fmt.Errorf("task schedule control returned an invalid response envelope")
	}
	if !envelope.OK {
		if envelope.Error == nil {
			return nil, fmt.Errorf("task schedule control request failed")
		}
		return nil, fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope.Result, nil
}
