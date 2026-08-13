package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	defaultMaxTasks      = 2
	maximumMaxTasks      = 8
	defaultRetainedTasks = 100
	maximumRetainedTasks = 1000
	defaultMaxEventMiB   = 16
	maximumMaxEventMiB   = 64
)

type config struct {
	ConfigRoot        string
	AgentDir          string
	StateRoot         string
	AgentdRoot        string
	TasksRoot         string
	WorktreesRoot     string
	AttachCursorRoot  string
	SupportRoot       string
	SessionDir        string
	SocketPath        string
	PIDPath           string
	LogPath           string
	AgentBinary       string
	ExtensionCatalog  string
	SandboxBinary     string
	ConfigFingerprint string
	MaxTasks          int
	MaxRetainedTasks  int
	MaxEventSize      int64
}

func loadConfig() (config, error) {
	home := os.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return config{}, fmt.Errorf("HOME must be an absolute path")
	}
	configRoot := os.Getenv("HOBOT_CODE_CONFIG_DIR")
	if configRoot == "" {
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		configRoot = filepath.Join(configHome, "hobot-code")
	}
	if !filepath.IsAbs(configRoot) {
		return config{}, fmt.Errorf("HOBOT_CODE_CONFIG_DIR must be an absolute path")
	}
	agentDir := os.Getenv("HOBOT_CODING_AGENT_DIR")
	if agentDir == "" {
		agentDir = filepath.Join(configRoot, "agent")
	}
	if !filepath.IsAbs(agentDir) {
		return config{}, fmt.Errorf("HOBOT_CODING_AGENT_DIR must be an absolute path")
	}
	stateRoot := os.Getenv("HOBOT_CODE_STATE_DIR")
	if stateRoot == "" {
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			stateHome = filepath.Join(home, ".local", "state")
		}
		stateRoot = filepath.Join(stateHome, "hobot-code")
	}
	if !filepath.IsAbs(stateRoot) {
		return config{}, fmt.Errorf("HOBOT_CODE_STATE_DIR must be an absolute path")
	}
	sessionDir := os.Getenv("HOBOT_CODING_AGENT_SESSION_DIR")
	if sessionDir == "" {
		sessionDir = filepath.Join(stateRoot, "sessions")
	}
	if !filepath.IsAbs(sessionDir) {
		return config{}, fmt.Errorf("HOBOT_CODING_AGENT_SESSION_DIR must be an absolute path")
	}

	agentdRoot := filepath.Join(stateRoot, "agentd")
	socketRoot := os.Getenv("XDG_RUNTIME_DIR")
	if socketRoot != "" && filepath.IsAbs(socketRoot) {
		socketRoot = filepath.Join(socketRoot, "hobot-code")
	} else {
		socketRoot = filepath.Join(os.TempDir(), fmt.Sprintf("hobot-code-agentd-%d", os.Getuid()))
	}
	socketPath := os.Getenv("HOBOT_CODE_AGENTD_SOCKET")
	if socketPath == "" {
		socketPath = filepath.Join(socketRoot, "agentd.sock")
	}
	if !filepath.IsAbs(socketPath) {
		return config{}, fmt.Errorf("HOBOT_CODE_AGENTD_SOCKET must be an absolute path")
	}
	if len(socketPath) > 100 {
		return config{}, fmt.Errorf("agentd socket path is too long: %s", socketPath)
	}

	agentBinary := os.Getenv("HOBOT_CODE_AGENT_BINARY")
	if agentBinary == "" {
		agentBinary = "/usr/local/lib/hobot-code/hobot"
	}
	if !filepath.IsAbs(agentBinary) {
		return config{}, fmt.Errorf("HOBOT_CODE_AGENT_BINARY must be an absolute path")
	}
	extensionCatalog := os.Getenv("HOBOT_CODE_EXTENSION_CATALOG")
	if extensionCatalog == "" {
		extensionCatalog = "/usr/local/lib/hobot-code/extensions/catalog.json"
	}
	if !filepath.IsAbs(extensionCatalog) {
		return config{}, fmt.Errorf("HOBOT_CODE_EXTENSION_CATALOG must be an absolute path")
	}
	sandboxBinary, err := configuredSandboxBinary(os.Getenv("HOBOT_CODE_BWRAP"))
	if err != nil {
		return config{}, err
	}
	maxTasks := boundedInteger(os.Getenv("HOBOT_CODE_MAX_BACKGROUND_TASKS"), defaultMaxTasks, 1, maximumMaxTasks)
	maxRetainedTasks := boundedInteger(os.Getenv("HOBOT_CODE_MAX_RETAINED_TASKS"), defaultRetainedTasks, 10, maximumRetainedTasks)
	maxEventMiB := boundedInteger(os.Getenv("HOBOT_CODE_MAX_EVENT_MIB"), defaultMaxEventMiB, 1, maximumMaxEventMiB)
	configFingerprint, err := normalizeConfigFingerprint(os.Getenv("HOBOT_CODE_CONFIG_FINGERPRINT"))
	if err != nil {
		return config{}, err
	}

	return config{
		ConfigRoot:        filepath.Clean(configRoot),
		AgentDir:          filepath.Clean(agentDir),
		StateRoot:         filepath.Clean(stateRoot),
		AgentdRoot:        filepath.Clean(agentdRoot),
		TasksRoot:         filepath.Join(agentdRoot, "tasks"),
		WorktreesRoot:     filepath.Join(agentdRoot, "worktrees"),
		AttachCursorRoot:  filepath.Join(agentdRoot, "attach-cursors"),
		SupportRoot:       filepath.Join(agentdRoot, "support"),
		SessionDir:        filepath.Clean(sessionDir),
		SocketPath:        filepath.Clean(socketPath),
		PIDPath:           filepath.Join(agentdRoot, "agentd.pid"),
		LogPath:           filepath.Join(agentdRoot, "agentd.log"),
		AgentBinary:       filepath.Clean(agentBinary),
		ExtensionCatalog:  filepath.Clean(extensionCatalog),
		SandboxBinary:     sandboxBinary,
		ConfigFingerprint: configFingerprint,
		MaxTasks:          maxTasks,
		MaxRetainedTasks:  maxRetainedTasks,
		MaxEventSize:      int64(maxEventMiB) * 1024 * 1024,
	}, nil
}

func normalizeConfigFingerprint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) != 64 {
		return "", fmt.Errorf("HOBOT_CODE_CONFIG_FINGERPRINT must be a 64-character SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("HOBOT_CODE_CONFIG_FINGERPRINT must be a 64-character SHA-256 hex digest")
	}
	return strings.ToLower(value), nil
}

func boundedInteger(value string, fallback, minimum, maximum int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return fallback
	}
	return parsed
}

func ensurePrivateDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("private path must be a directory and not a symbolic link: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("private path is owned by uid %d, expected %d: %s", stat.Uid, os.Getuid(), path)
	}
	return os.Chmod(path, 0o700)
}

func preparePaths(cfg config) error {
	for _, path := range []string{cfg.StateRoot, cfg.AgentdRoot, cfg.TasksRoot, cfg.WorktreesRoot, cfg.AttachCursorRoot, cfg.SupportRoot, cfg.SessionDir, filepath.Dir(cfg.SocketPath)} {
		if err := ensurePrivateDir(path); err != nil {
			return err
		}
	}
	return nil
}
