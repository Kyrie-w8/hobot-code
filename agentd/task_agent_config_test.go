package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskAgentConfigurationIsWritableAndCredentialScoped(t *testing.T) {
	cfg := testConfig(t)
	for name, content := range map[string]string{
		"settings.json": `{"extensions":["/usr/local/lib/hobot-code/extensions"]}`,
		"models.json":   `{"providers":{}}`,
		"auth.json":     `{"provider":{"token":"private-login-token"}}`,
	} {
		if err := os.WriteFile(filepath.Join(cfg.AgentDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	current := &task{
		manager:  &taskManager{cfg: cfg},
		metadata: taskMetadata{ID: "00112233445566778899aabb"},
	}

	directory, err := current.prepareTaskAgentConfiguration(networkModeModelOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"settings.json", "models.json"} {
		info, statErr := os.Stat(filepath.Join(directory, name))
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("runtime %s was not copied privately: info=%v err=%v", name, info, statErr)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("restricted worker received Pi login credentials: %v", err)
	}
	if err := os.Mkdir(filepath.Join(directory, "settings.json.lock"), 0o700); err != nil {
		t.Fatal(err)
	}

	directory, err = current.prepareTaskAgentConfiguration(networkModeShared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "auth.json")); err != nil {
		t.Fatalf("shared Pi login worker did not receive auth snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "settings.json.lock")); !os.IsNotExist(err) {
		t.Fatalf("stale Pi settings lock was preserved: %v", err)
	}

	if _, err := current.prepareTaskAgentConfiguration(networkModeOffline); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("offline worker retained Pi login credentials: %v", err)
	}
}

func TestTaskAgentConfigurationRejectsUnsafeSource(t *testing.T) {
	cfg := testConfig(t)
	outside := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cfg.AgentDir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	current := &task{
		manager:  &taskManager{cfg: cfg},
		metadata: taskMetadata{ID: "00112233445566778899aabb"},
	}
	if _, err := current.prepareTaskAgentConfiguration(networkModeModelOnly); err == nil {
		t.Fatal("task runtime accepted a symbolic-link settings source")
	}
}
