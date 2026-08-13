package main

import (
	"io"
	"strings"
	"testing"
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
