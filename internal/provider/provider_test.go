package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func TestOpenAICompatibleToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "local-model" {
			t.Errorf("model=%v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"system_snapshot","arguments":"{}"}}]}}],"usage":{"total_tokens":9}}`))
	}))
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "openai-compatible", Model: "local-model", BaseURL: server.URL + "/v1", TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	response, err := p.Complete(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "system_snapshot" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestOpenAIResponsesTextAndTool(t *testing.T) {
	server := jsonServer(t, "/responses", `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"checking"}]},{"type":"function_call","call_id":"c2","name":"system_snapshot","arguments":"{}"}],"usage":{"total_tokens":7}}`)
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "openai-responses", Model: "m", BaseURL: server.URL, APIKey: "key", TimeoutSec: 5})
	if err != nil {
		t.Fatal(err)
	}
	response, err := p.Complete(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "checking" || len(response.ToolCalls) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestAnthropicResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("authorization=%q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("unexpected x-api-key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stop_reason":"tool_use","content":[{"type":"text","text":"read"},{"type":"tool_use","id":"a1","name":"system_snapshot","input":{}}],"usage":{"input_tokens":1}}`))
	}))
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "anthropic", Model: "m", BaseURL: server.URL, APIKey: "key", AuthStyle: "bearer", TimeoutSec: 5, Settings: map[string]any{"max_tokens": 32}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := p.Complete(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "read" || len(response.ToolCalls) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestAnthropicStreamEmitsReasoningTextAndTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true {
			t.Fatalf("stream=%v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"check "}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call-1","name":"echo","input":{}}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"text\":\"ok\"}"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
			`{"type":"message_stop"}`,
		}
		for _, event := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "anthropic", Model: "m", BaseURL: server.URL, APIKey: "key", TimeoutSec: 5, Settings: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	streaming := p.(core.StreamingProvider)
	var chunks []core.ProviderChunk
	response, err := streaming.CompleteStream(context.Background(), request(), func(chunk core.ProviderChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Reasoning != "check " || response.Content != "done" || len(response.ToolCalls) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.ToolCalls[0].Arguments["text"] != "ok" {
		t.Fatalf("unexpected tool call: %+v", response.ToolCalls[0])
	}
	if len(chunks) != 2 || chunks[0].Type != core.ProviderReasoningDelta || chunks[1].Type != core.ProviderTextDelta {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
}

func TestAnthropicEmptyStreamFallsBackToComplete(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0}}}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"fallback"}],"usage":{"output_tokens":2}}`)
	}))
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "anthropic", Model: "m", BaseURL: server.URL, APIKey: "key", TimeoutSec: 5, Settings: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	response, err := p.(core.StreamingProvider).CompleteStream(context.Background(), request(), func(chunk core.ProviderChunk) {
		if chunk.Type == core.ProviderTextDelta {
			text += chunk.Delta
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || response.Content != "fallback" || text != "fallback" {
		t.Fatalf("requests=%d response=%+v text=%q", requests, response, text)
	}
}

func TestGeminiResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/models/local-model:generateContent") {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"done"}]}}],"usageMetadata":{"totalTokenCount":4}}`))
	}))
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "gemini", Model: "m", BaseURL: server.URL, APIKey: "key", TimeoutSec: 5, Settings: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := p.Complete(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "done" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func request() core.ProviderRequest {
	return core.ProviderRequest{
		Model: "local-model", SystemPrompt: "system", Messages: []core.Message{{Role: "user", Content: "status"}},
		Tools: []core.ToolDefinition{{Name: "system_snapshot", Description: "status", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}, Settings: map[string]any{},
	}
}

func jsonServer(t *testing.T, path, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
}
