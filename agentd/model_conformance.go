package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	modelConformanceCacheTTL        = time.Hour
	modelConformanceRequestTimeout  = 45 * time.Second
	modelConformanceSchema          = 1
	modelConformanceScope           = "gateway-protocol"
	modelConformanceRuntimeStatus   = "not-tested"
	modelConformanceRDKTaskStatus   = "not-tested"
	modelConformanceMaximumAttempts = 6
	conformanceToolName             = "hobot_conformance"
	conformanceToolCallValue        = "ping"
	conformanceToolPrompt           = "Call hobot_conformance exactly once with value ping. Do not answer in text."
)

type modelConformanceParams struct {
	Model string `json:"model,omitempty"`
	Force bool   `json:"force,omitempty"`
}

type modelConformanceCheck struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latencyMs,omitempty"`
}

type modelConformanceResult struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Scope         string                  `json:"scope"`
	RuntimeStatus string                  `json:"agentRuntimeStatus"`
	RDKTaskStatus string                  `json:"rdkTaskStatus"`
	Provider      string                  `json:"provider"`
	Model         string                  `json:"model"`
	Status        string                  `json:"status"`
	Message       string                  `json:"message"`
	CheckedAt     time.Time               `json:"checkedAt"`
	ExpiresAt     time.Time               `json:"expiresAt"`
	Duration      int64                   `json:"durationMs,omitempty"`
	Attempts      int                     `json:"attempts"`
	Cached        bool                    `json:"cached"`
	Checks        []modelConformanceCheck `json:"checks"`
}

type modelConformanceCacheEntry struct {
	result modelConformanceResult
}

type modelConformanceService struct {
	mu       sync.Mutex
	cache    map[string]modelConformanceCacheEntry
	inflight map[string]chan struct{}
	now      func() time.Time
	probe    func(context.Context, modelOption) modelConformanceResult
	token    string
}

func newModelConformanceService(token ...string) *modelConformanceService {
	service := &modelConformanceService{
		cache: make(map[string]modelConformanceCacheEntry), inflight: make(map[string]chan struct{}), now: time.Now,
	}
	if len(token) > 0 {
		service.token = token[0]
	}
	service.probe = func(ctx context.Context, model modelOption) modelConformanceResult {
		return probeDroboticsModelConformanceWithToken(ctx, model, service.token)
	}
	return service
}

func (service *modelConformanceService) check(manager *taskManager, params modelConformanceParams) (modelConformanceResult, error) {
	models, err := manager.availableModels()
	if err != nil {
		return modelConformanceResult{}, fmt.Errorf("discover models: %w", err)
	}
	selection := normalizeModelSelection(params.Model)
	if strings.TrimSpace(params.Model) != "" && selection == "" {
		return modelConformanceResult{}, fmt.Errorf("model must use provider/model format")
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
		return modelConformanceResult{}, fmt.Errorf("model is not available: %s", selection)
	}
	if model.Provider != "drobotics" {
		return modelConformanceResult{}, fmt.Errorf("model verification currently supports D-Robotics models only")
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

		ctx, cancel := context.WithTimeout(context.Background(), modelConformanceRequestTimeout)
		result := normalizeModelConformanceResult(service.probe(ctx, model))
		cancel()
		result.Provider = model.Provider
		result.Model = model.ID
		result.CheckedAt = service.now().UTC()
		result.ExpiresAt = result.CheckedAt.Add(modelConformanceCacheTTL)
		result.Cached = false

		service.mu.Lock()
		service.cache[selection] = modelConformanceCacheEntry{result: result}
		delete(service.inflight, selection)
		close(pending)
		service.mu.Unlock()
		return result, nil
	}
}

