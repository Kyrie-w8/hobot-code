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

func TestCompactionLifecycleIsNormalizedWithoutProviderFields(t *testing.T) {
	started := normalizeWorkerEvent(json.RawMessage(`{"type":"compaction_start","provider":"private-provider","request_id":"private-request","sessionFile":"/root/private.jsonl"}`))
	if started == nil || started.Type != "compaction_start" || started.Schema != eventSchemaVersion || started.Item != nil {
		t.Fatalf("unexpected compaction start event: %+v", started)
	}
	if len(started.Data) != 0 {
		t.Fatalf("compaction start retained private provider fields: %+v", started.Data)
	}

	completed := normalizeWorkerEvent(json.RawMessage(`{"type":"compaction_end","summary":"private model output","tokensBefore":123456,"firstKeptEntryId":"private-entry"}`))
	if completed == nil || completed.Type != "compaction_end" || completed.Item != nil {
		t.Fatalf("unexpected compaction end event: %+v", completed)
	}
	if len(completed.Data) != 0 {
		t.Fatalf("compaction end retained private session fields: %+v", completed.Data)
	}
}

func TestWorkerEventsOmitStructuredImagePayloads(t *testing.T) {
	raw := json.RawMessage(`{"type":"retry_start","message":{"content":[{"type":"text","text":"keep"},{"type":"image","data":"private-simple","mimeType":"image/png"}]},"messages":[{"content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"private-source"}},{"type":"image_url","image_url":"data:image/webp;base64,private-url"}]}],"data":{"value":"keep-unrelated"}}`)
	redacted := redactEventImagePayloads(raw)
	for _, secret := range []string{"private-simple", "private-source", "private-url"} {
		if strings.Contains(string(redacted), secret) {
			t.Fatalf("structured image payload %q was retained: %s", secret, redacted)
		}
	}
	for _, retained := range []string{"image/png", "image/jpeg", "keep-unrelated", `"payloadOmitted":true`} {
		if !strings.Contains(string(redacted), retained) {
			t.Fatalf("image metadata %q was not retained: %s", retained, redacted)
		}
	}

	unrelated := json.RawMessage(`{"type":"response","data":{"value":"data:image/png;base64,not-an-image-block"}}`)
	if got := redactEventImagePayloads(unrelated); string(got) != string(unrelated) {
		t.Fatalf("unrelated event data was changed: %s", got)
	}
}
