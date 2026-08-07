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

func TestLoadAPIKeyFromProtectedEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "aster.env")
	if err := os.WriteFile(envPath, []byte("SECRET_TOKEN=test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	mustWrite(t, path, `{"env_file":"aster.env","provider":{"type":"anthropic","model":"m","api_key_env":"SECRET_TOKEN"}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.APIKey != "test-secret" {
		t.Fatal("env file API key was not loaded")
	}
}

func TestLoadRejectsReadableEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "aster.env")
	if err := os.WriteFile(envPath, []byte("SECRET_TOKEN=test-secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	mustWrite(t, path, `{"env_file":"aster.env","provider":{"type":"anthropic","model":"m","api_key_env":"SECRET_TOKEN"}}`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected insecure env file rejection")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
