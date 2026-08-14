package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRDKProfileRegistryIsVersionedBoundedAndTruthful(t *testing.T) {
	if len(rdkProbeProfiles) != 5 {
		t.Fatalf("profile count = %d, want 5", len(rdkProbeProfiles))
	}
	seen := map[string]bool{}
	for _, profile := range rdkProbeProfiles {
		if !validExtensionIdentifier(profile.ID) || seen[profile.ID] || profile.Name == "" || profile.Workflow == "" || profile.EvidenceClass == "" || len(profile.Targets) != 3 || len(profile.NotCovered) == 0 {
			t.Fatalf("invalid RDK profile definition: %+v", profile)
		}
		seen[profile.ID] = true
		if profile.Runnable && (profile.Query == "" || profile.Topic == "" || profile.EvidenceClass == "not-implemented") {
			t.Fatalf("runnable profile omitted its bounded evidence contract: %+v", profile)
		}
		if !profile.Runnable && profile.EvidenceClass != "not-implemented" {
			t.Fatalf("planned profile overstates implementation: %+v", profile)
		}
	}
}

func TestRDKMatrixPersistsProfilesIndependentlyAndMarksDrift(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigFingerprint = strings.Repeat("a", 64)
	store := newModelRDKMatrixStore(cfg)
	now := time.Date(2026, 8, 14, 6, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	build := qualificationBuild()
	for _, id := range []string{rdkProbeProfile, "read-only-model-deployment-planning-v1"} {
		profile, _ := rdkProbeProfileByID(id)
		if err := store.record(testRDKMatrixProbe(t, cfg, profile, now), build); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.get(&taskManager{cfg: cfg}, modelRDKMatrixParams{Model: "drobotics/kimi-k3"}, build, cfg.ConfigFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || len(result.Profiles) != len(rdkProbeProfiles) {
		t.Fatalf("unexpected matrix: %+v", result)
	}
	states := map[string]string{}
	for _, profile := range result.Profiles {
		states[profile.ID] = profile.EvidenceState
		if profile.ID == "isolated-workspace-coding-v1" && (profile.Availability != "planned" || profile.Result != nil) {
			t.Fatalf("planned profile was presented as runnable: %+v", profile)
		}
	}
	if states[rdkProbeProfile] != "current" || states["read-only-model-deployment-planning-v1"] != "current" || states["read-only-multimedia-planning-v1"] != "untested" {
		t.Fatalf("profile evidence was overwritten or invented: %+v", states)
	}
	stale, err := store.get(&taskManager{cfg: cfg}, modelRDKMatrixParams{Model: "drobotics/kimi-k3"}, build, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range stale.Profiles {
		if profile.Result != nil && (profile.EvidenceState != "stale" || !reflect.DeepEqual(profile.StaleReasons, []string{"configuration-changed"})) {
			t.Fatalf("drifted profile remained current: %+v", profile)
		}
	}
}

func TestRDKMatrixRejectsUnsafeStorageAndUnimplementedProfiles(t *testing.T) {
	cfg := testConfig(t)
	store := newModelRDKMatrixStore(cfg)
	profile, _ := rdkProbeProfileByID("isolated-workspace-coding-v1")
	result := modelRDKProbeResult{Profile: profile.ID, Provider: "drobotics", Model: "kimi-k3"}
	if err := store.record(result, qualificationBuild()); err == nil || !strings.Contains(err.Error(), "unsupported profile") {
		t.Fatalf("unimplemented profile was persisted: %v", err)
	}
	if err := os.WriteFile(store.path, []byte(`{"schemaVersion":1,"records":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.loadLocked(); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe matrix storage was accepted: %v", err)
	}
	linkTarget := filepath.Join(cfg.AgentdRoot, "matrix-target.json")
	if err := os.WriteFile(linkTarget, []byte(`{"schemaVersion":1,"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, store.path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.loadLocked(); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("symlinked matrix storage was accepted: %v", err)
	}
}

func TestRDKMatrixKeepsBoardEvidenceIndependent(t *testing.T) {
	cfg := testConfig(t)
	store := newModelRDKMatrixStore(cfg)
	now := time.Date(2026, 8, 14, 7, 0, 0, 0, time.UTC)
	profile := defaultRDKProbeProfile()
	for _, target := range []struct{ board, id, os string }{
		{"D-Robotics RDK S100", "s100", "4.0.5"},
		{"D-Robotics RDK S600", "s600", "5.1.0"},
	} {
		probe := testRDKMatrixProbe(t, cfg, profile, now)
		probe.Binding.Board = target.board
		probe.Binding.BoardID = target.id
		probe.Binding.RDKOSVersion = target.os
		probe.Binding.Architecture = "arm64"
		probe = normalizeModelRDKProbeResult(probe)
		if err := store.record(probe, qualificationBuild()); err != nil {
			t.Fatal(err)
		}
	}
	document, err := store.loadLocked()
	if err != nil || len(document.Records) != 2 {
		t.Fatalf("board-scoped records = %+v err=%v", document.Records, err)
	}
	if document.Records[0].Result.Binding.BoardID == document.Records[1].Result.Binding.BoardID {
		t.Fatalf("board evidence was overwritten: %+v", document.Records)
	}
}

func testRDKMatrixProbe(t *testing.T, cfg config, profile rdkProbeProfileDefinition, now time.Time) modelRDKProbeResult {
	t.Helper()
	_, _, _, manifest, extension, prompt, knowledge, err := rdkProbeResources(cfg)
	if err != nil {
		t.Fatal(err)
	}
	live := collectSystemSnapshot(cfg)
	checks := make([]modelRDKProbeCheck, 0, len(rdkProbeRequiredChecks))
	for _, name := range rdkProbeRequiredChecks {
		checks = append(checks, modelRDKProbeCheck{Name: name, Status: "passed", Message: "Bound check passed."})
	}
	return normalizeModelRDKProbeResult(modelRDKProbeResult{
		Profile: profile.ID, Provider: "drobotics", Model: "kimi-k3", CheckedAt: now, Checks: checks,
		Binding: modelRDKProbeBinding{
			ProductVersion: version, BuildStatus: "unavailable", ExpertPromptSHA256: prompt, RDKExtensionSHA256: extension, KnowledgePackSHA256: knowledge,
			KnowledgeVersion: manifest.KnowledgeVersion, KnowledgeUpdatedAt: manifest.UpdatedAt,
			Board: live.Board, BoardID: live.BoardID, RDKOSVersion: live.RDKOSVersion, Architecture: live.Architecture,
		},
	})
}
