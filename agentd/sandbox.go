package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	sandboxModeReview    = "review"
	sandboxModeWorkspace = "workspace"
	sandboxModeSystem    = "system"
	sandboxModeOff       = "off"
	sandboxUnavailable   = "unavailable"
)

type taskSandboxStatus struct {
	Requested            string `json:"requested"`
	Effective            string `json:"effective"`
	Backend              string `json:"backend"`
	FilesystemRestricted bool   `json:"filesystemRestricted"`
	DevicesRestricted    bool   `json:"devicesRestricted"`
	CapabilitiesDropped  bool   `json:"capabilitiesDropped"`
	NetworkRestricted    bool   `json:"networkRestricted"`
	Reason               string `json:"reason,omitempty"`
}

func configuredSandboxBinary(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("HOBOT_CODE_BWRAP must be an absolute path")
		}
		if err := validateSandboxBinary(value); err != nil {
			return "", fmt.Errorf("HOBOT_CODE_BWRAP: %w", err)
		}
		return filepath.Clean(value), nil
	}
	if runtime.GOOS != "linux" {
		return "", nil
	}
	for _, candidate := range []string{"/usr/bin/bwrap", "/bin/bwrap"} {
		if validateSandboxBinary(candidate) == nil {
			return candidate, nil
		}
	}
	return sandboxUnavailable, nil
}

func validateSandboxBinary(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("sandbox backend must be an executable regular file: %s", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("sandbox backend must not be group- or world-writable: %s", path)
	}
	if owner, ok := fileOwner(info); ok && owner != 0 && owner != os.Getuid() {
		return fmt.Errorf("sandbox backend is owned by unexpected uid %d: %s", owner, path)
	}
	return nil
}

func normalizeSandboxMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case sandboxModeReview, sandboxModeWorkspace, sandboxModeSystem, sandboxModeOff:
		return value, nil
	default:
		return "", fmt.Errorf("sandbox mode must be review, workspace, system, or off")
	}
}

func defaultSandboxMode(permissionMode string, deployment bool) string {
	if deployment {
		return sandboxModeSystem
	}
	if permissionMode == "review" {
		return sandboxModeReview
	}
	return sandboxModeWorkspace
}

func sandboxStatus(mode, backend, reason string) taskSandboxStatus {
	status := taskSandboxStatus{
		Requested: mode, Effective: mode, Backend: backend,
		FilesystemRestricted: mode != sandboxModeOff,
		DevicesRestricted:    mode == sandboxModeReview || mode == sandboxModeWorkspace,
		CapabilitiesDropped:  mode != sandboxModeOff,
		NetworkRestricted:    false,
		Reason:               reason,
	}
	if mode == sandboxModeOff {
		status.Backend = "none"
	}
	return status
}

func normalizePersistedSandbox(mode string, status taskSandboxStatus, permissionMode string, deployment bool) (string, taskSandboxStatus) {
	normalized, err := normalizeSandboxMode(mode)
	if err != nil {
		// Existing tasks predate process isolation. Preserve their behavior rather
		// than silently tightening the environment on Resume.
		return sandboxModeOff, sandboxStatus(sandboxModeOff, "none", "legacy task created before OS sandboxing")
	}
	if status.Effective == "" {
		backend := "bubblewrap"
		if normalized == sandboxModeOff {
			backend = "none"
		}
		status = sandboxStatus(normalized, backend, "restored from task metadata")
	}
	status.Requested = normalized
	status.Effective = normalized
	return normalized, status
}

func (manager *taskManager) resolveTaskSandbox(requested, permissionMode string, deployment bool) (string, taskSandboxStatus, error) {
	mode := strings.TrimSpace(requested)
	if mode == "" {
		mode = defaultSandboxMode(permissionMode, deployment)
	}
	mode, err := normalizeSandboxMode(mode)
	if err != nil {
		return "", taskSandboxStatus{}, err
	}
	if mode == sandboxModeOff {
		return mode, sandboxStatus(mode, "none", "disabled explicitly for this task"), nil
	}
	if manager.cfg.SandboxBinary == "" {
		// Non-Linux development hosts cannot enforce the board sandbox. Release
		// builds on Linux use the explicit unavailable sentinel and fail closed.
		return sandboxModeOff, sandboxStatus(sandboxModeOff, "none", "OS sandboxing is available on Linux board targets"), nil
	}
	if manager.cfg.SandboxBinary == sandboxUnavailable {
		return "", taskSandboxStatus{}, fmt.Errorf("OS sandbox is unavailable: install bubblewrap, restart agentd, or explicitly choose sandbox mode off")
	}
	if err := validateSandboxBinary(manager.cfg.SandboxBinary); err != nil {
		return "", taskSandboxStatus{}, err
	}
	return mode, sandboxStatus(mode, "bubblewrap", "host network remains available for model and developer services"), nil
}

