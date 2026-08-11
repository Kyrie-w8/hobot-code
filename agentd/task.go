package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type taskStatus string

const (
	statusStarting    taskStatus = "starting"
	statusIdle        taskStatus = "idle"
	statusRunning     taskStatus = "running"
	statusWaiting     taskStatus = "waiting"
	statusStopping    taskStatus = "stopping"
	statusStopped     taskStatus = "stopped"
	statusFailed      taskStatus = "failed"
	statusInterrupted taskStatus = "interrupted"
)

var taskNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

type taskMetadata struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Cwd          string     `json:"cwd"`
	Status       taskStatus `json:"status"`
	PID          int        `json:"pid,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	LastSequence uint64     `json:"lastSequence"`
	LogTruncated bool       `json:"logTruncated,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
}

type task struct {
	manager *taskManager
	dir     string
	events  string
	stderr  string

	mu          sync.Mutex
	metadata    taskMetadata
	command     *exec.Cmd
	stdin       io.WriteCloser
	writeMu     sync.Mutex
	stopping    bool
	interrupted bool
	eventBytes  int64
	subscribers map[uint64]chan taskEvent
	nextSubID   uint64
}

type taskManager struct {
	cfg     config
	mu      sync.RWMutex
	startMu sync.Mutex
	tasks   map[string]*task
}

type startTaskParams struct {
	Name    string `json:"name,omitempty"`
	Cwd     string `json:"cwd"`
	Prompt  string `json:"prompt"`
	Approve bool   `json:"approve,omitempty"`
}

type commandTaskParams struct {
	TaskID  string          `json:"taskId"`
	Command json.RawMessage `json:"command"`
}

type taskIDParams struct {
	TaskID string `json:"taskId"`
}

type subscribeParams struct {
	TaskID string `json:"taskId"`
	After  uint64 `json:"after,omitempty"`
	Follow bool   `json:"follow,omitempty"`
}

func newTaskManager(cfg config) (*taskManager, error) {
	manager := &taskManager{cfg: cfg, tasks: make(map[string]*task)}
	entries, err := os.ReadDir(cfg.TasksRoot)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !taskIDPattern.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(cfg.TasksRoot, entry.Name())
		content, err := readPrivateRegularFile(filepath.Join(dir, "metadata.json"), maxRequestBytes)
		if err != nil {
			continue
		}
		var metadata taskMetadata
		if json.Unmarshal(content, &metadata) != nil || metadata.ID != entry.Name() {
			continue
		}
		if isLiveStatus(metadata.Status) {
			metadata.Status = statusInterrupted
			metadata.PID = 0
			metadata.UpdatedAt = time.Now().UTC()
			metadata.LastError = "agentd restarted; an in-flight worker was not replayed"
		}
		current := &task{
			manager:     manager,
			dir:         dir,
			events:      filepath.Join(dir, "events.jsonl"),
			stderr:      filepath.Join(dir, "worker.stderr.log"),
			metadata:    metadata,
			subscribers: make(map[uint64]chan taskEvent),
		}
		info, err := privateRegularFileInfo(current.events, cfg.MaxEventSize)
		if err != nil {
			continue
		}
		current.eventBytes = info.Size()
		manager.tasks[metadata.ID] = current
		if metadata.Status == statusInterrupted {
			_ = current.saveMetadata()
		}
	}
	return manager, nil
}

func isLiveStatus(status taskStatus) bool {
	switch status {
	case statusStarting, statusIdle, statusRunning, statusWaiting, statusStopping:
		return true
	default:
		return false
	}
}

func (manager *taskManager) activeCount() int {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	count := 0
	for _, current := range manager.tasks {
		current.mu.Lock()
		live := isLiveStatus(current.metadata.Status)
		current.mu.Unlock()
		if live {
			count++
		}
	}
	return count
}

func newTaskID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func normalizeWorkingDirectory(value string) (string, error) {
	if value == "" {
		value = "."
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("task working directory is not a directory: %s", physical)
	}
	return physical, nil
}

