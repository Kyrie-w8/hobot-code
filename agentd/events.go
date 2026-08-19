package main

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	maximumApprovalText       = 4 * 1024
	maximumApprovalPrefill    = 16 * 1024
	maximumApprovalOptionText = 1024
	maximumPendingApprovals   = 16
	maximumAssistantErrorText = 8 * 1024
	maximumEventPreviewText   = 12 * 1024
	maximumModelRetries       = 5
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

func redactEventImagePayloads(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil || !redactStructuredImagePayloads(value) {
		return raw
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return encoded
}

func redactStructuredImagePayloads(value any) bool {
	changed := false
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			changed = redactStructuredImagePayloads(item) || changed
		}
	case map[string]any:
		kind, _ := current["type"].(string)
		switch kind {
		case "image":
			if _, present := current["data"]; present {
				delete(current, "data")
				current["payloadOmitted"] = true
				changed = true
			}
		case "base64":
			mediaType, _ := current["media_type"].(string)
			if mediaType == "" {
				mediaType, _ = current["mimeType"].(string)
			}
			if strings.HasPrefix(strings.ToLower(mediaType), "image/") {
				if _, present := current["data"]; present {
					delete(current, "data")
					current["payloadOmitted"] = true
					changed = true
				}
			}
		case "image_url":
			if dataURL, ok := current["image_url"].(string); ok && strings.HasPrefix(strings.ToLower(dataURL), "data:image/") {
				current["image_url"] = "[image payload omitted]"
				current["payloadOmitted"] = true
				changed = true
			}
		}
		for _, item := range current {
			changed = redactStructuredImagePayloads(item) || changed
		}
	}
	return changed
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
		copyEventFields(data, event, "attachments", "source", "scheduleId", "queueId", "queueStatus", "queuedAt")
	case "hobot_followup_queued":
		normalizedType = "followup.queued"
		copyEventFields(data, event, "queueId", "queuedAt", "status")
	case "hobot_followup_dispatching":
		normalizedType = "followup.dispatching"
		copyEventFields(data, event, "queueId", "queuedAt", "status")
	case "hobot_followup_sent":
		normalizedType = "followup.sent"
		copyEventFields(data, event, "queueId", "queuedAt", "status")
	case "hobot_followup_cancelled":
		normalizedType = "followup.cancelled"
		copyEventFields(data, event, "queueId", "queuedAt", "status", "reason")
	case "hobot_followup_blocked":
		normalizedType = "followup.blocked"
		copyEventFields(data, event, "queueId", "queuedAt", "status", "reason", "recovery")
	case "hobot_followup_armed":
		normalizedType = "followup.queued"
		copyEventFields(data, event, "queueId", "queuedAt", "status", "reason")
	case "hobot_task_queued":
		normalizedType = "task.queued"
		copyEventFields(data, event, "queuedAt", "operation")
	case "hobot_task_dequeued":
		normalizedType = "task.starting"
		copyEventFields(data, event, "queuedAt", "operation")
	case "hobot_task_queue_cancelled":
		normalizedType = "task.cancelled"
		copyEventFields(data, event, "queuedAt", "operation")
	case "hobot_task_failed":
		normalizedType = "task.failed"
		copyEventFields(data, event, "code", "message", "recovery")
	case "hobot_task_interrupted":
		normalizedType = "task.interrupted"
		copyEventFields(data, event, "code", "message", "recovery")
	case "hobot_task_stopped":
		normalizedType = "task.stopped"
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
		if errorMessage, _ := message["errorMessage"].(string); errorMessage != "" {
			data["errorMessage"] = boundedValue(errorMessage, maximumAssistantErrorText)
		}
	case "tool_execution_start":
		normalizedType = "tool.started"
		copyEventFields(data, event, "toolCallId", "toolName")
		copyToolEventDetails(data, event, false)
	case "tool_execution_update":
		normalizedType = "tool.progress"
		copyEventFields(data, event, "toolCallId", "toolName")
		copyToolEventDetails(data, event, true)
	case "tool_execution_end":
		normalizedType = "tool.completed"
		copyEventFields(data, event, "toolCallId", "toolName", "isError")
		copyToolEventDetails(data, event, true)
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
	case "hobot_approval_reviewed":
		normalizedType = "approval.reviewed"
		copyEventFields(data, event, "toolName", "status", "risk", "reason", "model")
	case "auto_retry_start":
		normalizedType = "retry_start"
		copyRetryEventFields(data, event, true)
	case "auto_retry_end":
		normalizedType = "retry_end"
		copyRetryEventFields(data, event, true)
	case "retry_start", "retry_end":
		normalizedType = eventType
		copyRetryEventFields(data, event, false)
		if value, ok := event["error"]; ok {
			data["error"] = value
		}
	case "compaction_start", "compaction_end", "extension_error":
		normalizedType = eventType
		if value, ok := event["error"]; ok {
			data["error"] = value
		}
	default:
		return nil
	}
	return &normalizedEvent{Schema: eventSchemaVersion, Type: normalizedType, Data: data, Item: normalizedItemFor(normalizedType, data)}
}

