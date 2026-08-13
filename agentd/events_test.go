package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssistantMessageFailureIsNormalizedAndBounded(t *testing.T) {
	errorDetail := strings.Repeat("x", maximumAssistantErrorText+512)
	normalized := normalizeWorkerEvent(json.RawMessage(`{"type":"message_end","message":{"role":"assistant","errorMessage":"` + errorDetail + `","stopReason":"error"}}`))
	if normalized == nil || normalized.Type != "assistant.message.completed" {
		t.Fatalf("unexpected normalized event: %+v", normalized)
	}
	message, ok := normalized.Data["errorMessage"].(string)
	if !ok || len(message) != maximumAssistantErrorText {
		t.Fatalf("normalized error length = %d, want %d", len(message), maximumAssistantErrorText)
	}
	if normalized.Data["stopReason"] != "error" {
		t.Fatalf("stop reason was not preserved: %+v", normalized.Data)
	}
	if got := normalizeWorkerEvent(json.RawMessage(`{"type":"message_end","message":{"role":"user","errorMessage":"failed"}}`)); got != nil {
		t.Fatalf("non-assistant message was normalized: %+v", got)
	}
}

func TestSchemaFourAddsStableItemSemanticsAndBoundedToolDetails(t *testing.T) {
	command := strings.Repeat("x", maximumEventPreviewText+512)
	normalized := normalizeWorkerEvent(json.RawMessage(`{"type":"tool_execution_start","toolCallId":"tool-1","toolName":"bash","args":{"command":"` + command + `","cwd":"/root/project"}}`))
	if normalized == nil || normalized.Schema != 4 || normalized.Type != "tool.started" || normalized.Item == nil {
		t.Fatalf("unexpected normalized tool event: %+v", normalized)
	}
	if normalized.Item.ID != "tool-1" || normalized.Item.Type != "commandExecution" || normalized.Item.Status != "inProgress" {
		t.Fatalf("unexpected item semantics: %+v", normalized.Item)
	}
	if normalized.Data["cwd"] != "/root/project" || len(normalized.Data["inputPreview"].(string)) != maximumEventPreviewText {
		t.Fatalf("tool details are incomplete or unbounded: %+v", normalized.Data)
	}
	completed := normalizeWorkerEvent(json.RawMessage(`{"type":"tool_execution_end","toolCallId":"tool-1","toolName":"bash","isError":true,"error":"failed"}`))
	if completed == nil || completed.Item == nil || completed.Item.Status != "failed" || completed.Data["outputPreview"] != "failed" {
		t.Fatalf("failed command completion was not normalized: %+v", completed)
	}
	queued := normalizeWorkerEvent(json.RawMessage(`{"type":"hobot_task_queued","operation":"start","queuedAt":"2026-08-13T00:00:00Z"}`))
	if queued == nil || queued.Type != "task.queued" || queued.Item == nil || queued.Item.Status != "queued" {
		t.Fatalf("queue lifecycle item missing: %+v", queued)
	}
	failed := normalizeWorkerEvent(json.RawMessage(`{"type":"hobot_task_failed","code":"worker-exited","message":"The Agent worker exited before the task completed.","recovery":"resume"}`))
	if failed == nil || failed.Type != "task.failed" || failed.Item == nil || failed.Item.Status != "failed" || failed.Data["recovery"] != "resume" {
		t.Fatalf("terminal lifecycle item missing: %+v", failed)
	}
}
