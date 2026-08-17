package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	supportBundleSchema      = 2
	maximumSupportBundleSize = 4 * 1024 * 1024
	retainedSupportBundles   = 5
	maximumSupportFindings   = 16
	supportRecentTaskWindow  = 24 * time.Hour
	supportTransitionTimeout = 5 * time.Minute
)

var supportBundleExcluded = []string{
	"conversation and session content",
	"system and user prompts",
	"tool inputs and outputs",
	"environment variables and credentials",
	"project files and workspace contents",
	"raw daemon and worker logs",
	"per-process identity",
}

type supportBundleParams struct {
	IncludeContent bool `json:"includeContent,omitempty"`
}

type supportBundleResult struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	CreatedAt     time.Time           `json:"createdAt"`
	Path          string              `json:"path"`
	SizeBytes     int                 `json:"sizeBytes"`
	SHA256        string              `json:"sha256"`
	Content       []byte              `json:"content,omitempty"`
	Excluded      []string            `json:"excluded"`
	Status        string              `json:"status"`
	Checks        supportCheckSummary `json:"checks"`
	Findings      []supportFinding    `json:"findings"`
}

type supportManifest struct {
	Schema       int       `json:"schema"`
	Product      string    `json:"product"`
	Version      string    `json:"version"`
	Protocol     int       `json:"protocol"`
	BundleID     string    `json:"bundleId"`
	CapturedAt   time.Time `json:"capturedAt"`
	Sections     []string  `json:"sections"`
	Excluded     []string  `json:"excluded"`
	Retention    int       `json:"retainedBundles"`
	PrivacyLevel string    `json:"privacyLevel"`
}

type supportDaemonSnapshot struct {
	Version              string        `json:"version"`
	Protocol             int           `json:"protocol"`
	StartedAt            time.Time     `json:"startedAt"`
	UptimeSeconds        uint64        `json:"uptimeSeconds"`
	ActiveTasks          int           `json:"activeTasks"`
	MaximumActiveTasks   int           `json:"maximumActiveTasks"`
	RetainedTasks        int           `json:"retainedTasks"`
	MaximumRetainedTasks int           `json:"maximumRetainedTasks"`
	MaximumEventBytes    int64         `json:"maximumEventBytes"`
	Capabilities         []string      `json:"capabilities"`
	Build                buildIdentity `json:"build"`
}

