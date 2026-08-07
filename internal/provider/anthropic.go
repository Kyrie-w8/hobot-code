package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func (p *HTTPProvider) anthropic(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	payload, headers := p.anthropicRequest(req)
	var raw struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
			ID       string `json:"id"`
			Name     string `json:"name"`
			Input    any    `json:"input"`
		} `json:"content"`
		Usage map[string]any `json:"usage"`
	}
	if err := p.post(ctx, "/v1/messages", headers, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	result := core.ProviderResponse{FinishReason: raw.StopReason, Usage: raw.Usage}
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "thinking", "reasoning":
			summary := block.Thinking
			if summary == "" {
				summary = block.Text
			}
			result.Reasoning += summary
		case "tool_use":
			args, err := parseArguments(block.Input)
			if err != nil {
				return core.ProviderResponse{}, fmt.Errorf("parse Anthropic tool input: %w", err)
			}
			result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: block.ID, Name: block.Name, Arguments: args})
		}
	}
	return result, nil
}

func (p *HTTPProvider) anthropicRequest(req core.ProviderRequest) (map[string]any, map[string]string) {
	var messages []map[string]any
	for _, message := range req.Messages {
		switch message.Role {
		case "user":
			messages = append(messages, map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": message.Content}}})
		case "assistant":
			var blocks []map[string]any
			if message.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": call.Arguments})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
		case "tool":
			block := map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content}
			if len(messages) > 0 && messages[len(messages)-1]["role"] == "user" {
				if blocks, ok := messages[len(messages)-1]["content"].([]map[string]any); ok && len(blocks) > 0 && blocks[0]["type"] == "tool_result" {
					messages[len(messages)-1]["content"] = append(blocks, block)
					continue
				}
			}
			messages = append(messages, map[string]any{"role": "user", "content": []map[string]any{block}})
		}
	}
	maxTokens := any(2048)
	if v, ok := req.Settings["max_tokens"]; ok {
		maxTokens = v
	}
	payload := map[string]any{"model": req.Model, "system": req.SystemPrompt, "messages": messages, "max_tokens": maxTokens}
	if len(req.Tools) > 0 {
		payload["tools"] = schemas(req.Tools, "anthropic")
	}
	copySettings(payload, req.Settings, "temperature", "top_p", "top_k", "tool_choice", "thinking")
	headers := map[string]string{"anthropic-version": "2023-06-01"}
	if p.authStyle == "bearer" {
		headers["Authorization"] = "Bearer " + p.apiKey
	} else {
		headers["x-api-key"] = p.apiKey
	}
	return payload, headers
}

type anthropicToolStream struct {
	id      string
	name    string
	partial strings.Builder
}

func (p *HTTPProvider) anthropicStream(ctx context.Context, req core.ProviderRequest, emit func(core.ProviderChunk)) (core.ProviderResponse, error) {
	payload, headers := p.anthropicRequest(req)
	payload["stream"] = true
	body, err := json.Marshal(payload)
	if err != nil {
		return core.ProviderResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return core.ProviderResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "aster-edge/0.2")
	for key, value := range p.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return core.ProviderResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		return core.ProviderResponse{}, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	result := core.ProviderResponse{Usage: map[string]any{}}
	toolsByIndex := map[int]*anthropicToolStream{}
	substantive := false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				Usage map[string]any `json:"usage"`
			} `json:"message"`
			ContentBlock struct {
				Type     string          `json:"type"`
				Text     string          `json:"text"`
				Thinking string          `json:"thinking"`
				ID       string          `json:"id"`
				Name     string          `json:"name"`
				Input    json.RawMessage `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage map[string]any `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return core.ProviderResponse{}, fmt.Errorf("decode Anthropic stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			mergeUsage(result.Usage, event.Message.Usage)
		case "content_block_start":
			switch event.ContentBlock.Type {
			case "text":
				result.Content += event.ContentBlock.Text
				if event.ContentBlock.Text != "" {
					substantive = true
					emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: event.ContentBlock.Text})
				}
			case "thinking", "reasoning":
				summary := event.ContentBlock.Thinking
				if summary == "" {
					summary = event.ContentBlock.Text
				}
				result.Reasoning += summary
				if summary != "" {
					substantive = true
					emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: summary})
				}
			case "tool_use":
				substantive = true
				toolsByIndex[event.Index] = &anthropicToolStream{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				result.Content += event.Delta.Text
				if event.Delta.Text != "" {
					substantive = true
					emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: event.Delta.Text})
				}
			case "thinking_delta", "reasoning_delta":
				summary := event.Delta.Thinking
				if summary == "" {
					summary = event.Delta.Text
				}
				result.Reasoning += summary
				if summary != "" {
					substantive = true
					emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: summary})
				}
			case "input_json_delta":
				if tool := toolsByIndex[event.Index]; tool != nil {
					tool.partial.WriteString(event.Delta.PartialJSON)
				}
			}
		case "message_delta":
			result.FinishReason = event.Delta.StopReason
			mergeUsage(result.Usage, event.Usage)
		case "error":
			return core.ProviderResponse{}, fmt.Errorf("Anthropic stream: %s", event.Error.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return core.ProviderResponse{}, err
	}
	if !substantive {
		fallback, err := p.anthropic(ctx, req)
		if err != nil {
			return core.ProviderResponse{}, fmt.Errorf("empty Anthropic stream; fallback failed: %w", err)
		}
		if fallback.Reasoning != "" {
			emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: fallback.Reasoning})
		}
		if fallback.Content != "" {
			emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: fallback.Content})
		}
		return fallback, nil
	}
	indices := make([]int, 0, len(toolsByIndex))
	for index := range toolsByIndex {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		tool := toolsByIndex[index]
		arguments := map[string]any{}
		if raw := strings.TrimSpace(tool.partial.String()); raw != "" {
			if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
				return core.ProviderResponse{}, fmt.Errorf("parse streamed Anthropic tool input: %w", err)
			}
		}
		result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: tool.id, Name: tool.name, Arguments: arguments})
	}
	return result, nil
}

func mergeUsage(dst, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}
