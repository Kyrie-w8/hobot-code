package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func TestAppendLoadAndExport(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
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
	defer store.Close()
	if _, err := store.Messages("../secret"); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestSQLiteWALUndoRedoAndBranch(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil || journalMode != "wal" {
		t.Fatalf("journal_mode=%q err=%v", journalMode, err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "aster.db")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("insecure mode %o for %s", info.Mode().Perm(), path)
		}
	}
	id := NewID()
	appendTurn(t, store, id, "one", "first")
	appendTurn(t, store, id, "two", "second")
	if err := store.Undo(id); err != nil {
		t.Fatal(err)
	}
	assertMessageContents(t, store, id, []string{"one", "first"})
	if err := store.Redo(id); err != nil {
		t.Fatal(err)
	}
	assertMessageContents(t, store, id, []string{"one", "first", "two", "second"})
	if err := store.Undo(id); err != nil {
		t.Fatal(err)
	}
	appendTurn(t, store, id, "branch", "replacement")
	if err := store.Redo(id); err == nil || !strings.Contains(err.Error(), "nothing to redo") {
		t.Fatalf("redo after branch err=%v", err)
	}
	assertMessageContents(t, store, id, []string{"one", "first", "branch", "replacement"})
}

func TestCompactKeepsAuditButReplacesContext(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := NewID()
	appendTurn(t, store, id, "question", "answer")
	if err := store.Compact(id, "The user asked a question and received an answer."); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "context" {
		t.Fatalf("messages=%+v", messages)
	}
	records, err := store.AllRecords(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 6 || !records[0].Compacted {
		t.Fatalf("audit records were not retained: %+v", records)
	}
}

func TestRestartArchivesInterruptedTurn(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := NewID()
	if err := store.AppendEvent(id, string(core.EventTurnStarted), map[string]any{"turn_id": "turn"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(id, core.Message{Role: "user", Content: "unfinished"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.RecoveredCount() != 1 {
		t.Fatalf("recovered=%d", reopened.RecoveredCount())
	}
	messages, err := reopened.Messages(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("interrupted messages remain active: %+v", messages)
	}
}

func TestLegacyJSONLImportIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	id := "legacy-session"
	record := Record{Type: "message", Timestamp: "2026-01-01T00:00:00Z", Message: &core.Message{Role: "user", Content: "legacy"}}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertMessageContents(t, store, id, []string{"legacy"})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertMessageContents(t, reopened, id, []string{"legacy"})
	records, err := reopened.AllRecords(id)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
}

func TestLegacyJSONLImportContinuesAfterRollback(t *testing.T) {
	dir := t.TempDir()
	id := "legacy-continued"
	path := filepath.Join(dir, id+".jsonl")
	first := Record{Type: "message", Timestamp: "2026-01-01T00:00:00Z", Message: &core.Message{Role: "user", Content: "before upgrade"}}
	data, _ := json.Marshal(first)
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	second := Record{Type: "message", Timestamp: "2026-01-01T00:01:00Z", Message: &core.Message{Role: "assistant", Content: "after rollback"}}
	data, _ = json.Marshal(second)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertMessageContents(t, reopened, id, []string{"before upgrade", "after rollback"})
	var lineCount int
	if err := reopened.db.QueryRow(`SELECT line_count FROM legacy_imports WHERE path=?`, path).Scan(&lineCount); err != nil || lineCount != 2 {
		t.Fatalf("line_count=%d err=%v", lineCount, err)
	}
}

func TestLegacyInterruptedTurnIsRecovered(t *testing.T) {
	dir := t.TempDir()
	id := "legacy-interrupted"
	records := []Record{
		{Type: "event", Timestamp: "2026-01-01T00:00:00Z", Event: string(core.EventTurnStarted)},
		{Type: "message", Timestamp: "2026-01-01T00:00:01Z", Message: &core.Message{Role: "user", Content: "unfinished"}},
	}
	var data []byte
	for _, record := range records {
		line, _ := json.Marshal(record)
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.RecoveredCount() != 1 {
		t.Fatalf("recovered=%d", store.RecoveredCount())
	}
	messages, err := store.Messages(id)
	if err != nil || len(messages) != 0 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
}

func TestMissingSessionReturnsNotExist(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.Messages("missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err=%v", err)
	}
}

func appendTurn(t *testing.T, store *Store, id, user, assistant string) {
	t.Helper()
	if err := store.AppendEvent(id, string(core.EventTurnStarted), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(id, core.Message{Role: "user", Content: user}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(id, core.Message{Role: "assistant", Content: assistant}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(id, string(core.EventTurnCompleted), nil); err != nil {
		t.Fatal(err)
	}
}

func assertMessageContents(t *testing.T, store *Store, id string, want []string) {
	t.Helper()
	messages, err := store.Messages(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != len(want) {
		t.Fatalf("messages=%+v want=%v", messages, want)
	}
	for index, content := range want {
		if messages[index].Content != content {
			t.Fatalf("message[%d]=%q want=%q", index, messages[index].Content, content)
		}
	}
}
