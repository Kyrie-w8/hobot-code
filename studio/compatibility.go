package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bryant-w/hobot-code/sdk/go/hobot"
)

//go:embed wails.json
var studioManifest []byte

type compatibilityIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Action   string `json:"action,omitempty"`
}

type connectionCompatibility struct {
	Status          string               `json:"status"`
	Summary         string               `json:"summary"`
	AppVersion      string               `json:"appVersion"`
	AgentdVersion   string               `json:"agentdVersion"`
	Protocol        int                  `json:"protocol"`
	EventSchema     int                  `json:"eventSchema"`
	BoardID         string               `json:"boardId,omitempty"`
	RDKOSVersion    string               `json:"rdkOsVersion,omitempty"`
	ValidatedTarget bool                 `json:"validatedTarget"`
	Issues          []compatibilityIssue `json:"issues"`
}

type validatedBoardTarget struct {
	Major             int
	Baseline          string
	ValidatedVersions []string
}

var validatedBoardTargets = map[string]validatedBoardTarget{
	"x5":   {Major: 3, Baseline: "3.5.0", ValidatedVersions: []string{"3.5.0"}},
	"s100": {Major: 4, Baseline: "4.0.5", ValidatedVersions: []string{"4.0.5", "4.0.5-Beta"}},
	"s600": {Major: 5, Baseline: "5.1.0", ValidatedVersions: []string{"5.1.0"}},
}

var requiredStudioCapabilities = []string{"tasks.lifecycle", "tasks.page", "events.page"}
var recommendedStudioCapabilities = []struct {
	Name   string
	Impact string
}{
	{Name: "extensions.catalog.v1", Impact: "Installed capabilities and their trust boundaries cannot be inspected from Studio."},
	{Name: "models.capabilities.v1", Impact: "Model input capabilities cannot be negotiated; image attachments stay disabled."},
	{Name: "build.identity.v1", Impact: "This board service cannot prove which source build and Pi runtime are installed."},
	{Name: "models.health.v1", Impact: "Model routes cannot be checked before starting a task."},
	{Name: "models.conformance.v1", Impact: "Models cannot be verified for streaming, tools, continuation, and declared input modes."},
	{Name: "system.snapshot", Impact: "Board health and hardware telemetry are unavailable."},
	{Name: "support.bundle.v1", Impact: "One-click support bundles are unavailable."},
	{Name: "deployments.v1", Impact: "The guided model deployment workflow is unavailable."},
	{Name: "tasks.fork", Impact: "Side agents and edit-from-history are unavailable."},
	{Name: "tasks.queue.v1", Impact: "Busy Agent requests cannot wait safely for a board worker slot."},
	{Name: "tasks.failure.v1", Impact: "Task failures cannot provide safe, structured recovery actions."},
	{Name: "tasks.turn-evidence.v1", Impact: "Interrupted tasks cannot show whether tools completed or the Git workspace changed."},
	{Name: "events.items.v1", Impact: "Rich command and task lifecycle details are unavailable."},
	{Name: "workspaces.browse", Impact: "Project folders cannot be browsed from Studio."},
	{Name: "workspaces.changes.v1", Impact: "Current workspace changes cannot be reviewed in Studio."},
	{Name: "workspaces.isolation.v1", Impact: "New tasks cannot use an isolated Git worktree."},
	{Name: "workspaces.write-leases.v1", Impact: "Concurrent workspace writes cannot be identified before they overlap."},
	{Name: "workspaces.delivery.v1", Impact: "Isolated changes cannot be safely applied to the original project from Studio."},
	{Name: "tasks.sandbox.v1", Impact: "Background Agents cannot expose or select their board-side OS isolation boundary."},
}

func currentStudioVersion() string {
	var manifest struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if json.Unmarshal(studioManifest, &manifest) != nil || strings.TrimSpace(manifest.Info.ProductVersion) == "" {
		return "unknown"
	}
	return strings.TrimSpace(manifest.Info.ProductVersion)
}

