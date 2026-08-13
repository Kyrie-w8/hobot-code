package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const (
	maximumTurnEvidenceRecords = 32
	maximumTurnEvidenceFiles   = 1000
	turnEvidenceTimeout        = time.Second
)

type turnWorkspaceEvidence struct {
	Status       string    `json:"status"`
	CapturedAt   time.Time `json:"capturedAt"`
	StateDigest  string    `json:"stateDigest,omitempty"`
	Dirty        bool      `json:"dirty,omitempty"`
	ChangedFiles int       `json:"changedFiles,omitempty"`
	Truncated    bool      `json:"truncated,omitempty"`
}

type taskTurnEvidence struct {
	Turn              uint64                 `json:"turn"`
	Status            string                 `json:"status"`
	Evidence          string                 `json:"evidence"`
	StartedAt         time.Time              `json:"startedAt"`
	EndedAt           *time.Time             `json:"endedAt,omitempty"`
	StartSequence     uint64                 `json:"startSequence"`
	EndSequence       uint64                 `json:"endSequence,omitempty"`
	ToolsStarted      int                    `json:"toolsStarted"`
	ToolsCompleted    int                    `json:"toolsCompleted"`
	ToolsFailed       int                    `json:"toolsFailed"`
	OpenTools         int                    `json:"openTools"`
	WorkspaceBefore   *turnWorkspaceEvidence `json:"workspaceBefore,omitempty"`
	WorkspaceAfter    *turnWorkspaceEvidence `json:"workspaceAfter,omitempty"`
	WorkspaceChanged  *bool                  `json:"workspaceChanged,omitempty"`
	RecommendedAction string                 `json:"recommendedAction"`
}

