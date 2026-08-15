package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func qualificationBuild() buildIdentity {
	dirty := true
	return buildIdentity{
		Status: "unavailable", Reason: "metadata-missing", Dirty: &dirty,
		Target: "darwin-arm64", PiVersion: "0.84.1",
	}
}

func qualificationHealth(now time.Time) modelHealthResult {
	return modelHealthResult{
		Provider: "drobotics", Model: "kimi-k3", Status: "available", Category: "ok",
		Message: modelHealthMessage("ok"), Transport: "sse", CheckedAt: now, ExpiresAt: now.Add(modelHealthCacheTTL), Attempts: 1,
	}
}

func qualificationConformance(now time.Time) modelConformanceResult {
	result := normalizeModelConformanceResult(modelConformanceResult{Checks: []modelConformanceCheck{
		conformanceCheck("streaming", true, 2), conformanceCheck("tool-call", true, 2),
		conformanceCheck("tool-result", true, 3), {Name: "image-input", Status: "skipped"},
	}})
	result.Provider, result.Model = "drobotics", "kimi-k3"
	result.CheckedAt, result.ExpiresAt, result.Attempts = now, now.Add(modelConformanceCacheTTL), 2
	return result
}

func TestModelQualificationPersistsSanitizedEvidenceAndExpiresLayers(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigFingerprint = strings.Repeat("a", 64)
	cfg.QualificationPath = filepath.Join(cfg.AgentdRoot, "qualification.json")
	store := newModelQualificationStore(cfg)
	now := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return now }
	build := qualificationBuild()
	health := qualificationHealth(now)
	health.Cached = true
	if err := store.recordHealth(health, build); err != nil {
		t.Fatal(err)
	}
	conformance := qualificationConformance(now)
	conformance.Cached = true
	if err := store.recordConformance(conformance, build); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cfg.QualificationPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("qualification file: info=%v err=%v", info, err)
	}
	result, err := store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, build)
	if err != nil || result.State != "current" || result.Level != "protocol" || result.Outcome != "passed" || result.Health == nil || result.Health.Cached || result.Conformance == nil || result.Conformance.Cached {
		t.Fatalf("current qualification = %+v err=%v", result, err)
	}
	now = now.Add(modelConformanceCacheTTL + time.Second)
	result, err = store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, build)
	if err != nil || result.State != "expired" || result.Level != "untested" || !reflect.DeepEqual(result.ExpiredLayers, []string{"route", "protocol"}) {
		t.Fatalf("expired qualification = %+v err=%v", result, err)
	}
}

func TestModelQualificationMarksChangedBindingsStaleWithoutReusingLayers(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigFingerprint = strings.Repeat("a", 64)
	cfg.QualificationPath = filepath.Join(cfg.AgentdRoot, "qualification.json")
	store := newModelQualificationStore(cfg)
	now := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return now }
	build := qualificationBuild()
	if err := store.recordHealth(qualificationHealth(now), build); err != nil {
		t.Fatal(err)
	}
	result, err := store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, build, strings.Repeat("b", 64))
	if err != nil || result.State != "stale" || result.Level != "untested" || !reflect.DeepEqual(result.StaleReasons, []string{"configuration-changed"}) || !reflect.DeepEqual(result.StaleLayers, []string{"route"}) {
		t.Fatalf("stale qualification = %+v err=%v", result, err)
	}
}

func TestModelQualificationDoesNotReuseExpiredLowerLayersWhenRDKIsStale(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigFingerprint = strings.Repeat("a", 64)
	cfg.QualificationPath = filepath.Join(cfg.AgentdRoot, "qualification.json")
	store := newModelQualificationStore(cfg)
	now := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return now }
	build := qualificationBuild()
	if err := store.recordHealth(qualificationHealth(now), build); err != nil {
		t.Fatal(err)
	}
	rdk := normalizeModelRDKProbeResult(modelRDKProbeResult{
		Provider: "drobotics", Model: "kimi-k3", CheckedAt: now,
		Binding: modelRDKProbeBinding{ProductVersion: version, BuildStatus: "unavailable"},
		Checks:  []modelRDKProbeCheck{},
	})
	if err := store.recordRDK(rdk, build); err != nil {
		t.Fatal(err)
	}
	now = now.Add(modelHealthCacheTTL + time.Second)
	result, err := store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, build)
	if err != nil || result.State != "stale" || result.Level != "untested" || result.Outcome != "unknown" ||
		!reflect.DeepEqual(result.StaleLayers, []string{"rdk"}) || !reflect.DeepEqual(result.ExpiredLayers, []string{"route"}) {
		t.Fatalf("stale RDK reused expired lower evidence: %+v err=%v", result, err)
	}
}