func normalizeModelConformanceResult(result modelConformanceResult) modelConformanceResult {
	result.SchemaVersion = modelConformanceSchema
	result.Scope = modelConformanceScope
	result.RuntimeStatus = modelConformanceRuntimeStatus
	result.RDKTaskStatus = modelConformanceRDKTaskStatus
	if result.Checks == nil {
		result.Checks = []modelConformanceCheck{}
	}
	expected := map[string]bool{
		"streaming": false, "tool-call": false, "tool-result": false, "image-input": false,
	}
	allRequiredPassed := len(result.Checks) == len(expected)
	for index := range result.Checks {
		check := &result.Checks[index]
		if _, known := expected[check.Name]; !known || expected[check.Name] {
			check.Status = "failed"
			check.Category = "check-failed"
			check.Message = conformanceCheckFailureMessage(check.Name, check.Category)
			allRequiredPassed = false
			continue
		}
		expected[check.Name] = true
		switch check.Status {
		case "passed":
			check.Category = "ok"
			check.Message = conformanceCheckMessage(check.Name, true)
		case "degraded":
			check.Category = "buffered-fallback"
			check.Message = conformanceCheckDegradedMessage(check.Name)
			if check.Name != "streaming" {
				allRequiredPassed = false
			}
		case "skipped":
			check.Category = "not-declared"
			check.Message = conformanceCheckMessage(check.Name, false)
			if check.Name != "image-input" {
				allRequiredPassed = false
			}
		case "blocked":
			check.Category = "dependency-failed"
			check.Message = "This check could not run because a required earlier protocol step failed."
			allRequiredPassed = false
		default:
			check.Status = "failed"
			check.Category = normalizeConformanceFailureCategory(check.Category)
			check.Message = conformanceCheckFailureMessage(check.Name, check.Category)
			allRequiredPassed = false
		}
	}
	for _, seen := range expected {
		if !seen {
			allRequiredPassed = false
		}
	}
	fullyVerified := allRequiredPassed
	for _, check := range result.Checks {
		if check.Status == "degraded" {
			fullyVerified = false
		}
	}
	if fullyVerified {
		result.Status = "verified"
		result.Message = "The gateway protocol probe passed streaming, tool calls, tool-result continuation, and declared input modes. Agent runtime behavior and RDK task quality were not tested."
	} else if allRequiredPassed {
		result.Status = "compatible"
		result.Message = "The gateway protocol probe works through the bounded buffered fallback, but tool streaming is incomplete. Agent runtime behavior and RDK task quality were not tested."
	} else {
		result.Status = "failed"
		result.Message = "The model did not pass the complete gateway protocol probe. Agent runtime behavior and RDK task quality were not tested."
	}
	return result
}

func conformanceCheckDegradedMessage(name string) string {
	if name == "streaming" {
		return "The tool stream ended without an explicit terminal event; the bounded buffered fallback completed successfully."
	}
	return "This protocol check works only through a supported fallback."
}

func conformanceCheckMessage(name string, passed bool) string {
	if !passed {
		if name == "image-input" {
			return "The model does not advertise image input, so this check was not required."
		}
		return "This optional check was not required."
	}
	switch name {
	case "streaming":
		return "The gateway completed a bounded stream with an explicit terminal event."
	case "tool-call":
		return "The model emitted the requested tool call with valid structured arguments."
	case "tool-result":
		return "The model accepted the matching tool result and entered a valid next assistant turn."
	case "image-input":
		return "The gateway accepted a valid image input and completed the response."
	default:
		return "The protocol check passed."
	}
}

func normalizeConformanceFailureCategory(category string) string {
	switch category {
	case "request-failed", "incomplete-response", "invalid-tool-call", "repeated-tool", "token-limit", "unexpected-stop", "empty-output":
		return category
	default:
		return "check-failed"
	}
}

func conformanceCheckFailureMessage(name, category string) string {
	switch category {
	case "request-failed":
		return "The gateway rejected the request or returned an invalid response."
	case "incomplete-response":
		return "The response ended without a complete protocol terminal event."
	case "invalid-tool-call":
		return "The model did not return the requested tool with matching structured arguments."
	case "repeated-tool":
		return "The model requested another tool instead of completing after the tool result."
	case "token-limit":
		return "The response reached its token limit before the protocol step completed."
	case "unexpected-stop":
		return "The response used an unexpected terminal reason for this protocol step."
	case "empty-output":
		return "The request completed without usable output."
	}
	switch name {
	case "streaming":
		return "The stream was rejected, malformed, or ended without an explicit terminal event."
	case "tool-call":
		return "The model did not return the required structured tool call."
	case "tool-result":
		return "The gateway rejected the matching tool result or the model did not complete the continuation."
	case "image-input":
		return "The model advertises image input, but the gateway did not complete a valid image request."
	default:
		return "The protocol check failed."
	}
}

type conformanceResponse struct {
	text        string
	toolID      string
	toolName    string
	toolInput   map[string]any
	stopReason  string
	terminal    bool
	contentType string
}

func probeDroboticsModelConformance(ctx context.Context, model modelOption) modelConformanceResult {
	return probeDroboticsModelConformanceWithToken(ctx, model, strings.TrimSpace(os.Getenv(gatewayTokenEnvironment)))
}

