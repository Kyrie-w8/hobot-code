package session

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/core"
	_ "modernc.org/sqlite"
)

type Record struct {
	Sequence  int64          `json:"sequence,omitempty"`
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	Message   *core.Message  `json:"message,omitempty"`
	Event     string         `json:"event,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Undone    bool           `json:"undone,omitempty"`
	Compacted bool           `json:"compacted,omitempty"`
}

type Store struct {
	dir       string
	db        *sql.DB
	recovered int
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "aster.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{dir: dir, db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.secureFiles(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.importLegacyJSONL(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.recoverInterrupted(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize() error {
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'idle'
		)`,
		`CREATE TABLE IF NOT EXISTS records (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			message_json BLOB,
			event TEXT,
			data_json BLOB,
			undone INTEGER NOT NULL DEFAULT 0,
			compacted INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS records_session_sequence ON records(session_id, sequence)`,
		`CREATE INDEX IF NOT EXISTS records_session_state ON records(session_id, undone, compacted, sequence)`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			kind TEXT NOT NULL,
			data_json BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS legacy_imports (
			path TEXT PRIMARY KEY,
			imported_at TEXT NOT NULL,
			line_count INTEGER NOT NULL DEFAULT 0
		)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize session database: %w", err)
		}
	}
	if err := ensureColumn(s.db, "legacy_imports", "line_count", `ALTER TABLE legacy_imports ADD COLUMN line_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, alterStatement string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(alterStatement)
	return err
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Backend() string { return "sqlite-wal" }

func (s *Store) RecoveredCount() int { return s.recovered }

func (s *Store) secureFiles() error {
	if err := os.Chmod(s.dir, 0700); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := filepath.Join(s.dir, "aster.db"+suffix)
		if err := os.Chmod(path, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
}

func (s *Store) AppendMessage(id string, message core.Message) error {
	return s.append(id, Record{Type: "message", Timestamp: now(), Message: &message})
}

func (s *Store) AppendEvent(id, event string, data map[string]any) error {
	return s.append(id, Record{Type: "event", Timestamp: now(), Event: event, Data: data})
}

func (s *Store) append(id string, record Record) error {
	if !validID(id) {
		return fmt.Errorf("invalid session id")
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureSession(tx, id, record.Timestamp); err != nil {
		return err
	}
	if record.Event == string(core.EventTurnStarted) {
		if _, err := tx.Exec(`UPDATE records SET undone=0, compacted=1 WHERE session_id=? AND undone=1`, id); err != nil {
			return err
		}
	}
	if err := insertRecord(tx, id, record); err != nil {
		return err
	}
	status := ""
	switch record.Event {
	case string(core.EventTurnStarted):
		status = "running"
	case string(core.EventTurnCompleted), string(core.EventTurnCancelled), string(core.EventTurnFailed):
		status = "idle"
	}
	if status == "" {
		_, err = tx.Exec(`UPDATE sessions SET updated_at=? WHERE id=?`, record.Timestamp, id)
	} else {
		_, err = tx.Exec(`UPDATE sessions SET updated_at=?, status=? WHERE id=?`, record.Timestamp, status, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func ensureSession(tx *sql.Tx, id, timestamp string) error {
	_, err := tx.Exec(`INSERT INTO sessions(id, created_at, updated_at, status) VALUES(?, ?, ?, 'idle') ON CONFLICT(id) DO NOTHING`, id, timestamp, timestamp)
	return err
}

func insertRecord(tx *sql.Tx, id string, record Record) error {
	var messageJSON, dataJSON []byte
	if record.Message != nil {
		messageJSON, _ = json.Marshal(record.Message)
	}
	if record.Data != nil {
		dataJSON, _ = json.Marshal(record.Data)
	}
	_, err := tx.Exec(`INSERT INTO records(session_id, type, timestamp, message_json, event, data_json, undone, compacted) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		id, record.Type, record.Timestamp, nullableBytes(messageJSON), nullableString(record.Event), nullableBytes(dataJSON), boolInt(record.Undone), boolInt(record.Compacted))
	return err
}

