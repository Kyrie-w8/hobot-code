package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type geminiRaw struct {
	Candidates []struct {
		FinishReason string `json:"finishReason"`
		Content      struct {
			Parts []struct {
				Text         string `json:"text"`
				Thought      bool   `json:"thought"`
				FunctionCall *struct {
					Name string `json:"name"`
					Args any    `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Usage map[string]any `json:"usageMetadata"`
}

func (p *HTTPProvider) gemini(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	payload := geminiRequest(req)
	endpoint := "/models/" + url.PathEscape(req.Model) + ":generateContent?key=" + url.QueryEscape(p.apiKey)
	var raw geminiRaw
	if err := p.post(ctx, endpoint, nil, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	return parseGemini(raw)
}

func geminiRequest(req core.ProviderRequest) map[string]any {
	var contents []map[string]any
	for _, message := range req.Messages {
		switch message.Role {
		case "user":
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": message.Content}}})
		case "assistant":
			var parts []map[string]any
			if message.Content != "" {
				parts = append(parts, map[string]any{"text": message.Content})
			}
			for _, call := range message.ToolCalls {
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": call.Arguments}})
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case "tool":
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{"functionResponse": map[string]any{"name": message.Name, "response": map[string]any{"result": message.Content}}}}})
		}
	}
	payload := map[string]any{"systemInstruction": map[string]any{"parts": []map[string]any{{"text": req.SystemPrompt}}}, "contents": contents}
	if len(req.Tools) > 0 {
		payload["tools"] = []map[string]any{{"functionDeclarations": schemas(req.Tools, "gemini")}}
	}
	if len(req.Settings) > 0 {
		payload["generationConfig"] = req.Settings
	}
	return payload
}

func parseGemini(raw geminiRaw) (core.ProviderResponse, error) {
	if len(raw.Candidates) == 0 {
		return core.ProviderResponse{}, fmt.Errorf("Gemini returned no candidates")
	}
	c := raw.Candidates[0]
	result := core.ProviderResponse{FinishReason: c.FinishReason, Usage: raw.Usage}
	for i, part := range c.Content.Parts {
		if part.Thought {
			result.Reasoning += part.Text
		} else {
			result.Content += part.Text
		}
		if part.FunctionCall != nil {
			args, err := parseArguments(part.FunctionCall.Args)
			if err != nil {
				return core.ProviderResponse{}, err
			}
			result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: fmt.Sprintf("gemini-%d", i), Name: part.FunctionCall.Name, Arguments: args})
		}
	}
	return result, nil
}

func (p *HTTPProvider) geminiStream(ctx context.Context, req core.ProviderRequest, emit func(core.ProviderChunk)) (core.ProviderResponse, error) {
	payload := geminiRequest(req)
	endpoint := "/models/" + url.PathEscape(req.Model) + ":streamGenerateContent?alt=sse&key=" + url.QueryEscape(p.apiKey)
	result := core.ProviderResponse{Usage: map[string]any{}}
	substantive := false
	err := p.stream(ctx, endpoint, nil, payload, func(data []byte) error {
		var chunk geminiRaw
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("decode Gemini stream event: %w", err)
		}
		if len(chunk.Candidates) == 0 {
			mergeUsage(result.Usage, chunk.Usage)
			return nil
		}
		candidate := chunk.Candidates[0]
		if candidate.FinishReason != "" {
			result.FinishReason = candidate.FinishReason
		}
		mergeUsage(result.Usage, chunk.Usage)
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				substantive = true
				if part.Thought {
					result.Reasoning += part.Text
					emit(core.ProviderChunk{Type: core.ProviderReasoningDelta, Delta: part.Text})
				} else {
					result.Content += part.Text
					emit(core.ProviderChunk{Type: core.ProviderTextDelta, Delta: part.Text})
				}
			}
			if part.FunctionCall != nil {
				substantive = true
				args, err := parseArguments(part.FunctionCall.Args)
				if err != nil {
					return err
				}
				result.ToolCalls = append(result.ToolCalls, core.ToolCall{ID: fmt.Sprintf("gemini-%d", len(result.ToolCalls)), Name: part.FunctionCall.Name, Arguments: args})
			}
		}
		return nil
	})
	if err != nil {
		return core.ProviderResponse{}, err
	}
	if !substantive {
		fallback, err := p.gemini(ctx, req)
		if err != nil {
			return core.ProviderResponse{}, fmt.Errorf("empty Gemini stream; fallback failed: %w", err)
		}
		emitProviderResponse(fallback, emit)
		return fallback, nil
	}
	return result, nil
}
