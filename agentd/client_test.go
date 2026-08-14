package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDaemonCallTimeoutOutlivesModelHealthServerTimeout(t *testing.T) {
	if got := daemonCallTimeout("models.health"); got <= modelHealthRequestTimeout {
		t.Fatalf("models.health client timeout %s must exceed server timeout %s", got, modelHealthRequestTimeout)
	}
	if got := daemonCallTimeout("task.list"); got != 10*time.Second {
		t.Fatalf("unexpected default daemon timeout: %s", got)
	}
	if got := daemonCallTimeout("models.conformance"); got <= modelConformanceRequestTimeout {
		t.Fatalf("models.conformance client timeout %s must exceed server timeout %s", got, modelConformanceRequestTimeout)
	}
}

func TestEventRetentionNoticeExplainsRecoverableHistoryBoundaries(t *testing.T) {
	if got := eventRetentionNotice(401, 800, 800, true, true); !strings.Contains(got, "event 401") || !strings.Contains(got, "no longer retained") {
		t.Fatalf("expired cursor notice = %q", got)
	}
	if got := eventRetentionNotice(401, 800, 800, true, false); !strings.Contains(got, "newest activity") {
		t.Fatalf("rolling retention notice = %q", got)
	}
	if got := eventRetentionNotice(1, 300, 500, true, false); !strings.Contains(got, "could not be recovered") {
		t.Fatalf("legacy durability notice = %q", got)
	}
	if got := eventRetentionNotice(1, 10, 10, false, false); got != "" {
		t.Fatalf("complete history produced a notice: %q", got)
	}
}

func TestConfigurationDriftScope(t *testing.T) {
	for _, method := range []string{"models.list", "models.health", "models.conformance", "deployment.start", "task.start", "task.model", "task.resume", "task.restart", "task.fork"} {
		if !daemonMethodNeedsCurrentConfiguration(method) {
			t.Fatalf("%s should require current configuration", method)
		}
	}
	if daemonMethodNeedsCurrentConfiguration("models.qualification") {
		t.Fatal("qualification evidence must remain readable so configuration drift can invalidate it")
	}
	if !daemonMethodNeedsCurrentConfiguration("models.rdk-matrix") {
		t.Fatal("RDK matrix discovery must use the current model configuration")
	}
	for _, method := range []string{"ping", "task.list", "task.stop", "daemon.shutdown"} {
		if daemonMethodNeedsCurrentConfiguration(method) {
			t.Fatalf("%s must remain usable while configuration has drifted", method)
		}
	}
}

