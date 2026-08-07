package provider

import (
	"context"
	"fmt"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func (p *HTTPProvider) anthropic(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
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
	copySettings(payload, req.Settings, "temperature", "top_p", "top_k", "tool_choice")
	var raw struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			ID    string `json:"id"`
			Name  string `json:"name"`
			Input any    `json:"input"`
		} `json:"content"`
		Usage map[string]any `json:"usage"`
	}
	headers := map[string]string{"x-api-key": p.apiKey, "anthropic-version": "2023-06-01"}
	if err := p.post(ctx, "/v1/messages", headers, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	result := core.ProviderResponse{FinishReason: raw.StopReason, Usage: raw.Usage}
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
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
