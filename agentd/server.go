package main

import (
	"bufio"
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

type daemonServer struct {
	cfg      config
	manager  *taskManager
	listener *net.UnixListener
	started  time.Time
	stopOnce sync.Once
	stop     chan struct{}
}

type daemonInfo struct {
	Version         string    `json:"version"`
	Protocol        int       `json:"protocol"`
	PID             int       `json:"pid"`
	StartedAt       time.Time `json:"startedAt"`
	ActiveTasks     int       `json:"activeTasks"`
	MaximumTasks    int       `json:"maximumTasks"`
	SocketPath      string    `json:"socketPath"`
	StateRoot       string    `json:"stateRoot"`
	BackgroundTasks bool      `json:"backgroundTasks"`
}

func newDaemonServer(cfg config) (*daemonServer, error) {
	manager, err := newTaskManager(cfg)
	if err != nil {
		return nil, err
	}
	return &daemonServer{
		cfg: cfg, manager: manager, started: time.Now().UTC(), stop: make(chan struct{}),
	}, nil
}

func (server *daemonServer) info() daemonInfo {
	return daemonInfo{
		Version: version, Protocol: protocolVersion, PID: os.Getpid(), StartedAt: server.started,
		ActiveTasks: server.manager.activeCount(), MaximumTasks: server.cfg.MaxTasks,
		SocketPath: server.cfg.SocketPath, StateRoot: server.cfg.StateRoot, BackgroundTasks: true,
	}
}

func (server *daemonServer) listen() error {
	if err := removeStaleSocket(server.cfg.SocketPath); err != nil {
		return err
	}
	address := &net.UnixAddr{Name: server.cfg.SocketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return err
	}
	if err := os.Chmod(server.cfg.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		return err
	}
	server.listener = listener
	if err := writePrivateFile(server.cfg.PIDPath, []byte(fmt.Sprintf("%d\n", os.Getpid()))); err != nil {
		_ = listener.Close()
		return err
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("agentd socket path is not a socket: %s", path)
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return fmt.Errorf("agentd socket is owned by uid %d, expected %d", owner, os.Getuid())
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("another Hobot Code agentd is already listening: %s", path)
	}
	return os.Remove(path)
}

func (server *daemonServer) serve() error {
	if err := server.listen(); err != nil {
		return err
	}
	defer func() {
		_ = server.listener.Close()
		_ = os.Remove(server.cfg.SocketPath)
		_ = os.Remove(server.cfg.PIDPath)
	}()
	for {
		connection, err := server.listener.AcceptUnix()
		if err != nil {
			select {
			case <-server.stop:
				return nil
			default:
				return err
			}
		}
		go server.handleConnection(connection)
	}
}

func (server *daemonServer) shutdown() {
	server.stopOnce.Do(func() {
		server.manager.interruptAll()
		close(server.stop)
		if server.listener != nil {
			_ = server.listener.Close()
		}
	})
}

func (server *daemonServer) handleConnection(connection *net.UnixConn) {
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
	if err := decoder.Decode(&req); err != nil {
		_ = writeJSON(connection, failure("", "invalid_json", err))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		_ = writeJSON(connection, failure(req.ID, "invalid_json", fmt.Errorf("request must contain exactly one JSON object")))
		return
	}
	if err := validateRequest(req); err != nil {
		_ = writeJSON(connection, failure(req.ID, "invalid_request", err))
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	server.dispatch(connection, req)
}

func (server *daemonServer) dispatch(connection *net.UnixConn, req request) {
	switch req.Method {
	case "ping":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, server.info()))
	case "daemon.shutdown":
		var params struct {
			Force bool `json:"force,omitempty"`
		}
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		if active := server.manager.activeCount(); active > 0 && !params.Force {
			_ = writeJSON(connection, failure(req.ID, "tasks_active", fmt.Errorf("%d background task(s) are active; stop them or use --force", active)))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"stopping": true}))
		go server.shutdown()
	case "task.start":
		var params startTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		metadata, err := server.manager.start(params)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_start_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, metadata))
	case "task.list":
		if err := decodeParams(req.Params, &struct{}{}); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, server.manager.list()))
	case "task.get":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, current.snapshot()))
	case "task.command":
		var params commandTaskParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err == nil {
			err = current.sendCommand(params.Command)
		}
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_command_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"accepted": true}))
	case "task.stop":
		var params taskIDParams
		if err := decodeParams(req.Params, &params); err != nil {
			_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
			return
		}
		current, err := server.manager.get(params.TaskID)
		if err == nil {
			err = current.stop()
		}
		if err != nil {
			_ = writeJSON(connection, failure(req.ID, "task_stop_failed", err))
			return
		}
		_ = writeJSON(connection, success(req.ID, map[string]bool{"stopping": true}))
	case "task.subscribe":
		server.subscribe(connection, req)
	default:
		_ = writeJSON(connection, failure(req.ID, "method_not_found", fmt.Errorf("unknown method: %s", req.Method)))
	}
}

func (server *daemonServer) subscribe(connection *net.UnixConn, req request) {
	var params subscribeParams
	if err := decodeParams(req.Params, &params); err != nil {
		_ = writeJSON(connection, failure(req.ID, "invalid_params", err))
		return
	}
	current, err := server.manager.get(params.TaskID)
	if err != nil {
		_ = writeJSON(connection, failure(req.ID, "task_not_found", err))
		return
	}
	replayed, events, cancel, err := current.subscribe(params.After, params.Follow)
	if err != nil {
		_ = writeJSON(connection, failure(req.ID, "event_log_failed", err))
		return
	}
	defer cancel()
	if err := writeJSON(connection, success(req.ID, map[string]any{"replayed": len(replayed), "following": events != nil})); err != nil {
		return
	}
	for _, event := range replayed {
		if err := writeJSON(connection, event); err != nil {
			return
		}
	}
	if events == nil {
		return
	}
	for event := range events {
		if err := writeJSON(connection, event); err != nil {
			return
		}
	}
}

func readBoundedLine(reader io.Reader, maximum int) ([]byte, error) {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	var result bytes.Buffer
	for {
		fragment, prefix, err := buffered.ReadLine()
		if result.Len()+len(fragment) > maximum {
			return nil, fmt.Errorf("request exceeds %d bytes", maximum)
		}
		result.Write(fragment)
		if err != nil {
			return nil, err
		}
		if !prefix {
			return result.Bytes(), nil
		}
	}
}

func writeJSON(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	for len(encoded) > 0 {
		written, err := writer.Write(encoded)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		encoded = encoded[written:]
	}
	return nil
}

func writePrivateFile(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agentd.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
