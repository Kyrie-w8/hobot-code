package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func writeAnthropicConformanceStream(response http.ResponseWriter, events ...map[string]any) {
	response.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		fmt.Fprintf(response, "data: %s\n\n", encoded)
	}
}

func anthropicToolCallEvents() []map[string]any {
	return []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": "msg-tool", "usage": map[string]any{"input_tokens": 10}}},
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "tool-1", "name": conformanceToolName, "input": map[string]any{}}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"value":"ping"}`}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}},
		{"type": "message_stop"},
	}
}

func anthropicTextEvents(text string) []map[string]any {
	return []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": "msg-text", "usage": map[string]any{"input_tokens": 10}}},
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}},
		{"type": "message_stop"},
	}
}

func TestProbeDroboticsModelConformanceCompletesAgentLoop(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("model verification omitted the board credential")
		}
		body, _ := io.ReadAll(io.LimitReader(request.Body, 32*1024))
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil || payload["stream"] != true {
			t.Fatalf("invalid conformance payload: %s", body)
		}
		switch calls.Add(1) {
		case 1:
			if _, ok := payload["tool_choice"]; ok {
				t.Fatal("conformance request used a tool_choice field unsupported by some compatible gateways")
			}
			writeAnthropicConformanceStream(response, anthropicToolCallEvents()...)
		case 2:
			if !strings.Contains(string(body), `"tool_use_id":"tool-1"`) {
				t.Fatalf("matching tool result is missing: %s", body)
			}
			writeAnthropicConformanceStream(response, anthropicTextEvents("HOBOT_OK")...)
		case 3:
			if !strings.Contains(string(body), `"type":"image"`) {
				t.Fatalf("image conformance payload is missing: %s", body)
			}
			writeAnthropicConformanceStream(response, anthropicTextEvents("VISION_OK")...)
		default:
			t.Fatalf("unexpected conformance call %d", calls.Load())
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-token")

	result := normalizeModelConformanceResult(probeDroboticsModelConformance(context.Background(), modelOption{
		Provider: "drobotics", ID: "kimi-k3", Capabilities: modelCapabilities{ImageInput: true},
	}))
	if result.Status != "verified" || result.Attempts != 3 || len(result.Checks) != 4 {
		t.Fatalf("conformance result = %+v", result)
	}
	for _, check := range result.Checks {
		if check.Status != "passed" || check.Message == "" {
			t.Fatalf("conformance check = %+v", check)
		}
	}
}

func TestConformanceFailsClosedOnIncompleteToolStream(t *testing.T) {
	events := anthropicToolCallEvents()
	events = events[:len(events)-1]
	body := strings.Builder{}
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		fmt.Fprintf(&body, "data: %s\n\n", encoded)
	}
	parsed, err := parseAnthropicConformanceSSE([]byte(body.String()), "text/event-stream")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.terminal {
		t.Fatal("incomplete stream was accepted as terminal")
	}
	result := normalizeModelConformanceResult(modelConformanceResult{Checks: []modelConformanceCheck{
		conformanceCheck("streaming", false, 1), conformanceCheck("tool-call", false, 1),
		conformanceCheck("tool-result", false, 0), {Name: "image-input", Status: "skipped"},
	}})
	if result.Status != "failed" || strings.Contains(result.Message, "gateway") {
		t.Fatalf("failed conformance was unsafe or incorrectly verified: %+v", result)
	}
}

func TestOpenAIConformanceParserReassemblesToolCalls(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"hobot_","arguments":"{\"value\":"}}]},"finish_reason":null}]}`,
		"",
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"conformance","arguments":"\"ping\"}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	result, err := parseOpenAIConformanceSSE([]byte(body), "text/event-stream")
	if err != nil || !result.terminal || result.toolID != "call_1" || result.toolName != conformanceToolName || result.toolInput["value"] != conformanceToolCallValue {
		t.Fatalf("OpenAI conformance stream = %+v err=%v", result, err)
	}
}

func TestConformanceSkipsUnadvertisedImagesWithoutWeakeningRequiredChecks(t *testing.T) {
	result := normalizeModelConformanceResult(modelConformanceResult{Checks: []modelConformanceCheck{
		conformanceCheck("streaming", true, 1), conformanceCheck("tool-call", true, 1),
		conformanceCheck("tool-result", true, 1), {Name: "image-input", Status: "skipped"},
	}})
	if result.Status != "verified" || result.Checks[3].Message == "" {
		t.Fatalf("text-only conformance result = %+v", result)
	}
}

func TestConformanceNormalizationRejectsMissingDuplicateAndUnknownChecks(t *testing.T) {
	valid := []modelConformanceCheck{
		conformanceCheck("streaming", true, 1), conformanceCheck("tool-call", true, 1),
		conformanceCheck("tool-result", true, 1), {Name: "image-input", Status: "skipped"},
	}
	for name, checks := range map[string][]modelConformanceCheck{
		"missing":   valid[:3],
		"duplicate": append(append([]modelConformanceCheck{}, valid[:3]...), valid[2]),
		"unknown":   append(append([]modelConformanceCheck{}, valid[:3]...), modelConformanceCheck{Name: "mystery", Status: "passed"}),
	} {
		if result := normalizeModelConformanceResult(modelConformanceResult{Checks: checks}); result.Status != "failed" {
			t.Fatalf("%s check set was accepted: %+v", name, result)
		}
	}
}