func (manager *taskManager) start(params startTaskParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if len(params.Prompt) == 0 || len(params.Prompt) > maxPromptBytes {
		return taskMetadata{}, fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
	}
	if manager.activeCount() >= manager.cfg.MaxTasks {
		return taskMetadata{}, fmt.Errorf("background task limit reached (%d)", manager.cfg.MaxTasks)
	}
	cwd, err := normalizeWorkingDirectory(params.Cwd)
	if err != nil {
		return taskMetadata{}, err
	}
	id, err := newTaskID()
	if err != nil {
		return taskMetadata{}, err
	}
	name := params.Name
	if name == "" {
		name = "task-" + id[:8]
	}
	if !taskNamePattern.MatchString(name) {
		return taskMetadata{}, fmt.Errorf("task name must start with a letter or digit and use at most 64 letters, digits, _ or -")
	}

	dir := filepath.Join(manager.cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return taskMetadata{}, err
	}
	keepDirectory := false
	defer func() {
		if !keepDirectory {
			_ = os.RemoveAll(dir)
		}
	}()
	current := &task{
		manager: manager,
		dir:     dir,
		events:  filepath.Join(dir, "events.jsonl"),
		stderr:  filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: name, Cwd: cwd, Status: statusStarting,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		return taskMetadata{}, err
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		return taskMetadata{}, err
	}
	if err := current.saveMetadata(); err != nil {
		return taskMetadata{}, err
	}

	manager.mu.Lock()
	manager.tasks[id] = current
	manager.mu.Unlock()
	keepDirectory = true
	if err := current.launch(params.Prompt, params.Approve); err != nil {
		current.setTerminal(statusFailed, err.Error())
		return current.snapshot(), err
	}
	return current.snapshot(), nil
}

func (current *task) launch(prompt string, approve bool) error {
	args := []string{"--mode", "rpc", "--session-dir", current.manager.cfg.SessionDir, "--name", current.metadata.Name}
	if approve {
		args = append(args, "--approve")
	} else {
		args = append(args, "--no-approve")
	}
	command := exec.Command(current.manager.cfg.AgentBinary, args...)
	command.Dir = current.metadata.Cwd
	command.Env = append(os.Environ(), "HOBOT_CODE_BACKGROUND_TASK=1", "HOBOT_CODE_BACKGROUND_TASK_ID="+current.metadata.ID)
	command.SysProcAttr = workerSysProcAttr()
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}

	current.mu.Lock()
	current.command = command
	current.stdin = stdin
	current.metadata.PID = command.Process.Pid
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		return err
	}

	go current.consumeStdout(stdout)
	go current.consumeStderr(stderr)
	go current.wait()
	startCommand, _ := json.Marshal(map[string]any{
		"id": "agentd-start", "type": "prompt", "message": prompt,
	})
	if err := current.sendCommand(startCommand); err != nil {
		_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
		return err
	}
	return nil
}

func (current *task) consumeStdout(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes)
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if !json.Valid(line) {
			current.failWorker("worker emitted invalid JSON")
			return
		}
		current.recordEvent(append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		current.failWorker("worker event stream failed: " + err.Error())
	}
}

