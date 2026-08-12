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
