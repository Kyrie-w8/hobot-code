package main

import (
	"encoding/json"
	"time"
)

const (
	maximumApprovalText       = 4 * 1024
	maximumApprovalPrefill    = 16 * 1024
	maximumApprovalOptionText = 1024
	maximumPendingApprovals   = 16
)

type pendingApproval struct {
	ID          string    `json:"id"`
	Method      string    `json:"method"`
	Title       string    `json:"title,omitempty"`
	Message     string    `json:"message,omitempty"`
	Options     []string  `json:"options,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
	Prefill     string    `json:"prefill,omitempty"`
	TimeoutMS   int       `json:"timeoutMs,omitempty"`
	RequestedAt time.Time `json:"requestedAt"`
	Active      bool      `json:"active"`
}

func normalizeWorkerEvent(raw json.RawMessage) *normalizedEvent {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil {
		return nil
	}
	eventType, _ := event["type"].(string)
	data := map[string]any{}
	var normalizedType string
	switch eventType {
	case "hobot_user_prompt":
		message, _ := event["message"].(string)
		if message == "" {
			return nil
		}
		normalizedType = "user.message"
		data["text"] = message
		copyEventFields(data, event, "attachments")
	case "agent_start":
		normalizedType = "task.running"
	case "agent_settled":
		normalizedType = "task.idle"
	case "message_update":
		update, _ := event["assistantMessageEvent"].(map[string]any)
		delta, _ := update["delta"].(string)
		switch update["type"] {
		case "thinking_delta":
			normalizedType = "assistant.thinking.delta"
		case "text_delta":
			normalizedType = "assistant.text.delta"
		default:
			return nil
		}
		data["delta"] = delta
	case "message_end":
		message, _ := event["message"].(map[string]any)
		if role, _ := message["role"].(string); role != "assistant" {
			return nil
		}
		normalizedType = "assistant.message.completed"
		copyEventFields(data, message, "usage", "stopReason", "timestamp")
	case "tool_execution_start":
		normalizedType = "tool.started"
		copyEventFields(data, event, "toolCallId", "toolName")
	case "tool_execution_update":
		normalizedType = "tool.progress"
		copyEventFields(data, event, "toolCallId", "toolName")
	case "tool_execution_end":
		normalizedType = "tool.completed"
		copyEventFields(data, event, "toolCallId", "toolName", "isError")
	case "extension_ui_request":
		method, _ := event["method"].(string)
		if !isApprovalMethod(method) {
			return nil
		}
		normalizedType = "approval.requested"
		copyEventFields(data, event, "id", "method", "title", "message", "options", "placeholder", "prefill", "timeout")
	case "hobot_approval_resolved":
		normalizedType = "approval.resolved"
		copyEventFields(data, event, "id")
	case "retry_start", "retry_end", "compaction_start", "compaction_end", "extension_error":
		normalizedType = eventType
		if value, ok := event["error"]; ok {
			data["error"] = value
		}
	default:
		return nil
	}
	return &normalizedEvent{Schema: eventSchemaVersion, Type: normalizedType, Data: data}
}

func copyEventFields(target, source map[string]any, fields ...string) {
	for _, field := range fields {
		if value, ok := source[field]; ok {
			target[field] = value
		}
	}
}

func isApprovalMethod(method string) bool {
	return method == "confirm" || method == "select" || method == "input" || method == "editor"
}

func approvalFromEvent(raw json.RawMessage) (pendingApproval, bool) {
	var value struct {
		ID          string   `json:"id"`
		Method      string   `json:"method"`
		Title       string   `json:"title"`
		Message     string   `json:"message"`
		Options     []string `json:"options"`
		Placeholder string   `json:"placeholder"`
		Prefill     string   `json:"prefill"`
		TimeoutMS   int      `json:"timeout"`
	}
	if json.Unmarshal(raw, &value) != nil || value.ID == "" || !isApprovalMethod(value.Method) {
		return pendingApproval{}, false
	}
	value.Title = boundedText(value.Title)
	value.Message = boundedText(value.Message)
	value.Placeholder = boundedText(value.Placeholder)
	value.Prefill = boundedValue(value.Prefill, maximumApprovalPrefill)
	if value.TimeoutMS < 0 || value.TimeoutMS > 24*60*60*1000 {
		value.TimeoutMS = 0
	}
	if len(value.Options) > 32 {
		value.Options = value.Options[:32]
	}
	for index := range value.Options {
		value.Options[index] = boundedValue(value.Options[index], maximumApprovalOptionText)
	}
	return pendingApproval{
		ID: value.ID, Method: value.Method, Title: value.Title, Message: value.Message,
		Options: value.Options, Placeholder: value.Placeholder, Prefill: value.Prefill,
		TimeoutMS: value.TimeoutMS, RequestedAt: time.Now().UTC(), Active: true,
	}, true
}

func boundedText(value string) string {
	return boundedValue(value, maximumApprovalText)
}

func boundedValue(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
