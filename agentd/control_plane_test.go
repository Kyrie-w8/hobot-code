package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelTableAndSelection(t *testing.T) {
	models := parseModelTable([]byte("provider model context max-out thinking images\ndrobotics kimi-k3 1M 8K yes yes\nopenai gpt-5 1M 128K yes no\n"))
	if len(models) != 2 || models[0].Provider != "drobotics" || models[0].ID != "kimi-k3" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if !models[0].Capabilities.Reasoning || !models[0].Capabilities.ImageInput || models[0].CapabilitySource != "runtime-model-table" {
		t.Fatalf("runtime capabilities were not parsed: %+v", models[0])
	}
	if !models[1].Capabilities.Reasoning || models[1].Capabilities.ImageInput {
		t.Fatalf("negative image capability was not parsed: %+v", models[1])
	}
	legacy := parseModelTable([]byte("provider model context max-out\ndrobotics legacy 1M 8K\n"))
	if len(legacy) != 1 || legacy[0].Capabilities.ImageInput || legacy[0].CapabilitySource != "conservative-default" {
		t.Fatalf("legacy runtime must use conservative capabilities: %+v", legacy)
	}
	t.Setenv("ANTHROPIC_MODEL", "openai/gpt-5")
	markDefaultModel(models)
	if !models[1].Default || models[0].Default {
		t.Fatalf("default model was not marked: %+v", models)
	}
	t.Setenv("ANTHROPIC_MODEL", "missing-model")
	for index := range models {
		models[index].Default = false
	}
	markDefaultModel(models)
	if !models[0].Default {
		t.Fatalf("missing configured default did not fall back to kimi-k3: %+v", models)
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

func TestEditEventHistoryKeepsOnlyEventsBeforeReplacedPrompt(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source-events.jsonl")
	targetPath := filepath.Join(root, "target-events.jsonl")
	events := []taskEvent{
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 1, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "first"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 2, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "assistant.text.delta", Data: map[string]any{"delta": "first answer"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 3, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "replace me"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 4, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "assistant.text.delta", Data: map[string]any{"delta": "remove me"}}},
	}
	var content bytes.Buffer
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		content.Write(encoded)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(sourcePath, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	eventBytes, lastSequence, err := writeEditEventHistory(targetPath, sourcePath, "source", "edited", 3, 1<<20)
	if err != nil || eventBytes <= 0 || lastSequence != 2 {
		t.Fatalf("copy edit history failed: bytes=%d sequence=%d err=%v", eventBytes, lastSequence, err)
	}
	copied, err := readEvents(targetPath, "edited", 0)
	if err != nil || len(copied) != 2 {
		t.Fatalf("unexpected copied history: events=%+v err=%v", copied, err)
	}
	if copied[0].Sequence != 1 || copied[1].Sequence != 2 || copied[0].TaskID != "edited" || copied[1].TaskID != "edited" {
		t.Fatalf("copied history was not rebound to the edit task: %+v", copied)
	}
	if text, _ := copied[1].Normalized.Data["delta"].(string); text != "first answer" {
		t.Fatalf("pre-edit assistant context was lost: %q", text)
	}
}

func TestEditEventHistoryTruncatesAtAUserTurn(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source-events.jsonl")
	targetPath := filepath.Join(root, "target-events.jsonl")
	events := []taskEvent{
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 1, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "old"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 2, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "assistant.text.delta", Data: map[string]any{"delta": strings.Repeat("x", 600)}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 3, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "recent"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 4, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "assistant.text.delta", Data: map[string]any{"delta": "recent answer"}}},
		{Protocol: protocolVersion, Kind: "event", TaskID: "source", Sequence: 5, Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "user.message", Data: map[string]any{"text": "replace"}}},
	}
	var content bytes.Buffer
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		content.Write(encoded)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(sourcePath, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writeEditEventHistory(targetPath, sourcePath, "source", "edited", 5, 1600); err != nil {
		t.Fatal(err)
	}
	copied, err := readEvents(targetPath, "edited", 0)
	if err != nil || len(copied) != 2 {
		t.Fatalf("unexpected bounded edit history: events=%+v err=%v", copied, err)
	}
	if copied[0].Normalized.Type != "user.message" || copied[0].Normalized.Data["text"] != "recent" {
		t.Fatalf("bounded history began in the middle of a turn: %+v", copied)
	}
}
