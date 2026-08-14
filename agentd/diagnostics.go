package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const diagnosticsSchemaVersion = 1

const (
	diagnosticRepairPrivatePermissions = "private-runtime-permissions"
	diagnosticRepairRestartDaemon      = "restart-daemon"
)

type diagnosticReport struct {
	SchemaVersion int                      `json:"schemaVersion"`
	CapturedAt    time.Time                `json:"capturedAt"`
	Status        string                   `json:"status"`
	Summary       supportCheckSummary      `json:"summary"`
	Checks        []supportCheck           `json:"checks"`
	Findings      []supportFinding         `json:"findings"`
	Repairs       []diagnosticRepairAction `json:"repairs"`
}

type diagnosticRepairAction struct {
	ID                   string `json:"id"`
	Executor             string `json:"executor"`
	Status               string `json:"status"`
	RequiresConfirmation bool   `json:"requiresConfirmation"`
	Summary              string `json:"summary"`
	Reason               string `json:"reason"`
}

type diagnosticRepairParams struct {
	Action  string `json:"action"`
	Confirm bool   `json:"confirm"`
}

type diagnosticRepairResult struct {
	SchemaVersion int              `json:"schemaVersion"`
	Action        string           `json:"action"`
	Changed       int              `json:"changed"`
	Report        diagnosticReport `json:"report"`
}

type diagnosticEvidence struct {
	capturedAt time.Time
	snapshot   systemSnapshot
	tasks      []supportTaskSummary
	checks     []supportCheck
	findings   []supportFinding
	status     string
}

type diagnosticPrivateTarget struct {
	name      string
	path      string
	directory bool
}

func (server *daemonServer) collectDiagnosticEvidence(clientFingerprint string) (diagnosticEvidence, error) {
	capturedAt := time.Now().UTC()
	fingerprintKey, err := newTaskID()
	if err != nil {
		return diagnosticEvidence{}, err
	}
	snapshot := collectSystemSnapshot(server.cfg)
	tasks := server.supportTaskSummaries(fingerprintKey)
	checks := server.supportChecks(snapshot, tasks, capturedAt)
	checks = append([]supportCheck{
		configurationSupportCheck(server.cfg, clientFingerprint),
		modelConfigurationSupportCheck(server.cfg),
	}, checks...)
	return diagnosticEvidence{
		capturedAt: capturedAt,
		snapshot:   snapshot,
		tasks:      tasks,
		checks:     checks,
		findings:   supportFindings(checks, tasks, capturedAt),
		status:     supportStatus(checks),
	}, nil
}

func (server *daemonServer) inspectDiagnostics(clientFingerprint string) (diagnosticReport, error) {
	evidence, err := server.collectDiagnosticEvidence(clientFingerprint)
	if err != nil {
		return diagnosticReport{}, err
	}
	return server.diagnosticReport(evidence, clientFingerprint), nil
}

func (server *daemonServer) diagnosticReport(evidence diagnosticEvidence, clientFingerprint string) diagnosticReport {
	return diagnosticReport{
		SchemaVersion: diagnosticsSchemaVersion,
		CapturedAt:    evidence.capturedAt,
		Status:        evidence.status,
		Summary:       summarizeSupportChecks(evidence.checks),
		Checks:        evidence.checks,
		Findings:      evidence.findings,
		Repairs:       server.diagnosticRepairActions(clientFingerprint),
	}
}

func configurationSupportCheck(cfg config, clientFingerprint string) supportCheck {
	if cfg.ConfigFingerprint == "" || clientFingerprint == "" {
		return supportCheck{Name: "configuration-current", Status: "info", Summary: "configuration comparison is unavailable for this client"}
	}
	if cfg.ConfigFingerprint != clientFingerprint {
		return supportCheck{Name: "configuration-current", Status: "fail", Summary: "configuration changed after agentd started"}
	}
	return supportCheck{Name: "configuration-current", Status: "pass", Summary: "agentd is using the current configuration"}
}

