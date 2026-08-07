package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
	"github.com/Kyrie-w8/aster-edge/internal/policy"
	"github.com/Kyrie-w8/aster-edge/internal/session"
	"github.com/Kyrie-w8/aster-edge/internal/tools"
)

type sequenceProvider struct{ calls int }

func (p *sequenceProvider) Complete(_ context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	p.calls++
	if p.calls == 1 {
		return core.ProviderResponse{ToolCalls: []core.ToolCall{{Name: "echo", Arguments: map[string]any{"text": "ok"}}}}, nil
	}
	if len(req.Messages) < 3 {
		return core.ProviderResponse{}, nil
	}
	return core.ProviderResponse{Content: "done", Usage: map[string]any{"tokens": 3}}, nil
}

func TestAgentToolLoopPersistsCanonicalMessages(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.MaxSteps = 3
	store, _ := session.New(t.TempDir())
	registry := tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"echo"}}), nil, 1024)
	err := registry.Add(core.Tool{Definition: core.ToolDefinition{Name: "echo", Risk: "read", Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false}}, Handler: func(_ context.Context, args map[string]any) (any, error) { return args["text"], nil }})
	if err != nil {
		t.Fatal(err)
	}
	provider := &sequenceProvider{}
	engine := Engine{Config: cfg, Provider: provider, Tools: registry, Store: store, SystemPrompt: "test"}
	result, err := engine.Run(context.Background(), "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || result.Steps != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	messages, err := store.Messages(result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[2].Role != "tool" {
		t.Fatalf("unexpected trajectory: %+v", messages)
	}
	if messages[1].ToolCalls[0].ID == "" || messages[1].ToolCalls[0].ID != messages[2].ToolCallID {
		t.Fatalf("generated call id was not preserved: %+v", messages)
	}
}

func TestAgentEmitsOrderedLifecycleEvents(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.MaxSteps = 1
	store, _ := session.New(t.TempDir())
	registry := tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"*"}}), nil, 1024)
	engine := Engine{Config: cfg, Provider: &staticProvider{response: core.ProviderResponse{Content: "hello"}}, Tools: registry, Store: store}
	var events []core.AgentEvent
	result, err := engine.RunWithEvents(context.Background(), "", "hi", func(event core.AgentEvent) {
		events = append(events, event)
	})
	if err != nil || result.Content != "hello" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := []core.AgentEventType{core.EventTurnStarted, core.EventProviderStarted, core.EventTextDelta, core.EventTurnCompleted}
	if len(events) != len(want) {
		t.Fatalf("events=%+v", events)
	}
	for i, event := range events {
		if event.Type != want[i] || event.Sequence != uint64(i+1) || event.SessionID != result.SessionID || event.TurnID == "" {
			t.Fatalf("event[%d]=%+v", i, event)
		}
	}
}

type staticProvider struct {
	response core.ProviderResponse
}

func (p *staticProvider) Complete(context.Context, core.ProviderRequest) (core.ProviderResponse, error) {
	return p.response, nil
}

func TestAgentRejectsSilentEmptyResponse(t *testing.T) {
	cfg := config.Defaults()
	store, _ := session.New(t.TempDir())
	engine := Engine{
		Config: cfg, Provider: &staticProvider{}, Store: store,
		Tools: tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"*"}}), nil, 1024),
	}
	_, err := engine.Run(context.Background(), "", "hi")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("err=%v", err)
	}
}
