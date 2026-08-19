package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissionReviewerReservesOutputForReasoningModels(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, incoming *http.Request) {
		body, err := io.ReadAll(incoming.Body)
		if err != nil || json.Unmarshal(body, &request) != nil {
			t.Fatalf("decode reviewer request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(response, bytes.NewBufferString(`{"content":[{"type":"thinking","thinking":"bounded"},{"type":"text","text":"{\"decision\":\"approved\",\"risk\":\"low\",\"reason\":\"The action is scoped.\"}"}]}`))
	}))
	defer server.Close()
	service := newPermissionReviewerService(&modelEgressServer{
		client: server.Client(), slots: make(chan struct{}, 1), routes: map[string]modelEgressRoute{
			"drobotics": {ID: "drobotics", API: "drobotics-anthropic", Endpoint: server.URL, Credential: "secret"},
		},
	})
	content, err := service.callModel(context.Background(), modelOption{Provider: "drobotics", ID: "kimi-k3"}, permissionReviewEnvelope{Tool: "bash"})
	if err != nil || !bytes.Contains(content, []byte(`"decision":"approved"`)) {
		t.Fatalf("reasoning response was not decoded: %q err=%v", content, err)
	}
	if request["max_tokens"] != float64(permissionReviewMaxTokens) || permissionReviewMaxTokens < 4096 {
		t.Fatalf("reviewer output budget is too small for reasoning models: %+v", request["max_tokens"])
	}
}

func newPermissionReviewTestTask(t *testing.T) (*task, *permissionReviewerService) {
	t.Helper()
	root := t.TempDir()
	cfg := config{
		SessionDir: root, MaxEventSize: 1024 * 1024,
		modelEgressRoutes: map[string]modelEgressRoute{
			"drobotics": {ID: "drobotics", API: "drobotics-anthropic", Models: map[string]bool{"kimi-k3": true, "qwen3.8-max": true}},
		},
	}
	manager := &taskManager{cfg: cfg, tasks: map[string]*task{}, models: map[string]modelOption{
		"drobotics/kimi-k3":     {Provider: "drobotics", ID: "kimi-k3", Default: true},
		"drobotics/qwen3.8-max": {Provider: "drobotics", ID: "qwen3.8-max"},
	}}
	manager.modelsOnce.Do(func() {})
	events := filepath.Join(root, "events.jsonl")
	record := taskEvent{Protocol: protocolVersion, Kind: "event", TaskID: "00112233445566778899aabb", Sequence: 1, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "deploy the model on S600"}}}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(events, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{manager: manager, events: events, eventBytes: int64(len(encoded) + 1), eventLastSequence: 1, metadata: taskMetadata{
		ID: record.TaskID, Name: "deployment", Cwd: "/workspace", Model: "drobotics/kimi-k3",
		PermissionMode: "auto-review", SandboxMode: "system", NetworkMode: "shared", LastSequence: 1,
	}, subscribers: map[uint64]chan taskEvent{}}
	service := newPermissionReviewerService(&modelEgressServer{routes: cfg.modelEgressRoutes})
	manager.reviewer = service
	manager.tasks[current.metadata.ID] = current
	return current, service
}

func TestPermissionReviewerUsesIndependentApprovalModel(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	current.metadata.ApprovalModel = "drobotics/qwen3.8-max"
	service.call = func(_ context.Context, model modelOption, _ permissionReviewEnvelope) ([]byte, error) {
		if model.ID != "qwen3.8-max" {
			t.Fatalf("reviewer used task model instead of selected approval model: %+v", model)
		}
		return []byte(`{"decision":"approved","risk":"low","reason":"The action is scoped and reversible."}`), nil
	}
	input := map[string]any{"command": "hobot schedule pause 0123456789abcdef01234567"}
	result, err := service.review(current, permissionReviewParams{Tool: "bash", Input: input, Fingerprint: permissionReviewFingerprint("bash", input)})
	if err != nil || result.Model != "drobotics/qwen3.8-max" || result.Status != "approved" {
		t.Fatalf("unexpected independent reviewer result: %+v err=%v", result, err)
	}
}

func TestSetApprovalModelPersistsFixedAndFollowModes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "00112233445566778899aabb")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &taskManager{
		cfg: config{modelEgressRoutes: map[string]modelEgressRoute{
			"drobotics": {ID: "drobotics", API: "drobotics-anthropic", Models: map[string]bool{"kimi-k3": true, "qwen3.8-max": true}},
		}},
		tasks: map[string]*task{},
		models: map[string]modelOption{
			"drobotics/kimi-k3":     {Provider: "drobotics", ID: "kimi-k3", Default: true},
			"drobotics/qwen3.8-max": {Provider: "drobotics", ID: "qwen3.8-max"},
		},
	}
	manager.modelsOnce.Do(func() {})
	current := &task{manager: manager, dir: dir, metadata: taskMetadata{
		ID: "00112233445566778899aabb", Name: "approval-model", Status: statusIdle, Model: "drobotics/kimi-k3",
	}}
	manager.tasks[current.metadata.ID] = current
	metadata, err := manager.setApprovalModel(setTaskApprovalModelParams{TaskID: current.metadata.ID, Model: "drobotics/qwen3.8-max"})
	if err != nil || metadata.ApprovalModel != "drobotics/qwen3.8-max" {
		t.Fatalf("set fixed approval model: %+v err=%v", metadata, err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	var persisted taskMetadata
	if err != nil || json.Unmarshal(content, &persisted) != nil || persisted.ApprovalModel != "drobotics/qwen3.8-max" {
		t.Fatalf("fixed approval model was not persisted: %s err=%v", content, err)
	}
	metadata, err = manager.setApprovalModel(setTaskApprovalModelParams{TaskID: current.metadata.ID})
	if err != nil || metadata.ApprovalModel != "" {
		t.Fatalf("restore follow mode: %+v err=%v", metadata, err)
	}
	if _, err := manager.setApprovalModel(setTaskApprovalModelParams{TaskID: current.metadata.ID, Model: "missing/model"}); err == nil {
		t.Fatal("unavailable approval model was accepted")
	}
}

func TestPermissionReviewerNeverAutoApprovesHighRisk(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	service.call = func(context.Context, modelOption, permissionReviewEnvelope) ([]byte, error) {
		return []byte(`{"decision":"approved","risk":"high","reason":"The process can be stopped."}`), nil
	}
	input := map[string]any{"command": "kill 1234"}
	result, err := service.review(current, permissionReviewParams{Tool: "bash", Input: input, Fingerprint: permissionReviewFingerprint("bash", input)})
	if err != nil || result.Status != "manual-required" || result.Scope != nil || !strings.Contains(result.Reasons[0], "must confirm") {
		t.Fatalf("high-risk model approval was not downgraded: %+v err=%v", result, err)
	}
}

func TestPermissionReviewerRecordsVisibleDecisionEvent(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	service.call = func(context.Context, modelOption, permissionReviewEnvelope) ([]byte, error) {
		return []byte(`{"decision":"approved","risk":"low","reason":"The check is bounded."}`), nil
	}
	input := map[string]any{"command": "hobot schedule pause 0123456789abcdef01234567"}
	if _, err := service.review(current, permissionReviewParams{Tool: "bash", Input: input, Fingerprint: permissionReviewFingerprint("bash", input)}); err != nil {
		t.Fatal(err)
	}
	events, err := readEvents(current.events, current.metadata.ID, 0)
	if err != nil || len(events) < 2 {
		t.Fatalf("read reviewer event: events=%d err=%v", len(events), err)
	}
	last := events[len(events)-1]
	if last.Normalized == nil || last.Normalized.Type != "approval.reviewed" || last.Normalized.Data["status"] != "approved" {
		t.Fatalf("approval decision was not visible in the event stream: %+v", last.Normalized)
	}
}

func TestPermissionReviewerUsesTaskModelIntentAndExactScope(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	service.call = func(_ context.Context, model modelOption, envelope permissionReviewEnvelope) ([]byte, error) {
		if model.ID != "kimi-k3" || envelope.UserIntent != "deploy the model on S600" || envelope.BoardAccess != "system" || envelope.Network != "shared" {
			t.Fatalf("unexpected approval context: model=%+v envelope=%+v", model, envelope)
		}
		return []byte(`{"decision":"approved","risk":"medium","reason":"The SSH deployment matches the current task."}`), nil
	}
	input := map[string]any{"command": "ssh board install model"}
	result, err := service.review(current, permissionReviewParams{Tool: "bash", Input: input, Fingerprint: permissionReviewFingerprint("bash", input)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "approved" || result.Source != "approval-model" || result.Model != "drobotics/kimi-k3" || result.Scope == nil {
		t.Fatalf("unexpected review result: %+v", result)
	}
	audit, err := os.ReadFile(filepath.Join(current.permissionPolicyDirectory(), "approval-review-audit.jsonl"))
	if err != nil || !bytes.Contains(audit, []byte(`"source":"approval-model"`)) || bytes.Contains(audit, []byte("ssh board install model")) {
		t.Fatalf("approval audit is missing or retained tool input: %q err=%v", audit, err)
	}
}

func TestPermissionReviewerRedactsSecretsAndWriteBodiesBeforeModelCall(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	service.call = func(_ context.Context, _ modelOption, envelope permissionReviewEnvelope) ([]byte, error) {
		encoded, err := json.Marshal(envelope.Input)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		if strings.Contains(text, "super-secret-content") || strings.Contains(text, "sk-abcdefghijklmnop") || !strings.Contains(text, "contentSummary") {
			t.Fatalf("unsafe reviewer input: %s", text)
		}
		return []byte(`{"decision":"manual-required","risk":"high","reason":"The write needs a human decision."}`), nil
	}
	input := map[string]any{"path": "/etc/example", "content": "super-secret-content", "apiKey": "sk-abcdefghijklmnop"}
	result, err := service.review(current, permissionReviewParams{Tool: "write", Input: input, Fingerprint: permissionReviewFingerprint("write", input)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "manual-required" {
		t.Fatalf("unexpected review result: %+v", result)
	}
}

func TestPermissionReviewerRejectsMalformedModelOutput(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	for _, response := range []string{
		"```json\n{\"decision\":\"approved\"}\n```",
		`{"decision":"approved","risk":"low","reason":"ok","extra":true}`,
		`{"decision":"approved","risk":"unknown","reason":"ok"}`,
		`{"decision":"approved","risk":"low","reason":"ok"}{}`,
	} {
		service.call = func(context.Context, modelOption, permissionReviewEnvelope) ([]byte, error) {
			return []byte(response), nil
		}
		input := map[string]any{"command": "echo ok"}
		if _, err := service.review(current, permissionReviewParams{Tool: "bash", Input: input, Fingerprint: permissionReviewFingerprint("bash", input)}); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("malformed model decision was accepted: %q err=%v", response, err)
		}
	}
}