func modelConfigurationSupportCheck(cfg config) supportCheck {
	if routes, err := loadModelEgressRoutes(cfg); err == nil && len(routes) > 0 {
		return supportCheck{Name: "model-configuration", Status: "pass", Summary: fmt.Sprintf("private credentials are available for %d managed provider(s)", len(routes))}
	}
	if bundle, err := decodeGatewayCredentialBundle([]byte(gatewayCredentialPayload(cfg))); err == nil && len(bundle.ProviderKeys) > 0 {
		if providers, err := loadManagedProviderDefinitions(managedProviderConfigPath(cfg)); err == nil {
			configured := 0
			for _, provider := range providers {
				if bundle.ProviderKeys[provider["credentialEnv"].(string)] != "" {
					configured++
				}
			}
			if configured > 0 {
				return supportCheck{Name: "model-configuration", Status: "pass", Summary: fmt.Sprintf("private credentials are available for %d configured provider(s)", configured)}
			}
		}
	}
	if document, status := readPrivateInventoryConfig(filepath.Join(cfg.AgentDir, "auth.json")); status == "ok" && len(document) > 0 {
		return supportCheck{Name: "model-configuration", Status: "pass", Summary: "a private Pi authentication store is configured"}
	}
	if document, status := readPrivateInventoryConfig(filepath.Join(cfg.AgentDir, "models.json")); status == "ok" {
		if entries, valid := configuredProviders(document); valid && len(entries) > 0 {
			return supportCheck{Name: "model-configuration", Status: "pass", Summary: fmt.Sprintf("%d custom model provider(s) are configured", len(entries))}
		}
	}
	return supportCheck{Name: "model-configuration", Status: "warn", Summary: "no usable model provider is configured"}
}

func diagnosticPrivateTargets(cfg config) []diagnosticPrivateTarget {
	targets := []diagnosticPrivateTarget{
		{name: "config-directory", path: cfg.ConfigRoot, directory: true},
		{name: "agent-directory", path: cfg.AgentDir, directory: true},
		{name: "state-directory", path: cfg.StateRoot, directory: true},
		{name: "runtime-directory", path: cfg.AgentdRoot, directory: true},
		{name: "task-directory", path: cfg.TasksRoot, directory: true},
		{name: "worktree-directory", path: cfg.WorktreesRoot, directory: true},
		{name: "attach-cursor-directory", path: cfg.AttachCursorRoot, directory: true},
		{name: "support-directory", path: cfg.SupportRoot, directory: true},
		{name: "session-directory", path: cfg.SessionDir, directory: true},
		{name: "credential-directory", path: gatewayCredentialDirectory(cfg), directory: true},
		{name: "socket-directory", path: filepath.Dir(cfg.SocketPath), directory: true},
		{name: "agentd-log", path: cfg.LogPath},
	}
	if cfg.ModelEgressRoot != "" {
		targets = append(targets, diagnosticPrivateTarget{name: "model-egress-directory", path: cfg.ModelEgressRoot, directory: true})
	}
	seen := map[string]bool{}
	result := make([]diagnosticPrivateTarget, 0, len(targets))
	for _, target := range targets {
		target.path = filepath.Clean(target.path)
		if !safeDiagnosticRepairPath(target.path) || seen[target.path] {
			continue
		}
		seen[target.path] = true
		result = append(result, target)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].path < result[right].path })
	return result
}