func probeDroboticsModelConformanceWithToken(ctx context.Context, model modelOption, token string) modelConformanceResult {
	started := time.Now()
	result := modelConformanceResult{Checks: make([]modelConformanceCheck, 0, 4)}
	token = strings.TrimSpace(token)
	baseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultDroboticsBaseURL
	}
	openAICompatible := isOpenAICompatibleModel(model.ID)
	endpoint, err := modelHealthEndpoint(baseURL, openAICompatible)
	if token == "" || err != nil {
		result.Checks = append(result.Checks,
			conformanceCheck("streaming", false, 0),
			conformanceCheck("tool-call", false, 0),
			conformanceCheck("tool-result", false, 0),
		)
		appendImageConformanceCheck(&result, model, false, 0)
		return result
	}
	client := &http.Client{
		Timeout:       modelConformanceRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	firstPayload := anthropicToolRequest(model.ID)
	if openAICompatible {
		firstPayload = openAIToolRequest(model.ID)
	}
	firstStarted := time.Now()
	first, firstTransport, firstAttempts, parseErr := performConformanceExchange(ctx, client, endpoint, token, firstPayload, openAICompatible)
	result.Attempts += firstAttempts
	firstLatency := durationMilliseconds(time.Since(firstStarted))
	streamStatus := "failed"
	streamCategory := "request-failed"
	if parseErr == nil && first.terminal {
		if firstTransport == "sse" {
			streamStatus = "passed"
			streamCategory = "ok"
		} else if firstTransport == "json" {
			streamStatus = "degraded"
			streamCategory = "buffered-fallback"
		}
	} else if parseErr == nil {
		streamCategory = "incomplete-response"
	}
	result.Checks = append(result.Checks, modelConformanceCheck{Name: "streaming", Status: streamStatus, Category: streamCategory, LatencyMS: firstLatency})
	toolPassed := parseErr == nil && first.terminal && first.toolName == conformanceToolName && first.toolID != "" && first.toolInput["value"] == conformanceToolCallValue
	toolCheck := conformanceCheck("tool-call", toolPassed, firstLatency)
	if !toolPassed {
		toolCheck.Category = "invalid-tool-call"
		if parseErr != nil {
			toolCheck.Category = "request-failed"
		}
	}
	result.Checks = append(result.Checks, toolCheck)
	if toolPassed {
		secondPayload := anthropicToolResultRequest(model.ID, first)
		if openAICompatible {
			secondPayload = openAIToolResultRequest(model.ID, first)
		}
		secondStarted := time.Now()
		second, secondTransport, secondAttempts, secondErr := performConformanceExchange(ctx, client, endpoint, token, secondPayload, openAICompatible)
		result.Attempts += secondAttempts
		if secondTransport == "json" && result.Checks[0].Status == "passed" {
			result.Checks[0].Status = "degraded"
		}
		secondCategory := conformanceContinuationCategory(second, secondErr)
		secondCheck := conformanceCheck("tool-result", secondCategory == "ok", durationMilliseconds(time.Since(secondStarted)))
		secondCheck.Category = secondCategory
		result.Checks = append(result.Checks, secondCheck)
	} else {
		result.Checks = append(result.Checks, modelConformanceCheck{Name: "tool-result", Status: "blocked"})
	}

	if model.Capabilities.ImageInput {
		imageStarted := time.Now()
		imageResponse, imageTransport, imageAttempts, imageErr := performConformanceExchange(ctx, client, endpoint, token, anthropicImageRequest(model.ID), false)
		result.Attempts += imageAttempts
		if imageTransport == "json" && result.Checks[0].Status == "passed" {
			result.Checks[0].Status = "degraded"
		}
		imagePassed := imageErr == nil && imageResponse.terminal && strings.TrimSpace(imageResponse.text) != ""
		imageCheck := conformanceCheck("image-input", imagePassed, durationMilliseconds(time.Since(imageStarted)))
		if !imagePassed {
			imageCheck.Category = "empty-output"
			if imageErr != nil {
				imageCheck.Category = "request-failed"
			} else if !imageResponse.terminal {
				imageCheck.Category = "incomplete-response"
			}
		}
		result.Checks = append(result.Checks, imageCheck)
	} else {
		appendImageConformanceCheck(&result, model, false, 0)
	}
	result.Duration = durationMilliseconds(time.Since(started))
	return result
}

