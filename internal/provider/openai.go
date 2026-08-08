package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type chatCompletionRaw struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
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

type responsesRaw struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
		Summary   []struct {
			Text string `json:"text"`
		} `json:"summary"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage map[string]any `json:"usage"`
}

func (p *HTTPProvider) openAICompatible(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	payload, headers := p.openAICompatibleRequest(req)
	var raw chatCompletionRaw
	if err := p.post(ctx, "/chat/completions", headers, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	return parseChatCompletion(raw)
}

func (p *HTTPProvider) openAICompatibleRequest(req core.ProviderRequest) (map[string]any, map[string]string) {
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
	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	return payload, headers
}

func parseChatCompletion(raw chatCompletionRaw) (core.ProviderResponse, error) {
	if len(raw.Choices) == 0 {
		return core.ProviderResponse{}, fmt.Errorf("provider returned no choices")
	}
	choice := raw.Choices[0]
	reasoning := choice.Message.ReasoningContent
	if reasoning == "" {
		reasoning = choice.Message.Reasoning
	}
	result := core.ProviderResponse{Content: choice.Message.Content, Reasoning: reasoning, FinishReason: choice.FinishReason, Usage: raw.Usage}
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
	payload := openAIResponsesRequest(req)
	var raw responsesRaw
	if err := p.post(ctx, "/responses", map[string]string{"Authorization": "Bearer " + p.apiKey}, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	return parseResponses(raw)
}

func openAIResponsesRequest(req core.ProviderRequest) map[string]any {
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
	return payload
}

func parseResponses(raw responsesRaw) (core.ProviderResponse, error) {
	result := core.ProviderResponse{FinishReason: raw.Status, Usage: raw.Usage, Content: raw.OutputText}
	if result.FinishReason == "" {
		result.FinishReason = "completed"
	}
	var text string
	for _, item := range raw.Output {
		switch item.Type {
		case "reasoning":
			for _, summary := range item.Summary {
				result.Reasoning += summary.Text
			}
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

type streamedToolCall struct {
	id, name string
	args     strings.Builder
}

func (p *HTTPProvider) openAICompatibleStream(ctx context.Context, req core.ProviderRequest, emit func(core.ProviderChunk)) (core.ProviderResponse, error) {
	payload, headers := p.openAICompatibleRequest(req)
	payload["stream"] = true
	result := core.ProviderResponse{Usage: map[string]any{}}
	tools := map[int]*streamedToolCall{}
	substantive := false
	err := p.stream(ctx, "/chat/completions", headers, payload, func(data []byte) error {
		var event struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					Reasoning        string `json:"reasoning"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("decode OpenAI-compatible stream event: %w", err)
		}
		if event.Error != nil {
			return fmt.Errorf("OpenAI-compatible stream: %s", event.Error.Message)
		}
		mergeUsage(result.Usage, event.Usage)
		for _, choice := range event.Choices {
			if choice.FinishReason != "" {
				result.FinishReason = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				substantive = true
				result.Content += choice.Delta.Content
				emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: choice.Delta.Content})
			}
			reasoning := choice.Delta.ReasoningContent
			if reasoning == "" {
				reasoning = choice.Delta.Reasoning
			}
			if reasoning != "" {
				substantive = true
				result.Reasoning += reasoning
				emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: reasoning})
			}
			for _, call := range choice.Delta.ToolCalls {
				substantive = true
				tool := tools[call.Index]
				if tool == nil {
					tool = &streamedToolCall{}
					tools[call.Index] = tool
				}
				if call.ID != "" {
					tool.id = call.ID
				}
				if call.Function.Name != "" {
					tool.name = call.Function.Name
				}
				tool.args.WriteString(call.Function.Arguments)
			}
		}
		return nil
	})
	if err != nil {
		return core.ProviderResponse{}, err
	}
	if !substantive {
		fallback, err := p.openAICompatible(ctx, req)
		if err != nil {
			return core.ProviderResponse{}, fmt.Errorf("empty OpenAI-compatible stream; fallback failed: %w", err)
		}
		emitProviderResponse(fallback, emit)
		return fallback, nil
	}
	indices := make([]int, 0, len(tools))
	for index := range tools {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		tool := tools[index]
		args, err := parseArguments(tool.args.String())
		if err != nil {
			return core.ProviderResponse{}, fmt.Errorf("parse streamed tool %s arguments: %w", tool.name, err)
		}
		result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: tool.id, Name: tool.name, Arguments: args})
	}
	return result, nil
}

