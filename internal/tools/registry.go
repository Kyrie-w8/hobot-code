package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/core"
	"github.com/Kyrie-w8/aster-edge/internal/policy"
)

type ApprovalFunc func(core.ToolCall, core.ToolDefinition) bool

type Registry struct {
	items     map[string]core.Tool
	policy    *policy.Engine
	approval  ApprovalFunc
	maxOutput int
}

func New(engine *policy.Engine, approval ApprovalFunc, maxOutput int) *Registry {
	return &Registry{items: map[string]core.Tool{}, policy: engine, approval: approval, maxOutput: maxOutput}
}

func (r *Registry) Add(tool core.Tool) error {
	if tool.Definition.Name == "" || tool.Handler == nil {
		return fmt.Errorf("tool requires a name and handler")
	}
	if _, exists := r.items[tool.Definition.Name]; exists {
		return fmt.Errorf("duplicate tool %q", tool.Definition.Name)
	}
	if tool.Definition.Parameters == nil {
		tool.Definition.Parameters = objectSchema(nil, nil)
	}
	r.items[tool.Definition.Name] = tool
	return nil
}

func (r *Registry) Definitions() []core.ToolDefinition {
	defs := make([]core.ToolDefinition, 0, len(r.items))
	for _, item := range r.items {
		decision := r.policy.Decide(item.Definition.Name, item.Definition.Risk)
		if decision.Allowed {
			defs = append(defs, item.Definition)
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

func (r *Registry) Available() map[string]bool {
	result := map[string]bool{}
	for _, item := range r.Definitions() {
		result[item.Name] = true
	}
	return result
}

func (r *Registry) Execute(ctx context.Context, call core.ToolCall) core.ToolExecution {
	started := time.Now()
	result := core.ToolExecution{CallID: call.ID, Name: call.Name}
	tool, ok := r.items[call.Name]
	if !ok {
		result.Error = "unknown tool"
		return result
	}
	if err := validate(tool.Definition.Parameters, call.Arguments); err != nil {
		result.Error = err.Error()
		return result
	}
	decision := r.policy.Decide(call.Name, tool.Definition.Risk)
	if !decision.Allowed {
		result.Error = decision.Reason
		return result
	}
	if decision.RequiresApproval && (r.approval == nil || !r.approval(call, tool.Definition)) {
		result.Error = "approval denied"
		return result
	}
	timeout := tool.TimeoutSec
	if timeout <= 0 {
		timeout = 20
	}
	toolCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	type response struct {
		value any
		err   error
	}
	ch := make(chan response, 1)
	go func() { value, err := tool.Handler(toolCtx, call.Arguments); ch <- response{value, err} }()
	select {
	case <-toolCtx.Done():
		result.Error = toolCtx.Err().Error()
	case response := <-ch:
		if response.err != nil {
			result.Error = response.err.Error()
		} else {
			result.OK = true
			result.Output = truncate(response.value, r.maxOutput)
		}
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result
}

func validate(schema map[string]any, args map[string]any) error {
	if required, ok := schema["required"].([]string); ok {
		for _, key := range required {
			if _, exists := args[key]; !exists {
				return fmt.Errorf("missing required argument %q", key)
			}
		}
	} else if raw, ok := schema["required"].([]any); ok {
		for _, item := range raw {
			key, _ := item.(string)
			if _, exists := args[key]; key != "" && !exists {
				return fmt.Errorf("missing required argument %q", key)
			}
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for key, value := range args {
		raw, exists := properties[key]
		if !exists {
			if schema["additionalProperties"] == false {
				return fmt.Errorf("unknown argument %q", key)
			}
			continue
		}
		prop, _ := raw.(map[string]any)
		typ, _ := prop["type"].(string)
		if typ != "" && !typeMatches(typ, value) {
			return fmt.Errorf("argument %q must be %s", key, typ)
		}
	}
	return nil
}

func typeMatches(typ string, value any) bool {
	switch typ {
	case "string":
		_, ok := value.(string)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		v, ok := value.(float64)
		return ok && math.Trunc(v) == v
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}

func truncate(value any, max int) any {
	if max <= 0 {
		return value
	}
	b, err := json.Marshal(value)
	if err != nil || len(b) <= max {
		return value
	}
	return map[string]any{"truncated": true, "bytes": len(b), "preview": string(b[:max])}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