func TestConformanceReportsBufferedToolFallbackAsCompatible(t *testing.T) {
	toolJSON := []byte(`{"content":[{"type":"thinking","thinking":"probe"},{"type":"tool_use","id":"tool-1","name":"hobot_conformance","input":{"value":"ping"}}],"stop_reason":"tool_use"}`)
	response, err := parseAnthropicConformanceJSON(toolJSON, "application/json")
	if err != nil || !response.terminal || response.toolName != conformanceToolName || response.toolInput["value"] != conformanceToolCallValue {
		t.Fatalf("buffered tool response = %+v err=%v", response, err)
	}
	result := normalizeModelConformanceResult(modelConformanceResult{Checks: []modelConformanceCheck{
		{Name: "streaming", Status: "degraded"}, {Name: "tool-call", Status: "passed"},
		{Name: "tool-result", Status: "passed"}, {Name: "image-input", Status: "passed"},
	}})
	if result.Status != "compatible" || !strings.Contains(result.Checks[0].Message, "buffered fallback") {
		t.Fatalf("buffered fallback result = %+v", result)
	}
}

func TestConformanceExchangeFallsBackAfterIncompleteStream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(request.Body, 32*1024))
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			t.Fatalf("invalid conformance payload: %s", body)
		}
		switch calls.Add(1) {
		case 1:
			if payload["stream"] != true {
				t.Fatalf("first attempt did not request streaming: %s", body)
			}
			writeAnthropicConformanceStream(response, map[string]any{
				"type": "message_start", "message": map[string]any{"id": "msg-incomplete"},
			})
		case 2:
			if payload["stream"] != false {
				t.Fatalf("fallback attempt still requested streaming: %s", body)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"content":[{"type":"tool_use","id":"tool-1","name":"hobot_conformance","input":{"value":"ping"}}],"stop_reason":"tool_use"}`))
		default:
			t.Fatalf("unexpected conformance call %d", calls.Load())
		}
	}))
	t.Cleanup(server.Close)

	result, transport, attempts, err := performConformanceExchange(
		context.Background(), server.Client(), server.URL, "test-token", anthropicToolRequest("kimi-k3"), false,
	)
	if err != nil || transport != "json" || attempts != 2 || result.toolName != conformanceToolName {
		t.Fatalf("fallback result = %+v transport=%q attempts=%d err=%v", result, transport, attempts, err)
	}
}

func TestConformanceExchangeNeverRetriesAfterPartialToolOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		events := anthropicToolCallEvents()
		writeAnthropicConformanceStream(response, events[:2]...)
	}))
	t.Cleanup(server.Close)

	result, transport, attempts, err := performConformanceExchange(
		context.Background(), server.Client(), server.URL, "test-token", anthropicToolRequest("kimi-k3"), false,
	)
	if err != nil || transport != "" || attempts != 1 || calls.Load() != 1 || result.terminal || result.toolID == "" {
		t.Fatalf("partial stream = %+v transport=%q attempts=%d calls=%d err=%v", result, transport, attempts, calls.Load(), err)
	}
}

func TestConformanceExchangeAcceptsImmediateBufferedResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"content":[{"type":"tool_use","id":"tool-1","name":"hobot_conformance","input":{"value":"ping"}}],"stop_reason":"tool_use"}`))
	}))
	t.Cleanup(server.Close)

	result, transport, attempts, err := performConformanceExchange(
		context.Background(), server.Client(), server.URL, "test-token", anthropicToolRequest("kimi-k3"), false,
	)
	if err != nil || transport != "json" || attempts != 1 || calls.Load() != 1 || result.toolName != conformanceToolName {
		t.Fatalf("buffered result = %+v transport=%q attempts=%d calls=%d err=%v", result, transport, attempts, calls.Load(), err)
	}
}

func TestConformanceContinuationAcceptsSilentEndTurnButRejectsIncompleteTurns(t *testing.T) {
	if got := conformanceContinuationCategory(conformanceResponse{terminal: true, stopReason: "end_turn"}, nil); got != "ok" {
		t.Fatal("a valid thinking-only end turn was rejected")
	}
	for name, test := range map[string]struct {
		response conformanceResponse
		want     string
	}{
		"not-terminal": {response: conformanceResponse{stopReason: "end_turn"}, want: "incomplete-response"},
		"token-limit":  {response: conformanceResponse{terminal: true, stopReason: "max_tokens"}, want: "token-limit"},
		"another-tool": {response: conformanceResponse{terminal: true, stopReason: "tool_use", toolID: "tool-2", toolName: conformanceToolName}, want: "ok"},
	} {
		if got := conformanceContinuationCategory(test.response, nil); got != test.want {
			t.Fatalf("%s continuation category = %q, want %q", name, got, test.want)
		}
	}
}

func TestModelConformanceServiceCachesAndForceRefreshes(t *testing.T) {
	manager, err := newTaskManager(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	service := newModelConformanceService()
	var calls atomic.Int32
	service.probe = func(context.Context, modelOption) modelConformanceResult {
		calls.Add(1)
		return modelConformanceResult{Checks: []modelConformanceCheck{
			{Name: "streaming", Status: "passed"}, {Name: "tool-call", Status: "passed"},
			{Name: "tool-result", Status: "passed"}, {Name: "image-input", Status: "passed"},
		}}
	}
	first, err := service.check(manager, modelConformanceParams{Model: "drobotics/kimi-k3"})
	if err != nil || first.Cached || first.Status != "verified" || calls.Load() != 1 {
		t.Fatalf("first conformance check = %+v err=%v calls=%d", first, err, calls.Load())
	}
	second, err := service.check(manager, modelConformanceParams{Model: "drobotics/kimi-k3"})
	if err != nil || !second.Cached || calls.Load() != 1 {
		t.Fatalf("cached conformance check = %+v err=%v calls=%d", second, err, calls.Load())
	}
	third, err := service.check(manager, modelConformanceParams{Model: "drobotics/kimi-k3", Force: true})
	if err != nil || third.Cached || calls.Load() != 2 {
		t.Fatalf("forced conformance check = %+v err=%v calls=%d", third, err, calls.Load())
	}
}
