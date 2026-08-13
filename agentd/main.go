package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var version = "dev"
var releaseMarker = "HOBOT_CODE_AGENTD_VERSION=dev;"

func usage() {
	fmt.Fprintln(os.Stderr, `Hobot Code background task service

Usage:
  hobot tui [--sandbox review|workspace|system|off] [-- PI_OPTIONS...]
  hobot daemon start|status
  hobot daemon stop|restart [--force]
  hobot bridge --stdio
  hobot diagnose [--json]
  hobot extensions [--json]
  hobot model check [--force] [--json] PROVIDER/MODEL
  hobot model verify [--force] [--json] PROVIDER/MODEL
  hobot deploy inspect [--cwd DIR]
  hobot deploy start [--cwd DIR] [--goal deploy-and-validate|benchmark] [--profile PROFILE] [--name NAME] [--model PROVIDER/MODEL] [--permissions ask|developer] [--sandbox system|off] ARTIFACT
  hobot deploy status TASK_ID
  hobot workspace inspect [DIR]
  hobot workspace list
  hobot workspace writes
  hobot workspace delivery TASK_ID
  hobot workspace apply TASK_ID --yes
  hobot workspace cleanup TASK_ID --yes
  hobot task start [--name NAME] [--cwd DIR] [--workspace shared|worktree] [--model PROVIDER/MODEL] [--permissions review|ask|developer] [--sandbox review|workspace|system|off] [--trust-project] -- PROMPT
  hobot task list [--all]
  hobot task show TASK_ID [--details]
  hobot task logs TASK_ID [--after SEQUENCE] [--follow]
  hobot task attach TASK_ID [--after SEQUENCE | --replay-all]
  hobot task send TASK_ID [--] PROMPT
  hobot task abort TASK_ID
  hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE
  hobot task approvals TASK_ID [--details]
  hobot task resume TASK_ID [-- PROMPT]
  hobot task restart TASK_ID [--] PROMPT
  hobot task rename TASK_ID NAME
  hobot task model TASK_ID PROVIDER/MODEL
  hobot task permissions TASK_ID review|ask|developer
  hobot task sandbox TASK_ID review|workspace|system|off
  hobot task archive|unarchive TASK_ID
  hobot task delete TASK_ID --yes
  hobot task stop TASK_ID`)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a daemon or task command is required")
	}
	switch args[0] {
	case "serve":
		return runServer(cfg)
	case "tui":
		return runTUICLI(cfg, args[1:])
	case "daemon":
		return runDaemonCLI(cfg, args[1:])
	case "task":
		return runTaskCLI(cfg, args[1:])
	case "workspace":
		return runWorkspaceCLI(cfg, args[1:])
	case "deploy":
		return runDeploymentCLI(cfg, args[1:])
	case "diagnose":
		return runDiagnoseCLI(cfg, args[1:])
	case "extensions":
		return runExtensionsCLI(cfg, args[1:])
	case "model":
		return runModelCLI(cfg, args[1:])
	case "bridge":
		if len(args) != 2 || args[1] != "--stdio" {
			return fmt.Errorf("usage: hobot bridge --stdio")
		}
		return runStdioBridge(cfg)
	case "version", "--version", "-v":
		if releaseMarker == "" {
			return fmt.Errorf("agentd release marker is missing")
		}
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func parseTUIArgs(args []string) (string, []string, error) {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	defaultMode := strings.TrimSpace(os.Getenv("HOBOT_CODE_TUI_SANDBOX"))
	if defaultMode == "" {
		defaultMode = sandboxModeSystem
	}
	sandbox := flags.String("sandbox", defaultMode, "OS sandbox: review, workspace, system, or off")
	if err := flags.Parse(args); err != nil {
		return "", nil, err
	}
	mode, err := normalizeSandboxMode(*sandbox)
	if err != nil {
		return "", nil, err
	}
	return mode, flags.Args(), nil
}

func runTUICLI(cfg config, args []string) error {
	requested, agentArgs, err := parseTUIArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve TUI working directory: %w", err)
	}
	mode, status, err := resolveForegroundSandbox(cfg, requested)
	if err != nil {
		return err
	}
	commandName := cfg.AgentBinary
	commandArgs := agentArgs
	if mode != sandboxModeOff {
		if err := probeSandboxBackend(cfg.SandboxBinary); err != nil {
			return fmt.Errorf("OS sandbox self-test failed: %w; fix bubblewrap or explicitly choose --sandbox off", err)
		}
		commandName, commandArgs, err = foregroundSandboxCommand(cfg, workingDirectory, mode, agentArgs)
		if err != nil {
			return err
		}
	}
	environment := append(os.Environ(),
		"HOBOT_CODE_SANDBOX_SCOPE=foreground",
		"HOBOT_CODE_SANDBOX_MODE="+mode,
		"HOBOT_CODE_SANDBOX_BACKEND="+status.Backend,
	)
	return replaceProcess(commandName, commandArgs, environment)
}

