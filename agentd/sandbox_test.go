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
		filepath.Join(home, ".config", "hobot-code"), filepath.Join(home, ".ssh"), filepath.Join(home, ".aws"),
		filepath.Join(root, "state", "agentd", "tasks", "00112233445566778899aabb"),
		filepath.Join(root, "state", "agentd", "attach-cursors"), filepath.Join(root, "state", "agentd", "support"),
		filepath.Join(root, "state", "sessions"), filepath.Join(root, "workspace"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".netrc"), []byte("machine example.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	cfg := config{
		ConfigRoot: filepath.Join(home, ".config", "hobot-code"),
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
	for _, sensitive := range []string{filepath.Join(home, ".config", "hobot-code"), filepath.Join(home, ".ssh"), filepath.Join(home, ".aws"), filepath.Join(home, ".netrc")} {
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

func TestForegroundSandboxReprotectsPrivateStateInsideBroadWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path assertions are Unix-specific")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configRoot := filepath.Join(home, ".config", "hobot-code")
	agentDir := filepath.Join(configRoot, "agent")
	stateRoot := filepath.Join(home, ".local", "state", "hobot-code")
	sessionDir := filepath.Join(stateRoot, "sessions")
	for _, path := range []string{agentDir, sessionDir, filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	cfg := config{
		ConfigRoot: configRoot, AgentDir: agentDir, StateRoot: stateRoot,
		AgentdRoot: filepath.Join(stateRoot, "agentd"), SessionDir: sessionDir,
		AgentBinary: "/usr/local/lib/hobot-code/hobot", SandboxBinary: "/usr/bin/bwrap",
	}
	if err := os.WriteFile(filepath.Join(home, ".netrc"), []byte("machine example.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command, args, err := foregroundSandboxCommand(cfg, home, sandboxModeWorkspace, []string{"--resume"})
	if err != nil {
		t.Fatal(err)
	}
	if command != cfg.SandboxBinary || args[len(args)-1] != "--resume" {
		t.Fatalf("unexpected foreground command: %s %v", command, args)
	}
	workspaceBind := sandboxArgumentIndex(args, "--bind", home, home)
	configReadOnly := sandboxArgumentIndex(args, "--ro-bind", configRoot, configRoot)
	stateReadOnly := sandboxArgumentIndex(args, "--ro-bind", stateRoot, stateRoot)
	agentWritable := sandboxArgumentIndex(args, "--bind", agentDir, agentDir)
	sessionWritable := sandboxArgumentIndex(args, "--bind", sessionDir, sessionDir)
	if workspaceBind < 0 || configReadOnly <= workspaceBind || stateReadOnly <= workspaceBind || agentWritable <= configReadOnly || sessionWritable <= stateReadOnly {
		t.Fatalf("private foreground mounts are not ordered safely: %v", args)
	}
	if sandboxArgumentIndex(args, "--dev-bind", "/dev", "/dev") >= 0 {
		t.Fatalf("foreground sandbox exposed the complete host device tree: %v", args)
	}
	if sandboxArgumentIndex(args, "--new-session") >= 0 {
		t.Fatalf("foreground sandbox detached the interactive TTY session: %v", args)
	}
	for _, sensitive := range []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".netrc")} {
		if sandboxArgumentIndex(args, "--ro-bind", sensitive, sensitive) <= workspaceBind {
			t.Fatalf("sensitive path was not re-protected after the home workspace bind: %s %v", sensitive, args)
		}
	}

	_, reviewArgs, err := foregroundSandboxCommand(cfg, home, sandboxModeReview, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sandboxArgumentIndex(reviewArgs, "--bind", home, home) >= 0 {
		t.Fatalf("review sandbox made the workspace writable: %v", reviewArgs)
	}
}

func TestForegroundSandboxHonorsExplicitMutableStateFiles(t *testing.T) {
	root := t.TempDir()
	cfg := config{
		ConfigRoot: filepath.Join(root, "config"), AgentDir: filepath.Join(root, "config", "agent"),
		StateRoot: filepath.Join(root, "state"), SessionDir: filepath.Join(root, "state", "sessions"),
		AgentBinary: "/usr/local/lib/hobot-code/hobot",
	}
	for _, path := range []string{cfg.AgentDir, cfg.SessionDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	external := filepath.Join(root, "managed", "audit", "hooks.jsonl")
	t.Setenv("HOBOT_CODE_HOOK_AUDIT", external)
	writable, err := foregroundSandboxWritableDirectories(cfg, root, sandboxModeSystem)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(writable, "\n"), filepath.Dir(external)) {
		t.Fatalf("custom mutable state path is missing: %v", writable)
	}
	t.Setenv("HOBOT_CODE_HOOK_AUDIT", "relative/hooks.jsonl")
	if _, err := foregroundSandboxWritableDirectories(cfg, root, sandboxModeSystem); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative mutable state override was accepted: %v", err)
	}
}

func TestForegroundSandboxRejectsProtectedWritableWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config{ConfigRoot: filepath.Join(home, ".config/hobot-code"), AgentDir: filepath.Join(home, ".config/hobot-code/agent"), StateRoot: filepath.Join(home, ".local/state/hobot-code"), SessionDir: filepath.Join(home, ".local/state/hobot-code/sessions")}
	for _, path := range []string{"/", "/etc", "/etc/systemd", "/usr/local", "/var/lib/hobot-code"} {
		if _, err := foregroundSandboxWritableDirectories(cfg, path, sandboxModeSystem); err == nil || (!strings.Contains(err.Error(), "protected system path") && !strings.Contains(err.Error(), "filesystem root")) {
			t.Fatalf("protected workspace %s was accepted: %v", path, err)
		}
	}
	if _, err := foregroundSandboxWritableDirectories(cfg, home, sandboxModeSystem); err != nil {
		t.Fatalf("home workspace should remain usable with sensitive paths re-protected: %v", err)
	}
	if _, err := foregroundSandboxWritableDirectories(cfg, home, sandboxModeReview); err != nil {
		t.Fatalf("read-only review of home should remain available: %v", err)
	}
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOBOT_CODE_HOOK_AUDIT", "/etc/hobot-code/hooks.jsonl")
	if _, err := foregroundSandboxWritableDirectories(cfg, project, sandboxModeSystem); err == nil || !strings.Contains(err.Error(), "protected system path") {
		t.Fatalf("protected custom state path was accepted: %v", err)
	}
}

func TestForegroundSandboxDefaultsAndFailClosedBehavior(t *testing.T) {
	mode, args, err := parseTUIArgs([]string{"--", "--resume", "session.jsonl"})
	if err != nil || mode != sandboxModeSystem || strings.Join(args, " ") != "--resume session.jsonl" {
		t.Fatalf("unexpected default TUI options: mode=%q args=%v err=%v", mode, args, err)
	}
	t.Setenv("HOBOT_CODE_TUI_SANDBOX", sandboxModeReview)
	mode, _, err = parseTUIArgs(nil)
	if err != nil || mode != sandboxModeReview {
		t.Fatalf("TUI sandbox environment override was ignored: mode=%q err=%v", mode, err)
	}
	if _, _, err := resolveForegroundSandbox(config{SandboxBinary: sandboxUnavailable}, sandboxModeSystem); err == nil || !strings.Contains(err.Error(), "install bubblewrap") {
		t.Fatalf("foreground sandbox did not fail closed without bubblewrap: %v", err)
	}
	mode, status, err := resolveForegroundSandbox(config{SandboxBinary: sandboxUnavailable}, sandboxModeOff)
	if err != nil || mode != sandboxModeOff || status.Backend != "none" {
		t.Fatalf("explicit foreground sandbox opt-out failed: mode=%q status=%+v err=%v", mode, status, err)
	}
}

func sandboxArgumentIndex(args []string, values ...string) int {
	for index := 0; index+len(values) <= len(args); index++ {
		match := true
		for offset, value := range values {
			if args[index+offset] != value {
				match = false
				break
			}
		}
		if match {
			return index
		}
	}
	return -1
}
