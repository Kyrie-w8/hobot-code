package provider

import (
	"context"
	"encoding/json"
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
	server := jsonServer(t, "/v1/messages", `{"stop_reason":"tool_use","content":[{"type":"text","text":"read"},{"type":"tool_use","id":"a1","name":"system_snapshot","input":{}}],"usage":{"input_tokens":1}}`)
	defer server.Close()
	p, err := New(config.ProviderConfig{Type: "anthropic", Model: "m", BaseURL: server.URL, APIKey: "key", TimeoutSec: 5, Settings: map[string]any{"max_tokens": 32}})
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