func runExtensionsCLI(cfg config, args []string) error {
	flags := flag.NewFlagSet("extensions", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "print the extension catalog as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: hobot extensions [--json]")
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	result, err := client.call("extensions.list", struct{}{})
	if err != nil {
		return err
	}
	var catalog extensionCatalog
	if err := json.Unmarshal(result, &catalog); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(catalog)
	}
	fmt.Printf("Hobot Code extensions (%d)\n", len(catalog.Entries))
	for _, entry := range catalog.Entries {
		state := "available"
		if entry.Required {
			state = "required"
		} else if entry.DefaultEnabled {
			state = "enabled"
		}
		fmt.Printf("%-34s %-10s %-9s %s\n", entry.ID, entry.Kind, state, entry.Name)
	}
	fmt.Println("\nThis catalog is read-only. Tool permissions and execution remain enforced on the board.")
	return nil
}

func runWorkspaceCLI(cfg config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hobot workspace inspect [DIR] | list | writes | delivery TASK_ID | apply TASK_ID --yes | cleanup TASK_ID --yes")
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	switch args[0] {
	case "inspect":
		if len(args) > 2 {
			return fmt.Errorf("usage: hobot workspace inspect [DIR]")
		}
		path := ""
		if len(args) == 2 {
			path = args[1]
		} else {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		result, err := client.call("workspace.isolation", workspaceIsolationParams{Path: path})
		if err != nil {
			return err
		}
		var inspection workspaceIsolation
		if err := json.Unmarshal(result, &inspection); err != nil {
			return err
		}
		fmt.Printf("Recommended mode: %s\n", inspection.RecommendedMode)
		fmt.Printf("Detail: %s\n", inspection.Reason)
		if inspection.RepositoryRoot != "" {
			fmt.Printf("Repository: %s\n", inspection.RepositoryRoot)
			fmt.Printf("HEAD: %s\n", inspection.Head)
		}
		return nil
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: hobot workspace list")
		}
		result, err := client.call("workspace.worktrees", struct{}{})
		if err != nil {
			return err
		}
		var worktrees managedWorktreeList
		if err := json.Unmarshal(result, &worktrees); err != nil {
			return err
		}
		if len(worktrees.Worktrees) == 0 {
			fmt.Println("No managed task workspaces.")
			return nil
		}
		for _, workspace := range worktrees.Worktrees {
			state := "retained"
			if workspace.InUse {
				state = "in use"
			}
			fmt.Printf("%s  %s  %s\n", workspace.TaskID, state, workspace.ProjectCwd)
		}
		return nil
	case "cleanup":
		if len(args) != 3 || args[2] != "--yes" {
			return fmt.Errorf("usage: hobot workspace cleanup TASK_ID --yes")
		}
		result, err := client.call("workspace.cleanup", workspaceCleanupParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var cleanup workspaceCleanupResult
		if err := json.Unmarshal(result, &cleanup); err != nil {
			return err
		}
		fmt.Printf("Cleaned isolated workspace for task %s.\n", cleanup.TaskID)
		return nil
	case "writes":
		if len(args) != 1 {
			return fmt.Errorf("usage: hobot workspace writes")
		}
		result, err := client.call("workspace.writes", struct{}{})
		if err != nil {
			return err
		}
		var writes struct {
			Leases []workspaceWriteLeaseSnapshot `json:"leases"`
		}
		if err := json.Unmarshal(result, &writes); err != nil {
			return err
		}
		if len(writes.Leases) == 0 {
			fmt.Println("No Agent is changing a workspace.")
			return nil
		}
		for _, lease := range writes.Leases {
			fmt.Printf("%s  PID %d  %s\n", lease.TaskID, lease.PID, lease.Cwd)
		}
		return nil
	case "delivery":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot workspace delivery TASK_ID")
		}
		result, err := client.call("workspace.delivery", workspaceDeliveryParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var delivery workspaceDelivery
		if err := json.Unmarshal(result, &delivery); err != nil {
			return err
		}
		fmt.Printf("Ready: %t\n", delivery.Ready)
		fmt.Printf("Detail: %s\n", delivery.Reason)
		if delivery.PatchBytes > 0 {
			fmt.Printf("Patch: %d bytes\n", delivery.PatchBytes)
			fmt.Printf("SHA-256: %s\n", delivery.Digest)
		}
		return nil
	case "apply":
		if len(args) != 3 || args[2] != "--yes" {
			return fmt.Errorf("usage: hobot workspace apply TASK_ID --yes")
		}
		inspectionResult, err := client.call("workspace.delivery", workspaceDeliveryParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var inspection workspaceDelivery
		if err := json.Unmarshal(inspectionResult, &inspection); err != nil {
			return err
		}
		if !inspection.Ready || !validSHA256Digest(inspection.Digest) {
			return fmt.Errorf("workspace cannot be applied: %s", inspection.Reason)
		}
		result, err := client.call("workspace.apply", workspaceApplyParams{TaskID: args[1], ExpectedDigest: inspection.Digest})
		if err != nil {
			return err
		}
		var applied workspaceApplyResult
		if err := json.Unmarshal(result, &applied); err != nil {
			return err
		}
		fmt.Printf("Applied %d bytes to the original project as staged Git changes.\n", applied.PatchBytes)
		fmt.Printf("SHA-256: %s\n", applied.Digest)
		return nil
	default:
		return fmt.Errorf("unknown workspace command: %s", args[0])
	}
}

func runModelCLI(cfg config, args []string) error {
	if len(args) == 0 || (args[0] != "check" && args[0] != "verify") {
		return fmt.Errorf("usage: hobot model check|verify [--force] [--json] PROVIDER/MODEL")
	}
	operation := args[0]
	flags := flag.NewFlagSet("model "+operation, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	force := flags.Bool("force", false, "bypass the cached result")
	jsonOutput := flags.Bool("json", false, "print the result as JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: hobot model %s [--force] [--json] PROVIDER/MODEL", operation)
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	if operation == "verify" {
		result, err := client.call("models.conformance", modelConformanceParams{Model: flags.Arg(0), Force: *force})
		if err != nil {
			return err
		}
		var verification modelConformanceResult
		if err := json.Unmarshal(result, &verification); err != nil {
			return err
		}
		if *jsonOutput {
			if err := printJSON(verification); err != nil {
				return err
			}
		} else {
			fmt.Printf("Model: %s/%s\n", verification.Provider, verification.Model)
			fmt.Printf("Verification: %s\n", verification.Status)
			for _, check := range verification.Checks {
				fmt.Printf("  %-12s %s", check.Name, check.Status)
				if check.LatencyMS > 0 {
					fmt.Printf(" (%d ms)", check.LatencyMS)
				}
				fmt.Printf(" - %s\n", check.Message)
			}
			fmt.Printf("Checked: %s%s\n", verification.CheckedAt.Format(time.RFC3339), map[bool]string{true: " (cached)", false: ""}[verification.Cached])
		}
		if verification.Status != "verified" && verification.Status != "compatible" {
			return fmt.Errorf("model did not pass Agent protocol verification")
		}
		return nil
	}
	result, err := client.call("models.health", modelHealthParams{Model: flags.Arg(0), Force: *force})
	if err != nil {
		return err
	}
	var health modelHealthResult
	if err := json.Unmarshal(result, &health); err != nil {
		return err
	}
	if *jsonOutput {
		if err := printJSON(health); err != nil {
			return err
		}
		if health.Status != "available" {
			return fmt.Errorf("model is unavailable: %s", health.Message)
		}
		return nil
	}
	fmt.Printf("Model: %s/%s\n", health.Provider, health.Model)
	fmt.Printf("Status: %s (%s)\n", health.Status, health.Category)
	fmt.Printf("Detail: %s\n", health.Message)
	if health.LatencyMS > 0 {
		fmt.Printf("Latency: first byte %d ms, total %d ms via %s\n", health.FirstByteMS, health.LatencyMS, health.Transport)
	}
	fmt.Printf("Checked: %s%s\n", health.CheckedAt.Format(time.RFC3339), map[bool]string{true: " (cached)", false: ""}[health.Cached])
	if health.Status != "available" {
		return fmt.Errorf("model is unavailable: %s", health.Message)
	}
	return nil
}

func runDiagnoseCLI(cfg config, args []string) error {
	flags := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "print bundle metadata as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: hobot diagnose [--json]")
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	result, err := client.call("support.bundle", supportBundleParams{})
	if err != nil {
		return err
	}
	var bundle supportBundleResult
	if err := json.Unmarshal(result, &bundle); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(bundle)
	}
	fmt.Println("Hobot Code diagnostics completed.")
	fmt.Printf("Health: %d passed, %d warnings, %d failed.\n", bundle.Checks.Pass, bundle.Checks.Warn, bundle.Checks.Fail)
	fmt.Printf("Support bundle: %s\n", bundle.Path)
	fmt.Printf("Size: %d bytes  SHA-256: %s\n", bundle.SizeBytes, bundle.SHA256)
	fmt.Println("Privacy: no conversations, prompts, tool inputs, environment variables, credentials, or project files are included.")
	return nil
}

func runDeploymentCLI(cfg config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hobot deploy inspect|start|status")
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	switch args[0] {
	case "inspect":
		flags := flag.NewFlagSet("deploy inspect", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		cwd := flags.String("cwd", "", "workspace directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("usage: hobot deploy inspect [--cwd DIR]")
		}
		workingDirectory, err := deploymentWorkingDirectory(*cwd)
		if err != nil {
			return err
		}
		result, err := client.call("deployment.inspect", workspaceParams{Path: workingDirectory})
		if err != nil {
			return err
		}
		var inspection deploymentInspection
		if err := json.Unmarshal(result, &inspection); err != nil {
			return err
		}
		return printJSON(inspection)
	case "start":
		flags := flag.NewFlagSet("deploy start", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		cwd := flags.String("cwd", "", "workspace directory")
		goal := flags.String("goal", "deploy-and-validate", "deployment goal")
		name := flags.String("name", "", "task name")
		model := flags.String("model", "", "agent model")
		permissions := flags.String("permissions", "ask", "task permission mode")
		sandbox := flags.String("sandbox", sandboxModeSystem, "OS sandbox: system or off")
		profile := flags.String("profile", "", "frozen acceptance profile")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return fmt.Errorf("usage: hobot deploy start [options] ARTIFACT")
		}
		workingDirectory, err := deploymentWorkingDirectory(*cwd)
		if err != nil {
			return err
		}
		params := deploymentStartParams{Cwd: workingDirectory, ArtifactPath: flags.Arg(0), Goal: *goal, Name: *name, Model: *model, PermissionMode: *permissions, SandboxMode: *sandbox, Profile: *profile}
		result, err := client.call("deployment.start", params)
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Started model deployment %s (%s).\n", metadata.ID, metadata.Name)
		fmt.Printf("Follow: hobot task attach %s\n", metadata.ID)
		fmt.Printf("Status: hobot deploy status %s\n", metadata.ID)
		return nil
	case "status":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot deploy status TASK_ID")
		}
		result, err := client.call("deployment.status", taskIDParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var status deploymentStatus
		if err := json.Unmarshal(result, &status); err != nil {
			return err
		}
		return printJSON(status)
	default:
		return fmt.Errorf("unknown deploy command: %s", args[0])
	}
}

func deploymentWorkingDirectory(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}
	return os.Getwd()
}

func runServer(cfg config) error {
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)
	if err := preparePaths(cfg); err != nil {
		return err
	}
	server, err := newDaemonServer(cfg)
	if err != nil {
		return err
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			server.shutdown()
		case <-server.stop:
		}
	}()
	return server.serve()
}

