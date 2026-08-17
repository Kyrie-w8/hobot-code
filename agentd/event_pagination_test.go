package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePaginationEvents(t *testing.T, taskID string, payloads []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var content bytes.Buffer
	for index, payload := range payloads {
		event := taskEvent{Protocol: protocolVersion, Kind: "event", TaskID: taskID, Sequence: uint64(index + 1), Time: time.Unix(int64(index), 0), Event: json.RawMessage(`{}`), Normalized: &normalizedEvent{Schema: eventSchemaVersion, Type: "assistant.text.delta", Data: map[string]any{"delta": payload}}}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		content.Write(encoded)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(path, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadEventPageBeforePaginatesTwoThousandEvents(t *testing.T) {
	const taskID = "pagination-task"
	payloads := make([]string, 2000)
	for index := range payloads {
		payloads[index] = "history"
	}
	path := writePaginationEvents(t, taskID, payloads)
	tail, err := readEventPageBeforeWithRetention(path, taskID, 0, 200, false)
	if err != nil || len(tail.Events) != 200 || tail.Events[0].Sequence != 1801 || tail.Events[199].Sequence != 2000 || !tail.HasEarlier || tail.NextBefore != 1801 {
		t.Fatalf("unexpected durable tail: page=%+v err=%v", tail, err)
	}
	previous, err := readEventPageBeforeWithRetention(path, taskID, tail.NextBefore, 200, false)
	if err != nil || len(previous.Events) != 200 || previous.Events[0].Sequence != 1601 || previous.Events[199].Sequence != 1800 || !previous.HasEarlier || previous.NextBefore != 1601 {
		t.Fatalf("unexpected previous page: page=%+v err=%v", previous, err)
	}
}

func TestReadEventPageBeforeRespectsMixedRecordByteBudget(t *testing.T) {
	const taskID = "large-pagination-task"
	large := string(bytes.Repeat([]byte("x"), 3*1024*1024))
	path := writePaginationEvents(t, taskID, []string{"small", large, large, large})
	page, err := readEventPageBeforeWithRetention(path, taskID, 0, 1000, false)
	if err != nil || len(page.Events) != 2 || page.Events[0].Sequence != 3 || page.Events[1].Sequence != 4 || !page.HasEarlier {
		t.Fatalf("unexpected mixed-size page: page=%+v err=%v", page, err)
	}
	total := 0
	for _, event := range page.Events {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		total += len(encoded) + 1
	}
	if total > maxResponseBytes-64*1024 {
		t.Fatalf("page exceeds response limit: %d", total)
	}
}

func TestEventPageBeforeRejectsAmbiguousCursors(t *testing.T) {
	for _, params := range []eventPageParams{
		{TaskID: "task", Direction: "sideways"},
		{TaskID: "task", Direction: "before", After: 9},
		{TaskID: "task", Before: 9},
	} {
		if err := validateEventPageParams(params); err == nil {
			t.Fatalf("expected invalid event page params to fail: %+v", params)
		}
	}
	if err := validateEventPageParams(eventPageParams{TaskID: "task", Direction: "before", Before: 9}); err != nil {
		t.Fatalf("valid before page was rejected: %v", err)
	}
}

func TestReadEventPageBeforeReportsRetainedHistoryBoundary(t *testing.T) {
	const taskID = "retained-task"
	path := writePaginationEvents(t, taskID, []string{"older retained", "newer retained"})
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.ReplaceAll(content, []byte(`"sequence":1`), []byte(`"sequence":101`))
	content = bytes.ReplaceAll(content, []byte(`"sequence":2`), []byte(`"sequence":102`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := readEventPageBeforeWithRetention(path, taskID, 100, 200, true)
	if err != nil || !page.HistoryTruncated || !page.CursorExpired || page.RetainedFrom != 101 || len(page.Events) != 0 {
		t.Fatalf("retained boundary was not explicit: page=%+v err=%v", page, err)
	}
}
