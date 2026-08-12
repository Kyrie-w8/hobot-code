package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	modelHealthCacheTTL       = 5 * time.Minute
	modelHealthRequestTimeout = 12 * time.Second
	maximumHealthResponseSize = 256 * 1024
	defaultDroboticsBaseURL   = "https://ai-api.d-robotics.cc"
)

type modelHealthParams struct {
	Model string `json:"model,omitempty"`
	Force bool   `json:"force,omitempty"`
}

type modelHealthResult struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	Category    string    `json:"category"`
	Message     string    `json:"message"`
	Transport   string    `json:"transport,omitempty"`
	CheckedAt   time.Time `json:"checkedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	FirstByteMS int64     `json:"firstByteMs,omitempty"`
	LatencyMS   int64     `json:"latencyMs,omitempty"`
	Attempts    int       `json:"attempts"`
	Cached      bool      `json:"cached"`
}

type modelHealthCacheEntry struct {
	result modelHealthResult
}

type modelHealthService struct {
	mu       sync.Mutex
	cache    map[string]modelHealthCacheEntry
	inflight map[string]chan struct{}
	now      func() time.Time
	probe    func(context.Context, modelOption) modelHealthResult
}

func newModelHealthService() *modelHealthService {
	service := &modelHealthService{
		cache: make(map[string]modelHealthCacheEntry), inflight: make(map[string]chan struct{}), now: time.Now,
	}
	service.probe = probeDroboticsModel
	return service
}

func (service *modelHealthService) check(manager *taskManager, params modelHealthParams) (modelHealthResult, error) {
	models, err := manager.availableModels()
	if err != nil {
		return modelHealthResult{}, fmt.Errorf("discover models: %w", err)
	}
	selection := normalizeModelSelection(params.Model)
	if strings.TrimSpace(params.Model) != "" && selection == "" {
		return modelHealthResult{}, fmt.Errorf("model must use provider/model format")
	}
	if selection == "" {
		for key, model := range models {
			if model.Default {
				selection = key
				break
			}
		}
	}
	model, ok := models[selection]
	if !ok {
		return modelHealthResult{}, fmt.Errorf("model is not available: %s", selection)
	}
	if model.Provider != "drobotics" {
		return modelHealthResult{}, fmt.Errorf("model health checks currently support D-Robotics models only")
	}

	waited := false
	for {
		now := service.now().UTC()
		service.mu.Lock()
		if entry, ok := service.cache[selection]; ok && now.Before(entry.result.ExpiresAt) && (!params.Force || waited) {
			result := entry.result
			result.Cached = true
			service.mu.Unlock()
			return result, nil
		}
		if pending := service.inflight[selection]; pending != nil {
			service.mu.Unlock()
			<-pending
			waited = true
			continue
		}
		pending := make(chan struct{})
		service.inflight[selection] = pending
		service.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), modelHealthRequestTimeout)
		result := service.probe(ctx, model)
		cancel()
		result = normalizeModelHealthResult(result)
		result.Provider = model.Provider
		result.Model = model.ID
		result.CheckedAt = service.now().UTC()
		result.ExpiresAt = result.CheckedAt.Add(modelHealthCacheTTL)
		result.Cached = false

		service.mu.Lock()
		service.cache[selection] = modelHealthCacheEntry{result: result}
		delete(service.inflight, selection)
		close(pending)
		service.mu.Unlock()
		return result, nil
	}
}

func normalizeModelHealthResult(result modelHealthResult) modelHealthResult {
	if result.Status == "available" && result.Category == "ok" {
		result.Message = modelHealthMessage("ok")
		return result
	}
	result.Status = "unavailable"
	switch result.Category {
	case "configuration", "authentication", "rate-limited", "model-unavailable", "timeout", "network", "gateway", "protocol":
	default:
		result.Category = "protocol"
	}
	result.Message = modelHealthMessage(result.Category)
	return result
}

type healthAttempt struct {
	status      int
	contentType string
	body        []byte
	firstByte   time.Duration
	err         error
}

func probeDroboticsModel(ctx context.Context, model modelOption) modelHealthResult {
	started := time.Now()
	result := modelHealthResult{Status: "unavailable", Category: "configuration", Attempts: 0}
	token := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	if token == "" {
		result.Message = modelHealthMessage(result.Category)
		return result
	}
	baseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultDroboticsBaseURL
	}
	endpoint, err := modelHealthEndpoint(baseURL)
	if err != nil {
		result.Message = modelHealthMessage(result.Category)
		return result
	}
	client := &http.Client{
		Timeout:       modelHealthRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	body := map[string]any{
		"model": model.ID, "max_tokens": 16, "stream": true,
		"messages": []map[string]string{{"role": "user", "content": "Reply with OK."}},
	}
	if strings.HasPrefix(model.ID, "deepseek-v4-") {
		body["thinking"] = map[string]string{"type": "disabled"}
	}

	streamed := performHealthAttempt(ctx, client, endpoint, token, body, time.Now())
	result.Attempts = 1
	if streamed.err == nil && streamed.status >= 200 && streamed.status < 300 {
		if strings.Contains(streamed.contentType, "text/event-stream") {
			if validHealthSSE(streamed.body) {
				return successfulModelHealth(result, "sse", streamed.firstByte, time.Since(started))
			}
		} else if validHealthJSON(streamed.body) {
			return successfulModelHealth(result, "json", streamed.firstByte, time.Since(started))
		}
	}

	retryBuffered := shouldRetryBuffered(streamed)
	final := streamed
	if retryBuffered {
		body["stream"] = false
		final = performHealthAttempt(ctx, client, endpoint, token, body, time.Now())
		result.Attempts = 2
		if final.err == nil && final.status >= 200 && final.status < 300 && validHealthJSON(final.body) {
			return successfulModelHealth(result, "json", final.firstByte, time.Since(started))
		}
	}

	result.FirstByteMS = durationMilliseconds(final.firstByte)
	result.LatencyMS = durationMilliseconds(time.Since(started))
	result.Category = classifyModelHealthFailure(ctx, final)
	result.Message = modelHealthMessage(result.Category)
	return result
}

func shouldRetryBuffered(attempt healthAttempt) bool {
	if attempt.err != nil {
		return false
	}
	if attempt.status >= 200 && attempt.status < 300 {
		return true
	}
	if attempt.status == http.StatusUnsupportedMediaType {
		return true
	}
	if attempt.status != http.StatusBadRequest && attempt.status != http.StatusUnprocessableEntity {
		return false
	}
	detail := strings.ToLower(string(attempt.body))
	return strings.Contains(detail, "stream") || strings.Contains(detail, "event-stream")
}

func modelHealthEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("model gateway URL is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func performHealthAttempt(ctx context.Context, client *http.Client, endpoint, token string, payload map[string]any, started time.Time) healthAttempt {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return healthAttempt{err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return healthAttempt{err: err}
	}
	request.Header.Set("Accept", "text/event-stream, application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("User-Agent", "hobot-code-health/"+version)
	response, err := client.Do(request)
	if err != nil {
		return healthAttempt{err: err}
	}
	defer response.Body.Close()
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	body, firstByte, err := readHealthBody(response.Body, started, strings.Contains(contentType, "text/event-stream"))
	return healthAttempt{status: response.StatusCode, contentType: contentType, body: body, firstByte: firstByte, err: err}
}

func readHealthBody(reader io.Reader, started time.Time, stopAtMessageStop bool) ([]byte, time.Duration, error) {
	buffer := bytes.NewBuffer(make([]byte, 0, 4096))
	chunk := make([]byte, 16*1024)
	var firstByte time.Duration
	for {
		count, err := reader.Read(chunk)
		if count > 0 {
			if firstByte == 0 {
				firstByte = time.Since(started)
			}
			if buffer.Len()+count > maximumHealthResponseSize {
				return nil, firstByte, fmt.Errorf("model health response exceeded its size limit")
			}
			_, _ = buffer.Write(chunk[:count])
			if stopAtMessageStop && healthSSEHasStop(buffer.Bytes()) {
				return buffer.Bytes(), firstByte, nil
			}
		}
		if errors.Is(err, io.EOF) {
			return buffer.Bytes(), firstByte, nil
		}
		if err != nil {
			return nil, firstByte, err
		}
	}
}

func healthSSEHasStop(body []byte) bool {
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, block := range strings.Split(normalized, "\n\n") {
		for _, line := range strings.Split(block, "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) == nil && event.Type == "message_stop" {
				return true
			}
		}
	}
	return false
}

func validHealthSSE(body []byte) bool {
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	seenContent := false
	seenStop := false
	for _, block := range strings.Split(normalized, "\n\n") {
		data := make([]string, 0, 1)
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if len(data) == 0 || strings.Join(data, "\n") == "[DONE]" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(strings.Join(data, "\n")), &event) != nil {
			return false
		}
		typeName, _ := event["type"].(string)
		if typeName == "error" {
			return false
		}
		if typeName == "message_stop" {
			seenStop = true
		}
		if healthEventHasContent(typeName, event) {
			seenContent = true
		}
	}
	return seenContent && seenStop
}

func healthEventHasContent(typeName string, event map[string]any) bool {
	if typeName == "content_block_start" {
		return healthBlockHasContent(event["content_block"])
	}
	if typeName == "content_block_delta" {
		return healthBlockHasContent(event["delta"])
	}
	if typeName != "message_start" {
		return false
	}
	message, ok := event["message"].(map[string]any)
	if !ok {
		return false
	}
	content, ok := message["content"].([]any)
	if !ok {
		return false
	}
	for _, block := range content {
		if healthBlockHasContent(block) {
			return true
		}
	}
	return false
}

func healthBlockHasContent(value any) bool {
	block, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, field := range []string{"text", "thinking", "data"} {
		if text, ok := block[field].(string); ok && strings.TrimSpace(text) != "" {
			return true
		}
	}
	return false
}

func validHealthJSON(body []byte) bool {
	var response struct {
		Content []struct {
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
			Data     string `json:"data"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	seenContent := false
	for _, block := range response.Content {
		if strings.TrimSpace(block.Text) != "" || strings.TrimSpace(block.Thinking) != "" || strings.TrimSpace(block.Data) != "" {
			seenContent = true
			break
		}
	}
	if !seenContent {
		return false
	}
	switch response.StopReason {
	case "end_turn", "stop_sequence", "max_tokens", "refusal":
		return true
	default:
		return false
	}
}

