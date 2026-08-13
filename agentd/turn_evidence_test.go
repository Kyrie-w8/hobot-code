package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTurnWorkspaceEvidenceDetectsChangesWithoutPersistingNames(t *testing.T) {
	if trustedGitBinary() == "" {
		t.Skip("trusted git is unavailable")
	}
	cwd := t.TempDir()
	command := exec.Command(trustedGitBinary(), "init", "--quiet", cwd)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	before := captureTurnWorkspaceEvidence(cwd)
	privateName := "customer-secret-model.bin"
	if err := os.WriteFile(filepath.Join(cwd, privateName), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := captureTurnWorkspaceEvidence(cwd)
	changed := compareTurnWorkspaces(before, after)
	if before.Status != "captured" || after.Status != "captured" || changed == nil || !*changed || after.ChangedFiles != 1 {
		t.Fatalf("workspace evidence did not detect the change: before=%+v after=%+v changed=%v", before, after, changed)
	}
	encoded, err := json.Marshal(taskTurnEvidence{WorkspaceBefore: before, WorkspaceAfter: after, WorkspaceChanged: changed})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), privateName) || strings.Contains(string(encoded), cwd) {
		t.Fatalf("turn evidence leaked workspace content or paths: %s", encoded)
	}
}

func TestRecordEventMaintainsBoundedTurnToolEvidence(t *testing.T) {
	dir := t.TempDir()
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	current := &task{
		manager: &taskManager{cfg: config{MaxEventSize: 1024 * 1024}}, dir: dir, events: events,
		metadata:    taskMetadata{ID: "00112233445566778899aabb", Name: "test", Cwd: dir, Status: statusRunning, CreatedAt: now, UpdatedAt: now},
		subscribers: make(map[uint64]chan taskEvent),
	}
	beginTurnEvidenceLocked(&current.metadata, captureTurnWorkspaceEvidence(dir))
	current.recordEvent(json.RawMessage(`{"type":"tool_execution_start","toolCallId":"private-id","toolName":"bash","args":{"command":"echo secret"}}`))
	current.recordEvent(json.RawMessage(`{"type":"tool_execution_end","toolCallId":"private-id","toolName":"bash","isError":false,"result":"secret output"}`))
	current.recordEvent(json.RawMessage(`{"type":"agent_settled"}`))

	metadata := current.snapshot()
	if len(metadata.TurnEvidence) != 1 {
		t.Fatalf("unexpected turn evidence: %+v", metadata.TurnEvidence)
	}
	evidence := metadata.TurnEvidence[0]
	if evidence.Status != "completed" || evidence.Evidence != "complete" || evidence.ToolsStarted != 1 || evidence.ToolsCompleted != 1 || evidence.OpenTools != 0 || evidence.EndSequence != 3 {
		t.Fatalf("incorrect completed evidence: %+v", evidence)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-id") || strings.Contains(string(encoded), "echo secret") || strings.Contains(string(encoded), "secret output") {
		t.Fatalf("turn evidence leaked tool content: %s", encoded)
	}
}

func TestInterruptedTurnMarksOpenToolsUnknown(t *testing.T) {
	now := time.Now().UTC()
	metadata := taskMetadata{SessionFile: "/private/session", LastSequence: 7}
	beginTurnEvidenceLocked(&metadata, nil)
	updateTurnToolEvidenceLocked(&metadata, "tool_execution_start", false)
	finalizeTurnEvidenceLocked(&metadata, "interrupted", nil, 8)
	evidence := metadata.TurnEvidence[0]
	if evidence.Evidence != "partial" || evidence.OpenTools != 1 || evidence.RecommendedAction != "review-before-resume" || evidence.EndedAt == nil || evidence.EndedAt.Before(now) {
		t.Fatalf("interrupted tool was not preserved as uncertain: %+v", evidence)
	}
}

func TestToolEvidenceRejectsMismatchedCompletion(t *testing.T) {
	metadata := taskMetadata{}
	beginTurnEvidenceLocked(&metadata, nil)
	current := &task{metadata: metadata}
	current.updateTurnToolEvidenceLocked("tool_execution_start", "call-a", false)
	current.updateTurnToolEvidenceLocked("tool_execution_end", "call-b", false)
	evidence := current.metadata.TurnEvidence[0]
	if evidence.Evidence != "partial" || evidence.ToolsStarted != 1 || evidence.ToolsCompleted != 0 || evidence.OpenTools != 1 {
		t.Fatalf("mismatched tool completion was accepted: %+v", evidence)
	}
}

func TestNormalizePersistedTurnEvidenceBoundsAndSanitizes(t *testing.T) {
	now := time.Now().UTC()
	records := make([]taskTurnEvidence, 0, maximumTurnEvidenceRecords+2)
	for turn := 1; turn <= maximumTurnEvidenceRecords+2; turn++ {
		ended := now
		records = append(records, taskTurnEvidence{
			Turn: uint64(turn), Status: "completed", Evidence: "complete", StartedAt: now, EndedAt: &ended,
			StartSequence: uint64(turn), EndSequence: uint64(turn), RecommendedAction: "untrusted-value",
		})
	}
	normalized := normalizePersistedTurnEvidence(records, false)
	if len(normalized) != maximumTurnEvidenceRecords || normalized[0].Turn != 3 || normalized[len(normalized)-1].RecommendedAction != "none" {
		t.Fatalf("persisted evidence was not bounded and sanitized: first=%+v last=%+v", normalized[0], normalized[len(normalized)-1])
	}
}

func TestRestartFinalizesOnlyRunningEvidence(t *testing.T) {
	now := time.Now().UTC()
	metadata := taskMetadata{SessionFile: "/private/session", TurnEvidence: []taskTurnEvidence{{
		Turn: 1, Status: "completed", Evidence: "complete", StartedAt: now, EndedAt: &now,
		StartSequence: 1, EndSequence: 3, RecommendedAction: "none",
	}}}
	if finalizeRunningTurnAfterRestart(&metadata, 7) {
		t.Fatalf("completed turn was rewritten on restart: %+v", metadata.TurnEvidence[0])
	}
	beginTurnEvidenceLocked(&metadata, nil)
	updateTurnToolEvidenceLocked(&metadata, "tool_execution_start", false)
	if !finalizeRunningTurnAfterRestart(&metadata, 8) {
		t.Fatal("running turn was not finalized after restart")
	}
	last := metadata.TurnEvidence[len(metadata.TurnEvidence)-1]
	if last.Status != "interrupted" || last.Evidence != "partial" || last.RecommendedAction != "review-before-resume" {
		t.Fatalf("restart evidence was not conservative: %+v", last)
	}
}

func TestNormalizePersistedTurnRecoveryUsesValidatedSessionState(t *testing.T) {
	now := time.Now().UTC()
	record := taskTurnEvidence{
		Turn: 1, Status: "interrupted", Evidence: "partial", StartedAt: now, EndedAt: &now,
		StartSequence: 1, EndSequence: 2, RecommendedAction: "none",
	}
	withoutSession := normalizePersistedTurnEvidence([]taskTurnEvidence{record}, false)
	withSession := normalizePersistedTurnEvidence([]taskTurnEvidence{record}, true)
	if withoutSession[0].RecommendedAction != "review-before-restart" || withSession[0].RecommendedAction != "review-before-resume" {
		t.Fatalf("persisted recovery did not follow validated session state: without=%+v with=%+v", withoutSession[0], withSession[0])
	}
}
