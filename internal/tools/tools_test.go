package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyrie-w8/aster-edge/internal/board"
	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
	"github.com/Kyrie-w8/aster-edge/internal/policy"
)

func TestFilesystemBoundaryAndApproval(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.Security.WorkspaceRoot = root
	cfg.Security.AllowedTools = []string{"fs_*"}
	cfg.Security.ApprovalTools = []string{"fs_write"}
	approved := false
	registry := New(policy.New(cfg.Security), func(context.Context, core.ToolCall, core.ToolDefinition) bool { return approved }, 4096)
	if err := RegisterBuiltins(registry, cfg, board.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	call := core.ToolCall{ID: "1", Name: "fs_write", Arguments: map[string]any{"path": "note.txt", "content": "hello"}}
	result := registry.Execute(context.Background(), call)
	if result.OK || !strings.Contains(result.Error, "approval") {
		t.Fatalf("expected approval denial: %+v", result)
	}
	approved = true
	result = registry.Execute(context.Background(), call)
	if !result.OK {
		t.Fatalf("write failed: %+v", result)
	}
	b, _ := os.ReadFile(filepath.Join(root, "note.txt"))
	if string(b) != "hello" {
		t.Fatalf("content=%q", b)
	}
	read := registry.Execute(context.Background(), core.ToolCall{ID: "2", Name: "fs_read", Arguments: map[string]any{"path": "../outside"}})
	if read.OK || !strings.Contains(read.Error, "outside workspace") {
		t.Fatalf("traversal was not denied: %+v", read)
	}
}

func TestShellOutputIsBounded(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.Security.WorkspaceRoot = root
	cfg.Security.AllowedTools = []string{"shell_exec"}
	cfg.Security.ApprovalTools = nil
	cfg.Security.ApproveWrites = true
	cfg.Security.MaxToolOutput = 32
	registry := New(policy.New(cfg.Security), func(context.Context, core.ToolCall, core.ToolDefinition) bool { return true }, 1024)
	if err := RegisterBuiltins(registry, cfg, board.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	result := registry.Execute(context.Background(), core.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": "printf 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'"}})
	if !result.OK {
		t.Fatalf("shell failed: %+v", result)
	}
	payload := result.Output.(map[string]any)
	if payload["output_truncated"] != true {
		t.Fatalf("expected truncation: %+v", payload)
	}
}
