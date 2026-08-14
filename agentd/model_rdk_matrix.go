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
	modelRDKMatrixSchema         = 1
	maximumModelRDKMatrixRecords = 64
)

type modelRDKMatrixParams struct {
	Model string `json:"model"`
}

type modelRDKMatrixRecord struct {
	Provider  string                    `json:"provider"`
	Model     string                    `json:"model"`
	Profile   string                    `json:"profile"`
	UpdatedAt time.Time                 `json:"updatedAt"`
	Binding   modelQualificationBinding `json:"binding"`
	Result    modelRDKProbeResult       `json:"result"`
}

type modelRDKMatrixDocument struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Records       []modelRDKMatrixRecord `json:"records"`
}

type modelRDKProfileStatus struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Workflow      string               `json:"workflow"`
	EvidenceClass string               `json:"evidenceClass"`
	Description   string               `json:"description"`
	Availability  string               `json:"availability"`
	EvidenceState string               `json:"evidenceState"`
	Targets       []string             `json:"targets"`
	NotCovered    []string             `json:"notCovered"`
	StaleReasons  []string             `json:"staleReasons"`
	Result        *modelRDKProbeResult `json:"result,omitempty"`
}

type modelRDKMatrixResult struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Provider      string                  `json:"provider"`
	Model         string                  `json:"model"`
	BoardID       string                  `json:"boardId"`
	RDKOSVersion  string                  `json:"rdkOsVersion"`
	Architecture  string                  `json:"architecture"`
	CapturedAt    time.Time               `json:"capturedAt"`
	Profiles      []modelRDKProfileStatus `json:"profiles"`
}

type modelRDKMatrixStore struct {
	mu   sync.Mutex
	cfg  config
	path string
	now  func() time.Time
}

func newModelRDKMatrixStore(cfg config) *modelRDKMatrixStore {
	return &modelRDKMatrixStore{cfg: cfg, path: filepath.Join(cfg.AgentdRoot, "model-rdk-matrix.json"), now: time.Now}
}

