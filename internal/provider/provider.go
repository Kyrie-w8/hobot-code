package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type HTTPProvider struct {
	kind      string
	baseURL   string
	apiKey    string
	authStyle string
	headers   map[string]string
	client    *http.Client
}

func New(cfg config.ProviderConfig) (core.Provider, error) {
	kind := strings.ToLower(strings.ReplaceAll(cfg.Type, "_", "-"))
	if kind == "mock" {
		return Mock{}, nil
	}
	switch kind {
	case "openai-compatible", "openai-responses", "anthropic", "gemini":
	default:
		return nil, fmt.Errorf("unsupported provider type %q", cfg.Type)
	}
	if cfg.APIKey == "" && kind != "openai-compatible" {
		return nil, fmt.Errorf("provider API key is empty; set %s", cfg.APIKeyEnv)
	}
	return &HTTPProvider{
		kind: kind, baseURL: strings.TrimRight(cfg.BaseURL, "/"), apiKey: cfg.APIKey, authStyle: strings.ToLower(cfg.AuthStyle),
		headers: cfg.Headers, client: &http.Client{Timeout: time.Duration(cfg.TimeoutSec) * time.Second},
	}, nil
}

type Mock struct{}

func (Mock) Complete(_ context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = req.Messages[i].Content
			break
		}
	}
	return core.ProviderResponse{Content: "[offline mock] " + last, FinishReason: "stop"}, nil
}

func (p *HTTPProvider) Complete(ctx context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	switch p.kind {
	case "openai-compatible":
		return p.openAICompatible(ctx, req)
	case "openai-responses":
		return p.openAIResponses(ctx, req)
	case "anthropic":
		return p.anthropic(ctx, req)
	case "gemini":
		return p.gemini(ctx, req)
	default:
		return core.ProviderResponse{}, errors.New("unreachable provider type")
	}
}

func (p *HTTPProvider) CompleteStream(ctx context.Context, req core.ProviderRequest, emit func(core.ProviderChunk)) (core.ProviderResponse, error) {
	switch p.kind {
	case "anthropic":
		return p.anthropicStream(ctx, req, emit)
	case "openai-compatible":
		return p.openAICompatibleStream(ctx, req, emit)
	case "openai-responses":
		return p.openAIResponsesStream(ctx, req, emit)
	case "gemini":
		return p.geminiStream(ctx, req, emit)
	}
	return core.ProviderResponse{}, errors.New("unreachable provider type")
}

func (p *HTTPProvider) post(ctx context.Context, endpoint string, headers map[string]string, payload any, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "aster-edge/0.4")
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
}

func schemas(tools []core.ToolDefinition, style string) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		switch style {
		case "chat":
			result = append(result, map[string]any{"type": "function", "function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters,
			}})
		case "responses":
			result = append(result, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
		case "anthropic":
			result = append(result, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.Parameters})
		case "gemini":
			result = append(result, map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
		}
	}
	return result
}

func copySettings(dst map[string]any, settings map[string]any, allowed ...string) {
	for _, key := range allowed {
		if value, ok := settings[key]; ok {
			dst[key] = value
		}
	}
}

func parseArguments(value any) (map[string]any, error) {
	switch v := value.(type) {
	case map[string]any:
		return v, nil
	case string:
		var args map[string]any
		if v == "" {
			v = "{}"
		}
		if err := json.Unmarshal([]byte(v), &args); err != nil {
			return nil, err
		}
		return args, nil
	case nil:
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("tool arguments are not an object")
	}
}