func runDaemonCLI(cfg config, args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	client := daemonClient{cfg: cfg}
	switch args[0] {
	case "start":
		if len(args) != 1 {
			return fmt.Errorf("usage: hobot daemon start")
		}
		if err := client.ensureStarted(); err != nil {
			return err
		}
		info, err := client.ping()
		if err != nil {
			return err
		}
		fmt.Printf("Hobot Code agentd is running (pid %d, protocol %d).\n", info.PID, info.Protocol)
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: hobot daemon status")
		}
		info, err := client.ping()
		if err != nil {
			if isConnectionFailure(err) {
				fmt.Println("Hobot Code agentd is not running.")
				return nil
			}
			return err
		}
		return printJSON(info)
	case "stop", "restart":
		force := len(args) == 2 && args[1] == "--force"
		if len(args) > 2 || (len(args) == 2 && !force) {
			return fmt.Errorf("usage: hobot daemon %s [--force]", args[0])
		}
		_, err := client.call("daemon.shutdown", map[string]bool{"force": force})
		if err != nil {
			if isConnectionFailure(err) && args[0] == "stop" {
				fmt.Println("Hobot Code agentd is not running.")
				return nil
			}
			if isConnectionFailure(err) && args[0] == "restart" {
				if err := startDaemon(cfg); err != nil {
					return err
				}
				fmt.Println("Started Hobot Code agentd.")
				return nil
			}
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		stopped := false
		for time.Now().Before(deadline) {
			if _, err := os.Lstat(cfg.SocketPath); errors.Is(err, os.ErrNotExist) {
				stopped = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !stopped {
			return fmt.Errorf("agentd did not stop within 5 seconds")
		}
		if args[0] == "restart" {
			if err := startDaemon(cfg); err != nil {
				return err
			}
			fmt.Println("Restarted Hobot Code agentd.")
		} else {
			fmt.Println("Stopped Hobot Code agentd.")
		}
		return nil
	default:
		return fmt.Errorf("unknown daemon command: %s", args[0])
	}
}

func runTaskCLI(cfg config, args []string) error {
	if len(args) == 0 {
		printTaskUsage(os.Stderr)
		return fmt.Errorf("a task command is required")
	}
	if handled := printRequestedTaskHelp(args, os.Stdout); handled {
		return nil
	}
	client := daemonClient{cfg: cfg}
	if err := client.ensureStarted(); err != nil {
		return err
	}
	switch args[0] {
	case "start":
		return runTaskStart(client, args[1:])
	case "list":
		includeArchived := len(args) == 2 && args[1] == "--all"
		if len(args) > 2 || (len(args) == 2 && !includeArchived) {
			return fmt.Errorf("usage: hobot task list [--all]")
		}
		return taskList(client, includeArchived)
	case "show":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "--details") {
			return fmt.Errorf("usage: hobot task show TASK_ID [--details]")
		}
		result, err := client.call("task.get", taskIDParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		if len(args) == 3 {
			return printJSON(metadata)
		}
		return printJSON(summarizeTaskForCLI(metadata))
	case "logs", "attach":
		return runTaskLogs(client, args[0], args[1:])
	case "send":
		if len(args) < 3 {
			return fmt.Errorf("usage: hobot task send TASK_ID [--] PROMPT")
		}
		messageArgs, err := taskTextArguments(args[2:], false, "usage: hobot task send TASK_ID [--] PROMPT")
		if err != nil {
			return err
		}
		message := strings.Join(messageArgs, " ")
		if len(message) == 0 || len(message) > maxPromptBytes {
			return fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
		}
		return protocolCommand(client, args[1], workerCommand("prompt", map[string]any{"message": message}))
	case "abort":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task abort TASK_ID")
		}
		return protocolCommand(client, args[1], workerCommand("abort", nil))
	case "respond":
		return runTaskRespond(client, args[1:])
	case "approvals":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "--details") {
			return fmt.Errorf("usage: hobot task approvals TASK_ID [--details]")
		}
		result, err := client.call("task.approvals", taskIDParams{TaskID: args[1]})
		if err != nil {
			return err
		}
		var approvals []pendingApproval
		if err := json.Unmarshal(result, &approvals); err != nil {
			return err
		}
		if len(args) == 3 {
			return printJSON(approvals)
		}
		return printJSON(summarizeApprovalsForCLI(approvals))
	case "resume":
		if len(args) < 2 {
			return fmt.Errorf("usage: hobot task resume TASK_ID [-- PROMPT]")
		}
		promptArgs, err := taskTextArguments(args[2:], true, "usage: hobot task resume TASK_ID [-- PROMPT]")
		if err != nil {
			return err
		}
		result, err := client.call("task.resume", resumeTaskParams{TaskID: args[1], Prompt: strings.Join(promptArgs, " ")})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Resumed background task %s (%s).\n", metadata.ID, metadata.Name)
		return nil
	case "restart":
		if len(args) < 3 {
			return fmt.Errorf("usage: hobot task restart TASK_ID [--] PROMPT")
		}
		promptArgs, err := taskTextArguments(args[2:], false, "usage: hobot task restart TASK_ID [--] PROMPT")
		if err != nil {
			return err
		}
		result, err := client.call("task.restart", resumeTaskParams{TaskID: args[1], Prompt: strings.Join(promptArgs, " ")})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Restarted background task %s (%s) with a new session.\n", metadata.ID, metadata.Name)
		return nil
	case "rename":
		if len(args) != 3 {
			return fmt.Errorf("usage: hobot task rename TASK_ID NAME")
		}
		_, err := client.call("task.rename", renameTaskParams{TaskID: args[1], Name: args[2]})
		return err
	case "model":
		if len(args) != 3 {
			return fmt.Errorf("usage: hobot task model TASK_ID PROVIDER/MODEL")
		}
		provider, modelID, ok := strings.Cut(normalizeModelSelection(args[2]), "/")
		if !ok || provider == "" || modelID == "" {
			return fmt.Errorf("model must use provider/model format")
		}
		result, err := client.call("task.model", setTaskModelParams{TaskID: args[1], Provider: provider, ModelID: modelID})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Task %s model: %s\n", metadata.ID, metadata.Model)
		return nil
	case "permissions":
		if len(args) != 3 {
			return fmt.Errorf("usage: hobot task permissions TASK_ID review|ask|developer")
		}
		mode, err := normalizePermissionMode(args[2])
		if err != nil {
			return err
		}
		result, err := client.call("task.permissions", setTaskPermissionParams{TaskID: args[1], Mode: mode})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Task %s permissions: %s\n", metadata.ID, metadata.PermissionMode)
		return nil
	case "sandbox":
		if len(args) != 3 {
			return fmt.Errorf("usage: hobot task sandbox TASK_ID review|workspace|system|off")
		}
		mode, err := normalizeSandboxMode(args[2])
		if err != nil {
			return err
		}
		result, err := client.call("task.sandbox", setTaskSandboxParams{TaskID: args[1], Mode: mode})
		if err != nil {
			return err
		}
		var metadata taskMetadata
		if err := json.Unmarshal(result, &metadata); err != nil {
			return err
		}
		fmt.Printf("Task %s sandbox: %s (%s)\n", metadata.ID, metadata.SandboxMode, metadata.Sandbox.Backend)
		return nil
	case "archive", "unarchive":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task %s TASK_ID", args[0])
		}
		_, err := client.call("task.archive", archiveTaskParams{TaskID: args[1], Archive: args[0] == "archive"})
		return err
	case "delete":
		if len(args) != 3 || args[2] != "--yes" {
			return fmt.Errorf("usage: hobot task delete TASK_ID --yes")
		}
		_, err := client.call("task.delete", taskIDParams{TaskID: args[1]})
		return err
	case "stop":
		if len(args) != 2 {
			return fmt.Errorf("usage: hobot task stop TASK_ID")
		}
		_, err := client.call("task.stop", taskIDParams{TaskID: args[1]})
		return err
	default:
		return fmt.Errorf("unknown task command: %s", args[0])
	}
}

