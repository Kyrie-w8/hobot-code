package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type Record struct {
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	Message   *core.Message  `json:"message,omitempty"`
	Event     string         `json:"event,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
}

func (s *Store) AppendMessage(id string, message core.Message) error {
	return s.append(id, Record{Type: "message", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Message: &message})
}

func (s *Store) AppendEvent(id, event string, data map[string]any) error {
	return s.append(id, Record{Type: "event", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Event: event, Data: data})
}

func (s *Store) append(id string, record Record) error {
	if !validID(id) {
		return fmt.Errorf("invalid session id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, id+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(record)
}

func (s *Store) Messages(id string) ([]core.Message, error) {
	records, err := s.Records(id)
	if err != nil {
		return nil, err
	}
	var out []core.Message
	for _, record := range records {
		if record.Message != nil {
			out = append(out, *record.Message)
		}
	}
	return out, nil
}

func (s *Store) Records(id string) ([]Record, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid session id")
	}
	f, err := os.Open(filepath.Join(s.dir, id+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			ids = append(ids, strings.TrimSuffix(entry.Name(), ".jsonl"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

func (s *Store) Export(id string) ([]byte, error) {
	messages, err := s.Messages(id)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"session_id": id, "messages": messages})
}

func validID(value string) bool {
	if value == "" || strings.Contains(value, "..") || strings.ContainsAny(value, `/\\`) {
		return false
	}
	return true
}
