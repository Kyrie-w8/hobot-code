package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func testConfig(t *testing.T) config {
	t.Helper()
	root := t.TempDir()
	socketRoot, err := os.MkdirTemp("/tmp", "hobot-agentd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	worker, err := filepath.Abs(filepath.Join("testdata", "fake-hobot-rpc.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{
		StateRoot: root, AgentdRoot: filepath.Join(root, "agentd"),
		TasksRoot: filepath.Join(root, "agentd", "tasks"), SessionDir: filepath.Join(root, "sessions"),
		SocketPath: filepath.Join(socketRoot, "agentd.sock"), PIDPath: filepath.Join(root, "agentd", "agentd.pid"),
		LogPath: filepath.Join(root, "agentd", "agentd.log"), AgentBinary: worker,
		MaxTasks: 1, MaxRetainedTasks: 20, MaxEventSize: 1024 * 1024,
	}
	if err := preparePaths(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func waitForStatus(t *testing.T, current *task, expected taskStatus) taskMetadata {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		metadata := current.snapshot()
		if metadata.Status == expected {
			return metadata
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task did not reach %s; last state: %+v", expected, current.snapshot())
	return taskMetadata{}
}

func TestTaskLifecyclePersistsEventsAndBoundsConcurrency(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "background", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	if _, err := manager.start(startTaskParams{Name: "excess", Cwd: cfg.StateRoot, Prompt: "test"}); err == nil {
		t.Fatal("expected the background task limit to reject another live worker")
	}
	events, _, cancel, err := current.subscribe(0, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(events) < 4 || string(events[len(events)-1].Event) != `{"type":"agent_settled"}` {
		t.Fatalf("unexpected persisted events: %+v", events)
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	if info, err := os.Stat(current.events); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("event log permissions: info=%v err=%v", info, err)
	}
}

func TestEventLogRejectsBrokenSequence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "events.jsonl")
	record := taskEvent{
		Protocol: protocolVersion, Kind: "event", TaskID: "00112233445566778899aabb",
		Sequence: 2, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"agent_start"}`),
	}
	encoded, _ := json.Marshal(record)
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEvents(path, record.TaskID, 0); err == nil {
		t.Fatal("expected a non-contiguous event sequence to be rejected")
	}
}

func TestServerProtocolAndPrivateSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket test")
	}
	cfg := testConfig(t)
	server, err := newDaemonServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- server.serve()
	}()
	t.Cleanup(func() {
		server.shutdown()
		<-done
	})
	client := daemonClient{cfg: cfg}
	deadline := time.Now().Add(2 * time.Second)
	var lastPingError error
	for {
		if _, err := client.ping(); err == nil {
			break
		} else {
			lastPingError = err
		}
		if time.Now().After(deadline) {
			select {
			case serveError := <-done:
				t.Fatalf("server exited before becoming ready: %v (last ping: %v)", serveError, lastPingError)
			default:
				t.Fatalf("server did not become ready: %v", lastPingError)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := os.Stat(cfg.SocketPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket permissions: info=%v err=%v", info, err)
	}
	ping, err := client.ping()
	if err != nil || ping.Capabilities.EventSchema != eventSchemaVersion || len(ping.Capabilities.Capabilities) == 0 {
		t.Fatalf("capability negotiation failed: info=%+v err=%v", ping, err)
	}
	bridgeInput := bytes.NewBufferString(
		`{"protocol":1,"id":"bridge-1","method":"capabilities","params":{}}` + "\n" +
			`{"protocol":1,"id":"bridge-2","method":"ping","params":{}}` + "\n",
	)
	var bridgeOutput bytes.Buffer
	if err := bridgeStreams(cfg, bridgeInput, &bridgeOutput); err != nil {
		t.Fatalf("stdio bridge failed: %v", err)
	}
	if bytes.Count(bridgeOutput.Bytes(), []byte{'\n'}) != 2 || !bytes.Contains(bridgeOutput.Bytes(), []byte(`"id":"bridge-2"`)) {
		t.Fatalf("unexpected bridge output: %s", bridgeOutput.String())
	}
	result, err := client.call("task.start", startTaskParams{Name: "rpc", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var metadata taskMetadata
	if err := json.Unmarshal(result, &metadata); err != nil {
		t.Fatal(err)
	}
	current, _ := server.manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	if err := client.subscribe(metadata.ID, 0, false, true); err != nil {
		t.Fatalf("non-following event replay failed: %v", err)
	}
	if _, err := client.call("task.command", commandTaskParams{TaskID: metadata.ID, Command: workerCommand("abort", nil)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.call("task.stop", taskIDParams{TaskID: metadata.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestRestartMarksLiveTasksInterrupted(t *testing.T) {
	cfg := testConfig(t)
	id := "00112233445566778899aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := taskMetadata{
		ID: id, Name: "orphan", Cwd: cfg.StateRoot, Status: statusRunning, PID: 999999,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	encoded, _ := json.Marshal(metadata)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(id)
	restored := current.snapshot()
	if restored.Status != statusInterrupted || restored.PID != 0 || restored.LastError == "" {
		t.Fatalf("unexpected recovery state: %+v", restored)
	}
}

func TestLimitsAndValidationFailClosed(t *testing.T) {
	if err := validateRequest(request{Protocol: 99, ID: "id", Method: "ping"}); err == nil {
		t.Fatal("expected protocol version rejection")
	}
	if _, err := normalizeWorkingDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing workspace rejection")
	}
	var output bytes.Buffer
	writer := &boundedLogWriter{writer: &output, remaining: 4}
	written, err := writer.Write([]byte("12345678"))
	if err != nil || written != 8 || output.String() != "1234" {
		t.Fatalf("bounded writer failed: written=%d output=%q err=%v", written, output.String(), err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(link); err == nil {
		t.Fatal("expected private directory symlink rejection")
	}
	var params taskIDParams
	if err := decodeParams(json.RawMessage(`{"taskId":"valid"}{"extra":true}`), &params); err == nil {
		t.Fatal("expected trailing JSON parameters to be rejected")
	}
}

func TestWorkerCommandValidation(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "commands", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	if err := current.sendCommand(json.RawMessage(`{"type":"unknown"}`)); err == nil {
		t.Fatal("expected unknown worker command rejection")
	}
	if err := current.sendCommand(json.RawMessage(`{"type":"prompt","message":""}`)); err == nil {
		t.Fatal("expected empty prompt rejection")
	}
	if err := current.sendCommand(json.RawMessage(`{"type":"extension_ui_response","id":"request"}`)); err == nil {
		t.Fatal("expected approval response rejection while task is idle")
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
}

func TestTaskRestartWithoutSession(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "restartable", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	current.mu.Lock()
	current.metadata.SessionFile = ""
	current.metadata.SessionID = ""
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	restarted, err := manager.restart(resumeTaskParams{TaskID: metadata.ID, Prompt: "fresh session"})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.RestartCount != 1 || restarted.ResumeCount != 0 {
		t.Fatalf("unexpected restart counters: %+v", restarted)
	}
	waitForStatus(t, current, statusIdle)
	if state := current.snapshot(); state.SessionFile == "" || state.SessionID == "" {
		t.Fatalf("restart did not bind a fresh session: %+v", state)
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
}

func TestLateWorkerEventsDoNotReviveStoppingOrTerminalTasks(t *testing.T) {
	cfg := testConfig(t)
	taskDir := filepath.Join(cfg.TasksRoot, "00112233445566778899aabb")
	if err := os.Mkdir(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(taskDir, "events.jsonl")
	if err := os.WriteFile(events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager:     &taskManager{cfg: cfg},
		dir:         taskDir,
		events:      events,
		metadata:    taskMetadata{ID: "00112233445566778899aabb", Status: statusStopping},
		subscribers: make(map[uint64]chan taskEvent),
	}

	current.recordEvent(json.RawMessage(`{"type":"agent_settled"}`))
	if state := current.snapshot(); state.Status != statusStopping {
		t.Fatalf("late settled event changed stopping task state: %+v", state)
	}
	current.recordEvent(json.RawMessage(`{"type":"extension_ui_request","id":"late","method":"confirm"}`))
	if state := current.snapshot(); state.Status != statusStopping || len(state.Approvals) != 0 {
		t.Fatalf("late approval revived stopping task: %+v", state)
	}

	current.mu.Lock()
	current.metadata.Status = statusStopped
	current.mu.Unlock()
	current.recordEvent(json.RawMessage(`{"type":"agent_start"}`))
	if state := current.snapshot(); state.Status != statusStopped {
		t.Fatalf("late start event revived stopped task: %+v", state)
	}
}

func TestRecoveryRejectsSymlinkedState(t *testing.T) {
	cfg := testConfig(t)
	id := "00112233445566778899aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cfg.StateRoot, "outside-metadata.json")
	metadata := taskMetadata{ID: id, Name: "unsafe", Cwd: cfg.StateRoot, Status: statusIdle}
	encoded, _ := json.Marshal(metadata)
	if err := os.WriteFile(target, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.get(id); err == nil {
		t.Fatal("expected symlinked metadata to be ignored during recovery")
	}
}

func TestNormalizedEventsApprovalsAndSessionResume(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "resumable", Cwd: cfg.StateRoot, Prompt: "approval-test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusWaiting)
	state := current.snapshot()
	if state.SessionFile == "" || state.SessionID != "fake-session" {
		t.Fatalf("session binding was not persisted: %+v", state)
	}
	if len(state.Approvals) != 1 || !state.Approvals[0].Active {
		t.Fatalf("pending approval was not persisted: %+v", state.Approvals)
	}
	if err := current.sendCommand(json.RawMessage(`{"type":"extension_ui_response","id":"approval-1","confirmed":true}`)); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusIdle)
	events, _, cancel, err := current.subscribe(0, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	foundThinkingOrText := false
	foundApproval := false
	foundResolved := false
	for _, event := range events {
		if event.Normalized == nil || event.Normalized.Schema != eventSchemaVersion {
			continue
		}
		switch event.Normalized.Type {
		case "assistant.text.delta":
			foundThinkingOrText = true
		case "approval.requested":
			foundApproval = true
		case "approval.resolved":
			foundResolved = true
		}
	}
	if !foundApproval || !foundResolved {
		t.Fatalf("normalized approval lifecycle missing: requested=%v resolved=%v", foundApproval, foundResolved)
	}
	page, err := readEventPage(current.events, metadata.ID, 0, 2)
	if err != nil || len(page.Events) != 2 || !page.HasMore || page.NextAfter != page.Events[1].Sequence {
		t.Fatalf("unexpected event page: page=%+v err=%v", page, err)
	}
	_ = foundThinkingOrText
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	resumed, err := manager.resume(resumeTaskParams{TaskID: metadata.ID, Prompt: "continued"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ResumeCount != 1 {
		t.Fatalf("unexpected resume count: %+v", resumed)
	}
	waitForStatus(t, current, statusIdle)
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
}

func TestTaskLifecycleManagementAndPagination(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "managed", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	if _, err := manager.rename(renameTaskParams{TaskID: metadata.ID, Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	if _, err := manager.archive(archiveTaskParams{TaskID: metadata.ID, Archive: true}); err != nil {
		t.Fatal(err)
	}
	if len(manager.list()) != 0 {
		t.Fatal("archived task should be hidden from the compatibility list")
	}
	page, err := manager.page(pageTaskParams{Limit: 1, IncludeArchived: true})
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Name != "renamed" {
		t.Fatalf("unexpected task page: page=%+v err=%v", page, err)
	}
	if err := manager.delete(metadata.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.get(metadata.ID); err == nil {
		t.Fatal("deleted task remained registered")
	}
}