func TestHumanRendererShowsQueueLifecycle(t *testing.T) {
	var output bytes.Buffer
	renderer := newHumanEventRenderer("task-id", strings.NewReader(""), &output, false, func(json.RawMessage) error { return nil })
	for sequence, raw := range []string{
		`{"type":"hobot_task_queued","operation":"resume"}`,
		`{"type":"hobot_task_dequeued","operation":"resume"}`,
		`{"type":"hobot_task_queue_cancelled","operation":"resume"}`,
	} {
		if err := renderer.render(taskEvent{Sequence: uint64(sequence + 1), Event: json.RawMessage(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	text := output.String()
	for _, expected := range []string{"[queued] resume", "[starting] Agent slot acquired", "[cancelled] Queued task was not started"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("queue lifecycle %q missing from attach output: %q", expected, text)
		}
	}
}

func TestHumanRendererShowsSanitizedFailureRecovery(t *testing.T) {
	var output bytes.Buffer
	renderer := newHumanEventRenderer("task-id", strings.NewReader(""), &output, false, func(json.RawMessage) error { return nil })
	event := taskEvent{Event: json.RawMessage(`{"type":"hobot_task_failed","message":"The Agent worker exited before the task completed.","recovery":"resume"}`)}
	if err := renderer.render(event); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "[failed]") || !strings.Contains(text, "hobot task resume") || strings.Contains(text, "token=") {
		t.Fatalf("unsafe or incomplete recovery output: %q", text)
	}
}

func TestDaemonInfoDoesNotExposeConfigurationFingerprint(t *testing.T) {
	fingerprint := strings.Repeat("ab", 32)
	server := daemonServer{cfg: config{ConfigFingerprint: fingerprint}, manager: &taskManager{tasks: make(map[string]*task)}}
	current := server.info(fingerprint)
	if current.ConfigurationCurrent == nil || !*current.ConfigurationCurrent {
		t.Fatalf("matching fingerprint was not reported as current: %+v", current)
	}
	drifted := server.info(strings.Repeat("cd", 32))
	if drifted.ConfigurationCurrent == nil || *drifted.ConfigurationCurrent {
		t.Fatalf("drifted fingerprint was not reported: %+v", drifted)
	}
	legacy := server.info("")
	if legacy.ConfigurationCurrent != nil {
		t.Fatalf("fingerprint status should be omitted for legacy clients: %+v", legacy)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(fingerprint)) || bytes.Contains(encoded, []byte("configFingerprint")) {
		t.Fatalf("daemon info exposed a configuration fingerprint: %s", encoded)
	}
}

func TestNonInteractiveApprovalIsReadOnlyAndDoesNotLeakDetails(t *testing.T) {
	var output bytes.Buffer
	responses := 0
	renderer := newHumanEventRenderer("task with ' quote", strings.NewReader("yes\n"), &output, false, func(json.RawMessage) error {
		responses++
		return nil
	})
	event := approvalEvent(t, map[string]any{
		"type": "extension_ui_request", "method": "select", "id": "request'42",
		"title":   "Allow bash?\nTarget: curl -H 'Authorization: Bearer top-secret'",
		"options": []string{"Allow once", "Deny"},
		"prefill": "top-secret",
	})
	if err := renderer.render(event); err != nil {
		t.Fatal(err)
	}
	if responses != 0 {
		t.Fatal("non-interactive attach responded to an approval")
	}
	text := output.String()
	if strings.Contains(text, "top-secret") || strings.Contains(text, "Target:") {
		t.Fatalf("sensitive approval details leaked: %q", text)
	}
	if !strings.Contains(text, `hobot task respond 'task with '"'"' quote' 'request'"'"'42' VALUE`) {
		t.Fatalf("safe copyable response command missing: %q", text)
	}
}

func TestInteractiveSelectRespondsInPlace(t *testing.T) {
	var output bytes.Buffer
	var response map[string]any
	renderer := newHumanEventRenderer("task-id", strings.NewReader("9\n2\n"), &output, true, func(raw json.RawMessage) error {
		return json.Unmarshal(raw, &response)
	})
	event := approvalEvent(t, map[string]any{
		"type": "extension_ui_request", "method": "select", "id": "request-id",
		"title": "Allow edit?\nTarget: /root/private", "options": []string{"Allow once", "Deny"},
	})
	if err := renderer.render(event); err != nil {
		t.Fatal(err)
	}
	if response["type"] != "extension_ui_response" || response["id"] != "request-id" || response["value"] != "Deny" {
		t.Fatalf("unexpected approval response: %+v", response)
	}
	if strings.Contains(output.String(), "/root/private") {
		t.Fatalf("approval target leaked: %q", output.String())
	}
	if !strings.Contains(output.String(), "Enter one of the listed numbers.") || !strings.Contains(output.String(), "[approval sent]") {
		t.Fatalf("interactive feedback missing: %q", output.String())
	}
}

func TestInteractiveApprovalEOFDoesNotAbortOrRespond(t *testing.T) {
	var output bytes.Buffer
	responses := 0
	renderer := newHumanEventRenderer("task-id", strings.NewReader(""), &output, true, func(json.RawMessage) error {
		responses++
		return nil
	})
	event := approvalEvent(t, map[string]any{
		"type": "extension_ui_request", "method": "confirm", "id": "request-id", "title": "Allow?",
	})
	if err := renderer.render(event); err != nil {
		t.Fatal(err)
	}
	if responses != 0 {
		t.Fatal("closed input must not respond to or abort the task")
	}
	if !strings.Contains(output.String(), "hobot task respond") {
		t.Fatalf("manual response fallback missing: %q", output.String())
	}
}

func TestInteractiveEditorPreservesMultipleLines(t *testing.T) {
	var output bytes.Buffer
	var response map[string]any
	renderer := newHumanEventRenderer("task-id", strings.NewReader("first line\nsecond line\n.\n"), &output, true, func(raw json.RawMessage) error {
		return json.Unmarshal(raw, &response)
	})
	event := approvalEvent(t, map[string]any{
		"type": "extension_ui_request", "method": "editor", "id": "request-id", "title": "Edit instructions",
	})
	if err := renderer.render(event); err != nil {
		t.Fatal(err)
	}
	if response["value"] != "first line\nsecond line" {
		t.Fatalf("multi-line editor response was not preserved: %+v", response)
	}
	if !strings.Contains(output.String(), "line containing only a period") {
		t.Fatalf("editor completion guidance missing: %q", output.String())
	}
}

func TestTaskStartOptions(t *testing.T) {
	var help bytes.Buffer
	options, err := parseTaskStartArgs([]string{
		"--name", "inspect", "--workspace", "worktree", "--model", " drobotics/kimi-k3 ", "--permissions", "developer", "--sandbox", "system", "--network", "offline", "--trust-project", "--", "check", "board",
	}, "/workspace", &help)
	if err != nil {
		t.Fatal(err)
	}
	params := options.params
	if params.Name != "inspect" || params.Cwd != "/workspace" || params.Prompt != "check board" || params.Model != "drobotics/kimi-k3" || params.PermissionMode != "developer" || params.WorkspaceMode != "worktree" || params.SandboxMode != "system" || params.NetworkMode != "offline" || !params.Approve {
		t.Fatalf("unexpected task start params: %+v", params)
	}
	if options.usedApproveAlias {
		t.Fatal("--trust-project was reported as the legacy alias")
	}

	legacy, err := parseTaskStartArgs([]string{"--approve", "legacy prompt"}, "/workspace", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.params.Approve || !legacy.usedApproveAlias {
		t.Fatalf("legacy --approve alias was not preserved: %+v", legacy)
	}
}

func TestTaskStartOptionsRejectInvalidModelAndPermission(t *testing.T) {
	if _, err := parseTaskStartArgs([]string{"--model", "invalid", "prompt"}, "/workspace", io.Discard); err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("invalid model was accepted: %v", err)
	}
	if _, err := parseTaskStartArgs([]string{"--permissions", "unsafe", "prompt"}, "/workspace", io.Discard); err == nil || !strings.Contains(err.Error(), "review, ask, or developer") {
		t.Fatalf("invalid permission mode was accepted: %v", err)
	}
	if _, err := parseTaskStartArgs([]string{"--workspace", "unsafe", "prompt"}, "/workspace", io.Discard); err == nil || !strings.Contains(err.Error(), "shared or worktree") {
		t.Fatalf("invalid workspace mode was accepted: %v", err)
	}
	if _, err := parseTaskStartArgs([]string{"--sandbox", "unsafe", "prompt"}, "/workspace", io.Discard); err == nil || !strings.Contains(err.Error(), "review, workspace, system, or off") {
		t.Fatalf("invalid sandbox mode was accepted: %v", err)
	}
	if _, err := parseTaskStartArgs([]string{"--network", "unsafe", "prompt"}, "/workspace", io.Discard); err == nil || !strings.Contains(err.Error(), "shared, model-only, or offline") {
		t.Fatalf("invalid network mode was accepted: %v", err)
	}
}

func TestTaskApprovalResponseMatchesRequestMethod(t *testing.T) {
	options := []string{"Allow once", "Allow exact call", "Deny"}
	selected, err := taskApprovalResponse("request", "select", options, "yes")
	if err != nil || selected["value"] != "Allow once" {
		t.Fatalf("select yes did not choose the first option: response=%+v err=%v", selected, err)
	}
	denied, err := taskApprovalResponse("request", "select", options, "no")
	if err != nil || denied["value"] != "Deny" {
		t.Fatalf("select no did not choose the last option: response=%+v err=%v", denied, err)
	}
	confirmed, err := taskApprovalResponse("request", "confirm", nil, "yes")
	if err != nil || confirmed["confirmed"] != true {
		t.Fatalf("confirm yes was not preserved: response=%+v err=%v", confirmed, err)
	}
	input, err := taskApprovalResponse("request", "input", nil, "yes")
	if err != nil || input["value"] != "yes" {
		t.Fatalf("text input yes was reinterpreted: response=%+v err=%v", input, err)
	}
}

func approvalEvent(t *testing.T, value map[string]any) taskEvent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return taskEvent{Event: raw}
}
