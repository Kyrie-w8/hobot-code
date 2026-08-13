package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTaskHelpIsHandledBeforeStartingDaemon(t *testing.T) {
	tests := [][]string{
		{"--help"}, {"start", "--help"}, {"list", "--help"}, {"show", "--help"},
		{"logs", "task", "--help"}, {"attach", "task", "-h"}, {"send", "task", "--help"},
		{"abort", "--help"}, {"respond", "task", "request", "--help"}, {"approvals", "--help"},
		{"resume", "task", "--help"}, {"restart", "task", "--help"}, {"rename", "task", "--help"},
		{"model", "task", "--help"}, {"permissions", "task", "--help"},
		{"archive", "--help"}, {"unarchive", "--help"}, {"delete", "--help"}, {"stop", "--help"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if err := runTaskCLI(config{}, args); err != nil {
				t.Fatalf("help should be side-effect free, got %v", err)
			}
		})
	}
}

func TestTaskHelpCanBeLiteralTextOnlyAfterSeparator(t *testing.T) {
	if taskHelpRequested("resume", []string{"task", "--help"}) != true {
		t.Fatal("resume --help must request help")
	}
	if taskHelpRequested("resume", []string{"task", "--", "--help"}) {
		t.Fatal("text after -- must not request CLI help")
	}
	if taskHelpRequested("send", []string{"task", "explain", "--help"}) {
		t.Fatal("help text after an ordinary prompt start must stay literal")
	}
	if taskHelpRequested("start", []string{"--name", "demo", "--help"}) != true {
		t.Fatal("start options must detect help after option values")
	}
}

func TestTaskTextArgumentsRequireSeparatorForDashPrefix(t *testing.T) {
	if _, err := taskTextArguments([]string{"--help"}, false, "usage"); err == nil {
		t.Fatal("dash-prefixed text without -- must fail")
	}
	args, err := taskTextArguments([]string{"--", "--help"}, false, "usage")
	if err != nil || len(args) != 1 || args[0] != "--help" {
		t.Fatalf("literal help text was not preserved: %#v, %v", args, err)
	}
	args, err = taskTextArguments(nil, true, "usage")
	if err != nil || args != nil {
		t.Fatalf("optional resume text should remain empty: %#v, %v", args, err)
	}
}

func TestPrintRequestedTaskHelpWritesSpecificUsage(t *testing.T) {
	var output strings.Builder
	if !printRequestedTaskHelp([]string{"resume", "task", "--help"}, &output) {
		t.Fatal("resume help was not handled")
	}
	if !strings.Contains(output.String(), "hobot task resume TASK_ID") {
		t.Fatalf("unexpected help: %s", output.String())
	}
	if printRequestedTaskHelp([]string{"resume", "task", "--", "--help"}, io.Discard) {
		t.Fatal("literal text after -- was treated as help")
	}
}

func TestCLISummariesHideApprovalAndSessionDetails(t *testing.T) {
	now := time.Now().UTC()
	metadata := taskMetadata{
		ID: "00112233445566778899aabb", Name: "safe", Cwd: "/root/project", Status: statusWaiting,
		WorkspaceMode: workspaceModeShared, SessionFile: "/secret/session.jsonl", SessionID: "private-session",
		CreatedAt: now, UpdatedAt: now, Approvals: []pendingApproval{{
			ID: "approval-1", Method: "confirm", Title: "Allow bash?\nTarget: curl -H 'Token: secret'", Message: "secret details", Prefill: "secret", RequestedAt: now, Active: true,
		}, {
			ID: "approval-0", Method: "confirm", Title: "Old", RequestedAt: now, Active: false,
		}},
		SandboxMode: sandboxModeWorkspace,
		Sandbox:     taskSandboxStatus{Requested: sandboxModeWorkspace, Effective: sandboxModeWorkspace, Backend: "bubblewrap", FilesystemRestricted: true, DevicesRestricted: true, CapabilitiesDropped: true},
		Deployment: &deploymentRecord{
			Schema: 2, Board: "RDK S600", RDKOS: "4.0.5", Goal: "deploy-and-validate",
			Artifact:   deploymentArtifact{Path: "/secret/model.hbm", Name: "model.hbm", Kind: "hbm", Compatibility: "match"},
			ReportPath: "/secret/report.json", CreatedAt: now,
		},
		TurnEvidence: []taskTurnEvidence{{
			Turn: 1, Status: "interrupted", Evidence: "partial", StartedAt: now, ToolsStarted: 1, OpenTools: 1,
			WorkspaceBefore:   &turnWorkspaceEvidence{Status: "captured", CapturedAt: now, StateDigest: strings.Repeat("a", 64)},
			RecommendedAction: "review-before-resume",
		}},
	}
	summary := summarizeTaskForCLI(metadata)
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "secret") || strings.Contains(text, "sessionFile") || strings.Contains(text, "reportPath") || strings.Contains(text, "stateDigest") || strings.Contains(text, "artifact\":{") || summary.PendingApprovalCount != 1 || summary.InactiveApprovalCount != 1 {
		t.Fatalf("task summary leaked detail: %s", text)
	}
	if summary.LastTurnEvidence == nil || summary.LastTurnEvidence.OpenTools != 1 || summary.LastTurnEvidence.RecommendedAction != "review-before-resume" {
		t.Fatalf("task summary omitted turn recovery evidence: %s", text)
	}
	if summary.SandboxMode != sandboxModeWorkspace || summary.Sandbox.Backend != "bubblewrap" || !summary.Sandbox.FilesystemRestricted {
		t.Fatalf("task summary omitted the effective OS boundary: %s", text)
	}
	approvals := summarizeApprovalsForCLI(metadata.Approvals)
	encoded, err = json.Marshal(approvals)
	if err != nil {
		t.Fatal(err)
	}
	text = string(encoded)
	if strings.Contains(text, "Target:") || strings.Contains(text, "secret") || len(approvals) != 2 || approvals[0].Title != "Allow bash?" {
		t.Fatalf("approval summary leaked detail: %s", text)
	}
}
