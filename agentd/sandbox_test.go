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
	network, status := normalizePersistedNetwork("", mode, status)
	if network != networkModeShared || status.NetworkRestricted {
		t.Fatalf("legacy task did not migrate to shared networking: mode=%q status=%+v", network, status)
	}
	network, status = normalizePersistedNetwork(networkModeOffline, sandboxModeOff, sandboxStatus(sandboxModeOff, "none", "disabled"))
	if network != networkModeShared || status.NetworkRestricted {
		t.Fatalf("invalid persisted offline-without-sandbox state was preserved: mode=%q status=%+v", network, status)
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "unrelated-xdg-config"))
	cfg := config{
		ConfigRoot: filepath.Join(home, ".config", "hobot-code"),
		StateRoot:  filepath.Join(root, "state"), AgentdRoot: filepath.Join(root, "state", "agentd"),
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

func TestSandboxCapabilityReportsOnlyEnforceableNetworkModes(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "bwrap-full")
	if err := os.WriteFile(full, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	capability := sandboxCapabilityStatus(config{SandboxBinary: full})
	if !capability.Available || !capability.Network || strings.Join(capability.NetworkModes, ",") != "shared,offline" {
		t.Fatalf("full sandbox capability was not reported: %+v", capability)
	}
	capability = sandboxCapabilityStatus(config{
		SandboxBinary: full, gatewayToken: "secret", DRoboticsBaseURL: defaultDroboticsBaseURL,
		ModelEgressSocket: filepath.Join(root, "egress", "model.sock"),
	})
	if strings.Join(capability.NetworkModes, ",") != "shared,model-only,offline" {
		t.Fatalf("configured model egress was not advertised: %+v", capability)
	}
	sharedOnly := filepath.Join(root, "bwrap-shared")
	if err := os.WriteFile(sharedOnly, []byte("#!/bin/sh\ncase \" $* \" in *\" --unshare-net \"*) exit 9;; esac\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	capability = sandboxCapabilityStatus(config{SandboxBinary: sharedOnly})
	if !capability.Available || capability.Network || strings.Join(capability.NetworkModes, ",") != "shared" || !strings.Contains(capability.Reason, "cannot create") {
		t.Fatalf("unsupported offline networking was advertised: %+v", capability)
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
	command, args, err := foregroundSandboxCommand(cfg, home, sandboxModeWorkspace, networkModeShared, []string{"--resume"})
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
	credentialFile := filepath.Join(configRoot, "hobot.env")
	if err := os.WriteFile(credentialFile, []byte("ANTHROPIC_AUTH_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, credentialArgs, err := foregroundSandboxCommand(cfg, home, sandboxModeSystem, networkModeShared, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	credentialMount := filepath.Join(stateRoot, "agentd", "credential")
	credentialPath := filepath.Join(credentialMount, "token")
	if sandboxArgumentIndex(credentialArgs, "--tmpfs", credentialMount) < 0 ||
		sandboxArgumentIndex(credentialArgs, "--file", gatewayTokenFDPlaceholder, credentialPath) < 0 ||
		sandboxArgumentIndex(credentialArgs, "--setenv", gatewayTokenFileEnvironment, credentialPath) < 0 ||
		sandboxArgumentIndex(credentialArgs, "--ro-bind", "/dev/null", credentialFile) < 0 {
		t.Fatalf("foreground credential transport or file masking is missing: %v", credentialArgs)
	}
	for _, sensitive := range []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".netrc")} {
		if sandboxArgumentIndex(args, "--ro-bind", sensitive, sensitive) <= workspaceBind {
			t.Fatalf("sensitive path was not re-protected after the home workspace bind: %s %v", sensitive, args)
		}
	}

	_, reviewArgs, err := foregroundSandboxCommand(cfg, home, sandboxModeReview, networkModeShared, nil)
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
	mode, network, args, err := parseTUIArgs([]string{"--", "--resume", "session.jsonl"})
	if err != nil || mode != sandboxModeSystem || network != networkModeShared || strings.Join(args, " ") != "--resume session.jsonl" {
		t.Fatalf("unexpected default TUI options: mode=%q network=%q args=%v err=%v", mode, network, args, err)
	}
	t.Setenv("HOBOT_CODE_TUI_SANDBOX", sandboxModeReview)
	t.Setenv("HOBOT_CODE_TUI_NETWORK", networkModeOffline)
	mode, network, _, err = parseTUIArgs(nil)
	if err != nil || mode != sandboxModeReview {
		t.Fatalf("TUI sandbox environment override was ignored: mode=%q err=%v", mode, err)
	}
	if network != networkModeOffline {
		t.Fatalf("TUI network environment override was ignored: %q", network)
	}
	if _, _, err := resolveForegroundSandbox(config{SandboxBinary: sandboxUnavailable}, sandboxModeSystem); err == nil || !strings.Contains(err.Error(), "install bubblewrap") {
		t.Fatalf("foreground sandbox did not fail closed without bubblewrap: %v", err)
	}
	mode, status, err := resolveForegroundSandbox(config{SandboxBinary: sandboxUnavailable}, sandboxModeOff)
	if err != nil || mode != sandboxModeOff || status.Backend != "none" {
		t.Fatalf("explicit foreground sandbox opt-out failed: mode=%q status=%+v err=%v", mode, status, err)
	}
}

func TestOfflineNetworkModeRequiresAndConfiguresBubblewrap(t *testing.T) {
	status := sandboxStatus(sandboxModeWorkspace, "bubblewrap", "shared")
	mode, status, err := resolveNetworkMode(networkModeOffline, sandboxModeWorkspace, status)
	if err != nil || mode != networkModeOffline || !status.NetworkRestricted {
		t.Fatalf("offline network mode was not resolved: mode=%q status=%+v err=%v", mode, status, err)
	}
	if _, _, err := resolveNetworkMode(networkModeOffline, sandboxModeOff, sandboxStatus(sandboxModeOff, "none", "disabled")); err == nil {
		t.Fatal("offline network mode was accepted without an OS sandbox")
	}
	home := t.TempDir()
	cfg := config{SandboxBinary: "/usr/bin/bwrap", AgentBinary: "/usr/bin/hobot-code", AgentDir: filepath.Join(home, "agent"), SessionDir: filepath.Join(home, "sessions"), ConfigRoot: filepath.Join(home, "config"), StateRoot: filepath.Join(home, "state")}
	_, args, err := foregroundSandboxCommand(cfg, home, sandboxModeWorkspace, networkModeOffline, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sandboxArgumentIndex(args, "--unshare-net") < 0 {
		t.Fatalf("offline foreground sandbox did not unshare networking: %v", args)
	}
}

func TestRestrictedForegroundNetworksExposeOnlyTheModelBroker(t *testing.T) {
	home := t.TempDir()
	egressRoot := filepath.Join(home, "run", "model-egress")
	if err := os.MkdirAll(egressRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	sandboxBinary := filepath.Join(home, "bwrap")
	if err := os.WriteFile(sandboxBinary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		SandboxBinary: sandboxBinary, AgentBinary: "/usr/bin/hobot-code",
		AgentDir: filepath.Join(home, "agent"), SessionDir: filepath.Join(home, "sessions"),
		ConfigRoot: filepath.Join(home, "config"), StateRoot: filepath.Join(home, "state"),
		AgentdRoot: filepath.Join(home, "state", "agentd"), ModelEgressRoot: egressRoot,
		ModelEgressSocket: filepath.Join(egressRoot, "model.sock"), DRoboticsBaseURL: defaultDroboticsBaseURL, gatewayCredential: "secret",
	}
	for _, path := range []string{cfg.AgentDir, cfg.SessionDir, cfg.ConfigRoot, cfg.StateRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, modelOnly, err := foregroundSandboxCommand(cfg, home, sandboxModeWorkspace, networkModeModelOnly, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if sandboxArgumentIndex(modelOnly, "--unshare-net") < 0 || sandboxArgumentIndex(modelOnly, "--ro-bind", egressRoot, egressRoot) < 0 {
		t.Fatalf("model-only sandbox omitted its network namespace or broker mount: %v", modelOnly)
	}
	if sandboxArgumentIndex(modelOnly, "--setenv", modelEgressSocketEnv, cfg.ModelEgressSocket) < 0 || sandboxArgumentIndex(modelOnly, "--setenv", modelEgressProvidersEnv, "drobotics") < 0 {
		t.Fatalf("model-only foreground worker omitted its broker capability: %v", modelOnly)
	}
	if sandboxArgumentIndex(modelOnly, "--file", gatewayTokenFDPlaceholder, gatewayCredentialFile(cfg)) >= 0 {
		t.Fatalf("model-only sandbox received a gateway credential: %v", modelOnly)
	}
	metadata := taskMetadata{
		ID: "00112233445566778899aabb", Cwd: home, SandboxMode: sandboxModeWorkspace,
		NetworkMode: networkModeModelOnly, Sandbox: sandboxStatus(sandboxModeWorkspace, "bubblewrap", "model-only"),
	}
	if err := os.MkdirAll(filepath.Join(cfg.SessionDir, metadata.ID, "policy"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, background, _, err := (&taskManager{cfg: cfg}).sandboxCommand(metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sandboxArgumentIndex(background, "--setenv", modelEgressSocketEnv, cfg.ModelEgressSocket) < 0 || sandboxArgumentIndex(background, "--setenv", modelEgressProvidersEnv, "drobotics") < 0 {
		t.Fatalf("model-only background worker omitted its broker capability: %v", background)
	}
	_, offline, err := foregroundSandboxCommand(cfg, home, sandboxModeWorkspace, networkModeOffline, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if sandboxArgumentIndex(offline, "--tmpfs", egressRoot) < 0 || sandboxArgumentIndex(offline, "--ro-bind", egressRoot, egressRoot) >= 0 {
		t.Fatalf("offline sandbox did not hide the model broker: %v", offline)
	}
}

func TestModelOnlyAcceptsOnlyConfiguredBrokerModels(t *testing.T) {
	root := t.TempDir()
	providerConfig := filepath.Join(root, "providers.json")
	if err := os.WriteFile(providerConfig, []byte(`{"schemaVersion":1,"providers":[{"id":"acme","baseUrl":"https://models.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ACME","models":[{"id":"coder"}]},{"id":"google","baseUrl":"https://models.example/v1","api":"google-generative-ai","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_GOOGLE","models":[{"id":"gemini"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	credential, err := encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, DRobotics: "secret", ProviderKeys: map[string]string{
		"HOBOT_CODE_PROVIDER_KEY_ACME": "acme-secret", "HOBOT_CODE_PROVIDER_KEY_GOOGLE": "google-secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager := taskManager{cfg: config{
		DRoboticsBaseURL: defaultDroboticsBaseURL, ManagedProviderConfig: providerConfig,
		gatewayCredential: credential, ModelEgressSocket: "/run/user/1000/hobot-code/model-egress/model.sock",
	}}
	manager.modelsOnce.Do(func() {})
	manager.models = map[string]modelOption{
		"drobotics/kimi-k3": {Provider: "drobotics", ID: "kimi-k3", Default: true},
		"acme/coder":        {Provider: "acme", ID: "coder", Managed: true},
		"acme/other":        {Provider: "acme", ID: "other", Managed: true},
		"google/gemini":     {Provider: "google", ID: "gemini", Managed: true},
	}
	if err := manager.validateNetworkModel(networkModeModelOnly, "drobotics/kimi-k3"); err != nil {
		t.Fatalf("D-Robotics model was rejected: %v", err)
	}
	if err := manager.validateNetworkModel(networkModeModelOnly, "acme/coder"); err != nil {
		t.Fatalf("supported managed model was rejected: %v", err)
	}
	for _, model := range []string{"acme/other", "google/gemini"} {
		if err := manager.validateNetworkModel(networkModeModelOnly, model); err == nil || !strings.Contains(err.Error(), "does not support") {
			t.Fatalf("unadapted model %s entered model-only mode: %v", model, err)
		}
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
