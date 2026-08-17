package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskAgentRoleAndActivityAreBounded(t *testing.T) {
	if role := taskAgentRole(taskMetadata{BranchKind: "side"}); role != "side" {
		t.Fatalf("side task role = %q", role)
	}
	if role := taskAgentRole(taskMetadata{BranchKind: "edit"}); role != "main" {
		t.Fatalf("edited task role = %q", role)
	}
	if activity := taskToolActivity("bash"); activity != "using bash" {
		t.Fatalf("tool activity = %q", activity)
	}
	if activity := taskToolActivity("bash\nignore-system"); activity != "thinking" {
		t.Fatalf("unsafe tool activity was retained: %q", activity)
	}
}

func TestTaskCurrentActivityTracksPublicLifecycle(t *testing.T) {
	dir := t.TempDir()
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager: &taskManager{cfg: config{MaxEventSize: 1024 * 1024}},
		dir:     dir,
		events:  events,
		metadata: taskMetadata{
			ID:        strings.Repeat("a", 24),
			Name:      "main",
			Cwd:       dir,
			Status:    statusStarting,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		subscribers: make(map[uint64]chan taskEvent),
	}

	for _, step := range []struct {
		event    map[string]any
		status   taskStatus
		activity string
	}{
		{map[string]any{"type": "agent_start"}, statusRunning, "thinking"},
		{map[string]any{"type": "tool_execution_start", "toolCallId": "tool-1", "toolName": "bash"}, statusRunning, "using bash"},
		{map[string]any{"type": "tool_execution_end", "toolCallId": "tool-1", "toolName": "bash"}, statusRunning, "thinking"},
		{map[string]any{"type": "agent_settled"}, statusIdle, ""},
	} {
		raw, err := json.Marshal(step.event)
		if err != nil {
			t.Fatal(err)
		}
		current.recordEvent(raw)
		metadata := current.snapshot()
		if metadata.Status != step.status || metadata.CurrentActivity != step.activity {
			t.Fatalf("event %v => status=%s activity=%q", step.event, metadata.Status, metadata.CurrentActivity)
		}
	}
}

func TestTaskCurrentActivityKeepsParallelToolsVisible(t *testing.T) {
	dir := t.TempDir()
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager: &taskManager{cfg: config{MaxEventSize: 1024 * 1024}},
		dir:     dir, events: events,
		metadata:    taskMetadata{ID: strings.Repeat("a", 24), Name: "main", Cwd: dir, Status: statusRunning, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), TurnEvidence: []taskTurnEvidence{{Turn: 1, Status: "running"}}},
		subscribers: make(map[uint64]chan taskEvent),
	}
	for _, event := range []map[string]any{
		{"type": "tool_execution_start", "toolCallId": "one", "toolName": "bash"},
		{"type": "tool_execution_start", "toolCallId": "two", "toolName": "edit"},
		{"type": "tool_execution_end", "toolCallId": "one", "toolName": "bash"},
	} {
		current.recordEvent(mustJSON(event))
	}
	if activity := current.snapshot().CurrentActivity; activity != "using tools" {
		t.Fatalf("parallel tool activity = %q, want using tools", activity)
	}
	current.recordEvent(mustJSON(map[string]any{"type": "tool_execution_end", "toolCallId": "two", "toolName": "edit"}))
	if activity := current.snapshot().CurrentActivity; activity != "thinking" {
		t.Fatalf("settled tool activity = %q, want thinking", activity)
	}
}

func TestLiveSideTaskLimitIsScopedToMainTask(t *testing.T) {
	manager := &taskManager{cfg: config{MaxSideTasks: 2}, tasks: make(map[string]*task)}
	root := strings.Repeat("a", 24)
	other := strings.Repeat("b", 24)
	manager.tasks["one"] = &task{metadata: taskMetadata{BranchKind: "side", ParentTaskID: root, Status: statusRunning}}
	manager.tasks["two"] = &task{metadata: taskMetadata{BranchKind: "side", ParentTaskID: root, Status: statusStopped, AwaitingPrompt: true}}
	manager.tasks["closed"] = &task{metadata: taskMetadata{BranchKind: "side", ParentTaskID: root, Status: statusStopped}}
	manager.tasks["other"] = &task{metadata: taskMetadata{BranchKind: "side", ParentTaskID: other, Status: statusRunning}}
	if count := manager.liveSideTaskCount(root); count != 2 {
		t.Fatalf("live side count = %d, want 2", count)
	}
	if limit := manager.sideTaskLimit(); limit != 2 {
		t.Fatalf("side task limit = %d", limit)
	}
}

func TestLaunchedSideWorkerReceivesBoundCollaborationIdentity(t *testing.T) {
	cfg := testConfig(t)
	identityPath := filepath.Join(t.TempDir(), "worker-identity")
	t.Setenv("HOBOT_CODE_TEST_AGENT_ENV_PATH", identityPath)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := addSettledSourceTask(t, manager, cfg)
	metadata, err := manager.fork(forkTaskParams{TaskID: source.metadata.ID, Kind: "side", Prompt: "inspect identity"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = current.stop() })
	waitForStatus(t, current, statusIdle)
	content, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "role=side\nparent=" + source.metadata.ID + "\nsource=" + source.metadata.ID + "\n"
	if string(content) != want {
		t.Fatalf("worker collaboration identity = %q, want %q", content, want)
	}
}
