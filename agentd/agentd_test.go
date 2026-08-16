package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
		ConfigRoot: filepath.Join(root, "config"), AgentDir: filepath.Join(root, "config", "agent"),
		StateRoot: root, AgentdRoot: filepath.Join(root, "agentd"),
		TasksRoot: filepath.Join(root, "agentd", "tasks"), WorktreesRoot: filepath.Join(root, "agentd", "worktrees"),
		AttachCursorRoot: filepath.Join(root, "agentd", "attach-cursors"),
		SupportRoot:      filepath.Join(root, "agentd", "support"), SessionDir: filepath.Join(root, "sessions"),
		SocketPath: filepath.Join(socketRoot, "agentd.sock"), PIDPath: filepath.Join(root, "agentd", "agentd.pid"),
		LogPath: filepath.Join(root, "agentd", "agentd.log"), AgentBinary: worker,
		ExtensionCatalog: sourceExtensionCatalog(t),
		MaxTasks:         1, MaxRetainedTasks: 20, MaxEventSize: 1024 * 1024,
	}
	if err := preparePaths(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func addSettledSourceTask(t *testing.T, manager *taskManager, cfg config) *task {
	t.Helper()
	id := "00112233445566778899aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	stderrPath := filepath.Join(dir, "worker.stderr.log")
	if err := os.WriteFile(eventsPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(cfg.SessionDir, "source.jsonl")
	rows := []map[string]any{
		{"type": "session", "version": 3, "id": "source", "cwd": cfg.StateRoot},
		{"type": "model_change", "id": "model", "parentId": nil, "provider": "drobotics", "modelId": "kimi-k3"},
		{"type": "message", "id": "user", "parentId": "model", "message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": "source prompt"}}}},
		{"type": "message", "id": "assistant", "parentId": "user", "message": map[string]any{"role": "assistant", "stopReason": "stop", "content": []map[string]any{{"type": "text", "text": "source response"}}}},
	}
	var session bytes.Buffer
	for _, row := range rows {
		encoded, _ := json.Marshal(row)
		session.Write(encoded)
		session.WriteByte('\n')
	}
	if err := os.WriteFile(sessionPath, session.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	current := &task{
		manager: manager,
		dir:     dir,
		events:  eventsPath,
		stderr:  stderrPath,
		metadata: taskMetadata{
			ID: id, Name: "source", Cwd: cfg.StateRoot, Status: statusStopped,
			CreatedAt: now, UpdatedAt: now, SessionFile: sessionPath, Model: "drobotics/kimi-k3",
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.tasks[id] = current
	manager.mu.Unlock()
	return current
}

func TestResolveForkSourceRecoversCorruptEditedSessionFromParent(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parent := addSettledSourceTask(t, manager, cfg)
	parentEvent := taskEvent{
		Protocol: protocolVersion, Kind: "event", TaskID: parent.metadata.ID, Sequence: 1,
		Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "source prompt"}},
	}
	encoded, _ := json.Marshal(parentEvent)
	if err := os.WriteFile(parent.events, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	id := "ffeeddccbbaa998877665544"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	currentEvent := parentEvent
	currentEvent.TaskID = id
	currentEvent.Normalized = &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "replacement prompt"}}
	encoded, _ = json.Marshal(currentEvent)
	if err := os.WriteFile(eventsPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker.stderr.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(cfg.SessionDir, id)
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	corruptSession := filepath.Join(sessionDir, "fork-corrupt.jsonl")
	if err := os.WriteFile(corruptSession, []byte(`{"type":"message","id":"tail","parentId":"missing","message":{"role":"toolResult","content":[]}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager: manager, dir: dir, events: eventsPath, stderr: filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: "edited", Cwd: cfg.StateRoot, Status: statusStopped,
			SessionFile: corruptSession, ParentTaskID: parent.metadata.ID, BranchKind: "edit", ForkSequence: 1,
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	manager.mu.Lock()
	manager.tasks[id] = current
	manager.mu.Unlock()

	resolved, sessionFile, lines, sequence, err := manager.resolveForkSource(current, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedSession, err := validateSessionFile(cfg.SessionDir, parent.metadata.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != parent || sessionFile != expectedSession || sequence != 1 {
		t.Fatalf("resolved source = %s file=%q sequence=%d", resolved.metadata.ID, sessionFile, sequence)
	}
	leaf, err := resolved.sessionLeafBeforePrompt(sequence, lines)
	if err != nil || leaf != "model" {
		t.Fatalf("recovered edit context leaf=%q err=%v", leaf, err)
	}
	laterEvent := currentEvent
	laterEvent.Sequence = 2
	laterEvent.Normalized = &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "later prompt with no recoverable Pi prefix"}}
	encoded, _ = json.Marshal(laterEvent)
	file, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := manager.resolveForkSource(current, 2); err == nil || !strings.Contains(err.Error(), "session header is missing") {
		t.Fatalf("post-anchor corruption must fail closed, got %v", err)
	}
}

func TestBlankSideTaskStartsOnlyAfterItsFirstPrompt(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := addSettledSourceTask(t, manager, cfg)

	metadata, err := manager.fork(forkTaskParams{TaskID: source.metadata.ID, Kind: "side"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Status != statusStopped || !metadata.AwaitingPrompt || metadata.PID != 0 {
		t.Fatalf("blank side task started a worker: %+v", metadata)
	}
	if manager.activeCount() != 0 {
		t.Fatalf("blank side task occupied an active slot: %d", manager.activeCount())
	}
	if metadata.SessionFile == "" || metadata.SessionFile == source.metadata.SessionFile {
		t.Fatalf("blank side task did not receive a private session fork: %+v", metadata)
	}
	current, err := manager.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}

	resumed, err := manager.resume(resumeTaskParams{TaskID: metadata.ID, Prompt: "first side prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.AwaitingPrompt {
		t.Fatalf("first prompt did not clear awaitingPrompt: %+v", resumed)
	}
	waitForStatus(t, current, statusIdle)
	if manager.activeCount() != 1 {
		t.Fatalf("started side task did not occupy one active slot: %d", manager.activeCount())
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilitiesAdvertiseDeferredSidePrompt(t *testing.T) {
	found := false
	for _, capability := range protocolCapabilities {
		if capability == "tasks.fork.deferred-prompt.v1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("deferred side prompts are implemented but not advertised")
	}
}

func TestEditForkStillRequiresAReplacementPrompt(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := addSettledSourceTask(t, manager, cfg)
	if _, err := manager.fork(forkTaskParams{TaskID: source.metadata.ID, Kind: "edit", Sequence: 1}); err == nil || !strings.Contains(err.Error(), "replacement prompt") {
		t.Fatalf("empty edit fork was accepted: %v", err)
	}
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
	page, _, cancel, err := current.subscribe(0, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	found := false
	for _, event := range page.Events {
		if bytes.Contains(event.Event, secretPayload) || bytes.Contains(event.Event, []byte(base64.StdEncoding.EncodeToString(secretPayload))) {
			t.Fatal("image payload leaked into the event log")
		}
		if event.Normalized == nil || event.Normalized.Type != "user.message" {
			continue
		}
		found = true
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

func TestImagePromptRejectsModelWithoutImageCapability(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.start(startTaskParams{
		Name: "text-only", Cwd: cfg.StateRoot, Prompt: "inspect this image", Model: "drobotics/text-only",
		Images: []imageContent{{Type: "image", MimeType: "image/png", Name: "board.png", Data: base64.StdEncoding.EncodeToString([]byte("png"))}},
	})
	if err == nil || !strings.Contains(err.Error(), "does not declare image input support") {
		t.Fatalf("text-only model accepted an image: %v", err)
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
	queuedMetadata, err := manager.start(startTaskParams{Name: "queued", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil || queuedMetadata.Status != statusQueued || queuedMetadata.QueuedAt == nil {
		t.Fatalf("busy workers should queue new work: metadata=%+v err=%v", queuedMetadata, err)
	}
	queued, _ := manager.get(queuedMetadata.ID)
	if err := current.sendCommand(json.RawMessage(`{"type":"extension_ui_response","id":"approval-1","confirmed":true}`)); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	waitForStatus(t, queued, statusIdle)
	page, _, cancel, err := current.subscribe(0, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	settled := false
	for _, event := range page.Events {
		if string(event.Event) == `{"type":"agent_settled"}` {
			settled = true
			break
		}
	}
	if len(page.Events) < 4 || !settled {
		t.Fatalf("unexpected persisted events: %+v", page.Events)
	}
	if err := queued.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, queued, statusStopped)
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

func TestQueuedTaskCanBeCancelledWithoutStartingWorker(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	activeMetadata, err := manager.start(startTaskParams{Name: "active", Cwd: cfg.StateRoot, Prompt: "approval-test"})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := manager.get(activeMetadata.ID)
	waitForStatus(t, active, statusWaiting)
	queuedMetadata, err := manager.start(startTaskParams{Name: "queued", Cwd: cfg.StateRoot, Prompt: "must not run"})
	if err != nil {
		t.Fatal(err)
	}
	queued, _ := manager.get(queuedMetadata.ID)
	if err := queued.stop(); err != nil {
		t.Fatal(err)
	}
	state := waitForStatus(t, queued, statusStopped)
	if state.PID != 0 || state.QueuedAt != nil || state.QueueOperation != "" || queued.pendingLaunch != nil {
		t.Fatalf("cancelled queue entry retained executable state: %+v", state)
	}
	if _, err := os.Stat(queued.queuePath()); !os.IsNotExist(err) {
		t.Fatalf("cancelled queue file still exists: %v", err)
	}
	events, err := readEvents(queued.events, queued.metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, 0, len(events))
	for _, event := range events {
		if event.Normalized != nil {
			types = append(types, event.Normalized.Type)
		}
	}
	if strings.Join(types, ",") != "task.queued,user.message,task.cancelled,task.stopped" {
		t.Fatalf("unexpected cancelled queue events: %v", types)
	}
	if err := active.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, active, statusStopped)
	manager.interruptAll()
}

func TestQueuedTasksRunFIFO(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	activeMetadata, err := manager.start(startTaskParams{Name: "active", Cwd: cfg.StateRoot, Prompt: "approval-test"})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := manager.get(activeMetadata.ID)
	waitForStatus(t, active, statusWaiting)

	firstMetadata, err := manager.start(startTaskParams{Name: "first", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := manager.start(startTaskParams{Name: "second", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if firstMetadata.Status != statusQueued || secondMetadata.Status != statusQueued || firstMetadata.QueuedAt == nil || secondMetadata.QueuedAt == nil {
		t.Fatalf("tasks were not queued: first=%+v second=%+v", firstMetadata, secondMetadata)
	}
	if firstMetadata.QueuedAt.After(*secondMetadata.QueuedAt) {
		t.Fatalf("queue timestamps are out of order: first=%s second=%s", firstMetadata.QueuedAt, secondMetadata.QueuedAt)
	}
	first, _ := manager.get(firstMetadata.ID)
	second, _ := manager.get(secondMetadata.ID)
	if err := active.sendCommand(json.RawMessage(`{"type":"extension_ui_response","id":"approval-1","confirmed":true}`)); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, active, statusStopped)
	waitForStatus(t, first, statusStopped)
	waitForStatus(t, second, statusIdle)

	firstEvents, err := readEvents(first.events, first.metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondEvents, err := readEvents(second.events, second.metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstStarted, secondStarted := false, false
	for _, event := range firstEvents {
		firstStarted = firstStarted || (event.Normalized != nil && event.Normalized.Type == "task.starting")
	}
	for _, event := range secondEvents {
		secondStarted = secondStarted || (event.Normalized != nil && event.Normalized.Type == "task.starting")
	}
	if !firstStarted || !secondStarted {
		t.Fatalf("queued tasks did not both start: first=%t second=%t", firstStarted, secondStarted)
	}
	if err := second.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, second, statusStopped)
}

func TestCancelledStartingTaskCannotLaunchWorker(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now().UTC()
	current := &task{
		manager: &taskManager{cfg: cfg},
		dir:     cfg.TasksRoot,
		events:  filepath.Join(cfg.TasksRoot, "cancelled.events.jsonl"),
		stderr:  filepath.Join(cfg.TasksRoot, "cancelled.stderr.log"),
		metadata: taskMetadata{
			ID: "44556677889900aabbccddee", Name: "cancelled", Cwd: cfg.StateRoot,
			Status: statusStopped, CreatedAt: now, UpdatedAt: now, PermissionMode: "ask",
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := current.launch("must not run", nil, false, "", false); !errors.Is(err, errTaskLaunchCancelled) {
		t.Fatalf("cancelled launch was accepted: %v", err)
	}
	state := current.snapshot()
	if state.Status != statusStopped || state.PID != 0 || current.command != nil || current.stdin != nil {
		t.Fatalf("cancelled task launched a worker: %+v", state)
	}
}

func TestQueuedTaskSurvivesManagerRestart(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	activeMetadata, err := manager.start(startTaskParams{Name: "active", Cwd: cfg.StateRoot, Prompt: "approval-test"})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := manager.get(activeMetadata.ID)
	waitForStatus(t, active, statusWaiting)
	queuedMetadata, err := manager.start(startTaskParams{Name: "recover", Cwd: cfg.StateRoot, Prompt: "recover after restart"})
	if err != nil || queuedMetadata.Status != statusQueued {
		t.Fatalf("task was not queued: metadata=%+v err=%v", queuedMetadata, err)
	}
	queueInfo, err := os.Stat(filepath.Join(cfg.TasksRoot, queuedMetadata.ID, "queue.json"))
	if err != nil || queueInfo.Mode().Perm() != 0o600 {
		t.Fatalf("queue file is not private: info=%v err=%v", queueInfo, err)
	}
	// Simulate a daemon crash: recovery marks the old in-flight worker as
	// interrupted and starts the persisted queue entry without replaying it.
	recovered, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recoveredTask, err := recovered.get(queuedMetadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, recoveredTask, statusIdle)
	events, err := readEvents(recoveredTask.events, queuedMetadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	userMessages := 0
	for _, event := range events {
		if event.Normalized != nil && event.Normalized.Type == "user.message" {
			userMessages++
		}
	}
	if userMessages != 1 {
		t.Fatalf("recovered queued prompt was duplicated: %d user events", userMessages)
	}
	if err := recoveredTask.stop(); err != nil {
		t.Fatal(err)
	}
	if err := active.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, recoveredTask, statusStopped)
	waitForStatus(t, active, statusStopped)
	manager.interruptAll()
	recovered.interruptAll()
	time.Sleep(50 * time.Millisecond)
}

func TestQueuedPromptRecoveryRepairsMissingUserEvent(t *testing.T) {
	cfg := testConfig(t)
	id := "11223344556677889900aabb"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	queuedAt := time.Now().UTC().Truncate(time.Microsecond)
	metadata := taskMetadata{
		ID: id, Name: "recover prompt", Cwd: cfg.StateRoot, Status: statusQueued,
		CreatedAt: queuedAt, UpdatedAt: queuedAt, QueuedAt: &queuedAt, QueueOperation: "start", PermissionMode: "ask",
	}
	current := &task{
		manager: &taskManager{cfg: cfg}, dir: dir, events: filepath.Join(dir, "events.jsonl"), stderr: filepath.Join(dir, "worker.stderr.log"),
		metadata: metadata, subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	queued := queuedLaunch{Operation: "start", Prompt: "restore exactly once", QueuedAt: queuedAt}
	if err := writeQueuedLaunch(current.queuePath(), queued); err != nil {
		t.Fatal(err)
	}
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _ := manager.get(id)
	waitForStatus(t, recovered, statusIdle)
	events, err := readEvents(recovered.events, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	userMessages := 0
	for _, event := range events {
		if event.Normalized != nil && event.Normalized.Type == "user.message" {
			userMessages++
		}
	}
	if userMessages != 1 {
		t.Fatalf("recovery did not reconstruct exactly one user event: %d", userMessages)
	}
	if err := recovered.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, recovered, statusStopped)
}

func TestQueueLaunchingHandoffNeverReplaysAfterRestart(t *testing.T) {
	cfg := testConfig(t)
	id := "223344556677889900aabbcc"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	queuedAt := time.Now().UTC().Truncate(time.Microsecond)
	metadata := taskMetadata{
		ID: id, Name: "handoff", Cwd: cfg.StateRoot, Status: statusQueued,
		CreatedAt: queuedAt, UpdatedAt: queuedAt, QueuedAt: &queuedAt, QueueOperation: "start", PermissionMode: "ask",
	}
	current := &task{
		manager: &taskManager{cfg: cfg}, dir: dir, events: filepath.Join(dir, "events.jsonl"), stderr: filepath.Join(dir, "worker.stderr.log"),
		metadata: metadata, subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	queued := queuedLaunch{State: "launching", Operation: "start", Prompt: "do not replay", QueuedAt: queuedAt}
	if err := writeQueuedLaunch(current.queuePath(), queued); err != nil {
		t.Fatal(err)
	}
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := manager.get(id)
	if err != nil {
		t.Fatal(err)
	}
	state := recovered.snapshot()
	if state.Status != statusInterrupted || state.PID != 0 || state.Failure == nil || state.Failure.Code != "handoff-uncertain" || state.Failure.Recovery != "restart" {
		t.Fatalf("uncertain handoff was replayed or hidden: %+v", state)
	}
	if _, err := os.Stat(recovered.queuePath()); !os.IsNotExist(err) {
		t.Fatalf("completed recovery retained the sensitive queue file: %v", err)
	}
}

func TestTaskFailureMigrationIsStableAndSanitized(t *testing.T) {
	cfg := testConfig(t)
	id := "556677889900aabbccddeeff"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	current := &task{
		manager: &taskManager{cfg: cfg}, dir: dir, events: filepath.Join(dir, "events.jsonl"), stderr: filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: "legacy", Cwd: cfg.StateRoot, Status: statusFailed,
			CreatedAt: now, UpdatedAt: now, LastError: "HTTP 401 token=top-secret at /root/private/project", PermissionMode: "ask",
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	first, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := first.get(id)
	if err != nil {
		t.Fatal(err)
	}
	state := migrated.snapshot()
	if state.Failure == nil || state.Failure.Code != "model-unavailable" || state.Failure.Recovery != "check-model" {
		t.Fatalf("legacy model failure was not classified: %+v", state)
	}
	encoded, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("top-secret")) || bytes.Contains(encoded, []byte("/root/private")) {
		t.Fatalf("legacy failure detail survived migration: %s", encoded)
	}
	second, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := second.get(id)
	if err != nil {
		t.Fatal(err)
	}
	stable := reloaded.snapshot()
	if stable.Failure == nil || *stable.Failure != *state.Failure || stable.LastError != state.LastError {
		t.Fatalf("structured failure changed across restarts: first=%+v second=%+v", state, stable)
	}
}

func TestTerminalLifecycleEventIsPersistedExactlyOnce(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "terminal", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusIdle)
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	current.setTerminal(statusFailed, "must be ignored")
	events, err := readEvents(current.events, metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	terminal := 0
	for _, event := range events {
		if event.Normalized != nil && event.Normalized.Type == "task.stopped" {
			terminal++
		}
		if event.Normalized != nil && event.Normalized.Type == "task.failed" {
			t.Fatal("terminal state was overwritten by a late failure")
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal lifecycle count = %d, want 1", terminal)
	}
}

func TestRecoveryRemovesStaleQueueFromStartedTask(t *testing.T) {
	cfg := testConfig(t)
	id := "3344556677889900aabbccdd"
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	queuedAt := time.Now().UTC().Truncate(time.Microsecond)
	current := &task{
		manager: &taskManager{cfg: cfg}, dir: dir, events: filepath.Join(dir, "events.jsonl"), stderr: filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: "handoff", Cwd: cfg.StateRoot, Status: statusStarting,
			CreatedAt: queuedAt, UpdatedAt: queuedAt, PermissionMode: "ask",
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeQueuedLaunch(current.queuePath(), queuedLaunch{State: "launching", Operation: "start", Prompt: "already handed off", QueuedAt: queuedAt}); err != nil {
		t.Fatal(err)
	}
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := manager.get(id)
	if err != nil {
		t.Fatal(err)
	}
	if state := recovered.snapshot(); state.Status != statusInterrupted {
		t.Fatalf("started handoff was not marked interrupted: %+v", state)
	}
	if _, err := os.Stat(recovered.queuePath()); !os.IsNotExist(err) {
		t.Fatalf("stale queue file survived recovery: %v", err)
	}
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

func TestEventLogRollsNewestEventsAndExpiresOldCursor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	const maximumBytes = int64(1800)
	id := "00112233445566778899aabb"
	current := &task{
		manager: &taskManager{cfg: config{MaxEventSize: maximumBytes}}, dir: dir, events: path,
		metadata:    taskMetadata{ID: id, Name: "retained", Cwd: dir, Status: statusIdle},
		subscribers: make(map[uint64]chan taskEvent),
	}
	for index := 0; index < 18; index++ {
		current.recordEvent(mustJSON(map[string]any{
			"type": "hobot_user_prompt", "message": fmt.Sprintf("turn-%02d-%s", index, strings.Repeat("x", 220)),
		}))
	}
	state := current.snapshot()
	if !state.LogTruncated || state.LastSequence != 18 {
		t.Fatalf("event retention state was not recorded: %+v", state)
	}
	page, err := current.eventPage(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 || !page.HistoryTruncated || !page.CursorExpired || page.RetainedFrom <= 1 {
		t.Fatalf("retained event page did not disclose its history boundary: %+v", page)
	}
	if page.RetainedThrough != state.LastSequence || page.LatestSequence != state.LastSequence || page.Events[len(page.Events)-1].Sequence != state.LastSequence {
		t.Fatalf("newest event was not durably retained: %+v", page)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > maximumBytes || info.Mode().Perm() != 0o600 {
		t.Fatalf("retained event file is unsafe: info=%+v err=%v", info, err)
	}
	if _, _, _, err := recoverEventLog(path, id, maximumBytes, true); err != nil {
		t.Fatalf("retained event log did not survive recovery: %v", err)
	}
	current.recordEvent(mustJSON(map[string]any{"type": "agent_start"}))
	continued, err := current.eventPage(page.RetainedThrough, 10)
	if err != nil || len(continued.Events) != 1 || continued.Events[0].Sequence != 19 || continued.CursorExpired {
		t.Fatalf("new activity did not continue after retention: page=%+v err=%v", continued, err)
	}
	if matches, err := filepath.Glob(path + ".retained-*"); err != nil || len(matches) != 0 {
		t.Fatalf("retention left temporary files: %v err=%v", matches, err)
	}
}

func TestLegacyStoppedEventLogStartsFreshDurableTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	id := "00112233445566778899aabb"
	var content bytes.Buffer
	for sequence := uint64(1); sequence <= 3; sequence++ {
		event := taskEvent{Protocol: protocolVersion, Kind: "event", TaskID: id, Sequence: sequence, Time: time.Now().UTC(), Event: json.RawMessage(`{"type":"message_update"}`)}
		encoded, _ := json.Marshal(event)
		content.Write(encoded)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(path, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager: &taskManager{cfg: config{MaxEventSize: 4096}}, dir: dir, events: path, eventBytes: int64(content.Len()), eventLastSequence: 3,
		metadata:    taskMetadata{ID: id, Name: "legacy", Cwd: dir, Status: statusIdle, LastSequence: 9, LogTruncated: true},
		subscribers: make(map[uint64]chan taskEvent),
	}
	current.recordEvent(json.RawMessage(`{"type":"agent_start"}`))
	page, err := current.eventPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Sequence != 10 || page.RetainedFrom != 10 || page.RetainedThrough != 10 || page.LatestSequence != 10 || !page.CursorExpired {
		t.Fatalf("legacy durability gap was not replaced by a fresh tail: %+v", page)
	}
	if _, last, _, err := recoverEventLog(path, id, 4096, true); err != nil || last != 10 {
		t.Fatalf("fresh legacy tail did not recover: last=%d err=%v", last, err)
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
	if _, _, _, err := recoverEventLog(path, event.TaskID, 1024*1024, false); err == nil {
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
	if !containsString(ping.Capabilities.Capabilities, "models.health.v1") {
		t.Fatalf("model health capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	if !containsString(ping.Capabilities.Capabilities, "models.conformance.v1") {
		t.Fatalf("model conformance capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	if !containsString(ping.Capabilities.Capabilities, "models.runtime-probe.v1") {
		t.Fatalf("model runtime probe capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	if !containsString(ping.Capabilities.Capabilities, "models.rdk-probe.v1") {
		t.Fatalf("model RDK probe capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	if !containsString(ping.Capabilities.Capabilities, "models.rdk-matrix.v1") {
		t.Fatalf("model RDK matrix capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	if !containsString(ping.Capabilities.Capabilities, "models.qualification.v1") {
		t.Fatalf("persistent model qualification capability is missing: %+v", ping.Capabilities.Capabilities)
	}
	server.health.probe = func(context.Context, modelOption) modelHealthResult {
		return modelHealthResult{Status: "available", Category: "ok", Message: modelHealthMessage("ok"), Transport: "sse", FirstByteMS: 12, LatencyMS: 18, Attempts: 1}
	}
	healthResult, err := client.call("models.health", modelHealthParams{Model: "drobotics/kimi-k3"})
	if err != nil {
		t.Fatalf("model health failed: %v", err)
	}
	var health modelHealthResult
	if err := json.Unmarshal(healthResult, &health); err != nil || health.Status != "available" || health.Model != "kimi-k3" {
		t.Fatalf("unexpected model health: result=%+v err=%v", health, err)
	}
	server.verify.probe = func(context.Context, modelOption) modelConformanceResult {
		return modelConformanceResult{Checks: []modelConformanceCheck{
			{Name: "streaming", Status: "passed"}, {Name: "tool-call", Status: "passed"},
			{Name: "tool-result", Status: "passed"}, {Name: "image-input", Status: "passed"},
		}}
	}
	verificationResult, err := client.call("models.conformance", modelConformanceParams{Model: "drobotics/kimi-k3"})
	if err != nil {
		t.Fatalf("model conformance failed: %v", err)
	}
	var verification modelConformanceResult
	if err := json.Unmarshal(verificationResult, &verification); err != nil || verification.Status != "verified" || verification.Scope != "gateway-protocol" || verification.RuntimeStatus != "not-tested" || verification.RDKTaskStatus != "not-tested" || verification.Model != "kimi-k3" {
		t.Fatalf("unexpected model conformance: result=%+v err=%v", verification, err)
	}
	qualificationResult, err := client.call("models.qualification", modelQualificationParams{Model: "drobotics/kimi-k3"})
	if err != nil {
		t.Fatalf("model qualification read failed: %v", err)
	}
	var qualification modelQualificationResult
	if err := json.Unmarshal(qualificationResult, &qualification); err != nil || qualification.State != "current" || qualification.Level != "protocol" || qualification.Health == nil || qualification.Conformance == nil {
		t.Fatalf("unexpected persistent model qualification: result=%+v err=%v", qualification, err)
	}
	matrixResult, err := client.call("models.rdk-matrix", modelRDKMatrixParams{Model: "drobotics/kimi-k3"})
	if err != nil {
		t.Fatalf("model RDK matrix read failed: %v", err)
	}
	var matrix modelRDKMatrixResult
	if err := json.Unmarshal(matrixResult, &matrix); err != nil || len(matrix.Profiles) != len(rdkProbeProfiles) || matrix.Profiles[len(matrix.Profiles)-1].Availability != "planned" {
		t.Fatalf("unexpected model RDK matrix: result=%+v err=%v", matrix, err)
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
	}
	result, err := client.call("task.start", startTaskParams{Name: "rpc", Cwd: cfg.StateRoot, Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var metadata taskMetadata
	if err := json.Unmarshal(result, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.NetworkMode != networkModeShared || metadata.Sandbox.NetworkRestricted {
		t.Fatalf("new tasks must default to shared networking: %+v", metadata)
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
	networkResult, err := client.call("task.network", setTaskNetworkParams{TaskID: metadata.ID, Mode: networkModeShared})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(networkResult, &metadata); err != nil || metadata.NetworkMode != networkModeShared {
		t.Fatalf("network mode was not returned: metadata=%+v err=%v", metadata, err)
	}
	if _, err := client.call("task.network", setTaskNetworkParams{TaskID: metadata.ID, Mode: networkModeOffline}); err == nil || !strings.Contains(err.Error(), "requires an active bubblewrap sandbox") {
		t.Fatalf("offline networking was accepted without an OS sandbox: %v", err)
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

func TestInterruptAllPersistsOneTerminalLifecycleEvent(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.start(startTaskParams{Name: "interrupt", Cwd: cfg.StateRoot, Prompt: "approval-test"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusWaiting)
	manager.interruptAll()
	waitForStatus(t, current, statusInterrupted)
	events, err := readEvents(current.events, metadata.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := 0
	for _, event := range events {
		if event.Normalized != nil && event.Normalized.Type == "task.interrupted" {
			interrupted++
			if event.Normalized.Data["recovery"] != "resume" {
				t.Fatalf("interrupted task did not recommend its safe session recovery: %+v", event.Normalized)
			}
		}
	}
	if interrupted != 1 {
		t.Fatalf("interrupted lifecycle count = %d, want 1", interrupted)
	}
}

func TestFailureDetailLogRemainsStrictlyBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.stderr.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(maximumWorkerStderrBytes-32)), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{stderr: path, stderrBytes: maximumWorkerStderrBytes - 32}
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			current.appendFailureDetail(strings.Repeat("secret", 1024))
		}()
	}
	workers.Wait()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maximumWorkerStderrBytes {
		t.Fatalf("failure log size = %d, exceeds %d", info.Size(), maximumWorkerStderrBytes)
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

func TestTerminalStateInvalidatesPendingApprovals(t *testing.T) {
	cfg := testConfig(t)
	taskDir := filepath.Join(cfg.TasksRoot, "00112233445566778899aabb")
	if err := os.Mkdir(taskDir, 0o700); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager: &taskManager{cfg: cfg}, dir: taskDir,
		metadata:    taskMetadata{ID: "00112233445566778899aabb", Status: statusWaiting, Approvals: []pendingApproval{{ID: "approval", Active: true}}},
		subscribers: make(map[uint64]chan taskEvent),
	}
	current.setTerminal(statusStopped, "")
	state := current.snapshot()
	if state.Status != statusStopped || len(state.Approvals) != 1 || state.Approvals[0].Active {
		t.Fatalf("terminal task retained an active approval: %+v", state)
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
	page, _, cancel, err := current.subscribe(0, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	foundThinkingOrText := false
	foundUserMessage := false
	foundApproval := false
	foundResolved := false
	for _, event := range page.Events {
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
	eventPage, err := readEventPage(current.events, metadata.ID, 0, 2)
	if err != nil || len(eventPage.Events) != 2 || !eventPage.HasMore || eventPage.NextAfter != eventPage.Events[1].Sequence {
		t.Fatalf("unexpected event page: page=%+v err=%v", eventPage, err)
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