func (store *modelRDKMatrixStore) record(result modelRDKProbeResult, build buildIdentity) error {
	profile, ok := rdkProbeProfileByID(result.Profile)
	if !ok || !profile.Runnable {
		return errors.New("RDK matrix evidence has an unsupported profile")
	}
	if err := validateRDKMatrixProbe(result, profile); err != nil {
		return fmt.Errorf("refuse invalid RDK matrix evidence: %w", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	document, err := store.loadLocked()
	if err != nil {
		return err
	}
	key := modelRDKMatrixRecordKey(result.Provider, result.Model, result.Profile, result.Binding.BoardID)
	index := -1
	for candidate := range document.Records {
		record := &document.Records[candidate]
		if modelRDKMatrixRecordKey(record.Provider, record.Model, record.Profile, record.Result.Binding.BoardID) == key {
			index = candidate
			break
		}
	}
	if index < 0 {
		if len(document.Records) >= maximumModelRDKMatrixRecords {
			oldest := 0
			for candidate := 1; candidate < len(document.Records); candidate++ {
				if document.Records[candidate].UpdatedAt.Before(document.Records[oldest].UpdatedAt) {
					oldest = candidate
				}
			}
			index = oldest
		} else {
			document.Records = append(document.Records, modelRDKMatrixRecord{})
			index = len(document.Records) - 1
		}
	}
	document.Records[index] = modelRDKMatrixRecord{
		Provider: result.Provider, Model: result.Model, Profile: result.Profile, UpdatedAt: store.now().UTC(),
		Binding: qualificationBinding(store.cfg, build), Result: cloneModelRDKProbeResult(result),
	}
	sort.Slice(document.Records, func(left, right int) bool {
		leftKey := modelRDKMatrixRecordKey(document.Records[left].Provider, document.Records[left].Model, document.Records[left].Profile, document.Records[left].Result.Binding.BoardID)
		rightKey := modelRDKMatrixRecordKey(document.Records[right].Provider, document.Records[right].Model, document.Records[right].Profile, document.Records[right].Result.Binding.BoardID)
		return leftKey < rightKey
	})
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFileDurable(store.path, append(encoded, '\n')); err != nil {
		return fmt.Errorf("persist RDK profile matrix: %w", err)
	}
	return nil
}

func (store *modelRDKMatrixStore) get(manager *taskManager, params modelRDKMatrixParams, build buildIdentity, clientFingerprint string) (modelRDKMatrixResult, error) {
	models, err := manager.availableModels()
	if err != nil {
		return modelRDKMatrixResult{}, fmt.Errorf("discover models: %w", err)
	}
	selection := normalizeModelSelection(params.Model)
	if selection == "" {
		return modelRDKMatrixResult{}, errors.New("model must use provider/model format")
	}
	model, ok := models[selection]
	if !ok {
		return modelRDKMatrixResult{}, fmt.Errorf("model is not available: %s", selection)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	document, err := store.loadLocked()
	if err != nil {
		return modelRDKMatrixResult{}, err
	}
	live := collectSystemSnapshot(store.cfg)
	result := modelRDKMatrixResult{
		SchemaVersion: modelRDKMatrixSchema, Provider: model.Provider, Model: model.ID,
		BoardID: live.BoardID, RDKOSVersion: live.RDKOSVersion, Architecture: live.Architecture,
		CapturedAt: store.now().UTC(), Profiles: make([]modelRDKProfileStatus, 0, len(rdkProbeProfiles)),
	}
	fingerprint := store.cfg.ConfigFingerprint
	if clientFingerprint != "" {
		fingerprint = clientFingerprint
	}
	qualification := newModelQualificationStore(store.cfg)
	for _, profile := range rdkProbeProfiles {
		status := modelRDKProfileStatus{
			ID: profile.ID, Name: profile.Name, Workflow: profile.Workflow, EvidenceClass: profile.EvidenceClass,
			Description: profile.Description, Availability: "available", EvidenceState: "untested",
			Targets: append([]string(nil), profile.Targets...), NotCovered: append([]string(nil), profile.NotCovered...), StaleReasons: []string{},
		}
		if !profile.Runnable {
			status.Availability = "planned"
		} else if !rdkProbeContains(profile.Targets, live.BoardID) || live.Architecture != "arm64" {
			status.Availability = "unsupported-target"
		}
		for index := range document.Records {
			record := &document.Records[index]
			if record.Provider != model.Provider || record.Model != model.ID || record.Profile != profile.ID || record.Result.Binding.BoardID != live.BoardID {
				continue
			}
			copy := cloneModelRDKProbeResult(record.Result)
			status.Result = &copy
			status.EvidenceState = "current"
			fake := modelQualificationRecord{Provider: record.Provider, Model: record.Model, UpdatedAt: record.UpdatedAt, Binding: record.Binding, RDK: &copy}
			status.StaleReasons = qualification.staleReasons(fake, build, fingerprint)
			if len(status.StaleReasons) > 0 {
				status.EvidenceState = "stale"
			}
			break
		}
		result.Profiles = append(result.Profiles, status)
	}
	return result, nil
}

func (store *modelRDKMatrixStore) loadLocked() (modelRDKMatrixDocument, error) {
	document := modelRDKMatrixDocument{SchemaVersion: modelRDKMatrixSchema, Records: []modelRDKMatrixRecord{}}
	raw, missing, err := readPrivateConfigBytes(store.path)
	if err != nil {
		return document, fmt.Errorf("read RDK profile matrix: %w", err)
	}
	if missing {
		return document, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return document, fmt.Errorf("decode RDK profile matrix: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return document, errors.New("RDK profile matrix must contain exactly one JSON object")
	}
	if document.SchemaVersion != modelRDKMatrixSchema || document.Records == nil || len(document.Records) > maximumModelRDKMatrixRecords {
		return document, errors.New("RDK profile matrix has an unsupported schema or record count")
	}
	seen := map[string]bool{}
	for _, record := range document.Records {
		profile, ok := rdkProbeProfileByID(record.Profile)
		key := modelRDKMatrixRecordKey(record.Provider, record.Model, record.Profile, record.Result.Binding.BoardID)
		if !ok || !profile.Runnable || record.UpdatedAt.IsZero() || !validQualificationBinding(record.Binding) || joinModel(record.Provider, record.Model) == "" || record.Result.Binding.BoardID == "" || seen[key] ||
			record.Result.Provider != record.Provider || record.Result.Model != record.Model || record.Result.Profile != record.Profile {
			return document, errors.New("RDK profile matrix has an invalid or duplicate record")
		}
		seen[key] = true
		if err := validateRDKMatrixProbe(record.Result, profile); err != nil {
			return document, fmt.Errorf("invalid RDK matrix evidence for %s: %w", key, err)
		}
	}
	return document, nil
}

func modelRDKMatrixRecordKey(provider, model, profile, boardID string) string {
	return joinModel(provider, model) + "\x00" + profile + "\x00" + boardID
}

func validateRDKMatrixProbe(result modelRDKProbeResult, profile rdkProbeProfileDefinition) error {
	if result.SchemaVersion != rdkProbeSchema || result.Scope != "rdk-task-profile" || result.Profile != profile.ID || joinModel(result.Provider, result.Model) == "" ||
		result.CheckedAt.IsZero() || result.DurationMS < 0 || result.DurationMS > durationMilliseconds(rdkProbeTimeout) || len(result.Checks) != len(rdkProbeRequiredChecks) ||
		len(result.Sources) > 64 || !safeQualificationText(result.Message, 2048) || !validRDKQualificationBinding(result.Binding) || !reflect.DeepEqual(result.NotCovered, profile.NotCovered) {
		return errors.New("invalid RDK profile identity, binding, or limits")
	}
	seen := map[string]bool{}
	for _, check := range result.Checks {
		if seen[check.Name] || !rdkProbeContains(rdkProbeRequiredChecks, check.Name) || !safeQualificationText(check.Message, 2048) || (check.Status != "passed" && check.Status != "failed") {
			return errors.New("invalid RDK profile check")
		}
		seen[check.Name] = true
	}
	for _, source := range result.Sources {
		if !officialRDKSource(source) {
			return errors.New("invalid RDK evidence source")
		}
	}
	canonical := cloneModelRDKProbeResult(result)
	canonical = normalizeModelRDKProbeResult(canonical)
	if !reflect.DeepEqual(canonical, result) {
		return errors.New("non-canonical RDK profile evidence")
	}
	return nil
}

func cloneModelRDKProbeResult(result modelRDKProbeResult) modelRDKProbeResult {
	result.Checks = append([]modelRDKProbeCheck(nil), result.Checks...)
	result.Sources = append([]string(nil), result.Sources...)
	result.NotCovered = append([]string(nil), result.NotCovered...)
	result.Binding.Dirty = cloneBool(result.Binding.Dirty)
	return result
}