func (manager *taskManager) sandboxCommand(metadata taskMetadata, agentArgs []string) (string, []string, taskSandboxStatus, error) {
	mode, status, err := manager.resolveTaskSandbox(metadata.SandboxMode, metadata.PermissionMode, metadata.Deployment != nil)
	if err != nil {
		return "", nil, taskSandboxStatus{}, err
	}
	if mode == sandboxModeOff {
		return manager.cfg.AgentBinary, agentArgs, status, nil
	}
	if err := probeSandboxBackend(manager.cfg.SandboxBinary); err != nil {
		return "", nil, taskSandboxStatus{}, fmt.Errorf("OS sandbox self-test failed: %w; fix bubblewrap or explicitly choose sandbox mode off", err)
	}
	writable, err := manager.sandboxWritableDirectories(metadata, mode)
	if err != nil {
		return "", nil, taskSandboxStatus{}, err
	}
	args := []string{
		"--unshare-user", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup",
		"--die-with-parent", "--new-session", "--cap-drop", "ALL",
		"--ro-bind", "/", "/",
	}
	args = append(args, "--dev", "/dev")
	if mode == sandboxModeSystem {
		for _, device := range sandboxHardwareDevices() {
			args = append(args, "--dev-bind", device, device)
		}
	}
	for _, temporaryRoot := range []string{"/tmp", "/var/tmp"} {
		if manager.canMaskTemporaryRoot(temporaryRoot, metadata, writable) {
			args = append(args, "--tmpfs", temporaryRoot)
		}
	}
	for _, path := range writable {
		args = append(args, "--bind", path, path)
	}
	readOnlyPaths := manager.sandboxReadOnlyPaths()
	for _, path := range []string{manager.cfg.AgentdRoot, manager.cfg.SessionDir} {
		if pathIsDirectory(path) {
			readOnlyPaths = append(readOnlyPaths, filepath.Clean(path))
		}
	}
	sort.Slice(readOnlyPaths, func(left, right int) bool {
		return strings.Count(readOnlyPaths[left], string(filepath.Separator)) < strings.Count(readOnlyPaths[right], string(filepath.Separator))
	})
	for _, path := range readOnlyPaths {
		args = append(args, "--ro-bind", path, path)
	}
	for _, privateState := range []string{filepath.Dir(manager.cfg.SocketPath)} {
		if sandboxPathWithin(privateState, os.TempDir()) || sandboxPathWithin(privateState, "/var/tmp") {
			continue
		}
		if pathsOverlap(privateState, metadata.Cwd) || pathsOverlap(privateState, manager.cfg.SessionDir) {
			continue
		}
		if pathIsDirectory(privateState) {
			args = append(args, "--tmpfs", privateState)
		}
	}
	// Remount the policy directory rather than its file so daemon-side atomic
	// replacements remain visible to the long-running worker.
	args = append(args, "--bind", filepath.Join(manager.cfg.SessionDir, metadata.ID), filepath.Join(manager.cfg.SessionDir, metadata.ID))
	args = append(args, "--ro-bind", filepath.Join(manager.cfg.SessionDir, metadata.ID, "policy"), filepath.Join(manager.cfg.SessionDir, metadata.ID, "policy"))
	args = append(args, "--chdir", metadata.Cwd, "--", manager.cfg.AgentBinary)
	args = append(args, agentArgs...)
	return manager.cfg.SandboxBinary, args, status, nil
}

func (manager *taskManager) canMaskTemporaryRoot(root string, metadata taskMetadata, writable []string) bool {
	paths := append([]string{metadata.Cwd, manager.cfg.StateRoot, manager.cfg.SessionDir, manager.cfg.AgentBinary}, writable...)
	paths = append(paths, manager.sandboxReadOnlyPaths()...)
	for _, path := range paths {
		if filepath.IsAbs(path) && sandboxPathWithin(path, root) {
			return false
		}
	}
	return true
}

func sandboxPathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func pathIsDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func sandboxHardwareDevices() []string {
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil
	}
	result := make([]string, 0, 24)
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		allowed := name == "bpu" || name == "ion" || name == "hbmem" || name == "dma_heap" || name == "dri" ||
			name == "ac_isp" || name == "galcore" || strings.HasPrefix(name, "mali") ||
			strings.HasPrefix(name, "bpu_core") || strings.HasPrefix(name, "dnn") || strings.HasPrefix(name, "video") ||
			strings.HasPrefix(name, "media") || strings.HasPrefix(name, "v4l-subdev") || strings.HasPrefix(name, "vpu") ||
			strings.HasPrefix(name, "jpu") || strings.HasPrefix(name, "isp") || strings.HasPrefix(name, "vs-isp") ||
			strings.HasPrefix(name, "codec") || (strings.HasPrefix(name, "dcore") && strings.Contains(name, "bpu"))
		if !allowed || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		result = append(result, filepath.Join("/dev", entry.Name()))
	}
	sort.Strings(result)
	return result
}

func probeSandboxBackend(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path,
		"--unshare-user", "--die-with-parent", "--new-session", "--cap-drop", "ALL",
		"--ro-bind", "/", "/", "--dev", "/dev", "--", "/bin/true")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("timed out")
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 240 {
			message = message[:240]
		}
		if message != "" {
			return fmt.Errorf("%v: %s", err, message)
		}
		return err
	}
	return nil
}

