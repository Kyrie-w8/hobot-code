package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bryant-w/hobot-code/sdk/go/hobot"
)

func TestBoardStoreRoundTrip(t *testing.T) {
	store := &boardStore{path: filepath.Join(t.TempDir(), "boards.json")}
	want := []Board{{
		ID: "00112233445566778899aabb", Name: "RDK S100", Host: "10.112.10.98", User: "root", Port: 22,
	}}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("board store mode = %o, want 600", got)
	}
	got, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("loaded boards = %+v, want %+v", got, want)
	}
}

func TestBoardStoreRejectsUnsafeInput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    os.FileMode
		want    string
	}{
		{name: "permissions", content: `[]`, mode: 0o644, want: "permissions"},
		{name: "invalid ID", content: `[{"id":"bad","name":"RDK","host":"rdk","user":"root","port":22}]`, mode: 0o600, want: "invalid board"},
		{name: "duplicate ID", content: `[{"id":"00112233445566778899aabb","name":"A"},{"id":"00112233445566778899aabb","name":"B"}]`, mode: 0o600, want: "duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "boards.json")
			if err := os.WriteFile(path, []byte(test.content), test.mode); err != nil {
				t.Fatal(err)
			}
			_, err := (&boardStore{path: path}).load()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("load error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestBoardStoreRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "boards.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (&boardStore{path: link}).load(); err == nil {
		t.Fatal("symlink board store was accepted")
	}
}

func TestSortedBoards(t *testing.T) {
	boards := sortedBoards(map[string]Board{
		"b": {ID: "b", Name: "RDK X5"},
		"a": {ID: "a", Name: "RDK S100"},
	})
	if len(boards) != 2 || boards[0].Name != "RDK S100" || boards[1].Name != "RDK X5" {
		t.Fatalf("unexpected sort order: %+v", boards)
	}
}

func TestSafeExternalURL(t *testing.T) {
	for _, input := range []string{"https://developer.d-robotics.cc/docs", "http://10.112.10.98:8000/health"} {
		if got, err := safeExternalURL(input); err != nil || got != input {
			t.Fatalf("safeExternalURL(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://user:secret@example.com", "/relative"} {
		if _, err := safeExternalURL(input); err == nil {
			t.Fatalf("unsafe URL was accepted: %q", input)
		}
	}
}

func TestStudioTaskIsLive(t *testing.T) {
	for _, status := range []string{"starting", "idle", "running", "waiting", "stopping"} {
		if !studioTaskIsLive(status) {
			t.Fatalf("status %q should be live", status)
		}
	}
	for _, status := range []string{"stopped", "failed", "interrupted"} {
		if studioTaskIsLive(status) {
			t.Fatalf("status %q should be terminal", status)
		}
	}
}

func TestStudioModelsOnlyExposeDRobotics(t *testing.T) {
	models := studioModels([]hobot.ModelOption{
		{Provider: "anthropic", ID: "claude-sonnet", Name: "Claude Sonnet"},
		{Provider: "drobotics", ID: "claude-sonnet", Name: "Claude via gateway"},
		{Provider: "drobotics", ID: "kimi-k3", Name: "kimi-k3"},
		{Provider: "drobotics", ID: "qwen3.8-max", Name: "qwen3.8-max"},
		{Provider: "drobotics", ID: "glm-5.2", Name: "glm-5.2"},
	})
	if len(models) != 3 || models[0].ID != "kimi-k3" || models[1].ID != "qwen3.8-max" || models[2].ID != "glm-5.2" {
		t.Fatalf("unexpected Studio models: %+v", models)
	}
}
