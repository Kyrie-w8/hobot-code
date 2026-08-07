package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesProviderAndBoardOverlays(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.json")
	provider := filepath.Join(dir, "provider.json")
	board := filepath.Join(dir, "board.json")
	mustWrite(t, base, `{"agent":{"name":"Test","max_steps":5},"provider":{"type":"mock","model":"m"},"security":{"workspace_root":"workspace"}}`)
	mustWrite(t, provider, `{"provider":{"type":"openai-compatible","model":"local","base_url":"http://localhost:8080/v1"}}`)
	mustWrite(t, board, `{"board":{"profile":"x5"}}`)
	cfg, err := Load(base, provider, board)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Type != "openai-compatible" || cfg.Provider.Model != "local" {
		t.Fatalf("provider overlay not applied: %+v", cfg.Provider)
	}
	if cfg.Board.Profile != "x5" {
		t.Fatalf("board overlay not applied: %+v", cfg.Board)
	}
	if cfg.Security.WorkspaceRoot != filepath.Join(dir, "workspace") {
		t.Fatalf("path resolved to %s", cfg.Security.WorkspaceRoot)
	}
}

func TestLoadRejectsInvalidStepLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	mustWrite(t, path, `{"agent":{"max_steps":0},"provider":{"type":"mock","model":"m"}}`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid step limit error")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
