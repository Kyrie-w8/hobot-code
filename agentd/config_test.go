package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shortConfigTestDir(t *testing.T, prefix string) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func TestPrepareUserPathsCreatesAndTightensPrivateRoots(t *testing.T) {
	root := t.TempDir()
	cfg := config{
		ConfigRoot: filepath.Join(root, "config"),
		AgentDir:   filepath.Join(root, "config", "agent"),
		StateRoot:  filepath.Join(root, "state"),
		SessionDir: filepath.Join(root, "state", "sessions"),
	}
	if err := os.MkdirAll(cfg.StateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareUserPaths(cfg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cfg.ConfigRoot, cfg.AgentDir, cfg.StateRoot, cfg.SessionDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("private user path is unavailable: path=%s err=%v", path, err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("private user path was not tightened: path=%s mode=%v", path, info.Mode().Perm())
		}
	}
}

func TestNormalizeConfigFingerprint(t *testing.T) {
	upper := strings.Repeat("AB", 32)
	got, err := normalizeConfigFingerprint(upper)
	if err != nil || got != strings.ToLower(upper) {
		t.Fatalf("valid fingerprint was not normalized: got=%q err=%v", got, err)
	}
	for _, value := range []string{"short", strings.Repeat("z", 64)} {
		if _, err := normalizeConfigFingerprint(value); err == nil {
			t.Fatalf("invalid fingerprint was accepted: %q", value)
		}
	}
}

func TestManagedPrivateRootsRejectBroadPaths(t *testing.T) {
	home := "/home/developer"
	for _, path := range []string{"/", "/etc", "/root", home} {
		if safeManagedPrivateRoot(path, home) {
			t.Fatalf("broad private root was accepted: %s", path)
		}
	}
	for _, path := range []string{"/etc/hobot-code", "/data/hobot-code", home + "/.local/state/hobot-code"} {
		if !safeManagedPrivateRoot(path, home) {
			t.Fatalf("scoped private root was rejected: %s", path)
		}
	}
}

func TestConfigUsesBoundedStorageDefaultsAndOverrides(t *testing.T) {
	home := t.TempDir()
	socketRoot := shortConfigTestDir(t, "hobot-config-")
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("HOBOT_CODE_AGENTD_SOCKET", filepath.Join(socketRoot, "agentd.sock"))
	t.Setenv("HOBOT_CODE_AGENT_BINARY", "/usr/local/bin/hobot-test")
	t.Setenv("HOBOT_CODE_MAX_RETAINED_TASKS", "")
	t.Setenv("HOBOT_CODE_MAX_EVENT_MIB", "")
	defaults, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.MaxRetainedTasks != 100 || defaults.MaxEventSize != 16*1024*1024 {
		t.Fatalf("unexpected storage defaults: %+v", defaults)
	}

	t.Setenv("HOBOT_CODE_MAX_RETAINED_TASKS", "250")
	t.Setenv("HOBOT_CODE_MAX_EVENT_MIB", "32")
	overridden, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if overridden.MaxRetainedTasks != 250 || overridden.MaxEventSize != 32*1024*1024 {
		t.Fatalf("unexpected storage overrides: %+v", overridden)
	}

	t.Setenv("HOBOT_CODE_MAX_RETAINED_TASKS", "1001")
	t.Setenv("HOBOT_CODE_MAX_EVENT_MIB", "65")
	bounded, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if bounded.MaxRetainedTasks != 100 || bounded.MaxEventSize != 16*1024*1024 {
		t.Fatalf("out-of-range values did not fall back safely: %+v", bounded)
	}
}

func TestConfigFallsBackToPrivateStateSocketWithoutRuntimeDirectory(t *testing.T) {
	home := shortConfigTestDir(t, "hobot-state-")
	stateRoot := filepath.Join(home, "state")
	t.Setenv("HOME", home)
	t.Setenv("HOBOT_CODE_STATE_DIR", stateRoot)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOBOT_CODE_AGENTD_SOCKET", "")
	t.Setenv("HOBOT_CODE_AGENT_BINARY", "/usr/local/bin/hobot-test")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(stateRoot, "agentd", "run", "agentd.sock")
	if cfg.SocketPath != expected {
		t.Fatalf("agentd socket escaped the private state root: got=%q want=%q", cfg.SocketPath, expected)
	}
}

func TestConfigUsesAbsoluteRuntimeDirectoryWhenAvailable(t *testing.T) {
	home := t.TempDir()
	runtimeRoot := shortConfigTestDir(t, "hobot-runtime-")
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runtimeRoot)
	t.Setenv("HOBOT_CODE_AGENTD_SOCKET", "")
	t.Setenv("HOBOT_CODE_AGENT_BINARY", "/usr/local/bin/hobot-test")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(runtimeRoot, "hobot-code", "agentd.sock")
	if cfg.SocketPath != expected {
		t.Fatalf("agentd ignored XDG_RUNTIME_DIR: got=%q want=%q", cfg.SocketPath, expected)
	}
}

func TestConfigUsesOneCanonicalAgentDirectory(t *testing.T) {
	home := t.TempDir()
	socketRoot := shortConfigTestDir(t, "hobot-config-")
	configRoot := filepath.Join(home, "managed-config")
	agentDir := filepath.Join(home, "managed-agent")
	t.Setenv("HOME", home)
	t.Setenv("HOBOT_CODE_CONFIG_DIR", configRoot)
	t.Setenv("HOBOT_CODING_AGENT_DIR", agentDir)
	t.Setenv("HOBOT_CODE_AGENTD_SOCKET", filepath.Join(socketRoot, "agentd.sock"))
	t.Setenv("HOBOT_CODE_AGENT_BINARY", "/usr/local/bin/hobot-test")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigRoot != configRoot || cfg.AgentDir != agentDir {
		t.Fatalf("agent configuration paths diverged: %+v", cfg)
	}
}

func TestConfigUsesOneCanonicalManagedProviderConfiguration(t *testing.T) {
	home := t.TempDir()
	socketRoot := shortConfigTestDir(t, "hobot-config-")
	providerConfig := filepath.Join(home, "private", "providers.json")
	t.Setenv("HOME", home)
	t.Setenv("HOBOT_CODE_MANAGED_PROVIDER_CONFIG", providerConfig)
	t.Setenv("HOBOT_CODE_AGENTD_SOCKET", filepath.Join(socketRoot, "agentd.sock"))
	t.Setenv("HOBOT_CODE_AGENT_BINARY", "/usr/local/bin/hobot-test")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManagedProviderConfig != providerConfig || managedProviderConfigPath(cfg) != providerConfig {
		t.Fatalf("managed provider configuration paths diverged: %+v", cfg)
	}

	t.Setenv("HOBOT_CODE_MANAGED_PROVIDER_CONFIG", "relative/providers.json")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "must be an absolute path") {
		t.Fatalf("relative managed provider path was accepted: %v", err)
	}
}