func (manager *taskManager) sandboxWritableDirectories(metadata taskMetadata, mode string) ([]string, error) {
	paths := []string{
		filepath.Join(manager.cfg.SessionDir, metadata.ID),
		filepath.Join(manager.cfg.StateRoot, "audit"),
		filepath.Join(manager.cfg.StateRoot, "side-agent-leases"),
	}
	if mode == sandboxModeWorkspace || mode == sandboxModeSystem {
		paths = append(paths,
			filepath.Join(manager.cfg.StateRoot, "memory"),
			filepath.Join(manager.cfg.StateRoot, "goals"),
			filepath.Join(manager.cfg.StateRoot, "workspace-write-leases"),
		)
	}
	if mode == sandboxModeSystem {
		paths = append(paths, filepath.Join(manager.cfg.StateRoot, "hardware-leases"))
	}
	variables := []string{"HOBOT_CODE_HOOK_AUDIT"}
	if mode == sandboxModeWorkspace || mode == sandboxModeSystem {
		variables = append(variables, "HOBOT_CODE_MEMORY_DB", "HOBOT_CODE_GOAL_DB")
	}
	for _, variable := range variables {
		if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
			if !filepath.IsAbs(value) {
				return nil, fmt.Errorf("%s must be absolute for sandboxed tasks", variable)
			}
			paths = append(paths, filepath.Dir(filepath.Clean(value)))
		}
	}
	unique := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if path == string(filepath.Separator) {
			return nil, fmt.Errorf("refusing to make the filesystem root writable in the task sandbox")
		}
		if _, exists := unique[path]; exists {
			continue
		}
		if err := ensurePrivateDir(path); err != nil {
			return nil, fmt.Errorf("prepare sandbox writable directory %s: %w", path, err)
		}
		unique[path] = struct{}{}
		result = append(result, path)
	}
	if mode == sandboxModeWorkspace || mode == sandboxModeSystem {
		cwd := filepath.Clean(metadata.Cwd)
		if cwd == string(filepath.Separator) {
			return nil, fmt.Errorf("refusing to make the filesystem root writable in the task sandbox")
		}
		info, err := os.Lstat(cwd)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("task sandbox workspace must be an existing non-symbolic-link directory: %s", cwd)
		}
		if _, exists := unique[cwd]; !exists {
			unique[cwd] = struct{}{}
			result = append(result, cwd)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (manager *taskManager) sandboxReadOnlyPaths() []string {
	paths := []string{}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if filepath.IsAbs(home) {
		configRoot := strings.TrimSpace(os.Getenv("HOBOT_CODE_CONFIG_DIR"))
		if configRoot == "" {
			configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
			if !filepath.IsAbs(configHome) {
				configHome = filepath.Join(home, ".config")
			}
			configRoot = filepath.Join(configHome, "hobot-code")
		}
		paths = append(paths, configRoot, filepath.Join(home, ".ssh"), filepath.Join(home, ".gnupg"))
	}
	unique := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		if _, exists := unique[path]; !exists {
			unique[path] = struct{}{}
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result
}

func sandboxSupportCheck(cfg config) supportCheck {
	if cfg.SandboxBinary == "" {
		return supportCheck{Name: "os-sandbox", Status: "warn", Summary: "available only on Linux board targets"}
	}
	if cfg.SandboxBinary == sandboxUnavailable {
		return supportCheck{Name: "os-sandbox", Status: "fail", Summary: "bubblewrap is not installed"}
	}
	if err := validateSandboxBinary(cfg.SandboxBinary); err != nil {
		return supportCheck{Name: "os-sandbox", Status: "fail", Summary: "bubblewrap is unsafe or unavailable"}
	}
	if err := probeSandboxBackend(cfg.SandboxBinary); err != nil {
		return supportCheck{Name: "os-sandbox", Status: "fail", Summary: "bubblewrap self-test failed"}
	}
	return supportCheck{Name: "os-sandbox", Status: "pass", Summary: "bubblewrap filesystem, device, and capability isolation is available; network is shared"}
}

func sandboxCapabilityStatus(cfg config) sandboxCapability {
	capability := sandboxCapability{Profiles: []string{sandboxModeOff}}
	if cfg.SandboxBinary == "" {
		capability.Reason = "OS sandboxing is available on Linux board targets"
		return capability
	}
	if cfg.SandboxBinary == sandboxUnavailable {
		capability.Reason = "bubblewrap is not installed"
		return capability
	}
	if err := validateSandboxBinary(cfg.SandboxBinary); err != nil {
		capability.Reason = "bubblewrap is unsafe or unavailable"
		return capability
	}
	if err := probeSandboxBackend(cfg.SandboxBinary); err != nil {
		capability.Reason = "bubblewrap self-test failed"
		return capability
	}
	capability.Available = true
	capability.Backend = "bubblewrap"
	capability.Profiles = []string{sandboxModeReview, sandboxModeWorkspace, sandboxModeSystem, sandboxModeOff}
	capability.FilesystemWrites = true
	capability.Devices = true
	capability.Capabilities = true
	capability.Reason = "host network remains available for model and developer services"
	return capability
}
