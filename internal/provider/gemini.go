package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func (p *HTTPProvider) gemini(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
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
	endpoint := "/models/" + url.PathEscape(req.Model) + ":generateContent?key=" + url.QueryEscape(p.apiKey)
	var raw struct {
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string `json:"name"`
						Args any    `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Usage map[string]any `json:"usageMetadata"`
	}
	if err := p.post(ctx, endpoint, nil, payload, &raw); err != nil {
		return core.ProviderResponse{}, err
	}
	if len(raw.Candidates) == 0 {
		return core.ProviderResponse{}, fmt.Errorf("Gemini returned no candidates")
	}
	c := raw.Candidates[0]
	result := core.ProviderResponse{FinishReason: c.FinishReason, Usage: raw.Usage}
	for i, part := range c.Content.Parts {
		result.Content += part.Text
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