func captureTurnWorkspaceEvidence(cwd string) *turnWorkspaceEvidence {
	now := time.Now().UTC()
	unavailable := func(status string) *turnWorkspaceEvidence {
		return &turnWorkspaceEvidence{Status: status, CapturedAt: now}
	}
	physicalCwd, err := normalizeWorkingDirectory(cwd)
	if err != nil {
		return unavailable("unavailable")
	}
	git := trustedGitBinary()
	if git == "" {
		return unavailable("unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), turnEvidenceTimeout)
	defer cancel()
	select {
	case workspaceChangeSlots <- struct{}{}:
		defer func() { <-workspaceChangeSlots }()
	case <-ctx.Done():
		return unavailable("unavailable")
	}
	rootOutput, _, err := runGitBounded(ctx, git, physicalCwd, 4096, "rev-parse", "--show-toplevel")
	if errors.Is(err, errGitNotRepository) {
		return unavailable("not-repository")
	}
	if err != nil {
		return unavailable("unavailable")
	}
	root := strings.TrimSpace(string(rootOutput))
	if !filepath.IsAbs(root) {
		return unavailable("unavailable")
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !pathWithin(physicalRoot, physicalCwd) {
		return unavailable("unavailable")
	}
	statusOutput, statusTruncated, err := runGitBounded(ctx, git, physicalCwd, maximumGitStatusBytes,
		"status", "--porcelain=v2", "-z", "--untracked-files=normal", "--ignore-submodules=all", "--", ".")
	if err != nil {
		return unavailable("unavailable")
	}
	if statusTruncated {
		if lastRecord := bytes.LastIndexByte(statusOutput, 0); lastRecord >= 0 {
			statusOutput = statusOutput[:lastRecord+1]
		} else {
			statusOutput = nil
		}
	}
	files, parserTruncated, err := parseGitStatusV2(statusOutput, maximumTurnEvidenceFiles)
	if err != nil {
		return unavailable("unavailable")
	}
	head, _, headErr := runGitBounded(ctx, git, physicalCwd, 256, "rev-parse", "HEAD")
	if headErr != nil && !errors.Is(headErr, errGitCommandFailed) {
		return unavailable("unavailable")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("hobot-code-turn-evidence-v1\x00"))
	_, _ = hash.Write(head)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(statusOutput)
	truncated := statusTruncated || parserTruncated
	status := "captured"
	if truncated {
		status = "partial"
	}
	return &turnWorkspaceEvidence{
		Status: status, CapturedAt: now, StateDigest: hex.EncodeToString(hash.Sum(nil)),
		Dirty: len(files) > 0, ChangedFiles: len(files), Truncated: truncated,
	}
}

func beginTurnEvidenceLocked(metadata *taskMetadata, workspace *turnWorkspaceEvidence) {
	turn := uint64(1)
	if len(metadata.TurnEvidence) > 0 {
		turn = metadata.TurnEvidence[len(metadata.TurnEvidence)-1].Turn + 1
	}
	evidence := taskTurnEvidence{
		Turn: turn, Status: "running", Evidence: "in-progress", StartedAt: time.Now().UTC(),
		StartSequence: metadata.LastSequence + 1, WorkspaceBefore: workspace, RecommendedAction: "none",
	}
	metadata.TurnEvidence = append(metadata.TurnEvidence, evidence)
	if len(metadata.TurnEvidence) > maximumTurnEvidenceRecords {
		metadata.TurnEvidence = append([]taskTurnEvidence(nil), metadata.TurnEvidence[len(metadata.TurnEvidence)-maximumTurnEvidenceRecords:]...)
	}
}

func updateTurnToolEvidenceLocked(metadata *taskMetadata, eventType string, failed bool) {
	if len(metadata.TurnEvidence) == 0 {
		return
	}
	evidence := &metadata.TurnEvidence[len(metadata.TurnEvidence)-1]
	if evidence.Status != "running" {
		return
	}
	switch eventType {
	case "tool_execution_start":
		evidence.ToolsStarted++
		evidence.OpenTools++
	case "tool_execution_end":
		if evidence.OpenTools > 0 {
			evidence.ToolsCompleted++
			if failed {
				evidence.ToolsFailed++
			}
			evidence.OpenTools--
		} else {
			evidence.Evidence = "partial"
		}
	}
}

func (current *task) updateTurnToolEvidenceLocked(eventType, toolCallID string, failed bool) {
	if len(current.metadata.TurnEvidence) == 0 || current.metadata.TurnEvidence[len(current.metadata.TurnEvidence)-1].Status != "running" {
		return
	}
	evidence := &current.metadata.TurnEvidence[len(current.metadata.TurnEvidence)-1]
	switch eventType {
	case "tool_execution_start":
		if toolCallID == "" {
			current.openAnonymousTools++
		} else {
			if current.openToolCalls == nil {
				current.openToolCalls = make(map[string]struct{})
			}
			if _, duplicate := current.openToolCalls[toolCallID]; duplicate {
				evidence.Evidence = "partial"
				return
			}
			current.openToolCalls[toolCallID] = struct{}{}
		}
		updateTurnToolEvidenceLocked(&current.metadata, eventType, failed)
	case "tool_execution_end":
		matched := false
		if toolCallID == "" {
			if current.openAnonymousTools > 0 {
				current.openAnonymousTools--
				matched = true
			}
		} else if _, exists := current.openToolCalls[toolCallID]; exists {
			delete(current.openToolCalls, toolCallID)
			matched = true
		}
		if !matched {
			evidence.Evidence = "partial"
			return
		}
		updateTurnToolEvidenceLocked(&current.metadata, eventType, failed)
	}
}

func finalizeTurnEvidenceLocked(metadata *taskMetadata, status string, workspace *turnWorkspaceEvidence, endSequence uint64) {
	if len(metadata.TurnEvidence) == 0 {
		return
	}
	evidence := &metadata.TurnEvidence[len(metadata.TurnEvidence)-1]
	if evidence.Status != "running" {
		return
	}
	now := time.Now().UTC()
	evidence.Status = status
	evidence.EndedAt = &now
	evidence.EndSequence = endSequence
	evidence.WorkspaceAfter = workspace
	evidence.WorkspaceChanged = compareTurnWorkspaces(evidence.WorkspaceBefore, workspace)
	if evidence.OpenTools == 0 && evidence.ToolsCompleted == evidence.ToolsStarted && evidence.Evidence != "partial" {
		evidence.Evidence = "complete"
	} else {
		evidence.Evidence = "partial"
	}
	switch status {
	case "completed":
		if evidence.Evidence == "complete" {
			evidence.RecommendedAction = "none"
		} else {
			evidence.RecommendedAction = "review"
		}
	case "interrupted", "failed", "stopped":
		if metadata.SessionFile != "" {
			evidence.RecommendedAction = "review-before-resume"
		} else {
			evidence.RecommendedAction = "review-before-restart"
		}
	default:
		evidence.RecommendedAction = "review"
	}
}

func finalizeRunningTurnAfterRestart(metadata *taskMetadata, endSequence uint64) bool {
	if len(metadata.TurnEvidence) == 0 || metadata.TurnEvidence[len(metadata.TurnEvidence)-1].Status != "running" {
		return false
	}
	finalizeTurnEvidenceLocked(metadata, "interrupted", nil, endSequence)
	return true
}

func compareTurnWorkspaces(before, after *turnWorkspaceEvidence) *bool {
	if before == nil || after == nil || before.Status != "captured" || after.Status != "captured" || before.StateDigest == "" || after.StateDigest == "" {
		return nil
	}
	changed := before.StateDigest != after.StateDigest
	return &changed
}

func normalizePersistedTurnEvidence(records []taskTurnEvidence, hasSession bool) []taskTurnEvidence {
	if len(records) > maximumTurnEvidenceRecords {
		records = records[len(records)-maximumTurnEvidenceRecords:]
	}
	result := make([]taskTurnEvidence, 0, len(records))
	lastTurn := uint64(0)
	for _, evidence := range records {
		if evidence.Turn <= lastTurn || evidence.Turn == 0 || evidence.StartSequence == 0 || evidence.StartedAt.IsZero() ||
			evidence.ToolsStarted < 0 || evidence.ToolsCompleted < 0 || evidence.ToolsFailed < 0 || evidence.OpenTools < 0 ||
			evidence.ToolsCompleted > evidence.ToolsStarted || evidence.ToolsFailed > evidence.ToolsCompleted || evidence.OpenTools > evidence.ToolsStarted {
			continue
		}
		switch evidence.Status {
		case "running", "completed", "interrupted", "failed", "stopped":
		default:
			continue
		}
		evidence.WorkspaceBefore = normalizeTurnWorkspaceEvidence(evidence.WorkspaceBefore)
		evidence.WorkspaceAfter = normalizeTurnWorkspaceEvidence(evidence.WorkspaceAfter)
		if evidence.Status == "running" {
			evidence.EndedAt = nil
			evidence.EndSequence = 0
			evidence.WorkspaceAfter = nil
			evidence.WorkspaceChanged = nil
			evidence.Evidence = "in-progress"
			evidence.RecommendedAction = "none"
		} else {
			if evidence.EndedAt == nil || evidence.EndedAt.IsZero() || evidence.EndSequence < evidence.StartSequence {
				continue
			}
			if evidence.Evidence != "complete" || evidence.OpenTools != 0 || evidence.ToolsCompleted != evidence.ToolsStarted {
				evidence.Evidence = "partial"
			}
			evidence.WorkspaceChanged = compareTurnWorkspaces(evidence.WorkspaceBefore, evidence.WorkspaceAfter)
			switch evidence.Status {
			case "completed":
				if evidence.Evidence == "complete" {
					evidence.RecommendedAction = "none"
				} else {
					evidence.RecommendedAction = "review"
				}
			case "interrupted", "failed", "stopped":
				if hasSession {
					evidence.RecommendedAction = "review-before-resume"
				} else {
					evidence.RecommendedAction = "review-before-restart"
				}
			}
		}
		result = append(result, evidence)
		lastTurn = evidence.Turn
	}
	return result
}

func normalizeTurnWorkspaceEvidence(evidence *turnWorkspaceEvidence) *turnWorkspaceEvidence {
	if evidence == nil {
		return nil
	}
	copy := *evidence
	evidence = &copy
	if evidence.CapturedAt.IsZero() {
		evidence.Status = "unavailable"
	}
	switch evidence.Status {
	case "captured", "partial":
		if decoded, err := hex.DecodeString(evidence.StateDigest); err != nil || len(decoded) != sha256.Size {
			evidence.Status = "unavailable"
			evidence.StateDigest = ""
		}
	case "not-repository", "unavailable":
		evidence.StateDigest = ""
	default:
		evidence.Status = "unavailable"
		evidence.StateDigest = ""
	}
	if evidence.ChangedFiles < 0 || evidence.ChangedFiles > maximumTurnEvidenceFiles {
		evidence.ChangedFiles = 0
		evidence.Truncated = true
	}
	return evidence
}

func (current *task) currentRunningTurnLocked() uint64 {
	if len(current.metadata.TurnEvidence) == 0 {
		return 0
	}
	evidence := current.metadata.TurnEvidence[len(current.metadata.TurnEvidence)-1]
	if evidence.Status != "running" {
		return 0
	}
	return evidence.Turn
}

func (current *task) applyTurnWorkspaceEvidence(turn uint64, before bool, evidence *turnWorkspaceEvidence) {
	current.mu.Lock()
	for index := len(current.metadata.TurnEvidence) - 1; index >= 0; index-- {
		record := &current.metadata.TurnEvidence[index]
		if record.Turn != turn {
			continue
		}
		if before {
			record.WorkspaceBefore = evidence
		} else {
			record.WorkspaceAfter = evidence
		}
		record.WorkspaceChanged = compareTurnWorkspaces(record.WorkspaceBefore, record.WorkspaceAfter)
		break
	}
	current.mu.Unlock()
	_ = current.saveMetadata()
}
