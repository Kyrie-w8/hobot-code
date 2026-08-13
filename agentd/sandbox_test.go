package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSandboxModeDefaultsAndLegacyTasks(t *testing.T) {
	if got := defaultSandboxMode("review", false); got != sandboxModeReview {
		t.Fatalf("review permissions selected %q sandbox", got)
	}
	if got := defaultSandboxMode("developer", false); got != sandboxModeWorkspace {
		t.Fatalf("developer permissions selected %q sandbox", got)
	}
	if got := defaultSandboxMode("ask", true); got != sandboxModeSystem {
		t.Fatalf("deployment selected %q sandbox", got)
	}
	mode, status := normalizePersistedSandbox("", taskSandboxStatus{}, "developer", false)
	if mode != sandboxModeOff || status.Backend != "none" || !strings.Contains(status.Reason, "legacy") {
		t.Fatalf("legacy task did not preserve its original behavior: %+v", status)
	}
}

func TestSandboxCommandBoundsWorkerStateAndDevices(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path assertions are Unix-specific")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, path := range []string{
		filepath.Join(home, ".config", "hobot-code"), filepath.Join(home, ".ssh"),
		filepath.Join(root, "state", "agentd", "tasks", "00112233445566778899aabb"),
		filepath.Join(root, "state", "agentd", "attach-cursors"), filepath.Join(root, "state", "agentd", "support"),
		filepath.Join(root, "state", "sessions"), filepath.Join(root, "workspace"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	cfg := config{
		StateRoot: filepath.Join(root, "state"), AgentdRoot: filepath.Join(root, "state", "agentd"),
		TasksRoot: filepath.Join(root, "state", "agentd", "tasks"), AttachCursorRoot: filepath.Join(root, "state", "agentd", "attach-cursors"),
		SupportRoot: filepath.Join(root, "state", "agentd", "support"), SessionDir: filepath.Join(root, "state", "sessions"),
		SocketPath: filepath.Join(root, "run", "agentd.sock"), AgentBinary: "/usr/local/lib/hobot-code/hobot", SandboxBinary: "/usr/bin/bwrap",
	}
	manager := taskManager{cfg: cfg}
	metadata := taskMetadata{ID: "00112233445566778899aabb", Cwd: filepath.Join(root, "workspace")}
	if err := os.MkdirAll(filepath.Join(cfg.SessionDir, metadata.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.SessionDir, metadata.ID, "policy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.SessionDir, metadata.ID, "policy", "permissions.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writable, err := manager.sandboxWritableDirectories(metadata, sandboxModeWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(writable, "\n")
	if !strings.Contains(joined, metadata.Cwd) || !strings.Contains(joined, filepath.Join(cfg.SessionDir, metadata.ID)) {
		t.Fatalf("task workspace/session missing from writable set: %v", writable)
	}
	if !strings.Contains(joined, filepath.Join(cfg.StateRoot, "side-agent-leases")) {
		t.Fatalf("side-agent concurrency registry missing from writable set: %v", writable)
	}
	if strings.Contains(joined, cfg.TasksRoot) || strings.Contains(joined, cfg.SessionDir+"\n") {
		t.Fatalf("shared task state became writable: %v", writable)
	}
	reviewWritable, err := manager.sandboxWritableDirectories(metadata, sandboxModeReview)
	if err != nil {
		t.Fatal(err)
	}
	reviewJoined := strings.Join(reviewWritable, "\n")
	if strings.Contains(reviewJoined, metadata.Cwd) || strings.Contains(reviewJoined, filepath.Join(cfg.StateRoot, "memory")) || strings.Contains(reviewJoined, filepath.Join(cfg.StateRoot, "goals")) {
		t.Fatalf("review sandbox exposes mutable developer state: %v", reviewWritable)
	}
	if !strings.Contains(reviewJoined, filepath.Join(cfg.StateRoot, "side-agent-leases")) {
		t.Fatalf("review sandbox cannot coordinate side-agent limits: %v", reviewWritable)
	}
	readonly := strings.Join(manager.sandboxReadOnlyPaths(), "\n")
	for _, sensitive := range []string{filepath.Join(home, ".config", "hobot-code"), filepath.Join(home, ".ssh")} {
		if !strings.Contains(readonly, sensitive) {
			t.Fatalf("sensitive path %s is not remounted read-only: %v", sensitive, readonly)
		}
	}
	if !pathIsDirectory(cfg.AgentdRoot) || !pathIsDirectory(cfg.SessionDir) {
		t.Fatal("agentd and session roots must be available for read-only remounts")
	}
}

func TestResolveSandboxFailsClosedOnLinuxBoard(t *testing.T) {
	manager := taskManager{cfg: config{SandboxBinary: sandboxUnavailable}}
	if _, _, err := manager.resolveTaskSandbox(sandboxModeWorkspace, "developer", false); err == nil || !strings.Contains(err.Error(), "install bubblewrap") {
		t.Fatalf("missing bubblewrap did not fail closed: %v", err)
	}
	mode, status, err := manager.resolveTaskSandbox(sandboxModeOff, "developer", false)
	if err != nil || mode != sandboxModeOff || status.Backend != "none" {
		t.Fatalf("explicit sandbox opt-out was not honored: mode=%q status=%+v err=%v", mode, status, err)
	}
}

func TestSandboxKeepsTemporaryRuntimePathsVisible(t *testing.T) {
	manager := taskManager{cfg: config{StateRoot: "/tmp/hobot-state", SessionDir: "/tmp/hobot-state/sessions", AgentBinary: "/tmp/fake-hobot"}}
	metadata := taskMetadata{Cwd: "/tmp/project"}
	if manager.canMaskTemporaryRoot("/tmp", metadata, []string{"/tmp/hobot-state/audit"}) {
		t.Fatal("temporary runtime paths would be hidden by the task sandbox")
	}
	manager.cfg = config{StateRoot: "/root/state", SessionDir: "/root/state/sessions", AgentBinary: "/usr/local/lib/hobot-code/hobot"}
	metadata.Cwd = "/root/project"
	if !manager.canMaskTemporaryRoot("/tmp", metadata, []string{"/root/state/audit"}) {
		t.Fatal("private temporary storage was not enabled for a normal board task")
	}
}
