package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
	"github.com/Kyrie-w8/aster-edge/internal/session"
	"github.com/Kyrie-w8/aster-edge/internal/tools"
)

type Engine struct {
	Config       config.Config
	Provider     core.Provider
	Tools        *tools.Registry
	Store        *session.Store
	SystemPrompt string
}

func (e *Engine) Run(ctx context.Context, sessionID, input string) (core.AgentResult, error) {
	return e.RunWithEvents(ctx, sessionID, input, nil)
}

func (e *Engine) RunWithEvents(ctx context.Context, sessionID, input string, sink core.EventSink) (core.AgentResult, error) {
	if strings.TrimSpace(input) == "" {
		return core.AgentResult{}, fmt.Errorf("message is empty")
	}
	var messages []core.Message
	var err error
	if sessionID == "" {
		sessionID = session.NewID()
	} else {
		messages, err = e.Store.Messages(sessionID)
		if err != nil && !os.IsNotExist(err) {
			return core.AgentResult{}, err
		}
	}
	turnID := session.NewID()
	var sequence uint64
	emit := func(eventType core.AgentEventType, step int, delta string, call *core.ToolCall, execution *core.ToolExecution, data map[string]any) {
		sequence++
		event := core.AgentEvent{Type: eventType, SessionID: sessionID, TurnID: turnID, Sequence: sequence, Timestamp: time.Now().UTC(), Step: step, Delta: delta, ToolCall: call, Execution: execution, Data: data}
		if sink != nil {
			sink(event)
		}
		if eventType != core.EventTextDelta && eventType != core.EventReasoningDelta {
			_ = e.Store.AppendEvent(sessionID, string(eventType), eventRecord(event))
		}
	}
	emit(core.EventTurnStarted, 0, "", nil, nil, nil)
	user := core.Message{Role: "user", Content: input}
	messages = append(messages, user)
	if err := e.Store.AppendMessage(sessionID, user); err != nil {
		return core.AgentResult{}, err
	}
	usage := map[string]any{}
	for step := 1; step <= e.Config.Agent.MaxSteps; step++ {
		emit(core.EventProviderStarted, step, "", nil, nil, map[string]any{"model": e.Config.Provider.Model})
		systemPrompt, providerMessages := prepareProviderContext(e.SystemPrompt, messages)
		request := core.ProviderRequest{Model: e.Config.Provider.Model, SystemPrompt: systemPrompt, Messages: providerMessages, Tools: e.Tools.Definitions(), Settings: e.Config.Provider.Settings}
		var response core.ProviderResponse
		if streaming, ok := e.Provider.(core.StreamingProvider); ok {
			response, err = streaming.CompleteStream(ctx, request, func(chunk core.ProviderChunk) {
				switch chunk.Type {
				case core.ProviderTextDelta:
					emit(core.EventTextDelta, step, chunk.Delta, nil, nil, nil)
				case core.ProviderReasoningDelta:
					emit(core.EventReasoningDelta, step, chunk.Delta, nil, nil, nil)
				}
			})
		} else {
			response, err = e.Provider.Complete(ctx, request)
			if response.Reasoning != "" {
				emit(core.EventReasoningDelta, step, response.Reasoning, nil, nil, nil)
			}
			if response.Content != "" {
				emit(core.EventTextDelta, step, response.Content, nil, nil, nil)
			}
		}
		if err != nil {
			_ = e.Store.AppendEvent(sessionID, "provider_error", map[string]any{"error": err.Error(), "step": step})
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				emit(core.EventTurnCancelled, step, "", nil, nil, map[string]any{"error": err.Error()})
			} else {
				emit(core.EventTurnFailed, step, "", nil, nil, map[string]any{"error": err.Error()})
			}
			return core.AgentResult{}, err
		}
		if response.Content == "" && len(response.ToolCalls) == 0 {
			err = errors.New("provider returned an empty response")
			emit(core.EventTurnFailed, step, "", nil, nil, map[string]any{"error": err.Error()})
			return core.AgentResult{}, err
		}
		for i := range response.ToolCalls {
			if response.ToolCalls[i].ID == "" {
				response.ToolCalls[i].ID = fmt.Sprintf("call-%d-%d", step, i)
			}
		}
		assistant := core.Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls}
		messages = append(messages, assistant)
		if err := e.Store.AppendMessage(sessionID, assistant); err != nil {
			return core.AgentResult{}, err
		}
		for key, value := range response.Usage {
			usage[key] = value
		}
		if len(response.ToolCalls) == 0 {
			result := core.AgentResult{SessionID: sessionID, Content: response.Content, Steps: step, Usage: usage}
			emit(core.EventTurnCompleted, step, "", nil, nil, map[string]any{"usage": usage})
			return result, nil
		}
		for _, call := range response.ToolCalls {
			callCopy := call
			emit(core.EventToolRequested, step, "", &callCopy, nil, nil)
			emit(core.EventToolStarted, step, "", &callCopy, nil, nil)
			execution := e.Tools.Execute(ctx, call)
			executionCopy := execution
			emit(core.EventToolCompleted, step, "", &callCopy, &executionCopy, nil)
			toolMessage := core.Message{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: execution.JSON()}
			messages = append(messages, toolMessage)
			if err := e.Store.AppendMessage(sessionID, toolMessage); err != nil {
				return core.AgentResult{}, err
			}
			_ = e.Store.AppendEvent(sessionID, "tool_execution", map[string]any{"tool": call.Name, "ok": execution.OK, "duration_ms": execution.DurationMS})
		}
	}
	err = fmt.Errorf("agent exceeded max_steps=%d", e.Config.Agent.MaxSteps)
	emit(core.EventTurnFailed, e.Config.Agent.MaxSteps, "", nil, nil, map[string]any{"error": err.Error()})
	return core.AgentResult{}, err
}