func TestPermissionReviewerRequiresEnabledMode(t *testing.T) {
	current, service := newPermissionReviewTestTask(t)
	input := map[string]any{"command": "echo ok"}
	current.metadata.PermissionMode = "ask"
	if _, err := service.review(current, permissionReviewParams{Tool: "bash", Input: input, Fingerprint: permissionReviewFingerprint("bash", input)}); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("review was accepted outside auto-review mode: %v", err)
	}
}

func TestPermissionReviewResponseTextSupportsProviderShapes(t *testing.T) {
	for _, test := range []struct{ api, body string }{
		{"anthropic-messages", `{"content":[{"type":"text","text":"anthropic"}]}`},
		{"openai-completions", `{"choices":[{"message":{"content":"chat"}}]}`},
		{"openai-responses", `{"output":[{"content":[{"type":"output_text","text":"responses"}]}]}`},
	} {
		text, err := permissionReviewResponseText(test.api, []byte(test.body))
		if err != nil || text == "" {
			t.Fatalf("%s response failed: text=%q err=%v", test.api, text, err)
		}
	}
}

func TestPermissionReviewCapabilityRequiresAConfiguredModelRoute(t *testing.T) {
	contains := func(values []string, expected string) bool {
		for _, value := range values {
			if value == expected {
				return true
			}
		}
		return false
	}
	server := &daemonServer{manager: &taskManager{cfg: config{MaxSideTasks: 2}}}
	if contains(server.capabilities().Capabilities, "tasks.permissions.llm-review.v1") {
		t.Fatal("model reviewer capability was advertised without model egress")
	}
	if contains(server.capabilities().Capabilities, "tasks.permissions.model.v1") {
		t.Fatal("approval model selection was advertised without model egress")
	}
	server.cfg = config{ModelEgressSocket: "/tmp/hobot-model.sock", modelEgressRoutes: map[string]modelEgressRoute{
		"drobotics": {ID: "drobotics", API: "drobotics-anthropic"},
	}}
	if !contains(server.capabilities().Capabilities, "tasks.permissions.llm-review.v1") {
		t.Fatal("model reviewer capability was not advertised with a configured route")
	}
	if !contains(server.capabilities().Capabilities, "tasks.permissions.model.v1") {
		t.Fatal("approval model selection was not advertised with a configured route")
	}
}
