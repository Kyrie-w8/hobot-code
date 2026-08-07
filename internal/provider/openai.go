package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func (p *HTTPProvider) openAICompatible(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	messages := []map[string]any{{"role": "system", "content": req.SystemPrompt}}
	for _, message := range req.Messages {
		item := map[string]any{"role": message.Role, "content": message.Content}
		if message.Name != "" {
			item["name"] = message.Name
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				args, _ := json.Marshal(call.Arguments)
				calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(args)}})
			}
			item["tool_calls"] = calls
		}
		messages = append(messages, item)
	}
	payload := map[string]any{"model": req.Model, "messages": messages}
	if len(req.Tools) > 0 {
		payload["tools"] = schemas(req.Tools, "chat")
	}
	copySettings(payload, req.Settings, "temperature", "top_p", "max_tokens", "seed", "tool_choice", "parallel_tool_calls")
	var raw struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments any    `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	if err := p.post(ctx, "/chat/completions", headers, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	if len(raw.Choices) == 0 {
		return core.ProviderResponse{}, fmt.Errorf("provider returned no choices")
	}
	choice := raw.Choices[0]
	result := core.ProviderResponse{Content: choice.Message.Content, FinishReason: choice.FinishReason, Usage: raw.Usage}
	for _, item := range choice.Message.ToolCalls {
		args, err := parseArguments(item.Function.Arguments)
		if err != nil {
			return core.ProviderResponse{}, fmt.Errorf("parse tool %s arguments: %w", item.Function.Name, err)
		}
		result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: item.ID, Name: item.Function.Name, Arguments: args})
	}
	return result, nil
}

func (p *HTTPProvider) openAIResponses(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	input := make([]map[string]any, 0, len(req.Messages))
	for _, message := range req.Messages {
		if (message.Role == "user" || message.Role == "assistant") && message.Content != "" {
			input = append(input, map[string]any{"role": message.Role, "content": message.Content})
		}
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				args, _ := json.Marshal(call.Arguments)
				input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(args)})
			}
		}
		if message.Role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content})
		}
	}
	payload := map[string]any{"model": req.Model, "instructions": req.SystemPrompt, "input": input, "store": false}
	if len(req.Tools) > 0 {
		payload["tools"] = schemas(req.Tools, "responses")
	}
	copySettings(payload, req.Settings, "temperature", "top_p", "max_output_tokens", "tool_choice", "parallel_tool_calls", "reasoning", "text", "store")
	var raw struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments any    `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage map[string]any `json:"usage"`
	}
	if err := p.post(ctx, "/responses", map[string]string{"Authorization": "Bearer " + p.apiKey}, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	result := core.ProviderResponse{FinishReason: raw.Status, Usage: raw.Usage, Content: raw.OutputText}
	if result.FinishReason == "" {
		result.FinishReason = "completed"
	}
	var text string
	for _, item := range raw.Output {
		switch item.Type {
		case "function_call":
			args, err := parseArguments(item.Arguments)
			if err != nil {
				return core.ProviderResponse{}, err
			}
			id := item.CallID
			if id == "" {
				id = item.ID
			}
			result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: id, Name: item.Name, Arguments: args})
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					text += part.Text
				}
			}
		}
	}
	if text != "" {
		result.Content = text
	}
	return result, nil
}
