package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestModelEgressFixesEndpointAndCredential(t *testing.T) {
	cfg := config{
		DRoboticsBaseURL: "https://gateway.example/base",
		gatewayToken:     "board-secret",
		MaxTasks:         2,
	}
	service, err := newModelEgressServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	service.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://gateway.example/base/v1/messages" || request.Method != http.MethodPost {
			t.Fatalf("model request escaped the fixed endpoint: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer board-secret" || request.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Fatalf("broker did not inject its credential and protocol header: %v", request.Header)
		}
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), "board-secret") {
			t.Fatal("broker credential entered the model request body")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("event: message_stop\ndata: {}\n\n")),
		}, nil
	})
	request := httptest.NewRequest(http.MethodPost, "http://unix"+modelEgressPath, strings.NewReader(`{"model":"kimi-k3","stream":true,"messages":[]}`))
	request.Header.Set("Accept", "text/event-stream, application/json")
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "message_stop") {
		t.Fatalf("broker response was not streamed: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestModelEgressRejectsArbitraryRoutesAndInvalidBodies(t *testing.T) {
	service, err := newModelEgressServer(config{DRoboticsBaseURL: "https://gateway.example", gatewayToken: "secret", MaxTasks: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		path string
		body string
		want int
	}{
		{path: "/v1/open-proxy", body: `{"model":"kimi-k3","stream":true}`, want: http.StatusNotFound},
		{path: modelEgressPath, body: `{"model":"","stream":true}`, want: http.StatusBadRequest},
		{path: modelEgressPath, body: `{"model":"kimi-k3","stream":"yes"}`, want: http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPost, "http://unix"+scenario.path, strings.NewReader(scenario.body))
		recorder := httptest.NewRecorder()
		service.ServeHTTP(recorder, request)
		if recorder.Code != scenario.want {
			t.Fatalf("route %s returned %d, expected %d", scenario.path, recorder.Code, scenario.want)
		}
	}
}

func TestModelEgressBaseURLAndResponseLimits(t *testing.T) {
	for _, value := range []string{"http://remote.example", "https://user:pass@example.com", "https://example.com?target=other"} {
		if _, err := normalizeModelEgressBaseURL(value); err == nil {
			t.Fatalf("unsafe model base URL was accepted: %s", value)
		}
	}
	if value, err := normalizeModelEgressBaseURL("http://127.0.0.1:8080/v1"); err != nil || value != "http://127.0.0.1:8080/v1" {
		t.Fatalf("local test gateway was rejected: value=%q err=%v", value, err)
	}
	var destination bytes.Buffer
	if written, err := copyBoundedModelResponse(&destination, strings.NewReader("12345"), 4); err == nil || written != 4 || destination.String() != "1234" {
		t.Fatalf("oversized response was not bounded: written=%d value=%q err=%v", written, destination.String(), err)
	}
}

func TestManagedModelEgressRoutesFixProtocolCredentialAndModel(t *testing.T) {
	root := t.TempDir()
	providerConfig := filepath.Join(root, "providers.json")
	document := `{"schemaVersion":1,"providers":[` +
		`{"id":"anthropic","baseUrl":"https://anthropic.example","api":"anthropic-messages","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ANTHROPIC","models":[{"id":"claude-test"}]},` +
		`{"id":"anthropic-bearer","baseUrl":"https://bearer.example/api","api":"anthropic-messages","authHeader":true,"credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ANTHROPIC_BEARER","models":[{"id":"claude-bearer"}]},` +
		`{"id":"chat","baseUrl":"https://chat.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_CHAT","models":[{"id":"chat-test"}]},` +
		`{"id":"responses","baseUrl":"https://responses.example/v1","api":"openai-responses","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_RESPONSES","models":[{"id":"response-test"}]},` +
		`{"id":"google","baseUrl":"https://google.example/v1","api":"google-generative-ai","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_GOOGLE","models":[{"id":"gemini-test"}]}` +
		`]}`
	if err := os.WriteFile(providerConfig, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, DRobotics: "drobotics-secret", ProviderKeys: map[string]string{
		"HOBOT_CODE_PROVIDER_KEY_ANTHROPIC":        "anthropic-secret",
		"HOBOT_CODE_PROVIDER_KEY_ANTHROPIC_BEARER": "anthropic-bearer-secret",
		"HOBOT_CODE_PROVIDER_KEY_CHAT":             "chat-secret",
		"HOBOT_CODE_PROVIDER_KEY_RESPONSES":        "responses-secret",
		"HOBOT_CODE_PROVIDER_KEY_GOOGLE":           "google-secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{
		DRoboticsBaseURL: defaultDroboticsBaseURL, ManagedProviderConfig: providerConfig,
		gatewayCredential: bundle, MaxTasks: 2,
	}
	routes, err := loadModelEgressRoutes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 5 || routes["google"].ID != "" {
		t.Fatalf("unexpected managed model egress routes: %+v", routes)
	}
	for provider, endpoint := range map[string]string{
		"anthropic":        "https://anthropic.example/v1/messages",
		"anthropic-bearer": "https://bearer.example/api/v1/messages",
		"chat":             "https://chat.example/v1/chat/completions",
		"responses":        "https://responses.example/v1/responses",
	} {
		if routes[provider].Endpoint != endpoint {
			t.Fatalf("provider %s endpoint=%q, expected %q", provider, routes[provider].Endpoint, endpoint)
		}
	}

	cfg.modelEgressRoutes = routes
	service, err := newModelEgressServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct {
		endpoint      string
		authorization string
		apiKey        string
	}{
		"claude-test":   {endpoint: "https://anthropic.example/v1/messages", apiKey: "anthropic-secret"},
		"claude-bearer": {endpoint: "https://bearer.example/api/v1/messages", authorization: "Bearer anthropic-bearer-secret"},
		"chat-test":     {endpoint: "https://chat.example/v1/chat/completions", authorization: "Bearer chat-secret"},
		"response-test": {endpoint: "https://responses.example/v1/responses", authorization: "Bearer responses-secret"},
	}
	requests := 0
	service.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		body, _ := io.ReadAll(request.Body)
		var envelope struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		want, ok := expected[envelope.Model]
		if !ok || request.URL.String() != want.endpoint {
			t.Fatalf("request escaped its configured route: model=%q endpoint=%q", envelope.Model, request.URL)
		}
		if request.Header.Get("Authorization") != want.authorization || request.Header.Get("X-Api-Key") != want.apiKey {
			t.Fatalf("provider %s received wrong authentication headers: %v", envelope.Model, request.Header)
		}
		if strings.HasPrefix(envelope.Model, "claude") && request.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Fatalf("Anthropic protocol header is missing: %v", request.Header)
		}
		if envelope.Model == "claude-test" && request.Header.Get("Anthropic-Beta") != "interleaved-thinking-2025-05-14" {
			t.Fatalf("bounded Anthropic beta header was not forwarded: %v", request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: done\n\n"))}, nil
	})
	for provider, model := range map[string]string{
		"anthropic": "claude-test", "anthropic-bearer": "claude-bearer", "chat": "chat-test", "responses": "response-test",
	} {
		request := httptest.NewRequest(http.MethodPost, "http://unix"+modelEgressProviderPrefix+provider, strings.NewReader(`{"model":"`+model+`","stream":true}`))
		request.Header.Set("Authorization", "Bearer worker-secret")
		request.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")
		recorder := httptest.NewRecorder()
		service.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("provider %s returned %d: %s", provider, recorder.Code, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://unix"+modelEgressProviderPrefix+"chat", strings.NewReader(`{"model":"not-configured","stream":true}`))
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || requests != len(expected) {
		t.Fatalf("model allowlist was not enforced before transport: status=%d requests=%d", recorder.Code, requests)
	}

	if err := os.WriteFile(providerConfig, []byte(`{"schemaVersion":1,"providers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadModelEgressRoutes(cfg)
	if err != nil || len(snapshot) != len(routes) {
		t.Fatalf("daemon route snapshot drifted after config changed: routes=%+v err=%v", snapshot, err)
	}
}