func assessConnectionCompatibility(info hobot.DaemonInfo, snapshot *hobot.SystemSnapshot, snapshotErr error) (connectionCompatibility, error) {
	result := connectionCompatibility{
		Status: "supported", Summary: "Board and Studio capabilities are compatible.",
		AppVersion: currentStudioVersion(), AgentdVersion: info.Version, Protocol: info.Protocol,
		EventSchema: info.Capabilities.EventSchema, Issues: make([]compatibilityIssue, 0),
	}
	upgrade := func(code, message string) (connectionCompatibility, error) {
		result.Status = "upgrade-required"
		result.Summary = message
		result.Issues = append(result.Issues, compatibilityIssue{Code: code, Severity: "error", Message: message, Action: "Update Hobot Code on the board, then reconnect."})
		return result, fmt.Errorf("incompatible board service: %s", message)
	}
	if info.ConfigurationCurrent != nil && !*info.ConfigurationCurrent {
		message := "The board model configuration changed after its background service started."
		result.Status = "upgrade-required"
		result.Summary = message
		result.Issues = append(result.Issues, compatibilityIssue{
			Code:     "configuration-restart-required",
			Severity: "error",
			Message:  message,
			Action:   "Run `hobot daemon restart` on the board, then reconnect.",
		})
		return result, fmt.Errorf("board configuration changed; run `hobot daemon restart` on the board, then reconnect")
	}
	capabilities := info.Capabilities
	if info.Protocol != hobot.ProtocolVersion || capabilities.ProtocolMin > hobot.ProtocolVersion || capabilities.ProtocolMax < hobot.ProtocolVersion {
		return upgrade("protocol-incompatible", fmt.Sprintf("Studio protocol %d is outside the board range %d-%d", hobot.ProtocolVersion, capabilities.ProtocolMin, capabilities.ProtocolMax))
	}
	if capabilities.EventSchema < 2 {
		return upgrade("event-schema-incompatible", fmt.Sprintf("board event schema %d is older than the minimum supported schema 2", capabilities.EventSchema))
	}
	for _, capability := range requiredStudioCapabilities {
		if !containsValue(capabilities.Capabilities, capability) {
			return upgrade("missing-"+capability, fmt.Sprintf("board service does not provide required capability %s", capability))
		}
	}

	addWarning := func(code, message, action string) {
		result.Status = "limited"
		result.Summary = "Connected with limited features."
		result.Issues = append(result.Issues, compatibilityIssue{Code: code, Severity: "warning", Message: message, Action: action})
	}
	for _, capability := range recommendedStudioCapabilities {
		if !containsValue(capabilities.Capabilities, capability.Name) {
			addWarning("missing-"+capability.Name, capability.Impact, "Update Hobot Code on the board to enable this feature.")
		}
	}
	if containsValue(capabilities.Capabilities, "build.identity.v1") {
		switch info.Build.Status {
		case "verified":
			if info.Build.Dirty != nil && *info.Build.Dirty {
				addWarning("unreleased-board-build", "The board is running an unreleased build from a modified worktree.", "Install a clean signed Hobot Code release before production use.")
			}
		case "invalid":
			addWarning("invalid-build-identity", "The board executable and its release metadata cannot be trusted as one build.", "Reinstall Hobot Code from a verified release archive.")
		default:
			addWarning("missing-build-identity", "The board service could not verify its release provenance.", "Reinstall or update Hobot Code on the board before production use.")
		}
	}
	sandboxReported := capabilities.Sandbox.Backend != "" || capabilities.Sandbox.Reason != "" || len(capabilities.Sandbox.Profiles) > 0
	if containsValue(capabilities.Capabilities, "tasks.sandbox.v1") && sandboxReported && !capabilities.Sandbox.Available {
		reason := strings.TrimSpace(capabilities.Sandbox.Reason)
		if reason == "" {
			reason = "The board could not validate its OS sandbox backend."
		}
		addWarning("sandbox-unavailable", reason, "Install or repair bubblewrap on the board, restart agentd, and run Hobot Code diagnostics.")
	}
	if capabilities.EventSchema < 3 {
		addWarning("legacy-event-schema", "The board uses a legacy event schema; some live activity details may be incomplete.", "Update Hobot Code on the board for normalized event schema 3.")
	}
	if differentReleaseLine(result.AppVersion, result.AgentdVersion) {
		addWarning("version-line-mismatch", fmt.Sprintf("Studio %s and board service %s are from different release lines.", result.AppVersion, result.AgentdVersion), "Keep Studio and board-side Hobot Code on the same major and minor version.")
	}
	if snapshotErr != nil {
		addWarning("snapshot-unavailable", "Board identity and RDK OS compatibility could not be verified.", "Reconnect after checking the board-side system snapshot service.")
		return result, nil
	}
	if snapshot == nil {
		if containsValue(capabilities.Capabilities, "system.snapshot") {
			addWarning("snapshot-missing", "The board did not return hardware identity information.", "Run board diagnostics and reconnect.")
		}
		return result, nil
	}
	result.BoardID = strings.ToLower(strings.TrimSpace(snapshot.BoardID))
	result.RDKOSVersion = strings.TrimSpace(snapshot.RDKOSVersion)
	target, known := validatedBoardTargets[result.BoardID]
	if !known {
		addWarning("board-unverified", fmt.Sprintf("Board type %q is not in the Hobot Code validation matrix.", result.BoardID), "Use X5, S100, or S600 support as a reference and verify every hardware-specific workflow.")
		return result, nil
	}
	major, ok := versionMajor(result.RDKOSVersion)
	if !ok {
		addWarning("rdk-os-unknown", "The RDK OS version could not be parsed.", fmt.Sprintf("Verify the %s board is on the RDK OS %d.x release line.", strings.ToUpper(result.BoardID), target.Major))
		return result, nil
	}
	if major != target.Major {
		addWarning("rdk-os-line-mismatch", fmt.Sprintf("%s expects the RDK OS %d.x release line, but the board reports %s.", strings.ToUpper(result.BoardID), target.Major, result.RDKOSVersion), "Install a board-matched RDK OS before running hardware-specific deployment workflows.")
		return result, nil
	}
	result.ValidatedTarget = containsFoldedValue(target.ValidatedVersions, result.RDKOSVersion)
	if !result.ValidatedTarget {
		addWarning("rdk-os-unvalidated-version", fmt.Sprintf("RDK OS %s is on the supported %d.x line but is not one of the validated releases (%s; baseline %s).", result.RDKOSVersion, target.Major, strings.Join(target.ValidatedVersions, ", "), target.Baseline), "Save a support bundle and verify board-specific workflows before production deployment.")
	}
	return result, nil
}

func containsValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsFoldedValue(values []string, wanted string) bool {
	wanted = strings.TrimSpace(wanted)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

func versionMajor(value string) (int, bool) {
	part := strings.SplitN(strings.TrimSpace(value), ".", 2)[0]
	major, err := strconv.Atoi(part)
	return major, err == nil && major >= 0
}

func differentReleaseLine(left, right string) bool {
	line := func(value string) string {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
		if len(parts) < 2 {
			return ""
		}
		if _, err := strconv.Atoi(parts[0]); err != nil {
			return ""
		}
		if _, err := strconv.Atoi(parts[1]); err != nil {
			return ""
		}
		return parts[0] + "." + parts[1]
	}
	leftLine, rightLine := line(left), line(right)
	return leftLine != "" && rightLine != "" && leftLine != rightLine
}
