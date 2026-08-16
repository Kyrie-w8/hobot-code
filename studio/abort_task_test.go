package main

import (
	"strings"
	"testing"
)

func TestAbortTaskRejectsUnknownBoard(t *testing.T) {
	app := NewApp()
	if err := app.AbortTask("missing", "00112233445566778899aabb"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unexpected abort error: %v", err)
	}
}