type supportTaskSummary struct {
	Ref              string     `json:"ref"`
	Status           taskStatus `json:"status"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	LastSequence     uint64     `json:"lastSequence"`
	LogTruncated     bool       `json:"logTruncated"`
	ResumeCount      int        `json:"resumeCount"`
	RestartCount     int        `json:"restartCount"`
	Model            string     `json:"model,omitempty"`
	PermissionMode   string     `json:"permissionMode,omitempty"`
	SandboxMode      string     `json:"sandboxMode,omitempty"`
	SandboxBackend   string     `json:"sandboxBackend,omitempty"`
	NetworkMode      string     `json:"networkMode,omitempty"`
	BranchKind       string     `json:"branchKind,omitempty"`
	AwaitingPrompt   bool       `json:"awaitingPrompt,omitempty"`
	QueuedAt         *time.Time `json:"queuedAt,omitempty"`
	QueueOperation   string     `json:"queueOperation,omitempty"`
	ParentRef        string     `json:"parentRef,omitempty"`
	Archived         bool       `json:"archived"`
	ActiveApprovals  int        `json:"activeApprovals"`
	HasSession       bool       `json:"hasSession"`
	HasDeployment    bool       `json:"hasDeployment"`
	DeploymentGoal   string     `json:"deploymentGoal,omitempty"`
	ErrorCategory    string     `json:"errorCategory,omitempty"`
	ErrorFingerprint string     `json:"errorFingerprint,omitempty"`
	LastTurnStatus   string     `json:"lastTurnStatus,omitempty"`
	TurnEvidence     string     `json:"turnEvidence,omitempty"`
	OpenTools        int        `json:"openTools,omitempty"`
	WorkspaceChanged *bool      `json:"workspaceChanged,omitempty"`
}

type supportCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type supportCheckSummary struct {
	Pass int `json:"pass"`
	Info int `json:"info"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

type supportFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Scope    string `json:"scope"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
	Count    int    `json:"count,omitempty"`
}

type supportBundleDocument struct {
	Notice   string                `json:"notice"`
	Status   string                `json:"status"`
	Manifest supportManifest       `json:"manifest"`
	System   systemSnapshot        `json:"system"`
	Daemon   supportDaemonSnapshot `json:"daemon"`
	Tasks    []supportTaskSummary  `json:"tasks"`
	Checks   []supportCheck        `json:"checks"`
	Findings []supportFinding      `json:"findings"`
}

func (server *daemonServer) createSupportBundle(includeContent bool, clientFingerprint string) (supportBundleResult, error) {
	evidence, err := server.collectDiagnosticEvidence(clientFingerprint)
	if err != nil {
		return supportBundleResult{}, err
	}
	capturedAt := evidence.capturedAt
	bundleID, err := newTaskID()
	if err != nil {
		return supportBundleResult{}, err
	}
	bundleID = bundleID[:12]
	snapshot := evidence.snapshot
	sanitizeSupportSnapshot(&snapshot)
	tasks := evidence.tasks
	sections := []string{"manifest", "system", "daemon", "tasks", "checks", "findings"}
	manifest := supportManifest{
		Schema: supportBundleSchema, Product: "Hobot Code", Version: version, Protocol: protocolVersion,
		BundleID: bundleID, CapturedAt: capturedAt, Sections: sections, Excluded: append([]string(nil), supportBundleExcluded...),
		Retention: retainedSupportBundles, PrivacyLevel: "diagnostics-only",
	}
	daemon := supportDaemonSnapshot{
		Version: version, Protocol: protocolVersion, StartedAt: server.started,
		UptimeSeconds: uint64(time.Since(server.started).Seconds()), ActiveTasks: server.manager.activeCount(),
		MaximumActiveTasks: server.cfg.MaxTasks, RetainedTasks: len(tasks), MaximumRetainedTasks: server.cfg.MaxRetainedTasks,
		MaximumEventBytes: server.cfg.MaxEventSize, Capabilities: server.capabilities().Capabilities, Build: server.build,
	}
	checks := evidence.checks
	findings := evidence.findings
	status := evidence.status
	document := supportBundleDocument{
		Notice: strings.TrimSpace(supportBundleReadme), Status: status, Manifest: manifest, System: snapshot,
		Daemon: daemon, Tasks: tasks, Checks: checks, Findings: findings,
	}
	content := supportJSON(document)
	if len(content) > maximumSupportBundleSize {
		return supportBundleResult{}, fmt.Errorf("support bundle exceeds %d bytes", maximumSupportBundleSize)
	}
	name := fmt.Sprintf("hobot-code-support-%s-%s.json", capturedAt.Format("20060102T150405Z"), bundleID)
	path := filepath.Join(server.cfg.SupportRoot, name)
	if err := writePrivateFile(path, content); err != nil {
		return supportBundleResult{}, err
	}
	server.pruneSupportBundles(name)
	digest := sha256.Sum256(content)
	result := supportBundleResult{
		SchemaVersion: supportBundleSchema, ID: bundleID, CreatedAt: capturedAt, Path: path, SizeBytes: len(content), SHA256: hex.EncodeToString(digest[:]),
		Excluded: append([]string(nil), supportBundleExcluded...), Status: status, Checks: summarizeSupportChecks(checks), Findings: findings,
	}
	if includeContent {
		result.Content = content
	}
	return result, nil
}

func sanitizeSupportSnapshot(snapshot *systemSnapshot) {
	snapshot.Hostname = "[redacted]"
	snapshot.Disk.Path = "hobot-code-state"
	snapshot.Accelerator.Processes = nil
	for index := range snapshot.HardwareLeases {
		snapshot.HardwareLeases[index].TaskID = ""
		snapshot.HardwareLeases[index].PID = 0
		snapshot.HardwareLeases[index].Cwd = ""
	}
	for index := range snapshot.WorkspaceWrites {
		snapshot.WorkspaceWrites[index].TaskID = ""
		snapshot.WorkspaceWrites[index].PID = 0
		snapshot.WorkspaceWrites[index].Cwd = ""
	}
	for index, device := range snapshot.BPUDevices {
		snapshot.BPUDevices[index] = filepath.Base(device)
	}
}

func summarizeSupportChecks(checks []supportCheck) supportCheckSummary {
	result := supportCheckSummary{}
	for _, check := range checks {
		switch check.Status {
		case "pass":
			result.Pass++
		case "info":
			result.Info++
		case "warn":
			result.Warn++
		case "fail":
			result.Fail++
		}
	}
	return result
}

func (server *daemonServer) supportTaskSummaries(fingerprintKey string) []supportTaskSummary {
	server.manager.mu.RLock()
	metadata := make([]taskMetadata, 0, len(server.manager.tasks))
	for _, current := range server.manager.tasks {
		metadata = append(metadata, current.snapshot())
	}
	server.manager.mu.RUnlock()
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].CreatedAt.After(metadata[j].CreatedAt) })
	result := make([]supportTaskSummary, 0, len(metadata))
	for _, item := range metadata {
		activeApprovals := 0
		for _, approval := range item.Approvals {
			if approval.Active {
				activeApprovals++
			}
		}
		summary := supportTaskSummary{
			Ref: supportRef(fingerprintKey, item.ID), Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			LastSequence: item.LastSequence, LogTruncated: item.LogTruncated, ResumeCount: item.ResumeCount,
			RestartCount: item.RestartCount, Model: item.Model, PermissionMode: item.PermissionMode,
			SandboxMode: item.SandboxMode, SandboxBackend: item.Sandbox.Backend, NetworkMode: item.NetworkMode,
			BranchKind: item.BranchKind, AwaitingPrompt: item.AwaitingPrompt, QueuedAt: item.QueuedAt, QueueOperation: item.QueueOperation, Archived: item.ArchivedAt != nil, ActiveApprovals: activeApprovals,
			HasSession: item.SessionFile != "", HasDeployment: item.Deployment != nil,
		}
		if item.ParentTaskID != "" {
			summary.ParentRef = supportRef(fingerprintKey, item.ParentTaskID)
		}
		if item.Deployment != nil {
			summary.DeploymentGoal = item.Deployment.Goal
		}
		if item.Failure != nil {
			summary.ErrorCategory = item.Failure.Code
			summary.ErrorFingerprint = supportRef(fingerprintKey, item.Failure.Code)
		} else if item.LastError != "" {
			summary.ErrorCategory = supportErrorCategory(item.LastError)
			summary.ErrorFingerprint = supportRef(fingerprintKey, item.LastError)
		}
		if len(item.TurnEvidence) > 0 {
			last := item.TurnEvidence[len(item.TurnEvidence)-1]
			summary.LastTurnStatus = last.Status
			summary.TurnEvidence = last.Evidence
			summary.OpenTools = last.OpenTools
			summary.WorkspaceChanged = last.WorkspaceChanged
		}
		result = append(result, summary)
	}
	return result
}

func supportRef(key, value string) string {
	digest := hmac.New(sha256.New, []byte(key))
	_, _ = digest.Write([]byte(value))
	return hex.EncodeToString(digest.Sum(nil)[:6])
}

func supportErrorCategory(value string) string {
	lower := strings.ToLower(value)
	for _, category := range []struct {
		name  string
		terms []string
	}{
		{"authentication", []string{"unauthorized", "authentication", "permission denied"}},
		{"model-gateway", []string{"model gateway", "unsupported model", "message_stop", "http 400"}},
		{"network", []string{"timeout", "connection", "network", "broken pipe"}},
		{"storage", []string{"no space", "read-only file system", "disk"}},
		{"resource-limit", []string{"limit reached", "out of memory", "oom", "resource exhausted"}},
		{"agent-restart", []string{"agentd restarted", "in-flight worker"}},
	} {
		for _, term := range category.terms {
			if strings.Contains(lower, term) {
				return category.name
			}
		}
	}
	return "other"
}

func (server *daemonServer) supportChecks(snapshot systemSnapshot, tasks []supportTaskSummary, capturedAt time.Time) []supportCheck {
	checks := []supportCheck{
		buildSupportCheck(server.build),
		boardSupportCheck(snapshot),
		pathCheck("state-directory", server.cfg.StateRoot, true, true, true, false),
		pathCheck("task-directory", server.cfg.TasksRoot, true, true, true, false),
		pathCheck("schedule-directory", server.cfg.SchedulesRoot, true, true, true, false),
		pathCheck("task-control-directory", server.cfg.TaskControlRoot, true, true, true, false),
		pathCheck("session-directory", server.cfg.SessionDir, true, true, true, false),
		pathCheck("support-directory", server.cfg.SupportRoot, true, true, true, false),
		pathCheck("agent-binary", server.cfg.AgentBinary, false, false, false, true),
		optionalPathCheck("agentd-log", server.cfg.LogPath, false, true, true, false),
	}
	checks = append(checks, sandboxSupportCheck(server.cfg))
	checks = append(checks, modelEgressSupportCheck(server.cfg))
	checks = append(checks, resourceChecks(snapshot)...)
	checks = append(checks, taskLifecycleSupportCheck(tasks, server.manager.activeCount(), server.cfg.MaxTasks, capturedAt))
	utilities := make([]string, 0, len(snapshot.RDKUtilities))
	for name := range snapshot.RDKUtilities {
		utilities = append(utilities, name)
	}
	sort.Strings(utilities)
	for _, name := range utilities {
		status := "info"
		if snapshot.RDKUtilities[name] {
			status = "pass"
		}
		checkName := "utility-" + strings.ReplaceAll(name, "_", "-")
		checks = append(checks, supportCheck{Name: checkName, Status: status, Summary: map[bool]string{true: "available", false: "not found"}[snapshot.RDKUtilities[name]]})
	}
	return checks
}

func modelEgressSupportCheck(cfg config) supportCheck {
	if !modelEgressAvailable(cfg) {
		return supportCheck{Name: "model-egress", Status: "info", Summary: "model-only egress is not configured"}
	}
	if !modelEgressSocketReady(cfg) {
		return supportCheck{Name: "model-egress", Status: "fail", Summary: "configured but the private broker socket is unavailable"}
	}
	routes, _ := loadModelEgressRoutes(cfg)
	return supportCheck{Name: "model-egress", Status: "pass", Summary: fmt.Sprintf("private model broker is available for %d provider(s)", len(routes))}
}

func buildSupportCheck(build buildIdentity) supportCheck {
	if build.Status != "verified" || build.Dirty == nil {
		return supportCheck{Name: "release-integrity", Status: "fail", Summary: "release identity is unavailable or invalid"}
	}
	if *build.Dirty {
		return supportCheck{Name: "release-integrity", Status: "warn", Summary: "development build is not release-qualified"}
	}
	if !isLowerHex(build.PiCompatibilitySHA256, 64) {
		return supportCheck{Name: "release-integrity", Status: "fail", Summary: "Pi compatibility identity is unavailable"}
	}
	return supportCheck{Name: "release-integrity", Status: "pass", Summary: "binary, release metadata, and Pi contract are verified"}
}

func boardSupportCheck(snapshot systemSnapshot) supportCheck {
	expectedMajor := map[string]string{"x5": "3", "s100": "4", "s600": "5"}[snapshot.BoardID]
	if expectedMajor == "" || snapshot.Architecture != "arm64" {
		return supportCheck{Name: "board-target", Status: "warn", Summary: "board target is outside the validated X5, S100, and S600 ARM64 set"}
	}
	if !strings.HasPrefix(snapshot.RDKOSVersion, expectedMajor+".") {
		return supportCheck{Name: "board-target", Status: "warn", Summary: fmt.Sprintf("%s is expected on the RDK OS %s.x line", strings.ToUpper(snapshot.BoardID), expectedMajor)}
	}
	return supportCheck{Name: "board-target", Status: "pass", Summary: fmt.Sprintf("%s is on the supported RDK OS %s.x line", strings.ToUpper(snapshot.BoardID), expectedMajor)}
}

func optionalPathCheck(name, path string, directory, requirePrivate, requireOwner, requireExecutable bool) supportCheck {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return supportCheck{Name: name, Status: "info", Summary: "not created in the current runtime mode"}
	}
	return pathCheck(name, path, directory, requirePrivate, requireOwner, requireExecutable)
}

func taskLifecycleSupportCheck(tasks []supportTaskSummary, active, maximum int, capturedAt time.Time) supportCheck {
	recentFailures, interrupted, stalled, queued := 0, 0, 0, 0
	for _, task := range tasks {
		if task.Archived {
			continue
		}
		age := capturedAt.Sub(task.UpdatedAt)
		if age < 0 {
			age = 0
		}
		switch task.Status {
		case statusFailed:
			if age <= supportRecentTaskWindow {
				recentFailures++
			}
		case statusInterrupted:
			if age <= supportRecentTaskWindow {
				interrupted++
			}
		case statusStarting, statusStopping:
			if age >= supportTransitionTimeout {
				stalled++
			}
		case statusQueued:
			queued++
		}
	}
	if stalled > 0 || (queued > 0 && active < maximum) {
		return supportCheck{Name: "task-lifecycle", Status: "fail", Summary: fmt.Sprintf("%d stalled transition(s), %d queued task(s), %d of %d worker slots active", stalled, queued, active, maximum)}
	}
	if recentFailures > 0 || interrupted > 0 {
		return supportCheck{Name: "task-lifecycle", Status: "warn", Summary: fmt.Sprintf("%d recent failure(s) and %d interrupted task(s) need review", recentFailures, interrupted)}
	}
	return supportCheck{Name: "task-lifecycle", Status: "pass", Summary: fmt.Sprintf("no recent lifecycle faults across %d retained task(s)", len(tasks))}
}

func supportStatus(checks []supportCheck) string {
	summary := summarizeSupportChecks(checks)
	if summary.Fail > 0 {
		return "action-required"
	}
	if summary.Warn > 0 {
		return "attention"
	}
	return "healthy"
}

func supportFindings(checks []supportCheck, tasks []supportTaskSummary, capturedAt time.Time) []supportFinding {
	findings := make([]supportFinding, 0, maximumSupportFindings)
	seen := map[string]bool{}
	appendFinding := func(finding supportFinding) {
		if finding.Code == "" || seen[finding.Code] || len(findings) >= maximumSupportFindings {
			return
		}
		seen[finding.Code] = true
		findings = append(findings, finding)
	}
	for _, check := range checks {
		if check.Status != "warn" && check.Status != "fail" {
			continue
		}
		if finding, ok := supportFindingForCheck(check); ok {
			appendFinding(finding)
		}
	}
	failureCounts := map[string]int{}
	interrupted, truncated := 0, 0
	for _, task := range tasks {
		if task.Archived {
			continue
		}
		age := capturedAt.Sub(task.UpdatedAt)
		if age < 0 {
			age = 0
		}
		if age <= supportRecentTaskWindow {
			switch task.Status {
			case statusFailed:
				failureCounts[supportFailureGroup(task.ErrorCategory)]++
			case statusInterrupted:
				interrupted++
			}
		}
		if task.LogTruncated {
			truncated++
		}
	}
	groups := make([]string, 0, len(failureCounts))
	for group := range failureCounts {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	for _, group := range groups {
		count := failureCounts[group]
		title, action := supportFailureAdvice(group)
		appendFinding(supportFinding{
			Code: "recent-task-" + group, Severity: "warning", Scope: "tasks", Title: title,
			Summary: fmt.Sprintf("%d non-archived task(s) reported this failure category in the last 24 hours.", count),
			Action:  action, Count: count,
		})
	}
	if interrupted > 0 {
		appendFinding(supportFinding{
			Code: "interrupted-tasks", Severity: "warning", Scope: "tasks", Title: "Tasks were interrupted",
			Summary: fmt.Sprintf("%d non-archived task(s) were interrupted in the last 24 hours.", interrupted),
			Action:  "Review the task recovery state, then resume only tasks whose last tool call is complete.", Count: interrupted,
		})
	}
	if truncated > 0 {
		appendFinding(supportFinding{
			Code: "truncated-task-history", Severity: "info", Scope: "tasks", Title: "Older task events were rotated",
			Summary: fmt.Sprintf("%d non-archived task(s) no longer retain their earliest event records.", truncated),
			Action:  "Save diagnostics promptly when reproducing a problem; current task state and recent events remain available.", Count: truncated,
		})
	}
	sort.SliceStable(findings, func(left, right int) bool {
		rank := map[string]int{"error": 0, "warning": 1, "info": 2}
		if rank[findings[left].Severity] != rank[findings[right].Severity] {
			return rank[findings[left].Severity] < rank[findings[right].Severity]
		}
		return findings[left].Code < findings[right].Code
	})
	return findings
}

func supportFindingForCheck(check supportCheck) (supportFinding, bool) {
	severity := map[string]string{"warn": "warning", "fail": "error"}[check.Status]
	finding := supportFinding{Code: check.Name, Severity: severity, Summary: check.Summary, Count: 1}
	switch check.Name {
	case "configuration-current":
		finding.Scope, finding.Title = "runtime", "agentd is using an older configuration"
		finding.Action = "Finish or stop active tasks, then restart agentd from the current Hobot Code client."
	case "model-configuration":
		finding.Scope, finding.Title = "models", "No usable model provider is configured"
		finding.Action = "Run `hobot setup` for D-Robotics models or add a provider before starting an Agent conversation."
	case "release-integrity":
		finding.Scope, finding.Title = "runtime", "Release integrity is not production-ready"
		finding.Action = "Reinstall or update Hobot Code from a verified release before production use."
	case "board-target":
		finding.Scope, finding.Title = "board", "Board software is outside the validated baseline"
		finding.Action = "Use core Agent features cautiously and validate each hardware-specific workflow on this exact board and RDK OS."
	case "state-directory", "task-directory", "session-directory", "support-directory", "agentd-log":
		finding.Scope, finding.Title = "storage", "Private runtime storage is unavailable or unsafe"
		finding.Action = "Check ownership, file type, and private permissions under the current user's Hobot Code state directory, then restart agentd."
	case "agent-binary":
		finding.Scope, finding.Title = "runtime", "The Agent runtime executable is unavailable or unsafe"
		finding.Action = "Reinstall Hobot Code from a verified release archive."
	case "os-sandbox":
		finding.Scope, finding.Title = "security", "OS isolation is incomplete"
		finding.Action = "Install or repair bubblewrap, restart agentd, and run diagnostics again."
	case "model-egress":
		finding.Scope, finding.Title = "models", "The private model route is unavailable"
		finding.Action = "Restart agentd; if the issue remains, review the redacted provider status and run the selected model's readiness checks."
	case "memory":
		finding.Scope, finding.Title = "resources", "System memory is low"
		finding.Action = "Stop unused tasks or services before starting another model, conversion, or build workload."
	case "disk":
		finding.Scope, finding.Title = "resources", "Hobot Code state storage is low"
		finding.Action = "Free space on the filesystem containing Hobot Code state before continuing."
	case "temperature":
		finding.Scope, finding.Title = "resources", "Board temperature is high"
		finding.Action = "Reduce sustained workload and check cooling before continuing hardware-intensive work."
	case "task-lifecycle":
		if check.Status != "fail" {
			return supportFinding{}, false
		}
		finding.Scope, finding.Title = "tasks", "The task queue or a lifecycle transition is stalled"
		finding.Action = "Refresh task state, restart agentd only if no task is actively working, then run diagnostics again."
	default:
		return supportFinding{}, false
	}
	return finding, true
}

func supportFailureGroup(category string) string {
	lower := strings.ToLower(category)
	switch {
	case strings.Contains(lower, "auth"), strings.Contains(lower, "credential"), strings.Contains(lower, "permission"):
		return "authentication"
	case strings.Contains(lower, "model"), strings.Contains(lower, "gateway"), strings.Contains(lower, "provider"):
		return "model"
	case strings.Contains(lower, "network"), strings.Contains(lower, "timeout"), strings.Contains(lower, "connection"):
		return "network"
	case strings.Contains(lower, "storage"), strings.Contains(lower, "state"), strings.Contains(lower, "workspace"):
		return "storage"
	case strings.Contains(lower, "resource"), strings.Contains(lower, "memory"), strings.Contains(lower, "limit"):
		return "resources"
	case strings.Contains(lower, "restart"), strings.Contains(lower, "interrupt"):
		return "interruption"
	default:
		return "other"
	}
}

func supportFailureAdvice(group string) (string, string) {
	switch group {
	case "authentication":
		return "Recent tasks could not authenticate", "Review provider credential status, rotate the affected key if needed, restart agentd, and rerun model readiness."
	case "model":
		return "Recent tasks could not use their model", "Run the selected model's route and Agent runtime readiness checks before retrying the task."
	case "network":
		return "Recent tasks encountered network failures", "Check the board's VPN, DNS, route, and selected task network mode before retrying."
	case "storage":
		return "Recent tasks could not use required storage", "Check free space, ownership, and whether the task workspace still exists and is trusted."
	case "resources":
		return "Recent tasks exhausted a resource limit", "Stop unused workloads and review memory, task slots, and hardware leases before retrying."
	case "interruption":
		return "Recent tasks were interrupted by a runtime restart", "Review recovery evidence and resume only when the previous tool call is known to be complete."
	default:
		return "Recent tasks need review", "Open the affected task, follow its recovery action, and keep this support bundle if the failure repeats."
	}
}

func pathCheck(name, path string, directory, requirePrivate, requireOwner, requireExecutable bool) supportCheck {
	info, err := os.Lstat(path)
	if err != nil {
		return supportCheck{Name: name, Status: "warn", Summary: "not available"}
	}
	wantType := info.Mode().IsRegular()
	if directory {
		wantType = info.IsDir()
	}
	permissionsOK := !requirePrivate || info.Mode().Perm()&0o077 == 0
	executable := !requireExecutable || info.Mode().Perm()&0o111 != 0
	owner, ownerAvailable := fileOwner(info)
	wrongOwner := requireOwner && ownerAvailable && owner != os.Getuid()
	if !wantType || info.Mode()&os.ModeSymlink != 0 || !permissionsOK || !executable || wrongOwner {
		return supportCheck{Name: name, Status: "fail", Summary: fmt.Sprintf("unsafe type or permissions (%04o)", info.Mode().Perm())}
	}
	return supportCheck{Name: name, Status: "pass", Summary: fmt.Sprintf("private permissions (%04o)", info.Mode().Perm())}
}

func resourceChecks(snapshot systemSnapshot) []supportCheck {
	checks := []supportCheck{}
	if snapshot.Memory.TotalBytes > 0 {
		available := float64(snapshot.Memory.AvailableBytes) / float64(snapshot.Memory.TotalBytes)
		status := "pass"
		if available < 0.1 {
			status = "fail"
		} else if available < 0.2 {
			status = "warn"
		}
		checks = append(checks, supportCheck{Name: "memory", Status: status, Summary: fmt.Sprintf("%.1f%% available", available*100)})
	}
	if snapshot.Disk.TotalBytes > 0 {
		available := float64(snapshot.Disk.AvailableBytes) / float64(snapshot.Disk.TotalBytes)
		status := "pass"
		if available < 0.05 {
			status = "fail"
		} else if available < 0.1 {
			status = "warn"
		}
		checks = append(checks, supportCheck{Name: "disk", Status: status, Summary: fmt.Sprintf("%.1f%% available", available*100)})
	}
	maximumTemperature := 0.0
	for _, zone := range snapshot.ThermalZones {
		if zone.Celsius > maximumTemperature {
			maximumTemperature = zone.Celsius
		}
	}
	if maximumTemperature > 0 {
		status := "pass"
		if maximumTemperature >= 95 {
			status = "fail"
		} else if maximumTemperature >= 85 {
			status = "warn"
		}
		checks = append(checks, supportCheck{Name: "temperature", Status: status, Summary: fmt.Sprintf("maximum %.1f C", maximumTemperature)})
	}
	return checks
}

func supportJSON(value any) []byte {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	return append(encoded, '\n')
}

func (server *daemonServer) pruneSupportBundles(current string) {
	entries, err := os.ReadDir(server.cfg.SupportRoot)
	if err != nil {
		return
	}
	type candidate struct {
		name       string
		modifiedAt time.Time
	}
	candidates := []candidate{}
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "hobot-code-support-") && strings.HasSuffix(entry.Name(), ".json") {
			info, infoErr := entry.Info()
			if infoErr == nil {
				candidates = append(candidates, candidate{name: entry.Name(), modifiedAt: info.ModTime()})
			}
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].name == current {
			return true
		}
		if candidates[right].name == current {
			return false
		}
		if candidates[left].modifiedAt.Equal(candidates[right].modifiedAt) {
			return candidates[left].name > candidates[right].name
		}
		return candidates[left].modifiedAt.After(candidates[right].modifiedAt)
	})
	for _, item := range candidates[minimumInt(retainedSupportBundles, len(candidates)):] {
		_ = os.Remove(filepath.Join(server.cfg.SupportRoot, item.name))
	}
}

func minimumInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

const supportBundleReadme = `Hobot Code support bundle

This file contains bounded, structured diagnostics for technical support.
Task identifiers and raw error messages are replaced by one-way fingerprints.
The hostname and local paths are redacted.

It does not contain conversations, prompts, tool inputs or outputs, environment
variables, credentials, project files, workspace contents, or raw logs.
Review the manifest section before sharing the file.
`