func conformanceContinuationCategory(response conformanceResponse, err error) string {
	if err != nil {
		return "request-failed"
	}
	if !response.terminal {
		return "incomplete-response"
	}
	switch response.stopReason {
	case "tool_use", "tool_calls":
		if response.toolID != "" && response.toolName != "" {
			return "ok"
		}
		return "unexpected-stop"
	case "end_turn", "stop", "stop_sequence":
		return "ok"
	case "max_tokens", "length":
		return "token-limit"
	default:
		return "unexpected-stop"
	}
}

func isOpenAICompatibleModel(modelID string) bool {
	switch modelID {
	case "deepseek/deepseek-v4-flash", "deepseek-v4-flash", "deepseek-v4-pro":
		return true
	default:
		return false
	}
}

func conformanceCheck(name string, passed bool, latency int64) modelConformanceCheck {
	status := "failed"
	if passed {
		status = "passed"
	}
	return modelConformanceCheck{Name: name, Status: status, LatencyMS: latency}
}

func appendImageConformanceCheck(result *modelConformanceResult, model modelOption, passed bool, latency int64) {
	if !model.Capabilities.ImageInput {
		result.Checks = append(result.Checks, modelConformanceCheck{Name: "image-input", Status: "skipped"})
		return
	}
	result.Checks = append(result.Checks, conformanceCheck("image-input", passed, latency))
}

func anthropicToolDefinition() map[string]any {
	return map[string]any{
		"name": conformanceToolName, "description": "Return a deterministic protocol probe result.",
		"input_schema": map[string]any{
			"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required": []string{"value"},
		},
	}
}

func anthropicToolRequest(model string) map[string]any {
	return map[string]any{
		"model": model, "max_tokens": 256, "stream": true,
		"messages": []map[string]any{{"role": "user", "content": conformanceToolPrompt}},
		"tools":    []map[string]any{anthropicToolDefinition()},
	}
}

func anthropicToolResultRequest(model string, first conformanceResponse) map[string]any {
	return map[string]any{
		"model": model, "max_tokens": 256, "stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": conformanceToolPrompt},
			{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "id": first.toolID, "name": first.toolName, "input": first.toolInput}}},
			{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": first.toolID, "content": "probe-result"}}},
		},
		"tools": []map[string]any{anthropicToolDefinition()},
	}
}

func anthropicImageRequest(model string) map[string]any {
	return map[string]any{
		"model": model, "max_tokens": 256, "stream": true,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "Process this valid PNG image and reply exactly VISION_OK."},
				{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAJklEQVR42u3NMQ0AAAwDoPo33arYsQQMkB6LQCAQCAQCgUAg+BIMi1X0ptsIcT0AAAAASUVORK5CYII="}},
			},
		}},
	}
}

func openAIToolDefinition() map[string]any {
	anthropic := anthropicToolDefinition()
	return map[string]any{
		"type":     "function",
		"function": map[string]any{"name": anthropic["name"], "description": anthropic["description"], "parameters": anthropic["input_schema"]},
	}
}

func openAIToolRequest(model string) map[string]any {
	return map[string]any{
		"model": model, "max_tokens": 256, "stream": true,
		"messages":             []map[string]any{{"role": "user", "content": conformanceToolPrompt}},
		"tools":                []map[string]any{openAIToolDefinition()},
		"chat_template_kwargs": map[string]bool{"enable_thinking": false},
	}
}

func openAIToolResultRequest(model string, first conformanceResponse) map[string]any {
	arguments, _ := json.Marshal(first.toolInput)
	toolCall := map[string]any{
		"id": first.toolID, "type": "function",
		"function": map[string]any{"name": first.toolName, "arguments": string(arguments)},
	}
	return map[string]any{
		"model": model, "max_tokens": 256, "stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": conformanceToolPrompt},
			{"role": "assistant", "content": nil, "tool_calls": []map[string]any{toolCall}},
			{"role": "tool", "tool_call_id": first.toolID, "content": "probe-result"},
		},
		"tools":                []map[string]any{openAIToolDefinition()},
		"chat_template_kwargs": map[string]bool{"enable_thinking": false},
	}
}

