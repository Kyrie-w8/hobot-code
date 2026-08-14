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
	"time"
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
	ConfigRoot            string
	AgentDir              string
	StateRoot             string
	AgentdRoot            string
	TasksRoot             string
	WorktreesRoot         string
	AttachCursorRoot      string
	SupportRoot           string
	QualificationPath     string
	SessionDir            string
	SocketPath            string
	ModelEgressRoot       string
	ModelEgressSocket     string
	DRoboticsBaseURL      string
	ModelEgressTimeout    time.Duration
	PIDPath               string
	LogPath               string
	AgentBinary           string
	ExtensionCatalog      string
	ManagedProviderConfig string
	SandboxBinary         string
	ConfigFingerprint     string
	MaxTasks              int
	MaxRetainedTasks      int
	MaxEventSize          int64
	gatewayToken          string
	gatewayCredential     string
	modelEgressRoutes     map[string]modelEgressRoute
}

func loadConfig() (config, error) {
	gatewayCredentials, err := loadGatewayCredentials()
	if err != nil {
		return config{}, err
	}
	gatewayCredential, err := encodeGatewayCredentialBundle(gatewayCredentials)
	if err != nil {
		return config{}, err
	}
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
	managedProviderConfig := os.Getenv("HOBOT_CODE_MANAGED_PROVIDER_CONFIG")
	if managedProviderConfig == "" {
		managedProviderConfig = filepath.Join(agentDir, "providers.json")
	}
	if !filepath.IsAbs(managedProviderConfig) {
		return config{}, fmt.Errorf("HOBOT_CODE_MANAGED_PROVIDER_CONFIG must be an absolute path")
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
	for _, root := range []struct {
		name string
		path string
	}{{"HOBOT_CODE_CONFIG_DIR", configRoot}, {"HOBOT_CODING_AGENT_DIR", agentDir}, {"HOBOT_CODE_STATE_DIR", stateRoot}, {"HOBOT_CODING_AGENT_SESSION_DIR", sessionDir}} {
		if !safeManagedPrivateRoot(root.path, home) {
			return config{}, fmt.Errorf("%s must identify a scoped private directory, not a broad system or home root", root.name)
		}
	}

	agentdRoot := filepath.Join(stateRoot, "agentd")
	socketRoot := os.Getenv("XDG_RUNTIME_DIR")
	if socketRoot != "" && filepath.IsAbs(socketRoot) {
		socketRoot = filepath.Join(socketRoot, "hobot-code")
	} else {
		// Some RDK images make /tmp private to root. Keep the per-user daemon
		// reachable without relying on a login manager to set XDG_RUNTIME_DIR.
		socketRoot = filepath.Join(agentdRoot, "run")
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
	modelEgressRoot := filepath.Join(filepath.Dir(socketPath), "model")
	modelEgressSocket := filepath.Join(modelEgressRoot, "s")
	if len(modelEgressSocket) > 100 {
		return config{}, fmt.Errorf("model egress socket path is too long: %s", modelEgressSocket)
	}
	droboticsBaseURL, err := normalizeModelEgressBaseURL(os.Getenv("ANTHROPIC_BASE_URL"))
	if err != nil {
		return config{}, err
	}
	modelEgressTimeout := time.Duration(boundedInteger(os.Getenv("API_TIMEOUT_MS"), 3_000_000, 1_000, 3_600_000)) * time.Millisecond

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

	result := config{
		ConfigRoot:            filepath.Clean(configRoot),
		AgentDir:              filepath.Clean(agentDir),
		StateRoot:             filepath.Clean(stateRoot),
		AgentdRoot:            filepath.Clean(agentdRoot),
		TasksRoot:             filepath.Join(agentdRoot, "tasks"),
		WorktreesRoot:         filepath.Join(agentdRoot, "worktrees"),
		AttachCursorRoot:      filepath.Join(agentdRoot, "attach-cursors"),
		SupportRoot:           filepath.Join(agentdRoot, "support"),
		QualificationPath:     filepath.Join(agentdRoot, "model-qualification.json"),
		SessionDir:            filepath.Clean(sessionDir),
		SocketPath:            filepath.Clean(socketPath),
		ModelEgressRoot:       filepath.Clean(modelEgressRoot),
		ModelEgressSocket:     filepath.Clean(modelEgressSocket),
		DRoboticsBaseURL:      droboticsBaseURL,
		ModelEgressTimeout:    modelEgressTimeout,
		PIDPath:               filepath.Join(agentdRoot, "agentd.pid"),
		LogPath:               filepath.Join(agentdRoot, "agentd.log"),
		AgentBinary:           filepath.Clean(agentBinary),
		ExtensionCatalog:      filepath.Clean(extensionCatalog),
		ManagedProviderConfig: filepath.Clean(managedProviderConfig),
		SandboxBinary:         sandboxBinary,
		ConfigFingerprint:     configFingerprint,
		MaxTasks:              maxTasks,
		MaxRetainedTasks:      maxRetainedTasks,
		MaxEventSize:          int64(maxEventMiB) * 1024 * 1024,
		gatewayToken:          gatewayCredentials.DRobotics,
		gatewayCredential:     gatewayCredential,
	}
	result.modelEgressRoutes, err = buildModelEgressRoutes(result)
	if err != nil {
		return config{}, fmt.Errorf("load model egress routes: %w", err)
	}
	return result, nil
}

func safeManagedPrivateRoot(path, home string) bool {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == string(filepath.Separator) || path == filepath.Clean(home) {
		return false
	}
	for _, protected := range []string{"/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib64", "/mnt", "/opt", "/proc", "/root", "/run", "/sbin", "/srv", "/sys", "/tmp", "/usr", "/var"} {
		if path == protected {
			return false
		}
	}
	return true
}

func gatewayCredentialPayload(cfg config) string {
	if cfg.gatewayCredential != "" {
		return cfg.gatewayCredential
	}
	payload, _ := encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, DRobotics: cfg.gatewayToken})
	return payload
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

func prepareUserPaths(cfg config) error {
	for _, path := range []string{cfg.ConfigRoot, cfg.AgentDir, cfg.StateRoot, cfg.SessionDir} {
		if err := ensurePrivateDir(path); err != nil {
			return err
		}
	}
	return nil
}

func preparePaths(cfg config) error {
	if err := prepareUserPaths(cfg); err != nil {
		return err
	}
	paths := []string{cfg.StateRoot, cfg.AgentdRoot, cfg.TasksRoot, cfg.WorktreesRoot, cfg.AttachCursorRoot, cfg.SupportRoot, cfg.SessionDir, gatewayCredentialDirectory(cfg), filepath.Dir(cfg.SocketPath)}
	if cfg.ModelEgressRoot != "" {
		paths = append(paths, cfg.ModelEgressRoot)
	}
	for _, path := range paths {
		if err := ensurePrivateDir(path); err != nil {
			return err
		}
	}
	return nil
}