func (current *task) consumeStderr(reader io.Reader) {
	file, err := os.OpenFile(current.stderr, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = io.Copy(&boundedLogWriter{writer: file, remaining: 1024 * 1024}, reader)
}

type boundedLogWriter struct {
	writer    io.Writer
	remaining int64
}

func (writer *boundedLogWriter) Write(value []byte) (int, error) {
	originalLength := len(value)
	if writer.remaining > 0 {
		writeLength := int64(len(value))
		if writeLength > writer.remaining {
			writeLength = writer.remaining
		}
		if _, err := writer.writer.Write(value[:writeLength]); err != nil {
			return 0, err
		}
		writer.remaining -= writeLength
	}
	return originalLength, nil
}

func (current *task) wait() {
	current.mu.Lock()
	command := current.command
	current.mu.Unlock()
	if command == nil {
		return
	}
	err := command.Wait()
	current.mu.Lock()
	stopping := current.stopping
	interrupted := current.interrupted
	status := current.metadata.Status
	current.command = nil
	current.stdin = nil
	current.metadata.PID = 0
	current.mu.Unlock()
	if interrupted || status == statusInterrupted {
		current.setTerminal(statusInterrupted, "")
	} else if stopping || status == statusStopping {
		current.setTerminal(statusStopped, "")
	} else if err != nil {
		current.setTerminal(statusFailed, err.Error())
	} else {
		current.setTerminal(statusStopped, "")
	}
}

func (current *task) failWorker(message string) {
	current.setTerminal(statusFailed, message)
	current.mu.Lock()
	pid := current.metadata.PID
	current.mu.Unlock()
	if pid > 0 {
		_ = terminateProcessGroup(pid, syscall.SIGKILL)
	}
}

func (current *task) recordEvent(raw json.RawMessage) {
	var header struct {
		Type   string `json:"type"`
		Method string `json:"method"`
	}
	_ = json.Unmarshal(raw, &header)

	current.mu.Lock()
	current.metadata.LastSequence++
	current.metadata.UpdatedAt = time.Now().UTC()
	switch header.Type {
	case "agent_start":
		current.metadata.Status = statusRunning
	case "agent_settled":
		current.metadata.Status = statusIdle
	case "extension_ui_request":
		if header.Method == "confirm" || header.Method == "select" || header.Method == "input" || header.Method == "editor" {
			current.metadata.Status = statusWaiting
		}
	}
	event := taskEvent{
		Protocol: protocolVersion,
		Kind:     "event",
		TaskID:   current.metadata.ID,
		Sequence: current.metadata.LastSequence,
		Time:     time.Now().UTC(),
		Event:    raw,
	}
	encoded, _ := json.Marshal(event)
	wasTruncated := current.metadata.LogTruncated
	persisted := false
	if !current.metadata.LogTruncated && current.eventBytes+int64(len(encoded)+1) <= current.manager.cfg.MaxEventSize {
		if file, err := os.OpenFile(current.events, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			if _, err := file.Write(append(encoded, '\n')); err == nil {
				current.eventBytes += int64(len(encoded) + 1)
				persisted = true
			}
			if err := file.Close(); err != nil {
				persisted = false
			}
		}
	}
	if !persisted {
		current.metadata.LogTruncated = true
	}
	logBecameTruncated := !wasTruncated && current.metadata.LogTruncated
	for id, subscriber := range current.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(current.subscribers, id)
		}
	}
	current.mu.Unlock()
	if logBecameTruncated || header.Type == "agent_start" || header.Type == "agent_settled" || header.Type == "extension_ui_request" {
		_ = current.saveMetadata()
	}
}

func (current *task) sendCommand(command json.RawMessage) error {
	if len(command) == 0 || len(command) > maxRequestBytes || !json.Valid(command) {
		return fmt.Errorf("worker command must be valid JSON no larger than %d bytes", maxRequestBytes)
	}
	var header struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(command, &header); err != nil || header.Type == "" {
		return fmt.Errorf("worker command must contain a type")
	}
	current.mu.Lock()
	stdin := current.stdin
	status := current.metadata.Status
	current.mu.Unlock()
	if stdin == nil || !isLiveStatus(status) {
		return fmt.Errorf("task worker is not running")
	}
	switch header.Type {
	case "prompt":
		if len(header.Message) == 0 || len(header.Message) > maxPromptBytes {
			return fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
		}
		if status != statusStarting && status != statusIdle {
			return fmt.Errorf("task must be idle before accepting another prompt")
		}
	case "abort":
	case "extension_ui_response":
		if header.ID == "" || status != statusWaiting {
			return fmt.Errorf("task is not waiting for an approval response")
		}
	default:
		return fmt.Errorf("unsupported worker command: %s", header.Type)
	}
	current.writeMu.Lock()
	defer current.writeMu.Unlock()
	if _, err := stdin.Write(append(append([]byte(nil), command...), '\n')); err != nil {
		return err
	}
	if header.Type == "extension_ui_response" {
		current.mu.Lock()
		if current.metadata.Status == statusWaiting {
			current.metadata.Status = statusRunning
			current.metadata.UpdatedAt = time.Now().UTC()
		}
		current.mu.Unlock()
		_ = current.saveMetadata()
	}
	return nil
}

func (current *task) stop() error {
	current.mu.Lock()
	if !isLiveStatus(current.metadata.Status) {
		current.mu.Unlock()
		return nil
	}
	current.stopping = true
	current.metadata.Status = statusStopping
	current.metadata.UpdatedAt = time.Now().UTC()
	pid := current.metadata.PID
	current.mu.Unlock()
	_ = current.saveMetadata()
	if pid <= 0 {
		current.setTerminal(statusStopped, "")
		return nil
	}
	if err := terminateProcessGroup(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		<-timer.C
		current.mu.Lock()
		stillRunning := current.metadata.PID == pid
		current.mu.Unlock()
		if stillRunning {
			_ = terminateProcessGroup(pid, syscall.SIGKILL)
		}
	}()
	return nil
}

func terminateProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, signal)
}

