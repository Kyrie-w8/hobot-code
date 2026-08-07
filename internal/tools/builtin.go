package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kyrie-w8/aster-edge/internal/board"
	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
)

func RegisterBuiltins(registry *Registry, cfg config.Config, snapshot board.Snapshot) error {
	root, err := filepath.Abs(cfg.Security.WorkspaceRoot)
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	timeout := cfg.Security.ToolTimeoutSec
	items := []core.Tool{
		{Definition: core.ToolDefinition{Name: "system_snapshot", Description: "Inspect the current board, OS, CPU, memory, disk, BPU, temperature, devices, and installed runtimes.", Parameters: objectSchema(nil, nil), Risk: "read"}, TimeoutSec: timeout, Handler: func(context.Context, map[string]any) (any, error) { return snapshot, nil }},
		{Definition: core.ToolDefinition{Name: "fs_list", Description: "List files under the configured workspace root.", Parameters: objectSchema(map[string]any{"path": map[string]any{"type": "string"}, "max_entries": map[string]any{"type": "integer"}}, []string{"path"}), Risk: "read"}, TimeoutSec: timeout, Handler: listHandler(root)},
		{Definition: core.ToolDefinition{Name: "fs_read", Description: "Read a text file under the configured workspace root.", Parameters: objectSchema(map[string]any{"path": map[string]any{"type": "string"}, "max_bytes": map[string]any{"type": "integer"}}, []string{"path"}), Risk: "read"}, TimeoutSec: timeout, Handler: readHandler(root)},
		{Definition: core.ToolDefinition{Name: "fs_write", Description: "Write a UTF-8 text file under the configured workspace root. Requires approval by default.", Parameters: objectSchema(map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, []string{"path", "content"}), Risk: "write"}, TimeoutSec: timeout, Handler: writeHandler(root)},
		{Definition: core.ToolDefinition{Name: "shell_exec", Description: "Run a bounded shell command inside the configured workspace. Requires approval by default.", Parameters: objectSchema(map[string]any{"command": map[string]any{"type": "string"}, "cwd": map[string]any{"type": "string"}}, []string{"command"}), Risk: "dangerous"}, TimeoutSec: timeout, Handler: shellHandler(root, cfg.Security.MaxToolOutput)},
	}
	for _, item := range items {
		if err := registry.Add(item); err != nil {
			return err
		}
	}
	return nil
}

func listHandler(root string) core.ToolHandler {
	return func(_ context.Context, args map[string]any) (any, error) {
		path, err := safePath(root, stringArg(args, "path"), false)
		if err != nil {
			return nil, err
		}
		max := intArg(args, "max_entries", 500)
		if max < 1 || max > 5000 {
			return nil, fmt.Errorf("max_entries must be between 1 and 5000")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		out := make([]map[string]any, 0, min(len(entries), max))
		for i, entry := range entries {
			if i >= max {
				break
			}
			info, _ := entry.Info()
			item := map[string]any{"name": entry.Name(), "directory": entry.IsDir()}
			if info != nil {
				item["size"] = info.Size()
				item["mode"] = info.Mode().String()
			}
			out = append(out, item)
		}
		return map[string]any{"path": path, "entries": out, "truncated": len(entries) > max}, nil
	}
}

func readHandler(root string) core.ToolHandler {
	return func(_ context.Context, args map[string]any) (any, error) {
		path, err := safePath(root, stringArg(args, "path"), false)
		if err != nil {
			return nil, err
		}
		max := intArg(args, "max_bytes", 131072)
		if max < 1 || max > 2*1024*1024 {
			return nil, fmt.Errorf("max_bytes out of range")
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, int64(max+1)))
		if err != nil {
			return nil, err
		}
		truncated := len(b) > max
		if truncated {
			b = b[:max]
		}
		return map[string]any{"path": path, "content": strings.ToValidUTF8(string(b), "�"), "truncated": truncated}, nil
	}
}

func writeHandler(root string) core.ToolHandler {
	return func(_ context.Context, args map[string]any) (any, error) {
		path, err := safePath(root, stringArg(args, "path"), true)
		if err != nil {
			return nil, err
		}
		content := stringArg(args, "content")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "bytes": len(content)}, nil
	}
}

func shellHandler(root string, maxOutput int) core.ToolHandler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		command := stringArg(args, "command")
		if strings.TrimSpace(command) == "" {
			return nil, fmt.Errorf("command is empty")
		}
		cwd := root
		if value := stringArg(args, "cwd"); value != "" {
			var err error
			cwd, err = safePath(root, value, false)
			if err != nil {
				return nil, err
			}
		}
		cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
		cmd.Dir = cwd
		stdout, stderr := &limitedBuffer{max: maxOutput}, &limitedBuffer{max: maxOutput}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		waitErr := cmd.Run()
		exitCode := 0
		if waitErr != nil {
			if e, ok := waitErr.(*exec.ExitError); ok {
				exitCode = e.ExitCode()
			} else {
				return nil, waitErr
			}
		}
		return map[string]any{"command": command, "cwd": cwd, "exit_code": exitCode, "stdout": stdout.String(), "stderr": stderr.String(), "output_truncated": stdout.truncated || stderr.truncated}, nil
	}
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.max - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		b.truncated = true
		return original, nil
	}
	_, _ = b.buffer.Write(p)
	return original, nil
}

func (b *limitedBuffer) String() string { return strings.ToValidUTF8(b.buffer.String(), "�") }

func safePath(root, value string, allowMissing bool) (string, error) {
	if value == "" {
		value = "."
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	target, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", err
	}
	lexicalRel, lexicalErr := filepath.Rel(root, target)
	if lexicalErr != nil || lexicalRel == ".." || strings.HasPrefix(lexicalRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside workspace root")
	}
	check := target
	if allowMissing {
		for {
			if _, err := os.Lstat(check); err == nil {
				break
			}
			parent := filepath.Dir(check)
			if parent == check {
				return "", fmt.Errorf("no existing path ancestor")
			}
			check = parent
		}
	}
	resolved, err := filepath.EvalSymlinks(check)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside workspace root")
	}
	if check != target {
		suffix, err := filepath.Rel(check, target)
		if err != nil {
			return "", err
		}
		target = filepath.Join(resolved, suffix)
	} else {
		target = resolved
	}
	return target, nil
}

func stringArg(args map[string]any, key string) string { v, _ := args[key].(string); return v }
func intArg(args map[string]any, key string, fallback int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return fallback
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