func TestModelQualificationRejectsUnsafeAndCorruptStorage(t *testing.T) {
	cfg := testConfig(t)
	cfg.QualificationPath = filepath.Join(cfg.AgentdRoot, "qualification.json")
	store := newModelQualificationStore(cfg)
	now := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	health := qualificationHealth(now)
	health.Transport = "websocket"
	if err := validateModelQualificationRecord(modelQualificationRecord{
		Provider:  health.Provider,
		Model:     health.Model,
		UpdatedAt: now,
		Binding:   qualificationBinding(cfg, qualificationBuild()),
		Health:    &health,
	}); err == nil || !strings.Contains(err.Error(), "route evidence") {
		t.Fatalf("unknown route transport was accepted: %v", err)
	}
	if validRDKQualificationBinding(modelRDKProbeBinding{ProductVersion: "0.26.0", BuildStatus: "verified", Commit: strings.Repeat("A", 40)}) {
		t.Fatal("non-canonical RDK build commit was accepted")
	}
	if err := os.WriteFile(cfg.QualificationPath, []byte(`{"schemaVersion":1,"records":[],"secret":"leak"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, qualificationBuild()); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("corrupt evidence was accepted: %v", err)
	}
	if err := os.Remove(cfg.QualificationPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"schemaVersion":1,"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, cfg.QualificationPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, qualificationBuild()); err == nil || !strings.Contains(err.Error(), "private regular file") {
		t.Fatalf("linked evidence was accepted: %v", err)
	}
}

func TestModelQualificationReplacesCrossBuildEvidenceInsteadOfMixingLayers(t *testing.T) {
	cfg := testConfig(t)
	cfg.QualificationPath = filepath.Join(cfg.AgentdRoot, "qualification.json")
	store := newModelQualificationStore(cfg)
	now := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return now }
	first := qualificationBuild()
	if err := store.recordHealth(qualificationHealth(now), first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.PiVersion = "0.85.0"
	if err := store.recordConformance(qualificationConformance(now), second); err != nil {
		t.Fatal(err)
	}
	result, err := store.get(&taskManager{cfg: cfg}, modelQualificationParams{Model: "drobotics/kimi-k3"}, second)
	if err != nil || result.State != "current" || result.Health != nil || result.Conformance == nil {
		t.Fatalf("cross-build evidence was mixed: %+v err=%v", result, err)
	}
}

func TestModelQualificationAcceptsCompleteVisionFallbackAttemptBudget(t *testing.T) {
	cfg := testConfig(t)
	cfg.QualificationPath = filepath.Join(cfg.AgentdRoot, "qualification.json")
	store := newModelQualificationStore(cfg)
	now := time.Date(2026, 8, 15, 3, 4, 5, 0, time.UTC)
	store.now = func() time.Time { return now }
	result := normalizeModelConformanceResult(modelConformanceResult{Checks: []modelConformanceCheck{
		{Name: "streaming", Status: "degraded"},
		{Name: "tool-call", Status: "passed"},
		{Name: "tool-result", Status: "passed"},
		{Name: "image-input", Status: "passed"},
	}})
	result.Provider, result.Model = "drobotics", "kimi-k3"
	result.CheckedAt, result.ExpiresAt = now, now.Add(modelConformanceCacheTTL)
	result.Attempts = modelConformanceMaximumAttempts
	if err := store.recordConformance(result, qualificationBuild()); err != nil {
		t.Fatalf("complete vision fallback evidence was rejected: %v", err)
	}

	result.Attempts++
	if err := store.recordConformance(result, qualificationBuild()); err == nil || !strings.Contains(err.Error(), "invalid protocol evidence") {
		t.Fatalf("excessive conformance attempts were accepted: %v", err)
	}
}