func performConformanceExchange(ctx context.Context, client *http.Client, endpoint, token string, payload map[string]any, openAICompatible bool) (conformanceResponse, string, int, error) {
	streamed := performHealthAttempt(ctx, client, endpoint, token, payload, time.Now())
	result, err := parseConformanceAttempt(streamed, openAICompatible)
	if err == nil && result.terminal {
		if strings.Contains(streamed.contentType, "text/event-stream") {
			return result, "sse", 1, nil
		}
		return result, "json", 1, nil
	}
	if err == nil && (strings.TrimSpace(result.text) != "" || result.toolID != "" || result.toolName != "") {
		return result, "", 1, nil
	}
	if err != nil && streamed.status >= 200 && streamed.status < 300 {
		return conformanceResponse{}, "", 1, err
	}
	if !shouldRetryBuffered(streamed) {
		return conformanceResponse{}, "", 1, err
	}
	bufferedPayload := make(map[string]any, len(payload))
	for key, value := range payload {
		bufferedPayload[key] = value
	}
	bufferedPayload["stream"] = false
	buffered := performHealthAttempt(ctx, client, endpoint, token, bufferedPayload, time.Now())
	result, err = parseConformanceAttempt(buffered, openAICompatible)
	if err != nil || !result.terminal {
		return conformanceResponse{}, "", 2, err
	}
	return result, "json", 2, nil
}

func parseConformanceAttempt(attempt healthAttempt, openAICompatible bool) (conformanceResponse, error) {
	if attempt.err != nil || attempt.status < 200 || attempt.status >= 300 {
		return conformanceResponse{}, fmt.Errorf("model conformance request failed")
	}
	if !strings.Contains(attempt.contentType, "text/event-stream") {
		if openAICompatible {
			return parseOpenAIConformanceJSON(attempt.body, attempt.contentType)
		}
		return parseAnthropicConformanceJSON(attempt.body, attempt.contentType)
	}
	if openAICompatible {
		return parseOpenAIConformanceSSE(attempt.body, attempt.contentType)
	}
	return parseAnthropicConformanceSSE(attempt.body, attempt.contentType)
}

func parseAnthropicConformanceJSON(body []byte, contentType string) (conformanceResponse, error) {
	var response struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if json.Unmarshal(body, &response) != nil || response.StopReason == "" {
		return conformanceResponse{}, fmt.Errorf("invalid buffered Anthropic response")
	}
	result := conformanceResponse{contentType: contentType, stopReason: response.StopReason, terminal: true}
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			result.text += block.Text
		case "tool_use":
			if result.toolID != "" {
				return conformanceResponse{}, fmt.Errorf("multiple tool calls")
			}
			result.toolID, result.toolName, result.toolInput = block.ID, block.Name, block.Input
		}
	}
	return result, nil
}

func parseOpenAIConformanceJSON(body []byte, contentType string) (conformanceResponse, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Choices) != 1 || response.Choices[0].FinishReason == "" || len(response.Choices[0].Message.ToolCalls) > 1 {
		return conformanceResponse{}, fmt.Errorf("invalid buffered OpenAI response")
	}
	choice := response.Choices[0]
	result := conformanceResponse{contentType: contentType, text: choice.Message.Content, stopReason: choice.FinishReason, terminal: true}
	if len(choice.Message.ToolCalls) == 1 {
		tool := choice.Message.ToolCalls[0]
		result.toolID, result.toolName = tool.ID, tool.Function.Name
		if json.Unmarshal([]byte(tool.Function.Arguments), &result.toolInput) != nil {
			return conformanceResponse{}, fmt.Errorf("invalid buffered tool arguments")
		}
	}
	return result, nil
}

