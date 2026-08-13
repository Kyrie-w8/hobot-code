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
	supportBundleSchema      = 1
	maximumSupportBundleSize = 4 * 1024 * 1024
	retainedSupportBundles   = 5
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
	ID        string              `json:"id"`
	CreatedAt time.Time           `json:"createdAt"`
	Path      string              `json:"path"`
	SizeBytes int                 `json:"sizeBytes"`
	SHA256    string              `json:"sha256"`
	Content   []byte              `json:"content,omitempty"`
	Excluded  []string            `json:"excluded"`
	Checks    supportCheckSummary `json:"checks"`
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
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

type supportBundleDocument struct {
	Notice   string                `json:"notice"`
	Manifest supportManifest       `json:"manifest"`
	System   systemSnapshot        `json:"system"`
	Daemon   supportDaemonSnapshot `json:"daemon"`
	Tasks    []supportTaskSummary  `json:"tasks"`
	Checks   []supportCheck        `json:"checks"`
}

func (server *daemonServer) createSupportBundle(includeContent bool) (supportBundleResult, error) {
	capturedAt := time.Now().UTC()
	bundleID, err := newTaskID()
	if err != nil {
		return supportBundleResult{}, err
	}
	bundleID = bundleID[:12]
	fingerprintKey, err := newTaskID()
	if err != nil {
		return supportBundleResult{}, err
	}
	snapshot := collectSystemSnapshot(server.cfg)
	sanitizeSupportSnapshot(&snapshot)
	tasks := server.supportTaskSummaries(fingerprintKey)
	sections := []string{"manifest", "system", "daemon", "tasks", "checks"}
	manifest := supportManifest{
		Schema: supportBundleSchema, Product: "Hobot Code", Version: version, Protocol: protocolVersion,
		BundleID: bundleID, CapturedAt: capturedAt, Sections: sections, Excluded: append([]string(nil), supportBundleExcluded...),
		Retention: retainedSupportBundles, PrivacyLevel: "diagnostics-only",
	}
	daemon := supportDaemonSnapshot{
		Version: version, Protocol: protocolVersion, StartedAt: server.started,
		UptimeSeconds: uint64(time.Since(server.started).Seconds()), ActiveTasks: server.manager.activeCount(),
		MaximumActiveTasks: server.cfg.MaxTasks, RetainedTasks: len(tasks), MaximumRetainedTasks: server.cfg.MaxRetainedTasks,
		MaximumEventBytes: server.cfg.MaxEventSize, Capabilities: append([]string(nil), protocolCapabilities...), Build: server.build,
	}
	checks := server.supportChecks(snapshot)
	document := supportBundleDocument{
		Notice: strings.TrimSpace(supportBundleReadme), Manifest: manifest, System: snapshot,
		Daemon: daemon, Tasks: tasks, Checks: checks,
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
		ID: bundleID, CreatedAt: capturedAt, Path: path, SizeBytes: len(content), SHA256: hex.EncodeToString(digest[:]),
		Excluded: append([]string(nil), supportBundleExcluded...), Checks: summarizeSupportChecks(checks),
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
			SandboxMode: item.SandboxMode, SandboxBackend: item.Sandbox.Backend,
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

func (server *daemonServer) supportChecks(snapshot systemSnapshot) []supportCheck {
	checks := []supportCheck{
		pathCheck("state-directory", server.cfg.StateRoot, true, true, true, false),
		pathCheck("task-directory", server.cfg.TasksRoot, true, true, true, false),
		pathCheck("session-directory", server.cfg.SessionDir, true, true, true, false),
		pathCheck("support-directory", server.cfg.SupportRoot, true, true, true, false),
		pathCheck("agent-binary", server.cfg.AgentBinary, false, false, false, true),
		pathCheck("agentd-log", server.cfg.LogPath, false, true, true, false),
	}
	checks = append(checks, sandboxSupportCheck(server.cfg))
	checks = append(checks, resourceChecks(snapshot)...)
	utilities := make([]string, 0, len(snapshot.RDKUtilities))
	for name := range snapshot.RDKUtilities {
		utilities = append(utilities, name)
	}
	sort.Strings(utilities)
	for _, name := range utilities {
		status := "warn"
		if snapshot.RDKUtilities[name] {
			status = "pass"
		}
		checks = append(checks, supportCheck{Name: "utility-" + name, Status: status, Summary: map[bool]string{true: "available", false: "not found"}[snapshot.RDKUtilities[name]]})
	}
	return checks
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
