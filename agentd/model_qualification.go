package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"
)

const (
	modelQualificationSchema         = 1
	maximumModelQualificationRecords = 32
)

type modelQualificationParams struct {
	Model string `json:"model,omitempty"`
}

type modelQualificationBinding struct {
	ProductVersion        string `json:"productVersion"`
	ConfigFingerprint     string `json:"configFingerprint,omitempty"`
	BuildStatus           string `json:"buildStatus"`
	Commit                string `json:"commit,omitempty"`
	Dirty                 *bool  `json:"dirty,omitempty"`
	BuildTarget           string `json:"buildTarget,omitempty"`
	AgentdBinarySHA256    string `json:"agentdBinarySha256,omitempty"`
	PiVersion             string `json:"piVersion,omitempty"`
	PiCommit              string `json:"piCommit,omitempty"`
	PiCompatibilitySHA256 string `json:"piCompatibilitySha256,omitempty"`
}

type modelQualificationRecord struct {
	Provider    string                    `json:"provider"`
	Model       string                    `json:"model"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
	Binding     modelQualificationBinding `json:"binding"`
	Health      *modelHealthResult        `json:"health,omitempty"`
	Conformance *modelConformanceResult   `json:"conformance,omitempty"`
	Runtime     *modelRuntimeProbeResult  `json:"runtime,omitempty"`
	RDK         *modelRDKProbeResult      `json:"rdk,omitempty"`
}

type modelQualificationDocument struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Records       []modelQualificationRecord `json:"records"`
}

// modelQualificationResult is deliberately evidence-oriented: current means
// only that the stored binding still matches, not that every tested layer passed.
type modelQualificationResult struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Provider      string                   `json:"provider"`
	Model         string                   `json:"model"`
	State         string                   `json:"state"`
	Level         string                   `json:"level"`
	Outcome       string                   `json:"outcome"`
	UpdatedAt     time.Time                `json:"updatedAt,omitempty"`
	StaleReasons  []string                 `json:"staleReasons"`
	StaleLayers   []string                 `json:"staleLayers"`
	ExpiredLayers []string                 `json:"expiredLayers"`
	Health        *modelHealthResult       `json:"health,omitempty"`
	Conformance   *modelConformanceResult  `json:"conformance,omitempty"`
	Runtime       *modelRuntimeProbeResult `json:"runtime,omitempty"`
	RDK           *modelRDKProbeResult     `json:"rdk,omitempty"`
}

type modelQualificationStore struct {
	mu   sync.Mutex
	cfg  config
	path string
	now  func() time.Time
}

func newModelQualificationStore(cfg config) *modelQualificationStore {
	path := cfg.QualificationPath
	if path == "" {
		path = filepath.Join(cfg.AgentdRoot, "model-qualification.json")
	}
	return &modelQualificationStore{cfg: cfg, path: path, now: time.Now}
}

func qualificationBinding(cfg config, build buildIdentity) modelQualificationBinding {
	return modelQualificationBinding{
		ProductVersion: version, ConfigFingerprint: cfg.ConfigFingerprint,
		BuildStatus: build.Status, Commit: build.Commit, Dirty: cloneBool(build.Dirty), BuildTarget: build.Target,
		AgentdBinarySHA256: build.BinarySHA256, PiVersion: build.PiVersion, PiCommit: build.PiCommit,
		PiCompatibilitySHA256: build.PiCompatibilitySHA256,
	}
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (store *modelQualificationStore) recordHealth(result modelHealthResult, build buildIdentity) error {
	return store.update(result.Provider, result.Model, build, func(record *modelQualificationRecord) {
		copy := result
		copy.Cached = false
		record.Health = &copy
	})
}

func (store *modelQualificationStore) recordConformance(result modelConformanceResult, build buildIdentity) error {
	return store.update(result.Provider, result.Model, build, func(record *modelQualificationRecord) {
		copy := result
		copy.Cached = false
		record.Conformance = &copy
	})
}

func (store *modelQualificationStore) recordRuntime(result modelRuntimeProbeResult, build buildIdentity) error {
	return store.update(result.Provider, result.Model, build, func(record *modelQualificationRecord) { copy := result; record.Runtime = &copy })
}

func (store *modelQualificationStore) recordRDK(result modelRDKProbeResult, build buildIdentity) error {
	if result.Profile != rdkProbeProfile {
		return nil
	}
	return store.update(result.Provider, result.Model, build, func(record *modelQualificationRecord) { copy := result; record.RDK = &copy })
}

func (store *modelQualificationStore) update(provider, model string, build buildIdentity, mutate func(*modelQualificationRecord)) error {
	selection := joinModel(provider, model)
	if selection == "" {
		return errors.New("qualification evidence has an invalid model identity")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	document, err := store.loadLocked()
	if err != nil {
		return err
	}
	binding := qualificationBinding(store.cfg, build)
	index := -1
	for candidate := range document.Records {
		if joinModel(document.Records[candidate].Provider, document.Records[candidate].Model) == selection {
			index = candidate
			break
		}
	}
	if index < 0 {
		if len(document.Records) >= maximumModelQualificationRecords {
			oldest := 0
			for candidate := 1; candidate < len(document.Records); candidate++ {
				if document.Records[candidate].UpdatedAt.Before(document.Records[oldest].UpdatedAt) {
					oldest = candidate
				}
			}
			index = oldest
		} else {
			document.Records = append(document.Records, modelQualificationRecord{})
			index = len(document.Records) - 1
		}
	}
	record := &document.Records[index]
	if record.Provider != provider || record.Model != model || !reflect.DeepEqual(record.Binding, binding) {
		*record = modelQualificationRecord{Provider: provider, Model: model, Binding: binding}
	}
	mutate(record)
	record.UpdatedAt = store.now().UTC()
	if err := validateModelQualificationRecord(*record); err != nil {
		return fmt.Errorf("refuse invalid qualification evidence: %w", err)
	}
	sort.Slice(document.Records, func(left, right int) bool {
		return joinModel(document.Records[left].Provider, document.Records[left].Model) < joinModel(document.Records[right].Provider, document.Records[right].Model)
	})
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := writePrivateFileDurable(store.path, encoded); err != nil {
		return fmt.Errorf("persist model qualification evidence: %w", err)
	}
	return nil
}

func (store *modelQualificationStore) get(manager *taskManager, params modelQualificationParams, build buildIdentity, clientFingerprint ...string) (modelQualificationResult, error) {
	models, err := manager.availableModels()
	if err != nil {
		return modelQualificationResult{}, fmt.Errorf("discover models: %w", err)
	}
	selection := normalizeModelSelection(params.Model)
	if selection == "" {
		return modelQualificationResult{}, fmt.Errorf("model must use provider/model format")
	}
	model, ok := models[selection]
	if !ok {
		return modelQualificationResult{}, fmt.Errorf("model is not available: %s", selection)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	document, err := store.loadLocked()
	if err != nil {
		return modelQualificationResult{}, err
	}
	result := modelQualificationResult{
		SchemaVersion: modelQualificationSchema, Provider: model.Provider, Model: model.ID,
		State: "untested", Level: "untested", Outcome: "unknown", StaleReasons: []string{}, StaleLayers: []string{}, ExpiredLayers: []string{},
	}
	for index := range document.Records {
		record := &document.Records[index]
		if record.Provider != model.Provider || record.Model != model.ID {
			continue
		}
		result.UpdatedAt = record.UpdatedAt
		result.Health, result.Conformance, result.Runtime, result.RDK = record.Health, record.Conformance, record.Runtime, record.RDK
		now := store.now().UTC()
		if record.Health != nil && !now.Before(record.Health.ExpiresAt) {
			result.ExpiredLayers = append(result.ExpiredLayers, "route")
		}
		if record.Conformance != nil && !now.Before(record.Conformance.ExpiresAt) {
			result.ExpiredLayers = append(result.ExpiredLayers, "protocol")
		}
		fingerprint := store.cfg.ConfigFingerprint
		if len(clientFingerprint) > 0 && clientFingerprint[0] != "" {
			fingerprint = clientFingerprint[0]
		}
		result.StaleReasons = store.staleReasons(*record, build, fingerprint)
		if len(result.StaleReasons) > 0 {
			result.State = "stale"
			result.StaleLayers = staleQualificationLayers(*record, result.StaleReasons)
			current := recordWithoutLayers(*record, result.StaleLayers)
			result.Level, result.Outcome = qualificationLevelAndOutcome(current, result.ExpiredLayers)
			return result, nil
		}
		result.Level, result.Outcome = qualificationLevelAndOutcome(*record, result.ExpiredLayers)
		result.State = "current"
		if result.Level == "untested" && len(result.ExpiredLayers) > 0 {
			result.State = "expired"
		}
		return result, nil
	}
	return result, nil
}

func staleQualificationLayers(record modelQualificationRecord, reasons []string) []string {
	all := false
	for _, reason := range reasons {
		if reason != "board-changed" && reason != "rdk-resources-changed" {
			all = true
			break
		}
	}
	layers := make([]string, 0, 4)
	if all {
		if record.Health != nil {
			layers = append(layers, "route")
		}
		if record.Conformance != nil {
			layers = append(layers, "protocol")
		}
		if record.Runtime != nil {
			layers = append(layers, "runtime")
		}
	}
	if record.RDK != nil {
		layers = append(layers, "rdk")
	}
	return layers
}

func recordWithoutLayers(record modelQualificationRecord, layers []string) modelQualificationRecord {
	for _, layer := range layers {
		switch layer {
		case "route":
			record.Health = nil
		case "protocol":
			record.Conformance = nil
		case "runtime":
			record.Runtime = nil
		case "rdk":
			record.RDK = nil
		}
	}
	return record
}

func (store *modelQualificationStore) staleReasons(record modelQualificationRecord, build buildIdentity, configFingerprint string) []string {
	wanted := qualificationBinding(store.cfg, build)
	wanted.ConfigFingerprint = configFingerprint
	reasons := make([]string, 0, 4)
	if record.Binding.ConfigFingerprint != wanted.ConfigFingerprint {
		reasons = append(reasons, "configuration-changed")
	}
	if record.Binding.ProductVersion != wanted.ProductVersion {
		reasons = append(reasons, "product-version-changed")
	}
	if record.Binding.BuildStatus != wanted.BuildStatus || record.Binding.Commit != wanted.Commit || !equalBool(record.Binding.Dirty, wanted.Dirty) ||
		record.Binding.BuildTarget != wanted.BuildTarget || record.Binding.AgentdBinarySHA256 != wanted.AgentdBinarySHA256 {
		reasons = append(reasons, "build-changed")
	}
	if record.Binding.PiVersion != wanted.PiVersion || record.Binding.PiCommit != wanted.PiCommit || record.Binding.PiCompatibilitySHA256 != wanted.PiCompatibilitySHA256 {
		reasons = append(reasons, "pi-runtime-changed")
	}
	if record.RDK != nil {
		live := collectSystemSnapshot(store.cfg)
		binding := record.RDK.Binding
		if binding.BoardID != live.BoardID || binding.Board != live.Board || binding.RDKOSVersion != live.RDKOSVersion || binding.Architecture != live.Architecture {
			reasons = append(reasons, "board-changed")
		}
		_, _, _, manifest, extension, prompt, knowledge, err := rdkProbeResources(store.cfg)
		if err != nil || binding.RDKExtensionSHA256 != extension || binding.ExpertPromptSHA256 != prompt || binding.KnowledgePackSHA256 != knowledge ||
			binding.KnowledgeVersion != manifest.KnowledgeVersion || binding.KnowledgeUpdatedAt != manifest.UpdatedAt {
			reasons = append(reasons, "rdk-resources-changed")
		}
	}
	return reasons
}

func equalBool(left, right *bool) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func qualificationLevelAndOutcome(record modelQualificationRecord, expired []string) (string, string) {
	expiredSet := map[string]bool{}
	for _, layer := range expired {
		expiredSet[layer] = true
	}
	if record.RDK != nil {
		if record.RDK.Status == "failed" {
			return "rdk-profile", "failed"
		}
		if record.RDK.ReleaseEligible {
			return "rdk-profile-release", "passed"
		}
		return "rdk-profile", "passed"
	}
	if record.Runtime != nil {
		if record.Runtime.Status == "failed" {
			return "runtime", "failed"
		}
		return "runtime", "partial"
	}
	if record.Conformance != nil && !expiredSet["protocol"] {
		if record.Conformance.Status == "failed" {
			return "protocol", "failed"
		}
		if record.Conformance.Status == "compatible" {
			return "protocol", "partial"
		}
		return "protocol", "passed"
	}
	if record.Health != nil && !expiredSet["route"] {
		if record.Health.Status == "available" {
			return "route", "passed"
		}
		return "route", "failed"
	}
	return "untested", "unknown"
}

func (store *modelQualificationStore) loadLocked() (modelQualificationDocument, error) {
	document := modelQualificationDocument{SchemaVersion: modelQualificationSchema, Records: []modelQualificationRecord{}}
	raw, missing, err := readPrivateConfigBytes(store.path)
	if err != nil {
		return document, fmt.Errorf("read model qualification evidence: %w", err)
	}
	if missing {
		return document, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return document, fmt.Errorf("decode model qualification evidence: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return document, errors.New("model qualification evidence must contain exactly one JSON object")
	}
	if document.SchemaVersion != modelQualificationSchema || document.Records == nil || len(document.Records) > maximumModelQualificationRecords {
		return document, errors.New("model qualification evidence has an unsupported schema or record count")
	}
	seen := make(map[string]bool, len(document.Records))
	for _, record := range document.Records {
		selection := joinModel(record.Provider, record.Model)
		if selection == "" || seen[selection] {
			return document, errors.New("model qualification evidence has an invalid or duplicate model")
		}
		seen[selection] = true
		if err := validateModelQualificationRecord(record); err != nil {
			return document, fmt.Errorf("invalid model qualification evidence for %s: %w", selection, err)
		}
	}
	return document, nil
}

func validateModelQualificationRecord(record modelQualificationRecord) error {
	if joinModel(record.Provider, record.Model) == "" || record.UpdatedAt.IsZero() || !validQualificationBinding(record.Binding) {
		return errors.New("invalid identity, timestamp, or binding")
	}
	if record.Health == nil && record.Conformance == nil && record.Runtime == nil && record.RDK == nil {
		return errors.New("record has no evidence")
	}
	if record.Health != nil {
		if record.Health.Provider != record.Provider || record.Health.Model != record.Model || record.Health.CheckedAt.IsZero() || !record.Health.ExpiresAt.After(record.Health.CheckedAt) ||
			record.Health.Attempts < 0 || record.Health.Attempts > 2 || record.Health.FirstByteMS < 0 || record.Health.FirstByteMS > durationMilliseconds(modelHealthRequestTimeout) ||
			record.Health.LatencyMS < 0 || record.Health.LatencyMS > durationMilliseconds(modelHealthRequestTimeout) ||
			!rdkProbeContains([]string{"", "sse", "json"}, record.Health.Transport) || !safeQualificationText(record.Health.Message, 1024) {
			return errors.New("invalid route evidence")
		}
		normalized := normalizeModelHealthResult(*record.Health)
		if normalized.Status != record.Health.Status || normalized.Category != record.Health.Category || normalized.Message != record.Health.Message {
			return errors.New("non-canonical route evidence")
		}
	}
	if record.Conformance != nil {
		if record.Conformance.Provider != record.Provider || record.Conformance.Model != record.Model || record.Conformance.SchemaVersion != modelConformanceSchema ||
			record.Conformance.CheckedAt.IsZero() || !record.Conformance.ExpiresAt.After(record.Conformance.CheckedAt) || record.Conformance.Duration < 0 ||
			record.Conformance.Duration > durationMilliseconds(modelConformanceRequestTimeout) || record.Conformance.Attempts < 0 || record.Conformance.Attempts > modelConformanceMaximumAttempts || len(record.Conformance.Checks) != 4 || !safeQualificationText(record.Conformance.Message, 2048) {
			return errors.New("invalid protocol evidence")
		}
		canonical := *record.Conformance
		canonical.Checks = append([]modelConformanceCheck(nil), record.Conformance.Checks...)
		canonical = normalizeModelConformanceResult(canonical)
		if !reflect.DeepEqual(canonical, *record.Conformance) {
			return errors.New("non-canonical protocol evidence")
		}
	}
	if record.Runtime != nil {
		if record.Runtime.Provider != record.Provider || record.Runtime.Model != record.Model || record.Runtime.SchemaVersion != modelRuntimeProbeSchema || record.Runtime.Scope != "agent-runtime-partial" ||
			record.Runtime.CheckedAt.IsZero() || record.Runtime.DurationMS < 0 || len(record.Runtime.Checks) != len(runtimeProbeRequiredChecks) ||
			record.Runtime.DurationMS > durationMilliseconds(modelRuntimeProbeTimeout) || !safeQualificationText(record.Runtime.Message, 2048) || !reflect.DeepEqual(record.Runtime.Pending, runtimeProbePendingChecks) {
			return errors.New("invalid runtime evidence")
		}
		seen := map[string]bool{}
		for _, check := range record.Runtime.Checks {
			if seen[check.Name] || !runtimeProbeCheckRequired(check.Name) || !safeQualificationText(check.Message, 2048) ||
				(check.Status != "passed" && check.Status != "failed" && check.Status != "not-applicable") {
				return errors.New("invalid runtime check")
			}
			seen[check.Name] = true
		}
		if record.Runtime.Status != "partial" && record.Runtime.Status != "failed" {
			return errors.New("invalid runtime outcome")
		}
		canonical := *record.Runtime
		canonical.Checks = append([]modelRuntimeProbeCheck(nil), record.Runtime.Checks...)
		canonical.Pending = append([]string(nil), record.Runtime.Pending...)
		canonical = normalizeModelRuntimeProbeResult(canonical)
		if !reflect.DeepEqual(canonical, *record.Runtime) {
			return errors.New("non-canonical runtime evidence")
		}
	}
	if record.RDK != nil {
		if record.RDK.Provider != record.Provider || record.RDK.Model != record.Model || record.RDK.SchemaVersion != rdkProbeSchema || record.RDK.Scope != "rdk-task-profile" ||
			record.RDK.Profile != rdkProbeProfile || record.RDK.CheckedAt.IsZero() || record.RDK.DurationMS < 0 || len(record.RDK.Checks) != len(rdkProbeRequiredChecks) ||
			record.RDK.DurationMS > durationMilliseconds(rdkProbeTimeout) || len(record.RDK.Sources) > 64 || !safeQualificationText(record.RDK.Message, 2048) || !validRDKQualificationBinding(record.RDK.Binding) ||
			!reflect.DeepEqual(record.RDK.NotCovered, []string{"workspace-coding", "model-deployment", "multimedia-pipeline", "hardware-control"}) {
			return errors.New("invalid RDK profile evidence")
		}
		seen := map[string]bool{}
		for _, check := range record.RDK.Checks {
			if seen[check.Name] || !rdkProbeContains(rdkProbeRequiredChecks, check.Name) || !safeQualificationText(check.Message, 2048) || (check.Status != "passed" && check.Status != "failed") {
				return errors.New("invalid RDK profile check")
			}
			seen[check.Name] = true
		}
		for _, source := range record.RDK.Sources {
			if !officialRDKSource(source) {
				return errors.New("invalid RDK evidence source")
			}
		}
		canonical := *record.RDK
		canonical.Checks = append([]modelRDKProbeCheck(nil), record.RDK.Checks...)
		canonical.Sources = append([]string(nil), record.RDK.Sources...)
		canonical.NotCovered = append([]string(nil), record.RDK.NotCovered...)
		canonical = normalizeModelRDKProbeResult(canonical)
		if !reflect.DeepEqual(canonical, *record.RDK) {
			return errors.New("non-canonical RDK profile evidence")
		}
	}
	return nil
}

func validQualificationBinding(binding modelQualificationBinding) bool {
	if binding.ProductVersion == "" || binding.BuildStatus == "" || !safeQualificationText(binding.ProductVersion, 64) || !safeQualificationText(binding.BuildStatus, 32) {
		return false
	}
	if binding.ConfigFingerprint != "" && !isLowerHex(binding.ConfigFingerprint, 64) {
		return false
	}
	for _, digest := range []string{binding.AgentdBinarySHA256, binding.PiCompatibilitySHA256} {
		if digest != "" && !isLowerHex(digest, 64) {
			return false
		}
	}
	for _, commit := range []string{binding.Commit, binding.PiCommit} {
		if commit != "" && !isLowerHex(commit, 40) {
			return false
		}
	}
	return safeOptionalQualificationText(binding.BuildTarget, 64) && safeOptionalQualificationText(binding.PiVersion, 64)
}

func validRDKQualificationBinding(binding modelRDKProbeBinding) bool {
	for _, digest := range []string{binding.AgentdBinarySHA256, binding.PiCompatibilitySHA256, binding.ExpertPromptSHA256, binding.RDKExtensionSHA256, binding.KnowledgePackSHA256} {
		if digest != "" && !isLowerHex(digest, 64) {
			return false
		}
	}
	for _, commit := range []string{binding.Commit, binding.PiCommit} {
		if commit != "" && !isLowerHex(commit, 40) {
			return false
		}
	}
	return safeQualificationText(binding.ProductVersion, 64) && safeQualificationText(binding.BuildStatus, 32) &&
		safeOptionalQualificationText(binding.BuildTarget, 64) && safeOptionalQualificationText(binding.PiVersion, 64) &&
		safeOptionalQualificationText(binding.Board, 256) && safeOptionalQualificationText(binding.BoardID, 32) &&
		safeOptionalQualificationText(binding.RDKOSVersion, 128) && safeOptionalQualificationText(binding.Architecture, 32) &&
		safeOptionalQualificationText(binding.KnowledgeVersion, 128) && safeOptionalQualificationText(binding.KnowledgeUpdatedAt, 64)
}

func safeQualificationText(value string, maximum int) bool { return safeInventoryLabel(value, maximum) }
func safeOptionalQualificationText(value string, maximum int) bool {
	return value == "" || safeInventoryLabel(value, maximum)
}