func parseAnthropicConformanceSSE(body []byte, contentType string) (conformanceResponse, error) {
	events, _, err := conformanceSSEEvents(body)
	if err != nil {
		return conformanceResponse{}, err
	}
	result := conformanceResponse{contentType: contentType}
	toolInputs := make(map[int]*strings.Builder)
	toolIndexes := make(map[int]struct{ id, name string })
	for _, event := range events {
		typeName, _ := event["type"].(string)
		switch typeName {
		case "content_block_start":
			index, ok := integerJSONField(event["index"])
			block, blockOK := event["content_block"].(map[string]any)
			if !ok || !blockOK {
				return conformanceResponse{}, fmt.Errorf("invalid content block")
			}
			switch block["type"] {
			case "tool_use":
				id, idOK := block["id"].(string)
				name, nameOK := block["name"].(string)
				if !idOK || !nameOK || id == "" || name == "" {
					return conformanceResponse{}, fmt.Errorf("invalid tool call")
				}
				toolIndexes[index] = struct{ id, name string }{id: id, name: name}
				toolInputs[index] = &strings.Builder{}
				if input, ok := block["input"].(map[string]any); ok && len(input) > 0 {
					encoded, _ := json.Marshal(input)
					toolInputs[index].Write(encoded)
				}
			case "text":
				if text, ok := block["text"].(string); ok {
					result.text += text
				}
			}
		case "content_block_delta":
			index, ok := integerJSONField(event["index"])
			delta, deltaOK := event["delta"].(map[string]any)
			if !ok || !deltaOK {
				return conformanceResponse{}, fmt.Errorf("invalid content delta")
			}
			if delta["type"] == "text_delta" {
				text, _ := delta["text"].(string)
				result.text += text
			}
			if delta["type"] == "input_json_delta" {
				partial, partialOK := delta["partial_json"].(string)
				builder := toolInputs[index]
				if !partialOK || builder == nil {
					return conformanceResponse{}, fmt.Errorf("orphan tool arguments")
				}
				builder.WriteString(partial)
			}
		case "message_delta":
			delta, _ := event["delta"].(map[string]any)
			result.stopReason, _ = delta["stop_reason"].(string)
		case "message_stop":
			result.terminal = true
		case "error":
			return conformanceResponse{}, fmt.Errorf("gateway stream error")
		}
	}
	if len(toolIndexes) > 1 {
		return conformanceResponse{}, fmt.Errorf("multiple tool calls")
	}
	for index, tool := range toolIndexes {
		result.toolID, result.toolName = tool.id, tool.name
		arguments := strings.TrimSpace(toolInputs[index].String())
		if arguments == "" {
			arguments = "{}"
		}
		if json.Unmarshal([]byte(arguments), &result.toolInput) != nil {
			return conformanceResponse{}, fmt.Errorf("invalid tool arguments")
		}
	}
	return result, nil
}

func parseOpenAIConformanceSSE(body []byte, contentType string) (conformanceResponse, error) {
	events, done, err := conformanceSSEEvents(body)
	if err != nil {
		return conformanceResponse{}, err
	}
	result := conformanceResponse{contentType: contentType, terminal: done}
	type partialTool struct{ id, name, arguments string }
	tools := make(map[int]partialTool)
	for _, event := range events {
		choices, ok := event["choices"].([]any)
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]any)
		if !ok {
			return conformanceResponse{}, fmt.Errorf("invalid choice")
		}
		if finish, ok := choice["finish_reason"].(string); ok && finish != "" {
			result.stopReason = finish
		}
		delta, _ := choice["delta"].(map[string]any)
		if content, ok := delta["content"].(string); ok {
			result.text += content
		}
		toolCalls, _ := delta["tool_calls"].([]any)
		for _, value := range toolCalls {
			call, ok := value.(map[string]any)
			if !ok {
				return conformanceResponse{}, fmt.Errorf("invalid tool call")
			}
			index, ok := integerJSONField(call["index"])
			if !ok {
				return conformanceResponse{}, fmt.Errorf("invalid tool call index")
			}
			partial := tools[index]
			if id, ok := call["id"].(string); ok {
				partial.id += id
			}
			if function, ok := call["function"].(map[string]any); ok {
				if name, ok := function["name"].(string); ok {
					partial.name += name
				}
				if arguments, ok := function["arguments"].(string); ok {
					partial.arguments += arguments
				}
			}
			tools[index] = partial
		}
	}
	if len(tools) > 1 {
		return conformanceResponse{}, fmt.Errorf("multiple tool calls")
	}
	for _, tool := range tools {
		result.toolID, result.toolName = tool.id, tool.name
		arguments := strings.TrimSpace(tool.arguments)
		if arguments == "" {
			arguments = "{}"
		}
		if json.Unmarshal([]byte(arguments), &result.toolInput) != nil {
			return conformanceResponse{}, fmt.Errorf("invalid tool arguments")
		}
	}
	return result, nil
}

func conformanceSSEEvents(body []byte) ([]map[string]any, bool, error) {
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	events := make([]map[string]any, 0)
	done := false
	for _, block := range strings.Split(normalized, "\n\n") {
		data := make([]string, 0, 1)
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if len(data) == 0 {
			continue
		}
		payload := strings.Join(data, "\n")
		if payload == "[DONE]" {
			done = true
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(payload), &event) != nil {
			return nil, false, fmt.Errorf("invalid SSE JSON")
		}
		events = append(events, event)
	}
	return events, done, nil
}

func integerJSONField(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number != float64(int(number)) {
		return 0, false
	}
	return int(number), true
}