func printTaskUsage(output io.Writer) {
	fmt.Fprintln(output, `Hobot Code background tasks

Usage:
  hobot task start [options] -- PROMPT
  hobot task list [--all]
  hobot task show TASK_ID [--details]
  hobot task logs TASK_ID [--after SEQUENCE] [--follow]
  hobot task attach TASK_ID [--after SEQUENCE | --replay-all]
  hobot task send TASK_ID [--] PROMPT
  hobot task abort TASK_ID
  hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE
  hobot task approvals TASK_ID [--details]
  hobot task resume TASK_ID [-- PROMPT]
  hobot task restart TASK_ID [--] PROMPT
  hobot task rename TASK_ID NAME
  hobot task model TASK_ID PROVIDER/MODEL
  hobot task permissions TASK_ID review|ask|developer
  hobot task sandbox TASK_ID review|workspace|system|off
  hobot task archive|unarchive TASK_ID
  hobot task delete TASK_ID --yes
  hobot task stop TASK_ID

Run hobot task COMMAND --help for command-specific usage. Text beginning
with a dash must follow --, so asking for help can never start or alter a task.`)
}

func printRequestedTaskHelp(args []string, output io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	command := args[0]
	if command == "help" || command == "-h" || command == "--help" {
		printTaskUsage(output)
		return true
	}
	if !taskHelpRequested(command, args[1:]) {
		return false
	}
	usageByCommand := map[string]string{
		"start": "hobot task start [--name NAME] [--cwd DIR] [--workspace shared|worktree] [--model PROVIDER/MODEL] [--permissions review|ask|developer] [--sandbox review|workspace|system|off] [--trust-project] -- PROMPT",
		"list":  "hobot task list [--all]", "show": "hobot task show TASK_ID [--details]",
		"logs": "hobot task logs TASK_ID [--after SEQUENCE] [--follow]", "attach": "hobot task attach TASK_ID [--after SEQUENCE | --replay-all]",
		"send": "hobot task send TASK_ID [--] PROMPT", "abort": "hobot task abort TASK_ID",
		"respond": "hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE", "approvals": "hobot task approvals TASK_ID [--details]",
		"resume": "hobot task resume TASK_ID [-- PROMPT]", "restart": "hobot task restart TASK_ID [--] PROMPT",
		"rename": "hobot task rename TASK_ID NAME", "archive": "hobot task archive TASK_ID",
		"model": "hobot task model TASK_ID PROVIDER/MODEL", "permissions": "hobot task permissions TASK_ID review|ask|developer", "sandbox": "hobot task sandbox TASK_ID review|workspace|system|off",
		"unarchive": "hobot task unarchive TASK_ID", "delete": "hobot task delete TASK_ID --yes", "stop": "hobot task stop TASK_ID",
	}
	commandUsage, ok := usageByCommand[command]
	if !ok {
		printTaskUsage(output)
		return true
	}
	fmt.Fprintf(output, "Usage:\n  %s\n", commandUsage)
	return true
}

