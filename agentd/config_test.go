package main

import (
	"path/filepath"
	"testing"
)

func TestConfigUsesBoundedStorageDefaultsAndOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
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
