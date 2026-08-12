package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeDroboticsModelValidatesStreamingAndBufferedResponses(t *testing.T) {
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing health-check authorization header")
		}
		switch mode.Load() {
		case 0:
			response.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(response, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n")
			fmt.Fprint(response, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			fmt.Fprint(response, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n")
			fmt.Fprint(response, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		case 1:
			if request.Header.Get("Accept") != "text/event-stream, application/json" {
				t.Fatalf("unexpected Accept header")
			}
			requestBody, _ := io.ReadAll(io.LimitReader(request.Body, 4096))
			if strings.Contains(string(requestBody), `"stream":true`) {
				response.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(response, `{"error":{"message":"streaming unsupported"}}`)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			fmt.Fprint(response, `{"content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn"}`)
		default:
			response.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(response, `{"error":{"message":"Unsupported model: missing"}}`)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-token")

	health := probeDroboticsModel(context.Background(), modelOption{Provider: "drobotics", ID: "kimi-k3"})
	if health.Status != "available" || health.Transport != "sse" || health.Attempts != 1 {
		t.Fatalf("streaming health = %+v", health)
	}
	mode.Store(1)
	health = probeDroboticsModel(context.Background(), modelOption{Provider: "drobotics", ID: "kimi-k3"})
	if health.Status != "available" || health.Transport != "json" || health.Attempts != 2 {
		t.Fatalf("buffered health = %+v", health)
	}
	mode.Store(2)
	health = probeDroboticsModel(context.Background(), modelOption{Provider: "drobotics", ID: "missing"})
	if health.Status != "unavailable" || health.Category != "model-unavailable" || health.Attempts != 1 || strings.Contains(health.Message, "Unsupported model") {
		t.Fatalf("route failure was not sanitized: %+v", health)
	}
}

func TestBufferedRetryOnlyHandlesStreamCompatibilityFailures(t *testing.T) {
	if shouldRetryBuffered(healthAttempt{status: http.StatusBadRequest, body: []byte(`{"error":"unsupported model"}`)}) {
		t.Fatal("model routing failure triggered a duplicate buffered request")
	}
	if !shouldRetryBuffered(healthAttempt{status: http.StatusUnprocessableEntity, body: []byte(`{"error":"streaming unsupported"}`)}) {
		t.Fatal("stream compatibility failure did not trigger buffered fallback")
	}
}

func TestHealthValidatorsRejectEmptySuccess(t *testing.T) {
	emptySSE := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if validHealthSSE(emptySSE) {
		t.Fatal("empty SSE response was accepted")
	}
	if validHealthJSON([]byte(`{"content":[],"stop_reason":"end_turn"}`)) {
		t.Fatal("empty JSON response was accepted")
	}
}

func TestModelHealthResultNormalizationFailsClosed(t *testing.T) {
	result := normalizeModelHealthResult(modelHealthResult{Status: "available", Category: "unknown", Message: "raw gateway detail"})
	if result.Status != "unavailable" || result.Category != "protocol" || strings.Contains(result.Message, "raw gateway") {
		t.Fatalf("invalid model health did not fail closed: %+v", result)
	}
}

func TestModelHealthServiceCachesAndForceRefreshes(t *testing.T) {
	manager, err := newTaskManager(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	service := newModelHealthService()
	var calls atomic.Int32
	service.probe = func(context.Context, modelOption) modelHealthResult {
		calls.Add(1)
		return modelHealthResult{Status: "available", Category: "ok", Message: modelHealthMessage("ok"), Attempts: 1}
	}
	clock := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	service.now = func() time.Time { return clock }

	first, err := service.check(manager, modelHealthParams{Model: "drobotics/kimi-k3"})
	if err != nil || first.Cached || calls.Load() != 1 {
		t.Fatalf("first health check = %+v err=%v calls=%d", first, err, calls.Load())
	}
	second, err := service.check(manager, modelHealthParams{Model: "drobotics/kimi-k3"})
	if err != nil || !second.Cached || calls.Load() != 1 {
		t.Fatalf("cached health check = %+v err=%v calls=%d", second, err, calls.Load())
	}
	third, err := service.check(manager, modelHealthParams{Model: "drobotics/kimi-k3", Force: true})
	if err != nil || third.Cached || calls.Load() != 2 {
		t.Fatalf("forced health check = %+v err=%v calls=%d", third, err, calls.Load())
	}
	clock = clock.Add(modelHealthCacheTTL + time.Second)
	_, _ = service.check(manager, modelHealthParams{Model: "drobotics/kimi-k3"})
	if calls.Load() != 3 {
		t.Fatalf("expired result was not refreshed: calls=%d", calls.Load())
	}
}

func TestModelHealthServiceDeduplicatesConcurrentProbes(t *testing.T) {
	manager, err := newTaskManager(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	service := newModelHealthService()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	service.probe = func(context.Context, modelOption) modelHealthResult {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return modelHealthResult{Status: "available", Category: "ok", Attempts: 1}
	}

	results := make(chan error, 2)
	check := func() {
		_, checkErr := service.check(manager, modelHealthParams{Model: "drobotics/kimi-k3"})
		results <- checkErr
	}
	go check()
	<-started
	go check()
	close(release)
	for range 2 {
		if checkErr := <-results; checkErr != nil {
			t.Fatal(checkErr)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent checks issued %d probes, want 1", calls.Load())
	}
}

func TestModelHealthRejectsUnknownAndNonDroboticsModels(t *testing.T) {
	manager, err := newTaskManager(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	service := newModelHealthService()
	if _, err := service.check(manager, modelHealthParams{Model: "drobotics/missing"}); err == nil {
		t.Fatal("unknown model was accepted")
	}
	if _, err := service.check(manager, modelHealthParams{Model: "malformed"}); err == nil {
		t.Fatal("malformed model was accepted")
	}
}