func successfulModelHealth(result modelHealthResult, transport string, firstByte, total time.Duration) modelHealthResult {
	result.Status = "available"
	result.Category = "ok"
	result.Message = modelHealthMessage(result.Category)
	result.Transport = transport
	result.FirstByteMS = durationMilliseconds(firstByte)
	result.LatencyMS = durationMilliseconds(total)
	return result
}

func durationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return max(1, value.Milliseconds())
}

func classifyModelHealthFailure(ctx context.Context, attempt healthAttempt) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(attempt.err, context.DeadlineExceeded) {
		return "timeout"
	}
	var networkError net.Error
	if errors.As(attempt.err, &networkError) {
		if networkError.Timeout() {
			return "timeout"
		}
		return "network"
	}
	if attempt.err != nil {
		return "protocol"
	}
	switch attempt.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication"
	case http.StatusTooManyRequests:
		return "rate-limited"
	case http.StatusNotFound:
		return "model-unavailable"
	}
	lower := strings.ToLower(string(attempt.body))
	if attempt.status == http.StatusBadRequest || attempt.status == http.StatusUnprocessableEntity {
		if strings.Contains(lower, "unsupported model") || strings.Contains(lower, "model group") || strings.Contains(lower, "model not found") {
			return "model-unavailable"
		}
	}
	if attempt.status >= 500 {
		return "gateway"
	}
	return "protocol"
}

func modelHealthMessage(category string) string {
	switch category {
	case "ok":
		return "The model gateway completed a minimal response successfully."
	case "configuration":
		return "The board model gateway configuration is incomplete or invalid."
	case "authentication":
		return "The model gateway rejected the board credential."
	case "rate-limited":
		return "The model gateway is currently rate limited."
	case "model-unavailable":
		return "The configured gateway does not currently route this model."
	case "timeout":
		return "The model gateway did not complete the health check in time."
	case "network":
		return "The board could not reach the model gateway."
	case "gateway":
		return "The model gateway is temporarily unavailable."
	default:
		return "The model gateway returned an invalid or incomplete response."
	}
}