func safeDiagnosticRepairPath(path string) bool {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == string(filepath.Separator) {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == path {
		return false
	}
	for _, protected := range []string{"/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib64", "/mnt", "/opt", "/proc", "/root", "/run", "/sbin", "/srv", "/sys", "/tmp", "/usr", "/var"} {
		if path == protected {
			return false
		}
	}
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(strings.TrimPrefix(path, volume), string(filepath.Separator))
	return strings.Contains(relative, string(filepath.Separator))
}

func privatePermissionRepairState(cfg config) (repairable, blocked int) {
	for _, target := range diagnosticPrivateTargets(cfg) {
		eligible, unsafe, exists := privateTargetRepairable(target)
		if !exists {
			continue
		}
		if unsafe {
			blocked++
			continue
		}
		if eligible {
			repairable++
		}
	}
	return repairable, blocked
}

func privateTargetRepairable(target diagnosticPrivateTarget) (eligible, unsafe, exists bool) {
	info, err := os.Lstat(target.path)
	if os.IsNotExist(err) {
		return false, false, false
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || (target.directory && !info.IsDir()) || (!target.directory && !info.Mode().IsRegular()) {
		return false, true, true
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return false, true, true
	}
	return info.Mode().Perm()&0o077 != 0, false, true
}

func (server *daemonServer) diagnosticRepairActions(clientFingerprint string) []diagnosticRepairAction {
	actions := make([]diagnosticRepairAction, 0, 2)
	repairable, blocked := privatePermissionRepairState(server.cfg)
	if repairable > 0 || blocked > 0 {
		status := "available"
		reason := fmt.Sprintf("%d known private path(s) can be restricted to the current user", repairable)
		if repairable == 0 {
			status = "blocked"
			reason = "unsafe type, symbolic link, ownership, or unreadable metadata requires manual review"
		} else if blocked > 0 {
			reason += fmt.Sprintf("; %d unsafe path(s) will remain unchanged", blocked)
		}
		actions = append(actions, diagnosticRepairAction{
			ID: diagnosticRepairPrivatePermissions, Executor: "agentd", Status: status, RequiresConfirmation: true,
			Summary: "Restrict known Hobot Code runtime paths to the current user", Reason: reason,
		})
	}
	if server.cfg.ConfigFingerprint != "" && clientFingerprint != "" && server.cfg.ConfigFingerprint != clientFingerprint {
		status := "available"
		reason := "agentd can be restarted without interrupting work"
		active, queued := server.manager.activeCount(), server.manager.queuedCount()
		if active > 0 || queued > 0 {
			status = "blocked"
			reason = fmt.Sprintf("%d active and %d queued task(s) must finish or be stopped first", active, queued)
		}
		actions = append(actions, diagnosticRepairAction{
			ID: diagnosticRepairRestartDaemon, Executor: "client", Status: status, RequiresConfirmation: true,
			Summary: "Restart agentd with the current configuration", Reason: reason,
		})
	}
	return actions
}

func (server *daemonServer) repairDiagnostics(params diagnosticRepairParams, clientFingerprint string) (diagnosticRepairResult, error) {
	if !params.Confirm {
		return diagnosticRepairResult{}, fmt.Errorf("diagnostic repair requires explicit confirmation")
	}
	if params.Action != diagnosticRepairPrivatePermissions {
		return diagnosticRepairResult{}, fmt.Errorf("unsupported agentd repair action: %s", params.Action)
	}
	repairable, _ := privatePermissionRepairState(server.cfg)
	if repairable == 0 {
		return diagnosticRepairResult{}, fmt.Errorf("private runtime permission repair is not available")
	}
	changed := 0
	for _, target := range diagnosticPrivateTargets(server.cfg) {
		eligible, _, _ := privateTargetRepairable(target)
		if !eligible {
			continue
		}
		if err := chmodPrivatePathNoFollow(target.path, target.directory); err != nil {
			return diagnosticRepairResult{}, fmt.Errorf("repair %s: %w", target.name, err)
		}
		changed++
	}
	report, err := server.inspectDiagnostics(clientFingerprint)
	if err != nil {
		return diagnosticRepairResult{}, err
	}
	return diagnosticRepairResult{SchemaVersion: diagnosticsSchemaVersion, Action: params.Action, Changed: changed, Report: report}, nil
}

func diagnosticRepairByID(actions []diagnosticRepairAction, id string) (diagnosticRepairAction, bool) {
	for _, action := range actions {
		if strings.EqualFold(action.ID, id) {
			return action, true
		}
	}
	return diagnosticRepairAction{}, false
}