func (s *Store) Messages(id string) ([]core.Message, error) {
	records, err := s.Records(id)
	if err != nil {
		return nil, err
	}
	out := make([]core.Message, 0, len(records))
	for _, record := range records {
		if record.Message != nil {
			out = append(out, *record.Message)
		}
	}
	return out, nil
}

func (s *Store) Records(id string) ([]Record, error) {
	return s.records(id, false)
}

func (s *Store) AllRecords(id string) ([]Record, error) {
	return s.records(id, true)
}

func (s *Store) records(id string, includeArchived bool) ([]Record, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid session id")
	}
	exists, err := s.sessionExists(id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, os.ErrNotExist
	}
	query := `SELECT sequence, type, timestamp, message_json, COALESCE(event,''), data_json, undone, compacted FROM records WHERE session_id=?`
	if !includeArchived {
		query += ` AND undone=0 AND compacted=0`
	}
	query += ` ORDER BY sequence`
	rows, err := s.db.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []Record
	for rows.Next() {
		var record Record
		var messageJSON, dataJSON []byte
		var undone, compacted int
		if err := rows.Scan(&record.Sequence, &record.Type, &record.Timestamp, &messageJSON, &record.Event, &dataJSON, &undone, &compacted); err != nil {
			return nil, err
		}
		record.Undone = undone != 0
		record.Compacted = compacted != 0
		if len(messageJSON) > 0 {
			var message core.Message
			if err := json.Unmarshal(messageJSON, &message); err != nil {
				return nil, err
			}
			record.Message = &message
		}
		if len(dataJSON) > 0 {
			if err := json.Unmarshal(dataJSON, &record.Data); err != nil {
				return nil, err
			}
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) sessionExists(id string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)`, id).Scan(&exists)
	return exists != 0, err
}

func (s *Store) List() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM sessions ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) Undo(id string) error {
	return s.changeTurnState(id, true)
}

func (s *Store) Redo(id string) error {
	return s.changeTurnState(id, false)
}

func (s *Store) changeTurnState(id string, undo bool) error {
	if !validID(id) {
		return fmt.Errorf("invalid session id")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var start int64
	if undo {
		err = tx.QueryRow(`SELECT sequence FROM records WHERE session_id=? AND event=? AND undone=0 AND compacted=0 ORDER BY sequence DESC LIMIT 1`, id, string(core.EventTurnStarted)).Scan(&start)
	} else {
		err = tx.QueryRow(`SELECT sequence FROM records WHERE session_id=? AND event=? AND undone=1 AND compacted=0 ORDER BY sequence LIMIT 1`, id, string(core.EventTurnStarted)).Scan(&start)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if undo {
			return errors.New("nothing to undo")
		}
		return errors.New("nothing to redo")
	}
	if err != nil {
		return err
	}
	end := int64(1<<63 - 1)
	if !undo {
		_ = tx.QueryRow(`SELECT sequence FROM records WHERE session_id=? AND event=? AND undone=1 AND compacted=0 AND sequence>? ORDER BY sequence LIMIT 1`, id, string(core.EventTurnStarted), start).Scan(&end)
	}
	if err := createCheckpoint(tx, id, map[bool]string{true: "undo", false: "redo"}[undo]); err != nil {
		return err
	}
	if undo {
		_, err = tx.Exec(`UPDATE records SET undone=1 WHERE session_id=? AND sequence>=? AND undone=0 AND compacted=0`, id, start)
	} else {
		_, err = tx.Exec(`UPDATE records SET undone=0 WHERE session_id=? AND sequence>=? AND sequence<? AND undone=1 AND compacted=0`, id, start, end)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE sessions SET updated_at=?, status='idle' WHERE id=?`, now(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Compact(id, summary string) error {
	if !validID(id) {
		return fmt.Errorf("invalid session id")
	}
	if strings.TrimSpace(summary) == "" {
		return errors.New("compact summary is empty")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := createCheckpoint(tx, id, "compact"); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE records SET compacted=1 WHERE session_id=? AND undone=0 AND compacted=0`, id); err != nil {
		return err
	}
	timestamp := now()
	message := core.Message{Role: "context", Content: summary}
	if err := insertRecord(tx, id, Record{Type: "message", Timestamp: timestamp, Message: &message}); err != nil {
		return err
	}
	if err := insertRecord(tx, id, Record{Type: "event", Timestamp: timestamp, Event: "context.compacted", Data: map[string]any{"characters": len(summary)}}); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE sessions SET updated_at=?, status='idle' WHERE id=?`, timestamp, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func createCheckpoint(tx *sql.Tx, id, kind string) error {
	var maxSequence int64
	_ = tx.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM records WHERE session_id=? AND undone=0 AND compacted=0`, id).Scan(&maxSequence)
	data, _ := json.Marshal(map[string]any{"max_sequence": maxSequence})
	_, err := tx.Exec(`INSERT INTO checkpoints(session_id, created_at, kind, data_json) VALUES(?, ?, ?, ?)`, id, now(), kind, data)
	return err
}

func (s *Store) Export(id string) ([]byte, error) {
	messages, err := s.Messages(id)
	if err != nil {
		return nil, err
	}
	records, err := s.AllRecords(id)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"session_id": id, "messages": messages, "records": records, "backend": s.Backend()})
}

func (s *Store) recoverInterrupted() error {
	rows, err := s.db.Query(`SELECT id FROM sessions WHERE status='running'`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		var start int64
		err = tx.QueryRow(`SELECT sequence FROM records WHERE session_id=? AND event=? AND undone=0 AND compacted=0 ORDER BY sequence DESC LIMIT 1`, id, string(core.EventTurnStarted)).Scan(&start)
		if err == nil {
			if _, err = tx.Exec(`UPDATE records SET compacted=1 WHERE session_id=? AND sequence>=? AND undone=0`, id, start); err != nil {
				tx.Rollback()
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return err
		}
		timestamp := now()
		if err := insertRecord(tx, id, Record{Type: "event", Timestamp: timestamp, Event: "session.recovered", Data: map[string]any{"reason": "interrupted turn archived"}}); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`UPDATE sessions SET updated_at=?, status='interrupted' WHERE id=?`, timestamp, id); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.recovered++
	}
	return nil
}

func (s *Store) importLegacyJSONL() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		var importedLines int
		err := s.db.QueryRow(`SELECT line_count FROM legacy_imports WHERE path=?`, path).Scan(&importedLines)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := s.importLegacyFile(path, strings.TrimSuffix(entry.Name(), ".jsonl"), importedLines); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) importLegacyFile(path, id string, importedLines int) error {
	if !validID(id) {
		return fmt.Errorf("invalid legacy session id %q", id)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var records []Record
	lineCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lineCount++
		if lineCount <= importedLines {
			continue
		}
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("import legacy session %s: %w", path, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if lineCount < importedLines {
		return fmt.Errorf("legacy session %s shrank from %d to %d lines", path, importedLines, lineCount)
	}
	if lineCount == importedLines {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	timestamp := now()
	if len(records) > 0 && records[0].Timestamp != "" {
		timestamp = records[0].Timestamp
	}
	if err := ensureSession(tx, id, timestamp); err != nil {
		return err
	}
	for _, record := range records {
		if record.Timestamp == "" {
			record.Timestamp = timestamp
		}
		if err := insertRecord(tx, id, record); err != nil {
			return err
		}
		timestamp = record.Timestamp
	}
	status := legacySessionStatus(records)
	if _, err := tx.Exec(`UPDATE sessions SET updated_at=?, status=? WHERE id=?`, timestamp, status, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO legacy_imports(path, imported_at, line_count) VALUES(?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET imported_at=excluded.imported_at, line_count=excluded.line_count`, path, now(), lineCount); err != nil {
		return err
	}
	return tx.Commit()
}

func legacySessionStatus(records []Record) string {
	status := "idle"
	for _, record := range records {
		switch record.Event {
		case string(core.EventTurnStarted):
			status = "running"
		case string(core.EventTurnCompleted), string(core.EventTurnCancelled), string(core.EventTurnFailed):
			status = "idle"
		}
	}
	return status
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func validID(value string) bool {
	if value == "" || strings.Contains(value, "..") || strings.ContainsAny(value, `/\\`) {
		return false
	}
	return true
}
