package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestModelTableAndSelection(t *testing.T) {
	models := parseModelTable([]byte("provider model context max-out\ndrobotics kimi-k3 1M 8K\nopenai gpt-5 1M 128K\n"))
	if len(models) != 2 || models[0].Provider != "drobotics" || models[0].ID != "kimi-k3" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if normalized := normalizeModelSelection("drobotics/kimi-k3"); normalized != "drobotics/kimi-k3" {
		t.Fatalf("model selection was not preserved: %q", normalized)
	}
	if normalized := normalizeModelSelection("openrouter/anthropic/claude-sonnet"); normalized != "openrouter/anthropic/claude-sonnet" {
		t.Fatalf("hierarchical model ID was rejected: %q", normalized)
	}
	if normalized := normalizeModelSelection("../../unsafe"); normalized != "" {
		t.Fatalf("unsafe model selection was accepted: %q", normalized)
	}
}

func TestWorkspaceBrowserCreatesRealDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "existing"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "existing"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".hidden"), 0o700); err != nil {
		t.Fatal(err)
	}
	listing, err := browseWorkspace(workspaceParams{Path: root})
	if err != nil || len(listing.Directories) != 1 || listing.Directories[0].Name != "existing" {
		t.Fatalf("unexpected workspace listing: listing=%+v err=%v", listing, err)
	}
	created, err := createWorkspace(createWorkspaceParams{Parent: root, Name: "new-project"})
	physicalRoot, physicalErr := filepath.EvalSymlinks(root)
	if err != nil || physicalErr != nil || created.Path != filepath.Join(physicalRoot, "new-project") {
		t.Fatalf("workspace creation failed: listing=%+v err=%v", created, err)
	}
	if _, err := createWorkspace(createWorkspaceParams{Parent: root, Name: "../escape"}); err == nil {
		t.Fatal("expected unsafe workspace name rejection")
	}
}

func TestSessionForkUsesSettledAndHistoricalContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.jsonl")
	rows := []map[string]any{
		{"type": "session", "version": 3, "id": "source", "cwd": root},
		{"type": "model_change", "id": "model", "parentId": nil, "provider": "drobotics", "modelId": "kimi-k3"},
		{"type": "message", "id": "user-1", "parentId": "model", "message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": "first"}}}},
		{"type": "message", "id": "assistant-1", "parentId": "user-1", "message": map[string]any{"role": "assistant", "stopReason": "stop", "content": []map[string]any{{"type": "text", "text": "done"}}}},
		{"type": "message", "id": "user-2", "parentId": "assistant-1", "message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": "in flight"}}}},
	}
	var content []byte
	for _, row := range rows {
		encoded, _ := json.Marshal(row)
		content = append(content, encoded...)
		content = append(content, '\n')
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, lines, err := readSessionLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if leaf := safeSessionLeaf(lines); leaf != "assistant-1" {
		t.Fatalf("in-flight turn forked from %q, expected settled assistant", leaf)
	}
	forkPath, err := writeSessionFork(root, path, root, "model", lines)
	if err != nil {
		t.Fatal(err)
	}
	_, forkLines, err := readSessionLines(forkPath)
	if err != nil || len(forkLines) != 1 || forkLines[0].ID != "model" {
		t.Fatalf("historical fork contains the wrong branch: lines=%+v err=%v", forkLines, err)
	}
}

func TestHistoricalForkIgnoresPromptsFromOlderTaskSessions(t *testing.T) {
	root := t.TempDir()
	eventsPath := filepath.Join(root, "events.jsonl")
	events := []taskEvent{
		{Protocol: protocolVersion, Kind: "event", TaskID: "task", Sequence: 1, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "older session"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "task", Sequence: 2, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "current session"}}},
	}
	var encodedEvents []byte
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		encodedEvents = append(encodedEvents, encoded...)
		encodedEvents = append(encodedEvents, '\n')
	}
	if err := os.WriteFile(eventsPath, encodedEvents, 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{events: eventsPath, metadata: taskMetadata{ID: "task"}}
	lines := []sessionLine{
		{Type: "model_change", ID: "model"},
		{Type: "message", ID: "user", ParentID: "model", Role: "user", Text: "current session"},
	}
	leaf, err := current.sessionLeafBeforePrompt(2, lines)
	if err != nil || leaf != "model" {
		t.Fatalf("historical session lookup failed: leaf=%q err=%v", leaf, err)
	}
}