func taskHelpRequested(command string, args []string) bool {
	isHelp := func(value string) bool { return value == "-h" || value == "--help" }
	if len(args) == 0 {
		return false
	}
	if command == "start" {
		for index := 0; index < len(args); index++ {
			value := args[index]
			if value == "--" {
				return false
			}
			if isHelp(value) {
				return true
			}
			switch value {
			case "--name", "--cwd", "--workspace", "--model", "--permissions", "--sandbox":
				index++
			case "--trust-project", "--approve":
			default:
				if !strings.HasPrefix(value, "-") {
					return false
				}
			}
		}
		return false
	}
	if isHelp(args[0]) {
		return true
	}
	promptPosition := map[string]int{"send": 1, "resume": 1, "restart": 1, "respond": 2}[command]
	if promptPosition > 0 && len(args) > promptPosition {
		return isHelp(args[promptPosition])
	}
	for _, value := range args[1:] {
		if value == "--" {
			return false
		}
		if isHelp(value) {
			return true
		}
	}
	return false
}

func taskTextArguments(args []string, optional bool, commandUsage string) ([]string, error) {
	if len(args) == 0 {
		if optional {
			return nil, nil
		}
		return nil, fmt.Errorf("%s", commandUsage)
	}
	if args[0] == "--" {
		args = args[1:]
	} else if strings.HasPrefix(args[0], "-") {
		return nil, fmt.Errorf("text beginning with a dash must follow --; %s", commandUsage)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("%s", commandUsage)
	}
	return args, nil
}