func (e *Engine) Compact(ctx context.Context, sessionID string, onChunk func(core.ProviderChunk)) (string, error) {
	if sessionID == "" {
		return "", errors.New("no active session")
	}
	messages, err := e.Store.Messages(sessionID)
	if err != nil {
		return "", err
	}
	if len(messages) == 0 {
		return "", errors.New("session has no context to compact")
	}
	systemPrompt, providerMessages := prepareProviderContext(e.SystemPrompt, messages)
	providerMessages = append(providerMessages, core.Message{Role: "user", Content: "Create a compact context summary for continuing this session. Preserve user requirements, decisions, file paths, commands, tool results, unresolved problems, and next steps. Remove repetition. Return only the summary."})
	request := core.ProviderRequest{Model: e.Config.Provider.Model, SystemPrompt: systemPrompt, Messages: providerMessages, Settings: e.Config.Provider.Settings}
	var response core.ProviderResponse
	if streaming, ok := e.Provider.(core.StreamingProvider); ok {
		response, err = streaming.CompleteStream(ctx, request, func(chunk core.ProviderChunk) {
			if onChunk != nil {
				onChunk(chunk)
			}
		})
	} else {
		response, err = e.Provider.Complete(ctx, request)
	}
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(response.Content)
	if summary == "" {
		return "", errors.New("provider returned an empty compact summary")
	}
	if err := e.Store.Compact(sessionID, summary); err != nil {
		return "", err
	}
	return summary, nil
}

func prepareProviderContext(systemPrompt string, messages []core.Message) (string, []core.Message) {
	filtered := make([]core.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "context" {
			systemPrompt += "\n\n<session-context>\n" + message.Content + "\n</session-context>"
			continue
		}
		filtered = append(filtered, message)
	}
	return systemPrompt, filtered
}

func eventRecord(event core.AgentEvent) map[string]any {
	record := map[string]any{"turn_id": event.TurnID, "sequence": event.Sequence, "step": event.Step}
	if event.ToolCall != nil {
		record["tool_call"] = event.ToolCall
	}
	if event.Execution != nil {
		record["execution"] = event.Execution
	}
	for key, value := range event.Data {
		record[key] = value
	}
	return record
}