func (p *HTTPProvider) openAIResponsesStream(ctx context.Context, req core.ProviderRequest, emit func(core.ProviderChunk)) (core.ProviderResponse, error) {
	payload := openAIResponsesRequest(req)
	payload["stream"] = true
	result := core.ProviderResponse{Usage: map[string]any{}}
	tools := map[string]*streamedToolCall{}
	substantive := false
	err := p.stream(ctx, "/responses", map[string]string{"Authorization": "Bearer " + p.apiKey}, payload, func(data []byte) error {
		var event struct {
			Type   string `json:"type"`
			Delta  string `json:"delta"`
			ItemID string `json:"item_id"`
			Name   string `json:"name"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Item struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
			Response responsesRaw `json:"response"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("decode Responses stream event: %w", err)
		}
		if event.Error != nil {
			return fmt.Errorf("Responses stream: %s", event.Error.Message)
		}
		switch event.Type {
		case "response.output_text.delta":
			substantive = true
			result.Content += event.Delta
			emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: event.Delta})
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			substantive = true
			result.Reasoning += event.Delta
			emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: event.Delta})
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				substantive = true
				id := event.Item.CallID
				if id == "" {
					id = event.Item.ID
				}
				tools[event.Item.ID] = &streamedToolCall{id: id, name: event.Item.Name}
				tools[event.Item.ID].args.WriteString(event.Item.Arguments)
			}
		case "response.function_call_arguments.delta":
			tool := tools[event.ItemID]
			if tool == nil {
				tool = &streamedToolCall{id: event.ItemID, name: event.Name}
				tools[event.ItemID] = tool
			}
			tool.args.WriteString(event.Delta)
			substantive = true
		case "response.completed", "response.incomplete":
			completed, err := parseResponses(event.Response)
			if err != nil {
				return err
			}
			result.FinishReason = completed.FinishReason
			result.Usage = completed.Usage
			if result.Content == "" && completed.Content != "" {
				result.Content = completed.Content
				emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: completed.Content})
				substantive = true
			}
			if result.Reasoning == "" && completed.Reasoning != "" {
				result.Reasoning = completed.Reasoning
				emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: completed.Reasoning})
				substantive = true
			}
			if len(completed.ToolCalls) > 0 {
				result.ToolCalls = completed.ToolCalls
				substantive = true
			}
		}
		return nil
	})
	if err != nil {
		return core.ProviderResponse{}, err
	}
	if !substantive {
		fallback, err := p.openAIResponses(ctx, req)
		if err != nil {
			return core.ProviderResponse{}, fmt.Errorf("empty Responses stream; fallback failed: %w", err)
		}
		emitProviderResponse(fallback, emit)
		return fallback, nil
	}
	if len(result.ToolCalls) == 0 {
		ids := make([]string, 0, len(tools))
		for id := range tools {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			tool := tools[id]
			args, err := parseArguments(tool.args.String())
			if err != nil {
				return core.ProviderResponse{}, err
			}
			result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: tool.id, Name: tool.name, Arguments: args})
		}
	}
	return result, nil
}

func emitProviderResponse(response core.ProviderResponse, emit func(core.ProviderChunk)) {
	if response.Reasoning != "" {
		emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: response.Reasoning})
	}
	if response.Content != "" {
		emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: response.Content})
	}
}