func runTaskStart(client daemonClient, args []string) error {
	defaultCwd, err := os.Getwd()
	if err != nil {
		return err
	}
	options, err := parseTaskStartArgs(args, defaultCwd, os.Stderr)
	if err != nil {
		return err
	}
	if options.usedApproveAlias {
		fmt.Fprintln(os.Stderr, "Note: --approve is a compatibility alias for --trust-project; it does not bypass tool permissions.")
	}
	result, err := client.call("task.start", options.params)
	if err != nil {
		return err
	}
	var metadata taskMetadata
	if err := json.Unmarshal(result, &metadata); err != nil {
		return err
	}
	if metadata.Status == statusQueued {
		fmt.Printf("Queued background task %s (%s); it will start when a board Agent slot is available.\n", metadata.ID, metadata.Name)
	} else {
		fmt.Printf("Started background task %s (%s).\n", metadata.ID, metadata.Name)
	}
	if metadata.Model == "" {
		fmt.Println("Model: configured default")
	} else {
		fmt.Printf("Model: %s\n", metadata.Model)
	}
	fmt.Printf("Workspace: %s\n", metadata.WorkspaceMode)
	if metadata.WorkspaceMode == workspaceModeWorktree {
		fmt.Printf("Project: %s\n", metadata.ProjectCwd)
	}
	fmt.Printf("Permissions: %s\n", metadata.PermissionMode)
	fmt.Printf("OS sandbox: %s (%s; network shared)\n", metadata.SandboxMode, metadata.Sandbox.Backend)
	fmt.Printf("Project resources: %s\n", map[bool]string{true: "trusted", false: "not trusted"}[metadata.Approved])
	fmt.Printf("Attach: hobot task attach %s\n", metadata.ID)
	return nil
}

