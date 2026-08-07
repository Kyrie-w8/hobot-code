package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type pluginManifest struct {
	Name    string                `json:"name"`
	Command string                `json:"command"`
	Args    []string              `json:"args"`
	Env     map[string]string     `json:"env"`
	Tools   []core.ToolDefinition `json:"tools"`
}

func RegisterPlugins(registry *Registry, configs []config.PluginConfig, timeoutSec int) error {
	for _, item := range configs {
		if !item.Enabled {
			continue
		}
		b, err := os.ReadFile(item.Manifest)
		if err != nil {
			return fmt.Errorf("read plugin manifest: %w", err)
		}
		var manifest pluginManifest
		if err := json.Unmarshal(b, &manifest); err != nil {
			return fmt.Errorf("parse plugin manifest: %w", err)
		}
		if manifest.Command == "" {
			return fmt.Errorf("plugin %q has no command", manifest.Name)
		}
		if !filepath.IsAbs(manifest.Command) {
			manifest.Command = filepath.Join(filepath.Dir(item.Manifest), manifest.Command)
		}
		for _, definition := range manifest.Tools {
			definition := definition
			if definition.Risk == "" {
				definition.Risk = "dangerous"
			}
			tool := core.Tool{Definition: definition, TimeoutSec: timeoutSec, Handler: pluginHandler(manifest, definition.Name)}
			if err := registry.Add(tool); err != nil {
				return err
			}
		}
	}
	return nil
}

func pluginHandler(manifest pluginManifest, toolName string) core.ToolHandler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		request := map[string]any{"jsonrpc": "2.0", "id": time.Now().UnixNano(), "method": "tools/call", "params": map[string]any{"name": toolName, "arguments": args}}
		in, _ := json.Marshal(request)
		in = append(in, '\n')
		cmd := exec.CommandContext(ctx, manifest.Command, manifest.Args...)
		cmd.Stdin = bytes.NewReader(in)
		cmd.Env = os.Environ()
		for key, value := range manifest.Env {
			cmd.Env = append(cmd.Env, key+"="+os.ExpandEnv(value))
		}
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", manifest.Name, err)
		}
		var response struct {
			Result any `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			return nil, fmt.Errorf("plugin returned invalid JSON-RPC: %w", err)
		}
		if response.Error != nil {
			return nil, fmt.Errorf("plugin error %d: %s", response.Error.Code, response.Error.Message)
		}
		return response.Result, nil
	}
}
