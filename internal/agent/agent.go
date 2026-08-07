package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	user := core.Message{Role: "user", Content: input}
	messages = append(messages, user)
	if err := e.Store.AppendMessage(sessionID, user); err != nil {
		return core.AgentResult{}, err
	}
	usage := map[string]any{}
	for step := 1; step <= e.Config.Agent.MaxSteps; step++ {
		response, err := e.Provider.Complete(ctx, core.ProviderRequest{Model: e.Config.Provider.Model, SystemPrompt: e.SystemPrompt, Messages: messages, Tools: e.Tools.Definitions(), Settings: e.Config.Provider.Settings})
		if err != nil {
			_ = e.Store.AppendEvent(sessionID, "provider_error", map[string]any{"error": err.Error(), "step": step})
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
			return core.AgentResult{SessionID: sessionID, Content: response.Content, Steps: step, Usage: usage}, nil
		}
		for _, call := range response.ToolCalls {
			execution := e.Tools.Execute(ctx, call)
			toolMessage := core.Message{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: execution.JSON()}
			messages = append(messages, toolMessage)
			if err := e.Store.AppendMessage(sessionID, toolMessage); err != nil {
				return core.AgentResult{}, err
			}
			_ = e.Store.AppendEvent(sessionID, "tool_execution", map[string]any{"tool": call.Name, "ok": execution.OK, "duration_ms": execution.DurationMS})
		}
	}
	return core.AgentResult{}, fmt.Errorf("agent exceeded max_steps=%d", e.Config.Agent.MaxSteps)
}