type taskStartCLIOptions struct {
	params           startTaskParams
	usedApproveAlias bool
}

func parseTaskStartArgs(args []string, defaultCwd string, output io.Writer) (taskStartCLIOptions, error) {
	flags := flag.NewFlagSet("task start", flag.ContinueOnError)
	flags.SetOutput(output)
	name := flags.String("name", "", "task name")
	cwd := flags.String("cwd", "", "working directory")
	model := flags.String("model", "", "agent model in provider/model form")
	permissions := flags.String("permissions", defaultTaskPermissionMode, "permission mode: review, ask, or developer")
	workspace := flags.String("workspace", workspaceModeShared, "workspace mode: shared or worktree")
	sandbox := flags.String("sandbox", "", "OS sandbox: review, workspace, system, or off")
	trustProject := false
	flags.BoolVar(&trustProject, "trust-project", false, "load trusted project resources")
	flags.BoolVar(&trustProject, "approve", false, "compatibility alias for --trust-project")
	if err := flags.Parse(args); err != nil {
		return taskStartCLIOptions{}, err
	}
	usedApproveAlias := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == "approve" {
			usedApproveAlias = true
		}
	})
	promptArgs := flags.Args()
	if len(promptArgs) > 0 && promptArgs[0] == "--" {
		promptArgs = promptArgs[1:]
	}
	if len(promptArgs) == 0 {
		return taskStartCLIOptions{}, fmt.Errorf("usage: hobot task start [--name NAME] [--cwd DIR] [--workspace shared|worktree] [--model PROVIDER/MODEL] [--permissions review|ask|developer] [--sandbox review|workspace|system|off] [--trust-project] -- PROMPT")
	}
	workingDirectory := *cwd
	if workingDirectory == "" {
		workingDirectory = defaultCwd
	}
	modelSelection := strings.TrimSpace(*model)
	if modelSelection != "" {
		modelSelection = normalizeModelSelection(modelSelection)
		if modelSelection == "" {
			return taskStartCLIOptions{}, fmt.Errorf("model must use provider/model format")
		}
	}
	permissionMode, err := normalizePermissionMode(strings.TrimSpace(*permissions))
	if err != nil {
		return taskStartCLIOptions{}, err
	}
	workspaceMode, err := normalizeWorkspaceMode(*workspace)
	if err != nil {
		return taskStartCLIOptions{}, err
	}
	sandboxMode := strings.TrimSpace(*sandbox)
	if sandboxMode != "" {
		sandboxMode, err = normalizeSandboxMode(sandboxMode)
		if err != nil {
			return taskStartCLIOptions{}, err
		}
	}
	prompt := strings.Join(promptArgs, " ")
	if len(prompt) == 0 || len(prompt) > maxPromptBytes {
		return taskStartCLIOptions{}, fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
	}
	return taskStartCLIOptions{
		params: startTaskParams{
			Name: *name, Cwd: workingDirectory, Prompt: prompt, Approve: trustProject,
			Model: modelSelection, PermissionMode: permissionMode, WorkspaceMode: workspaceMode, SandboxMode: sandboxMode,
		},
		usedApproveAlias: usedApproveAlias,
	}, nil
}

