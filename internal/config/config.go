package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	EnvFile  string         `json:"env_file"`
	Agent    AgentConfig    `json:"agent"`
	Provider ProviderConfig `json:"provider"`
	Board    BoardConfig    `json:"board"`
	Security SecurityConfig `json:"security"`
	Skills   SkillsConfig   `json:"skills"`
	Plugins  []PluginConfig `json:"plugins"`
	MCP      []MCPConfig    `json:"mcp_servers"`
	Session  SessionConfig  `json:"session"`
	Server   ServerConfig   `json:"server"`
	Raw      map[string]any `json:"-"`
}

type AgentConfig struct {
	Name             string   `json:"name"`
	SystemPrompt     string   `json:"system_prompt"`
	SystemPromptFile string   `json:"system_prompt_file"`
	MaxSteps         int      `json:"max_steps"`
	EnabledSkills    []string `json:"enabled_skills"`
}

type ProviderConfig struct {
	Type       string            `json:"type"`
	Model      string            `json:"model"`
	BaseURL    string            `json:"base_url"`
	APIKey     string            `json:"api_key"`
	APIKeyEnv  string            `json:"api_key_env"`
	AuthStyle  string            `json:"auth_style"`
	TimeoutSec int               `json:"timeout_seconds"`
	Headers    map[string]string `json:"headers"`
	Settings   map[string]any    `json:"settings"`
}

type BoardConfig struct {
	Profile string `json:"profile"`
	Prompt  string `json:"prompt"`
}

type SecurityConfig struct {
	WorkspaceRoot  string   `json:"workspace_root"`
	AllowedTools   []string `json:"allowed_tools"`
	DeniedTools    []string `json:"denied_tools"`
	ApprovalTools  []string `json:"approval_tools"`
	ApproveWrites  bool     `json:"approve_writes"`
	ToolTimeoutSec int      `json:"tool_timeout_seconds"`
	MaxToolOutput  int      `json:"max_tool_output_bytes"`
}

type SkillsConfig struct {
	Roots []string `json:"roots"`
}

type PluginConfig struct {
	Manifest string `json:"manifest"`
	Enabled  bool   `json:"enabled"`
}

type MCPConfig struct {
	Name            string            `json:"name"`
	Command         string            `json:"command"`
	Args            []string          `json:"args"`
	Env             map[string]string `json:"env"`
	ProtocolVersion string            `json:"protocol_version"`
	Enabled         bool              `json:"enabled"`
}

type SessionConfig struct {
	Dir string `json:"dir"`
}

type ServerConfig struct {
	Listen   string `json:"listen"`
	TokenEnv string `json:"token_env"`
}

func Defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Agent: AgentConfig{Name: "Aster", MaxSteps: 12},
		Provider: ProviderConfig{
			Type: "mock", Model: "offline-mock", TimeoutSec: 90,
			Settings: map[string]any{}, Headers: map[string]string{},
		},
		Board: BoardConfig{Profile: "auto"},
		Security: SecurityConfig{
			WorkspaceRoot: ".", AllowedTools: []string{"system_*", "fs_*"},
			DeniedTools: []string{"system_power_*"}, ApprovalTools: []string{"fs_write", "shell_exec"},
			ToolTimeoutSec: 20, MaxToolOutput: 131072,
		},
		Skills:  SkillsConfig{Roots: []string{"./skills", filepath.Join(home, ".config", "aster", "skills")}},
		Session: SessionConfig{Dir: filepath.Join(home, ".local", "share", "aster", "sessions")},
		Server:  ServerConfig{Listen: "127.0.0.1:7337", TokenEnv: "ASTER_SERVER_TOKEN"},
	}
}

func Load(path string, overlayPaths ...string) (Config, error) {
	cfg := Defaults()
	base := map[string]any{}
	if path != "" {
		var err error
		base, err = readObject(path)
		if err != nil {
			return Config{}, err
		}
	}
	for _, overlayPath := range overlayPaths {
		if overlayPath == "" {
			continue
		}
		overlay, err := readObject(overlayPath)
		if err != nil {
			return Config{}, err
		}
		merge(base, overlay)
	}
	if len(base) > 0 {
		b, _ := json.Marshal(base)
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
	}
	cfg.Raw = base
	if err := cfg.normalize(path); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(os.ExpandEnv(string(b))), &value); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return value, nil
}

func merge(dst, src map[string]any) {
	for key, value := range src {
		if sm, ok := value.(map[string]any); ok {
			if dm, ok := dst[key].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[key] = value
	}
}

func (c *Config) normalize(configPath string) error {
	if c.Agent.Name == "" {
		c.Agent.Name = "Aster"
	}
	if c.Agent.MaxSteps < 1 || c.Agent.MaxSteps > 64 {
		return errors.New("agent.max_steps must be between 1 and 64")
	}
	if c.Provider.Type == "" || c.Provider.Model == "" {
		return errors.New("provider.type and provider.model are required")
	}
	if c.Provider.TimeoutSec <= 0 {
		c.Provider.TimeoutSec = 90
	}
	if c.Security.ToolTimeoutSec <= 0 {
		c.Security.ToolTimeoutSec = 20
	}
	if c.Security.MaxToolOutput <= 0 {
		c.Security.MaxToolOutput = 131072
	}
	baseDir, _ := filepath.Abs(".")
	if configPath != "" {
		abs, err := filepath.Abs(configPath)
		if err == nil {
			baseDir = filepath.Dir(abs)
		}
	}
	c.EnvFile = resolve(baseDir, c.EnvFile)
	c.Agent.SystemPromptFile = resolve(baseDir, c.Agent.SystemPromptFile)
	c.Security.WorkspaceRoot = resolve(baseDir, c.Security.WorkspaceRoot)
	c.Session.Dir = resolve(baseDir, c.Session.Dir)
	for i := range c.Skills.Roots {
		c.Skills.Roots[i] = resolve(baseDir, c.Skills.Roots[i])
	}
	for i := range c.Plugins {
		c.Plugins[i].Manifest = resolve(baseDir, c.Plugins[i].Manifest)
	}
	envValues := map[string]string{}
	if c.EnvFile != "" {
		var err error
		envValues, err = readEnvFile(c.EnvFile)
		if err != nil {
			return err
		}
	}
	if c.Provider.APIKey == "" && c.Provider.APIKeyEnv != "" {
		if value, exists := os.LookupEnv(c.Provider.APIKeyEnv); exists {
			c.Provider.APIKey = value
		} else {
			c.Provider.APIKey = envValues[c.Provider.APIKeyEnv]
		}
	}
	return nil
}

func readEnvFile(path string) (map[string]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat env file %s: %w", path, err)
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("env file %s must not be accessible by group or others", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read env file %s: %w", path, err)
	}
	values := map[string]string{}
	for lineNumber, raw := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validEnvKey(key) {
			return nil, fmt.Errorf("invalid env file entry at %s:%d", path, lineNumber+1)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values, nil
}

func validEnvKey(value string) bool {
	if value == "" || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z') || value[0] == '_') {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !((value[i] >= 'A' && value[i] <= 'Z') || (value[i] >= 'a' && value[i] <= 'z') || (value[i] >= '0' && value[i] <= '9') || value[i] == '_') {
			return false
		}
	}
	return true
}

func resolve(base, value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "~/") {
		home, _ := os.UserHomeDir()
		value = filepath.Join(home, value[2:])
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(base, value))
}
