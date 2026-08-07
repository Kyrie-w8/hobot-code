package session

import (
	"encoding/json"
	"github.com/Kyrie-w8/aster-edge/internal/core"
	"testing"
)

func TestAppendLoadAndExport(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := NewID()
	message := core.Message{Role: "user", Content: "hello"}
	if err := store.AppendMessage(id, message); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
	b, err := store.Export(id)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["session_id"] != id {
		t.Fatal("missing session id")
	}
}

func TestRejectsTraversalSessionID(t *testing.T) {
	store, _ := New(t.TempDir())
	if _, err := store.Messages("../secret"); err == nil {
		t.Fatal("expected invalid id error")
	}
}