func runTaskLogs(client daemonClient, command string, args []string) error {
	if len(args) == 0 {
		if command == "attach" {
			return fmt.Errorf("usage: hobot task attach TASK_ID [--after SEQUENCE | --replay-all]")
		}
		return fmt.Errorf("usage: hobot task logs TASK_ID [--after SEQUENCE] [--follow]")
	}
	taskID := args[0]
	after := uint64(0)
	afterSet := false
	replayAll := false
	follow := command == "attach"
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--follow":
			if command != "logs" {
				return fmt.Errorf("--follow is valid only with task logs")
			}
			follow = true
		case "--after":
			if index+1 >= len(args) {
				return fmt.Errorf("--after requires a sequence")
			}
			index++
			value, err := parseAfter(args[index])
			if err != nil {
				return err
			}
			after = value
			afterSet = true
		case "--replay-all":
			if command != "attach" {
				return fmt.Errorf("--replay-all is valid only with task attach")
			}
			replayAll = true
		default:
			return fmt.Errorf("unknown task log option: %s", args[index])
		}
	}
	if afterSet && replayAll {
		return fmt.Errorf("--after and --replay-all cannot be used together")
	}
	if command == "logs" && !follow {
		return replayEventPage(client, taskID, after, 200)
	}
	if command == "attach" {
		if !afterSet && !replayAll {
			value, err := readAttachCursor(client.cfg.AttachCursorRoot, taskID)
			if err != nil {
				return err
			}
			after = value
		}
		interactive := isInteractiveTerminal(os.Stdin, os.Stdout)
		if interactive {
			fmt.Fprintln(os.Stderr, "Attached. Ctrl+C detaches; the background task keeps running.")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		lastSequence := after
		lastPersisted := after
		lastPersistedAt := time.Now()
		err := client.subscribeWithContext(ctx, taskID, after, follow, true, os.Stdin, os.Stdout, interactive, func(sequence uint64) error {
			if sequence > lastSequence {
				lastSequence = sequence
			}
			if lastSequence > lastPersisted && time.Since(lastPersistedAt) >= 2*time.Second {
				if err := writeAttachCursor(client.cfg.AttachCursorRoot, taskID, lastSequence); err != nil {
					return fmt.Errorf("checkpoint attach cursor: %w", err)
				}
				lastPersisted = lastSequence
				lastPersistedAt = time.Now()
			}
			return nil
		})
		if lastSequence > lastPersisted {
			if cursorErr := writeAttachCursor(client.cfg.AttachCursorRoot, taskID, lastSequence); cursorErr != nil {
				if err != nil {
					return fmt.Errorf("%v; save attach cursor: %w", err, cursorErr)
				}
				return fmt.Errorf("save attach cursor: %w", cursorErr)
			}
		}
		return err
	}
	return client.subscribe(taskID, after, follow, command == "attach")
}

func isInteractiveTerminal(input, output *os.File) bool {
	inputInfo, inputErr := input.Stat()
	outputInfo, outputErr := output.Stat()
	return inputErr == nil && outputErr == nil && inputInfo.Mode()&os.ModeCharDevice != 0 && outputInfo.Mode()&os.ModeCharDevice != 0
}

func replayEventPage(client daemonClient, taskID string, after uint64, limit int) error {
	result, err := client.call("task.events", eventPageParams{TaskID: taskID, After: after, Limit: limit})
	if err != nil {
		return err
	}
	var page eventPage
	if err := json.Unmarshal(result, &page); err != nil {
		return err
	}
	for _, event := range page.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	}
	if page.HasMore {
		fmt.Fprintf(os.Stderr, "More events are available; continue with --after %d.\n", page.NextAfter)
	}
	return nil
}

func runTaskRespond(client daemonClient, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE")
	}
	value := strings.Join(args[2:], " ")
	method, options, err := activeApprovalShape(client, args[0], args[1])
	if err != nil {
		return err
	}
	command, err := taskApprovalResponse(args[1], method, options, value)
	if err != nil {
		return err
	}
	return protocolCommand(client, args[0], mustJSON(command))
}

func taskApprovalResponse(requestID, method string, options []string, value string) (map[string]any, error) {
	command := map[string]any{"type": "extension_ui_response", "id": requestID}
	switch strings.ToLower(value) {
	case "cancel":
		command["cancelled"] = true
	case "yes", "y":
		switch method {
		case "confirm":
			command["confirmed"] = true
		case "select":
			if len(options) == 0 {
				return nil, fmt.Errorf("the select approval has no available options")
			}
			command["value"] = options[0]
		default:
			command["value"] = value
		}
	case "no", "n":
		switch method {
		case "confirm":
			command["confirmed"] = false
		case "select":
			if len(options) == 0 {
				return nil, fmt.Errorf("the select approval has no available options")
			}
			command["value"] = options[len(options)-1]
		default:
			command["value"] = value
		}
	default:
		command["value"] = value
	}
	return command, nil
}

func activeApprovalShape(client daemonClient, taskID, requestID string) (string, []string, error) {
	result, err := client.call("task.approvals", taskIDParams{TaskID: taskID})
	if err != nil {
		return "", nil, err
	}
	var approvals []pendingApproval
	if err := json.Unmarshal(result, &approvals); err != nil {
		return "", nil, err
	}
	for _, approval := range approvals {
		if approval.ID == requestID && approval.Active {
			return approval.Method, append([]string(nil), approval.Options...), nil
		}
	}
	return "", nil, fmt.Errorf("task is not waiting for approval %s", requestID)
}

func init() {
	if value := os.Getenv("HOBOT_CODE_VERSION"); version == "dev" && value != "" {
		version = value
	}
}
