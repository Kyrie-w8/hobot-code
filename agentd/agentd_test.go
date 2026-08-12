package main

import (
	"bytes"
	"encoding/base64"
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
		TasksRoot: filepath.Join(root, "agentd", "tasks"), SupportRoot: filepath.Join(root, "agentd", "support"), SessionDir: filepath.Join(root, "sessions"),
		SocketPath: filepath.Join(socketRoot, "agentd.sock"), PIDPath: filepath.Join(root, "agentd", "agentd.pid"),
		LogPath: filepath.Join(root, "agentd", "agentd.log"), AgentBinary: worker,
		MaxTasks: 1, MaxRetainedTasks: 20, MaxEventSize: 1024 * 1024,
	}
	if err := preparePaths(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestImagePromptPersistsOnlyAttachmentMetadata(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	secretPayload := []byte("private-image-payload")
	metadata, err := manager.start(startTaskParams{
		Name: "image", Cwd: cfg.StateRoot, Prompt: "inspect this image",
		Images: []imageContent{{Type: "image", MimeType: "image/png", Name: "board.png", Data: base64.StdEncoding.EncodeToString(secretPayload)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	events, _, cancel, err := current.subscribe(0, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	found := false
	for _, event := range events {
		if event.Normalized == nil || event.Normalized.Type != "user.message" {
			continue
		}
		found = true
		if bytes.Contains(event.Event, secretPayload) || bytes.Contains(event.Event, []byte(base64.StdEncoding.EncodeToString(secretPayload))) {
			t.Fatal("image payload leaked into the event log")
		}
		attachments, ok := event.Normalized.Data["attachments"].([]any)
		if !ok || len(attachments) != 1 {
			t.Fatalf("attachment metadata missing: %+v", event.Normalized.Data)
		}
	}
	if !found {
		t.Fatal("image prompt did not create a user message event")
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	if state := current.snapshot(); state.Status != statusStopped || state.PID != 0 {
		t.Fatalf("stop returned before worker exit: %+v", state)
	}
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
	metadata, err := manager.start(startTaskParams{Name: "background", Cwd: cfg.StateRoot, Prompt: "approval-test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusWaiting)
	if _, err := manager.start(startTaskParams{Name: "excess", Cwd: cfg.StateRoot, Prompt: "test"}); err == nil {
		t.Fatal("expected the background task limit to reject another working agent")
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
	settled := false
	for _, event := range events {
		if string(event.Event) == `{"type":"agent_settled"}` {
			settled = true
			break
		}
	}
	if len(events) < 4 || !settled {
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

func TestStartingTaskSuspendsOldestIdleWorker(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstMetadata, err := manager.start(startTaskParams{Name: "first", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := manager.get(firstMetadata.ID)
	waitForStatus(t, first, statusIdle)
	secondMetadata, err := manager.start(startTaskParams{Name: "second", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, first, statusStopped)
	second, _ := manager.get(secondMetadata.ID)
	waitForStatus(t, second, statusIdle)
	if first.snapshot().SessionFile == "" {
		t.Fatal("suspended task lost its resumable session")
	}
	if err := second.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, second, statusStopped)
}

func TestSideTaskDisplayParentUsesConversationRoot(t *testing.T) {
	manager := &taskManager{tasks: make(map[string]*task)}
	root := &task{metadata: taskMetadata{ID: "00112233445566778899aabb"}}
	side := &task{metadata: taskMetadata{ID: "11223344556677889900aabb", ParentTaskID: root.metadata.ID, BranchKind: "side"}}
	manager.tasks[root.metadata.ID] = root
	manager.tasks[side.metadata.ID] = side
	if got := manager.rootTaskID(side.snapshot()); got != root.metadata.ID {
		t.Fatalf("rootTaskID() = %q, want %q", got, root.metadata.ID)
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

func TestRecoveryUsesDurableEventSequenceAndRepairsRestartRollback(t *testing.T) {
	cfg := testConfig(t)
	id := "00112233445566778899aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := taskMetadata{
		ID: id, Name: "rollback", Cwd: cfg.StateRoot, Status: statusStopped,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastSequence: 2,
	}
	encodedMetadata, _ := json.Marshal(metadata)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), encodedMetadata, 0o600); err != nil {
		t.Fatal(err)
	}
	events := []taskEvent{
		{Protocol: protocolVersion, Kind: "event", TaskID: id, Sequence: 1, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"agent_start"}`)},
		{Protocol: protocolVersion, Kind: "event", TaskID: id, Sequence: 2, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"message_update"}`)},
		{Protocol: protocolVersion, Kind: "event", TaskID: id, Sequence: 3, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"agent_settled"}`)},
		{Protocol: protocolVersion, Kind: "event", TaskID: id, Sequence: 3, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"hobot_user_prompt","message":"continued"}`)},
		{Protocol: protocolVersion, Kind: "event", TaskID: id, Sequence: 4, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"agent_start"}`)},
	}
	var eventLog bytes.Buffer
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		eventLog.Write(encoded)
		eventLog.WriteByte('\n')
	}
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, eventLog.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := current.snapshot().LastSequence; got != 5 {
		t.Fatalf("recovered last sequence = %d, want 5", got)
	}
	replayed, err := readEvents(path, id, 0)
	if err != nil || len(replayed) != 5 {
		t.Fatalf("repaired event log: count=%d err=%v", len(replayed), err)
	}
	for index, event := range replayed {
		if event.Sequence != uint64(index+1) {
			t.Fatalf("event %d sequence = %d", index, event.Sequence)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("repaired event log permissions: info=%v err=%v", info, err)
	}
}

func TestRecoveryRejectsEventGap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "events.jsonl")
	event := taskEvent{
		Protocol: protocolVersion, Kind: "event", TaskID: "00112233445566778899aabb",
		Sequence: 2, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"agent_start"}`),
	}
	encoded, _ := json.Marshal(event)
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := recoverEventLog(path, event.TaskID, 1024*1024); err == nil {
		t.Fatal("event gap was repaired instead of rejected")
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
	if !containsString(ping.Capabilities.Capabilities, "system.snapshot") {
		t.Fatalf("system snapshot capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	if !containsString(ping.Capabilities.Capabilities, "deployments.v1") {
		t.Fatalf("deployment capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	snapshotResult, err := client.call("system.snapshot", struct{}{})
	if err != nil {
		t.Fatalf("system snapshot failed: %v", err)
	}
	var snapshot systemSnapshot
	if err := json.Unmarshal(snapshotResult, &snapshot); err != nil || snapshot.CapturedAt.IsZero() || snapshot.Architecture == "" || snapshot.CPUCores < 1 {
		t.Fatalf("unexpected system snapshot: snapshot=%+v err=%v", snapshot, err)
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
	permissionResult, err := client.call("task.permissions", setTaskPermissionParams{TaskID: metadata.ID, Mode: "developer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(permissionResult, &metadata); err != nil || metadata.PermissionMode != "developer" {
		t.Fatalf("permission mode was not returned: metadata=%+v err=%v", metadata, err)
	}
	if _, err := os.Stat(current.permissionPolicyPath()); err != nil {
		t.Fatalf("task permission policy was not written: %v", err)
	}
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestStoppedTaskModelChangePersistsForResume(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "model-change", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := manager.get(metadata.ID)
	waitForStatus(t, current, statusIdle)
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	updated, err := manager.setModel(setTaskModelParams{TaskID: metadata.ID, Provider: "drobotics", ModelID: "glm-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Model != "drobotics/glm-5.2" {
		t.Fatalf("updated model = %q", updated.Model)
	}
	recovered, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := recovered.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.snapshot().Model != "drobotics/glm-5.2" {
		t.Fatalf("persisted model = %q", loaded.snapshot().Model)
	}
	if _, err := manager.setModel(setTaskModelParams{TaskID: metadata.ID, Provider: "bad/provider", ModelID: "model"}); err == nil {
		t.Fatal("invalid model provider was accepted")
	}
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
	foundUserMessage := false
	foundApproval := false
	foundResolved := false
	for _, event := range events {
		if event.Normalized == nil || event.Normalized.Schema != eventSchemaVersion {
			continue
		}
		switch event.Normalized.Type {
		case "user.message":
			if event.Normalized.Data["text"] == "approval-test" {
				foundUserMessage = true
			}
		case "assistant.text.delta":
			foundThinkingOrText = true
		case "approval.requested":
			foundApproval = true
		case "approval.resolved":
			foundResolved = true
		}
	}
	if !foundUserMessage || !foundApproval || !foundResolved {
		t.Fatalf("normalized conversation lifecycle missing: user=%v requested=%v resolved=%v", foundUserMessage, foundApproval, foundResolved)
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

func TestSessionBindingWaitsForARealPrivateSessionFile(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	id := "00112233445566778899aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager:     manager,
		dir:         dir,
		events:      filepath.Join(dir, "events.jsonl"),
		stderr:      filepath.Join(dir, "worker.stderr.log"),
		metadata:    taskMetadata{ID: id, Name: "late-session", Cwd: cfg.StateRoot, Status: statusStarting},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(cfg.SessionDir, "late.jsonl")
	current.recordEvent(mustJSON(map[string]any{
		"type": "response", "command": "get_state", "success": true,
		"data": map[string]any{
			"sessionFile": session,
			"sessionId":   "late-session",
			"model":       map[string]string{"provider": "unknown", "id": "unknown"},
		},
	}))
	state := current.snapshot()
	if state.SessionFile != "" || state.SessionID != "" || state.Model != "" {
		t.Fatalf("unavailable session or placeholder model was persisted: %+v", state)
	}
	if err := os.WriteFile(session, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current.recordEvent(json.RawMessage(`{"type":"agent_start"}`))
	state = current.snapshot()
	physicalSession, err := filepath.EvalSymlinks(session)
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionFile != physicalSession || state.SessionID != "late-session" {
		t.Fatalf("real session was not bound after creation: %+v", state)
	}
}

func TestManagerRecoveryClearsUnavailableSessionBinding(t *testing.T) {
	cfg := testConfig(t)
	id := "00112233445566778899aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := taskMetadata{
		ID: id, Name: "stale-session", Cwd: cfg.StateRoot, Status: statusStopped,
		SessionFile: filepath.Join(cfg.SessionDir, "missing.jsonl"), SessionID: "missing", Model: "unknown/unknown",
	}
	encoded, _ := json.MarshalIndent(metadata, "", "  ")
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
	current, err := manager.get(id)
	if err != nil {
		t.Fatal(err)
	}
	state := current.snapshot()
	if state.SessionFile != "" || state.SessionID != "" || state.Model != "" {
		t.Fatalf("recovery retained unavailable legacy metadata: %+v", state)
	}
	persisted, err := readPrivateRegularFile(filepath.Join(dir, "metadata.json"), maxRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte("missing.jsonl")) {
		t.Fatalf("recovery did not persist the cleared binding: %s", persisted)
	}
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