func (current *task) setTerminal(status taskStatus, message string) {
	current.mu.Lock()
	if current.metadata.Status == statusStopped && status != statusStopped {
		current.mu.Unlock()
		return
	}
	current.metadata.Status = status
	current.metadata.PID = 0
	current.metadata.UpdatedAt = time.Now().UTC()
	if message != "" {
		current.metadata.LastError = message
	}
	for id, subscriber := range current.subscribers {
		close(subscriber)
		delete(current.subscribers, id)
	}
	current.mu.Unlock()
	_ = current.saveMetadata()
}

func (current *task) snapshot() taskMetadata {
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.metadata
}

func (current *task) saveMetadata() error {
	current.mu.Lock()
	metadata := current.metadata
	current.mu.Unlock()
	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(current.dir, ".metadata.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(current.dir, "metadata.json"))
}

func (manager *taskManager) get(id string) (*task, error) {
	manager.mu.RLock()
	current := manager.tasks[id]
	manager.mu.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("task does not exist: %s", id)
	}
	return current, nil
}

func (manager *taskManager) list() []taskMetadata {
	manager.mu.RLock()
	result := make([]taskMetadata, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		result = append(result, current.snapshot())
	}
	manager.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (current *task) subscribe(after uint64, follow bool) ([]taskEvent, <-chan taskEvent, func(), error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	replayed, err := readEvents(current.events, current.metadata.ID, after)
	if err != nil {
		return nil, nil, nil, err
	}
	if !follow || !isLiveStatus(current.metadata.Status) {
		return replayed, nil, func() {}, nil
	}
	current.nextSubID++
	id := current.nextSubID
	channel := make(chan taskEvent, 128)
	current.subscribers[id] = channel
	cancel := func() {
		current.mu.Lock()
		if registered, ok := current.subscribers[id]; ok {
			close(registered)
			delete(current.subscribers, id)
		}
		current.mu.Unlock()
	}
	return replayed, channel, cancel, nil
}

func readEvents(path, taskID string, after uint64) ([]taskEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := make([]taskEvent, 0)
	lastSequence := uint64(0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes+1024)
	for scanner.Scan() {
		var event taskEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("corrupt task event log: %w", err)
		}
		if event.Protocol != protocolVersion || event.Kind != "event" || event.TaskID != taskID || event.Sequence != lastSequence+1 {
			return nil, fmt.Errorf("corrupt task event envelope at sequence %d", event.Sequence)
		}
		lastSequence = event.Sequence
		if event.Sequence > after {
			result = append(result, event)
		}
	}
	return result, scanner.Err()
}

func (manager *taskManager) interruptAll() {
	manager.mu.RLock()
	tasks := make([]struct {
		current *task
		pid     int
		live    bool
	}, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		current.mu.Lock()
		pid := current.metadata.PID
		live := isLiveStatus(current.metadata.Status)
		current.mu.Unlock()
		tasks = append(tasks, struct {
			current *task
			pid     int
			live    bool
		}{current: current, pid: pid, live: live})
	}
	manager.mu.RUnlock()
	for index := range tasks {
		active := &tasks[index]
		current := active.current
		current.mu.Lock()
		live := isLiveStatus(current.metadata.Status)
		active.live = live
		if live {
			current.interrupted = true
			current.metadata.Status = statusInterrupted
			current.metadata.LastError = "agentd stopped; worker was not replayed"
			current.metadata.PID = 0
			current.metadata.UpdatedAt = time.Now().UTC()
		}
		current.mu.Unlock()
		if live {
			_ = current.saveMetadata()
			_ = terminateProcessGroup(active.pid, syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, active := range tasks {
		pid := active.pid
		if !active.live || pid <= 0 {
			continue
		}
		for time.Now().Before(deadline) {
			if err := syscall.Kill(-pid, 0); err != nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err := syscall.Kill(-pid, 0); err == nil {
			_ = terminateProcessGroup(pid, syscall.SIGKILL)
		}
	}
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request parameters: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("invalid request parameters: multiple JSON values")
		}
		return fmt.Errorf("invalid request parameters: %w", err)
	}
	return nil
}

func readPrivateRegularFile(path string, maximum int64) ([]byte, error) {
	_, err := privateRegularFileInfo(path, maximum)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func privateRegularFileInfo(path string, maximum int64) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, fmt.Errorf("private state file is not a bounded regular file: %s", path)
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return nil, fmt.Errorf("private state file is owned by uid %d, expected %d: %s", owner, os.Getuid(), path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private state file is accessible by another user: %s", path)
	}
	return info, nil
}
