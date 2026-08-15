package hobot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const (
	maximumQualificationChecks              = 32
	maximumQualificationConformanceAttempts = 6
)

var qualificationProviderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var qualificationModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+/-]{0,255}$`)
var qualificationLowerHexPattern = regexp.MustCompile(`^[0-9a-f]+$`)
var qualificationReleaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

var qualificationRuntimeChecks = []string{
	"rpc-lifecycle", "model-selection", "tool-call", "tool-result", "continuation", "settled",
	"parallel-tools", "invalid-argument-recovery", "thinking-stream", "approval-flow", "image-input",
	"context-compaction", "interrupted-session-recovery",
}

var qualificationRDKChecks = []string{
	"model-selection", "target-identity", "live-board-evidence", "versioned-knowledge",
	"evidence-synthesis", "tool-isolation", "settled", "resource-stability",
}

var qualificationRDKProfiles = map[string][]string{
	"read-only-rdk-diagnostic-v1":            {"workspace-coding", "model-deployment", "multimedia-pipeline", "hardware-control"},
	"read-only-model-deployment-planning-v1": {"model-conversion", "board-inference", "accuracy-validation", "performance-benchmark"},
	"read-only-multimedia-planning-v1":       {"camera-capture", "codec-execution", "pipeline-throughput", "device-integration"},
	"read-only-hardware-safety-planning-v1":  {"gpio-write", "can-control", "firmware-update", "power-cycle"},
	"isolated-workspace-coding-v1":           {"repository-edit", "quality-gate", "change-review"},
}
var qualificationRDKProfileRunnable = map[string]bool{
	"read-only-rdk-diagnostic-v1": true, "read-only-model-deployment-planning-v1": true,
	"read-only-multimedia-planning-v1": true, "read-only-hardware-safety-planning-v1": true,
	"isolated-workspace-coding-v1": false,
}
var qualificationRDKNotCovered = qualificationRDKProfiles["read-only-rdk-diagnostic-v1"]

func decodeModelQualification(raw []byte) (ModelQualification, error) {
	var result ModelQualification
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode model qualification: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, fmt.Errorf("decode model qualification: expected one JSON object")
	}
	if err := validateModelQualification(result); err != nil {
		return result, fmt.Errorf("board returned invalid model qualification: %w", err)
	}
	return result, nil
}

func validateModelQualification(result ModelQualification) error {
	if result.SchemaVersion != 1 || !qualificationProviderPattern.MatchString(result.Provider) || !qualificationModelPattern.MatchString(result.Model) {
		return fmt.Errorf("invalid schema or model identity")
	}
	if !oneOf(result.State, "untested", "current", "expired", "stale") || !oneOf(result.Level, "untested", "route", "protocol", "runtime", "rdk-profile", "rdk-profile-release") ||
		!oneOf(result.Outcome, "unknown", "passed", "partial", "failed") || !validQualificationCodes(result.StaleReasons, staleReasonCodes) ||
		!validQualificationCodes(result.StaleLayers, qualificationLayerCodes) || !validQualificationCodes(result.ExpiredLayers, qualificationLayerCodes) {
		return fmt.Errorf("invalid state, level, outcome, or reason")
	}
	if result.State == "untested" {
		if !result.UpdatedAt.IsZero() || result.Level != "untested" || result.Outcome != "unknown" || result.Health != nil || result.Conformance != nil || result.Runtime != nil || result.RDK != nil || len(result.StaleReasons)+len(result.StaleLayers)+len(result.ExpiredLayers) != 0 {
			return fmt.Errorf("untested evidence contains results")
		}
	} else if result.UpdatedAt.IsZero() {
		return fmt.Errorf("tested evidence omitted its timestamp")
	}
	if result.State == "stale" && (len(result.StaleReasons) == 0 || len(result.StaleLayers) == 0) {
		return fmt.Errorf("stale evidence omitted its reason or affected layers")
	}
	if result.State != "stale" && (len(result.StaleReasons) != 0 || len(result.StaleLayers) != 0) {
		return fmt.Errorf("current evidence contains stale markers")
	}
	if result.State == "expired" && len(result.ExpiredLayers) == 0 {
		return fmt.Errorf("expired evidence omitted its affected layers")
	}
	if result.Health != nil && !validQualificationHealth(*result.Health, result.Provider, result.Model) {
		return fmt.Errorf("invalid route evidence")
	}
	if result.Conformance != nil && !validQualificationConformance(*result.Conformance, result.Provider, result.Model) {
		return fmt.Errorf("invalid protocol evidence")
	}
	if result.Runtime != nil && !validQualificationRuntime(*result.Runtime, result.Provider, result.Model) {
		return fmt.Errorf("invalid runtime evidence")
	}
	if result.RDK != nil && (result.RDK.Profile != "read-only-rdk-diagnostic-v1" || !validQualificationRDK(*result.RDK, result.Provider, result.Model)) {
		return fmt.Errorf("invalid RDK profile evidence")
	}
	if result.State != "untested" && result.Health == nil && result.Conformance == nil && result.Runtime == nil && result.RDK == nil {
		return fmt.Errorf("tested qualification contains no evidence")
	}
	if !qualificationConclusionMatchesEvidence(result) {
		return fmt.Errorf("qualification conclusion does not match its current evidence")
	}
	return nil
}

var staleReasonCodes = map[string]bool{
	"configuration-changed": true, "product-version-changed": true, "build-changed": true,
	"pi-runtime-changed": true, "board-changed": true, "rdk-resources-changed": true,
}
var qualificationLayerCodes = map[string]bool{"route": true, "protocol": true, "runtime": true, "rdk": true}

func validQualificationCodes(values []string, allowed map[string]bool) bool {
	if values == nil || len(values) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !allowed[value] || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validQualificationHealth(value ModelHealth, provider, model string) bool {
	return value.Provider == provider && value.Model == model && !value.CheckedAt.IsZero() && value.CheckedAt.Before(value.ExpiresAt) &&
		oneOf(value.Status, "available", "unavailable") && oneOf(value.Category, "ok", "configuration", "authentication", "rate-limited", "model-unavailable", "timeout", "network", "gateway", "protocol") &&
		oneOf(value.Transport, "", "sse", "json") && value.Attempts >= 0 && value.Attempts <= 2 && value.FirstByteMS >= 0 && value.FirstByteMS <= 12_000 && value.LatencyMS >= 0 && value.LatencyMS <= 12_000 &&
		!value.Cached && safeQualificationLabel(value.Message, 1024)
}

func validQualificationConformance(value ModelConformance, provider, model string) bool {
	if value.Provider != provider || value.Model != model || value.SchemaVersion != 1 || value.Scope != "gateway-protocol" || value.RuntimeStatus != "not-tested" || value.RDKTaskStatus != "not-tested" ||
		!oneOf(value.Status, "verified", "compatible", "failed") || value.CheckedAt.IsZero() || !value.CheckedAt.Before(value.ExpiresAt) || value.DurationMS < 0 || value.DurationMS > 45_000 || value.Attempts < 0 || value.Attempts > maximumQualificationConformanceAttempts || value.Cached ||
		len(value.Checks) != 4 || !safeQualificationLabel(value.Message, 2048) {
		return false
	}
	seen := map[string]bool{}
	for _, check := range value.Checks {
		if seen[check.Name] || !oneOf(check.Name, "streaming", "tool-call", "tool-result", "image-input") || !oneOf(check.Status, "passed", "degraded", "failed", "blocked", "skipped") ||
			!safeQualificationLabel(check.Category, 64) || !safeQualificationLabel(check.Message, 2048) || check.LatencyMS < 0 || check.LatencyMS > 45_000 {
			return false
		}
		seen[check.Name] = true
	}
	return true
}

func validQualificationRuntime(value ModelRuntimeProbe, provider, model string) bool {
	if value.Provider != provider || value.Model != model || value.SchemaVersion != 1 || value.Scope != "agent-runtime-partial" || !oneOf(value.Status, "partial", "failed") ||
		value.CheckedAt.IsZero() || value.DurationMS < 0 || value.DurationMS > 15*60*1000 || len(value.Checks) != len(qualificationRuntimeChecks) || len(value.Pending) != 1 || value.Pending[0] != "rdk-task-suite" || !safeQualificationLabel(value.Message, 2048) {
		return false
	}
	seen := map[string]bool{}
	passed := true
	for _, check := range value.Checks {
		if seen[check.Name] || !containsQualificationValue(qualificationRuntimeChecks, check.Name) || !oneOf(check.Status, "passed", "failed", "not-applicable") || !safeQualificationLabel(check.Message, 2048) {
			return false
		}
		seen[check.Name] = true
		switch check.Name {
		case "thinking-stream":
			passed = passed && (value.ReasoningDeclared && check.Status == "passed" || !value.ReasoningDeclared && check.Status == "not-applicable")
		case "image-input":
			passed = passed && (value.ImageInputDeclared && check.Status == "passed" || !value.ImageInputDeclared && check.Status == "not-applicable")
		default:
			passed = passed && check.Status == "passed"
		}
	}
	return passed && value.Status == "partial" && value.Category == "" ||
		!passed && value.Status == "failed" && oneOf(value.Category, "preparation", "configuration", "process", "protocol", "timeout")
}

func validQualificationRDK(value ModelRDKProbe, provider, model string) bool {
	notCovered, knownProfile := qualificationRDKProfiles[value.Profile]
	if value.Provider != provider || value.Model != model || value.SchemaVersion != 1 || value.Scope != "rdk-task-profile" || !knownProfile || !qualificationRDKProfileRunnable[value.Profile] ||
		!oneOf(value.Status, "passed", "failed") || value.CheckedAt.IsZero() || value.DurationMS < 0 || value.DurationMS > 5*60*1000 || len(value.Checks) < 1 || len(value.Checks) > maximumQualificationChecks ||
		len(value.Checks) != len(qualificationRDKChecks) || len(value.Sources) > 64 || !equalQualificationValues(value.NotCovered, notCovered) || !safeQualificationLabel(value.Message, 2048) || !validQualificationRDKBinding(value.Binding) {
		return false
	}
	if value.ReleaseEligible && value.Status != "passed" {
		return false
	}
	seenChecks := map[string]bool{}
	passed := true
	for _, check := range value.Checks {
		if seenChecks[check.Name] || !containsQualificationValue(qualificationRDKChecks, check.Name) || !oneOf(check.Status, "passed", "failed") || !safeQualificationLabel(check.Message, 2048) {
			return false
		}
		seenChecks[check.Name] = true
		passed = passed && check.Status == "passed"
	}
	seenSources := map[string]bool{}
	for _, source := range value.Sources {
		if seenSources[source] || !officialQualificationRDKSource(source) {
			return false
		}
		seenSources[source] = true
	}
	if passed != (value.Status == "passed") || passed && value.Category != "" || !passed && !oneOf(value.Category, "preparation", "configuration", "target", "process", "protocol", "timeout") {
		return false
	}
	return !value.ReleaseEligible || releaseEligibleQualificationBinding(value.Binding)
}

func validQualificationRDKBinding(value ModelRDKProbeBinding) bool {
	return safeQualificationLabel(value.ProductVersion, 64) && safeQualificationLabel(value.BuildStatus, 32) &&
		validOptionalQualificationHex(value.Commit, 40) && safeOptionalQualificationLabel(value.BuildTarget, 64) && validOptionalQualificationHex(value.AgentdBinarySHA256, 64) &&
		safeOptionalQualificationLabel(value.PiVersion, 64) && validOptionalQualificationHex(value.PiCommit, 40) && validOptionalQualificationHex(value.PiCompatibilitySHA256, 64) &&
		validOptionalQualificationHex(value.ExpertPromptSHA256, 64) && validOptionalQualificationHex(value.RDKExtensionSHA256, 64) && validOptionalQualificationHex(value.KnowledgePackSHA256, 64) &&
		safeOptionalQualificationLabel(value.Board, 256) && safeOptionalQualificationLabel(value.BoardID, 32) && safeOptionalQualificationLabel(value.RDKOSVersion, 128) &&
		safeOptionalQualificationLabel(value.Architecture, 32) && safeOptionalQualificationLabel(value.KnowledgeVersion, 128) && safeOptionalQualificationLabel(value.KnowledgeUpdatedAt, 64)
}

func validOptionalQualificationHex(value string, size int) bool {
	return value == "" || len(value) == size && qualificationLowerHexPattern.MatchString(value)
}

func officialQualificationRDKSource(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "developer.d-robotics.cc":
		return parsed.Port() == "" && strings.HasPrefix(parsed.EscapedPath(), "/")
	case "github.com":
		return parsed.Port() == "" && (parsed.EscapedPath() == "/D-Robotics" || strings.HasPrefix(parsed.EscapedPath(), "/D-Robotics/"))
	default:
		return false
	}
}

func releaseEligibleQualificationBinding(value ModelRDKProbeBinding) bool {
	return qualificationReleaseVersionPattern.MatchString(value.ProductVersion) && value.BuildStatus == "verified" && value.Dirty != nil && !*value.Dirty &&
		validOptionalQualificationHex(value.Commit, 40) && value.Commit != "" && value.BuildTarget == "linux-arm64" &&
		validOptionalQualificationHex(value.AgentdBinarySHA256, 64) && value.AgentdBinarySHA256 != "" && value.PiVersion != "" &&
		validOptionalQualificationHex(value.PiCommit, 40) && value.PiCommit != "" && validOptionalQualificationHex(value.PiCompatibilitySHA256, 64) && value.PiCompatibilitySHA256 != "" &&
		validOptionalQualificationHex(value.ExpertPromptSHA256, 64) && value.ExpertPromptSHA256 != "" && validOptionalQualificationHex(value.RDKExtensionSHA256, 64) && value.RDKExtensionSHA256 != "" &&
		validOptionalQualificationHex(value.KnowledgePackSHA256, 64) && value.KnowledgePackSHA256 != "" && value.KnowledgeVersion != "" && value.KnowledgeUpdatedAt != "" &&
		oneOf(value.BoardID, "x5", "s100", "s600") && value.RDKOSVersion != "" && value.Architecture == "arm64"
}

func qualificationConclusionMatchesEvidence(result ModelQualification) bool {
	if result.State == "untested" {
		return true
	}
	present := map[string]bool{
		"route": result.Health != nil, "protocol": result.Conformance != nil, "runtime": result.Runtime != nil, "rdk": result.RDK != nil,
	}
	if result.State == "stale" {
		all := false
		for _, reason := range result.StaleReasons {
			if reason != "board-changed" && reason != "rdk-resources-changed" {
				all = true
			}
		}
		expected := make([]string, 0, 4)
		for _, layer := range []string{"route", "protocol", "runtime"} {
			if all && present[layer] {
				expected = append(expected, layer)
			}
		}
		if present["rdk"] {
			expected = append(expected, "rdk")
		}
		if !equalQualificationValues(result.StaleLayers, expected) {
			return false
		}
	}
	for _, layer := range result.ExpiredLayers {
		if !oneOf(layer, "route", "protocol") || !present[layer] {
			return false
		}
	}
	excluded := map[string]bool{}
	for _, layer := range append(append([]string{}, result.StaleLayers...), result.ExpiredLayers...) {
		excluded[layer] = true
	}
	level, outcome := "untested", "unknown"
	switch {
	case result.RDK != nil && !excluded["rdk"]:
		level = "rdk-profile"
		if result.RDK.ReleaseEligible {
			level = "rdk-profile-release"
		}
		outcome = "passed"
		if result.RDK.Status == "failed" {
			outcome = "failed"
		}
	case result.Runtime != nil && !excluded["runtime"]:
		level, outcome = "runtime", "partial"
		if result.Runtime.Status == "failed" {
			outcome = "failed"
		}
	case result.Conformance != nil && !excluded["protocol"]:
		level, outcome = "protocol", "passed"
		if result.Conformance.Status == "compatible" {
			outcome = "partial"
		} else if result.Conformance.Status == "failed" {
			outcome = "failed"
		}
	case result.Health != nil && !excluded["route"]:
		level, outcome = "route", "passed"
		if result.Health.Status == "unavailable" {
			outcome = "failed"
		}
	}
	expectedState := "current"
	if len(result.StaleLayers) > 0 {
		expectedState = "stale"
	} else if level == "untested" && len(result.ExpiredLayers) > 0 {
		expectedState = "expired"
	}
	return result.State == expectedState && result.Level == level && result.Outcome == outcome
}

func containsQualificationValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func equalQualificationValues(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func safeQualificationLabel(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func safeOptionalQualificationLabel(value string, maximum int) bool {
	return value == "" || safeQualificationLabel(value, maximum)
}
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