func copyRetryEventFields(target, source map[string]any, automatic bool) {
	target["automatic"] = automatic
	for _, field := range []string{"attempt", "maxAttempts"} {
		if value, ok := boundedRetryInteger(source[field], 1, maximumModelRetries); ok {
			target[field] = value
		}
	}
	if value, ok := boundedRetryInteger(source["delayMs"], 0, 10*60*1000); ok {
		target["delayMs"] = value
	}
	if success, ok := source["success"].(bool); ok {
		target["success"] = success
	}
}

func boundedRetryInteger(value any, minimum, maximum int) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < float64(minimum) || number > float64(maximum) {
		return 0, false
	}
	integer := int(number)
	return integer, float64(integer) == number
}

func normalizedItemFor(eventType string, data map[string]any) *normalizedItem {
	item := &normalizedItem{}
	switch eventType {
	case "user.message":
		item.Type, item.Status = "userMessage", "completed"
	case "assistant.thinking.delta":
		item.Type, item.Status = "reasoning", "inProgress"
	case "assistant.text.delta":
		item.Type, item.Status = "agentMessage", "inProgress"
	case "assistant.message.completed":
		item.Type, item.Status = "agentMessage", "completed"
		if data["errorMessage"] != nil {
			item.Status = "failed"
		}
	case "tool.started", "tool.progress", "tool.completed":
		item.ID, _ = data["toolCallId"].(string)
		item.Type = "toolCall"
		if name, _ := data["toolName"].(string); name == "bash" {
			item.Type = "commandExecution"
		}
		item.Status = "inProgress"
		if eventType == "tool.completed" {
			item.Status = "completed"
			if failed, _ := data["isError"].(bool); failed {
				item.Status = "failed"
			}
		}
	case "approval.requested":
		item.ID, _ = data["id"].(string)
		item.Type, item.Status = "approval", "waiting"
	case "approval.resolved":
		item.ID, _ = data["id"].(string)
		item.Type, item.Status = "approval", "completed"
	case "approval.reviewed":
		item.Type, item.Status = "approval", "completed"
	case "task.queued":
		item.Type, item.Status = "task", "queued"
	case "task.starting", "task.running":
		item.Type, item.Status = "task", "inProgress"
	case "task.idle":
		item.Type, item.Status = "task", "ready"
	case "task.cancelled":
		item.Type, item.Status = "task", "cancelled"
	case "task.failed":
		item.Type, item.Status = "task", "failed"
	case "task.interrupted":
		item.Type, item.Status = "task", "interrupted"
	case "task.stopped":
		item.Type, item.Status = "task", "stopped"
	case "followup.queued":
		item.ID, _ = data["queueId"].(string)
		item.Type, item.Status = "followup", "queued"
	case "followup.dispatching":
		item.ID, _ = data["queueId"].(string)
		item.Type, item.Status = "followup", "inProgress"
	case "followup.sent":
		item.ID, _ = data["queueId"].(string)
		item.Type, item.Status = "followup", "completed"
	case "followup.cancelled":
		item.ID, _ = data["queueId"].(string)
		item.Type, item.Status = "followup", "cancelled"
	case "followup.blocked":
		item.ID, _ = data["queueId"].(string)
		item.Type, item.Status = "followup", "blocked"
	default:
		return nil
	}
	return item
}

func copyToolEventDetails(target, source map[string]any, includeOutput bool) {
	if args, ok := source["args"]; ok {
		target["inputPreview"] = previewEventValue(args)
		if values, ok := args.(map[string]any); ok {
			for _, field := range []string{"command", "cwd", "path", "file_path"} {
				if value, ok := values[field].(string); ok {
					target[field] = boundedValue(value, maximumEventPreviewText)
				}
			}
		}
	}
	if !includeOutput {
		return
	}
	for _, field := range []string{"result", "output", "error"} {
		if value, ok := source[field]; ok {
			target["outputPreview"] = previewEventValue(value)
			return
		}
	}
}

func previewEventValue(value any) string {
	if text, ok := value.(string); ok {
		return boundedValue(text, maximumEventPreviewText)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return boundedValue("[unavailable]", maximumEventPreviewText)
	}
	return boundedValue(string(encoded), maximumEventPreviewText)
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
