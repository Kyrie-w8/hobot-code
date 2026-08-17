package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestScheduleCreateCLIUsesCurrentTaskAndPromptFlag(t *testing.T) {
	currentTask := "0123456789abcdef01234567"
	params, err := parseScheduleCreate([]string{
		"--every", "15m",
		"--prompt", "check compile progress and report changes",
	}, currentTask)
	if err != nil {
		t.Fatal(err)
	}
	if params.TaskID != currentTask || params.Every != "15m" || params.Prompt != "check compile progress and report changes" || params.Name != "" {
		t.Fatalf("unexpected inferred schedule: %+v", params)
	}
}

func TestScheduleCreateCLIExternalCallsRequireTask(t *testing.T) {
	_, err := parseScheduleCreate([]string{"--every", "15m", "--prompt", "check"}, "")
	if err == nil || !strings.Contains(err.Error(), "required outside a running main Agent") {
		t.Fatalf("missing external task returned an unhelpful error: %v", err)
	}
	params, err := parseScheduleCreate([]string{
		"--task", "0123456789abcdef01234567",
		"--at", "2026-08-18T09:00:00+08:00",
		"--", "check", "once",
	}, "")
	if err != nil || params.Prompt != "check once" || params.Name != "" {
		t.Fatalf("explicit external schedule was not parsed: %+v %v", params, err)
	}
}

func TestScheduleCreateCLIExplainsUnsupportedCron(t *testing.T) {
	_, err := parseScheduleCreate([]string{
		"--cron", "*/15 * * * *",
		"--prompt", "check",
	}, "0123456789abcdef01234567")
	if err == nil || !strings.Contains(err.Error(), "replace") || !strings.Contains(err.Error(), "--every 15m") {
		t.Fatalf("common cron expression returned an unhelpful error: %v", err)
	}
	_, err = parseScheduleCreate([]string{"--cron", "0 9 * * *", "--prompt", "check"}, "0123456789abcdef01234567")
	if err == nil || !strings.Contains(err.Error(), "--at RFC3339") {
		t.Fatalf("general cron expression returned an unhelpful error: %v", err)
	}
}

func TestScheduleCreateCLIRejectsAmbiguousPromptsAndOptions(t *testing.T) {
	currentTask := "0123456789abcdef01234567"
	for _, args := range [][]string{
		{"--every", "15m", "--prompt", "first", "--", "second"},
		{"--every", "15m", "--prompt", "first", "--prompt", "second"},
		{"--every", "15m", "--unknown", "value", "--prompt", "check"},
		{"--every", "15m", "--prompt", ""},
	} {
		if _, err := parseScheduleCreate(args, currentTask); err == nil {
			t.Fatalf("ambiguous schedule options were accepted: %v", args)
		}
	}
}

func TestScheduleCreateHelpShowsAgentSafeSyntax(t *testing.T) {
	var output bytes.Buffer
	if !printRequestedScheduleHelp([]string{"create", "--help"}, &output) {
		t.Fatal("create help was not handled")
	}
	help := output.String()
	for _, expected := range []string{"[--task TASK_ID]", "--prompt PROMPT", "--every 15m", "cron expressions are not supported"} {
		if !strings.Contains(help, expected) {
			t.Fatalf("create help is missing %q:\n%s", expected, help)
		}
	}
}
