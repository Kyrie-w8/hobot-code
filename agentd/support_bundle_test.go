package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSupportBundleIsPrivateAndExcludesUserContent(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-private-token-from-prompt"
	current := &task{manager: manager, metadata: taskMetadata{
		ID: "00112233445566778899aabb", Name: "secret project", Cwd: "/root/private/project",
		Status: statusFailed, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		LastError: "HTTP 400 at /root/private/project using " + secret,
	}, subscribers: map[uint64]chan taskEvent{}}
	manager.tasks[current.metadata.ID] = current
	server := &daemonServer{cfg: cfg, manager: manager, started: time.Now().Add(-time.Minute)}
	bundle, err := server.createSupportBundle(true)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Path == "" || len(bundle.Content) == 0 || bundle.SHA256 == "" {
		t.Fatalf("incomplete bundle result: %+v", bundle)
	}
	info, err := os.Stat(bundle.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("bundle permissions: info=%v err=%v", info, err)
	}
	content := string(bundle.Content)
	for _, forbidden := range []string{secret, "secret project", "/root/private/project", current.metadata.ID} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("support bundle leaked %q", forbidden)
		}
	}
	var document supportBundleDocument
	if err := json.Unmarshal(bundle.Content, &document); err != nil {
		t.Fatal(err)
	}
	if document.System.Hostname != "[redacted]" || len(document.Tasks) != 1 || document.Tasks[0].ErrorFingerprint == "" {
		t.Fatalf("unexpected sanitized document: %+v", document)
	}
}

func TestSupportBundleRetentionIsBounded(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := &daemonServer{cfg: cfg, manager: manager, started: time.Now()}
	for index := 0; index < retainedSupportBundles+2; index++ {
		if _, err := server.createSupportBundle(false); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	entries, err := os.ReadDir(cfg.SupportRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != retainedSupportBundles {
		t.Fatalf("retained bundles = %d, want %d", len(entries), retainedSupportBundles)
	}
}

func TestSupportPathChecksDistinguishPrivateStateFromExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := dir + "/hobot"
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := pathCheck("agent-binary", executable, false, false, false, true); got.Status != "pass" {
		t.Fatalf("executable check = %+v", got)
	}
	if got := pathCheck("private-file", executable, false, true, true, false); got.Status != "fail" {
		t.Fatalf("private check accepted public permissions: %+v", got)
	}
}
