package hobot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type sdkRDKProfileSpec struct {
	id, name, workflow, evidenceClass, description string
}

var sdkRDKProfileSpecs = []sdkRDKProfileSpec{
	{"read-only-rdk-diagnostic-v1", "Board diagnostics", "board-diagnostics", "live-read-only", "Live board identity and version-matched diagnostic knowledge."},
	{"read-only-model-deployment-planning-v1", "Model deployment planning", "model-deployment", "knowledge-grounded-planning", "Board-aware deployment planning."},
	{"read-only-multimedia-planning-v1", "Multimedia pipeline planning", "multimedia-pipeline", "knowledge-grounded-planning", "Board-aware multimedia planning."},
	{"read-only-hardware-safety-planning-v1", "Hardware safety planning", "hardware-safety", "knowledge-grounded-planning", "Board-aware hardware safety planning."},
	{"isolated-workspace-coding-v1", "Workspace coding", "workspace-coding", "not-implemented", "Isolated workspace coding."},
}

func TestDecodeModelRDKMatrixPreservesEvidenceBoundaries(t *testing.T) {
	matrix := validSDKRDKMatrix(t)
	raw, err := json.Marshal(matrix)
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeModelRDKMatrix(raw)
	if err != nil || len(result.Profiles) != 5 || result.Profiles[0].EvidenceState != "current" || result.Profiles[4].Availability != "planned" {
		t.Fatalf("matrix = %+v err=%v", result, err)
	}

	for name, mutate := range map[string]func(*ModelRDKMatrix){
		"wrong-profile-result": func(value *ModelRDKMatrix) { value.Profiles[0].Result.Profile = value.Profiles[1].ID },
		"planned-is-current":   func(value *ModelRDKMatrix) { value.Profiles[4].EvidenceState = "current" },
		"stale-without-reason": func(value *ModelRDKMatrix) { value.Profiles[0].EvidenceState = "stale" },
		"inflated-not-covered": func(value *ModelRDKMatrix) { value.Profiles[1].NotCovered = []string{"nothing"} },
		"duplicate-profile":    func(value *ModelRDKMatrix) { value.Profiles[1].ID = value.Profiles[0].ID },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := validSDKRDKMatrix(t)
			mutate(&candidate)
			encoded, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeModelRDKMatrix(encoded); err == nil {
				t.Fatal("invalid matrix was accepted")
			}
		})
	}
	if _, err := decodeModelRDKMatrix(append(raw[:len(raw)-1], []byte(`,"secret":"leak"}`)...)); err == nil {
		t.Fatal("unknown matrix field was accepted")
	}
}

func TestModelRDKMethodsRejectInvalidSelectionsAndProfilesBeforeTransport(t *testing.T) {
	client := &Client{}
	if _, err := client.ModelRDKProbe(context.Background(), "missing-slash"); err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("invalid selection error = %v", err)
	}
	if _, err := client.ModelRDKProbe(context.Background(), "drobotics/kimi-k3", "isolated-workspace-coding-v1"); err == nil || !strings.Contains(err.Error(), "not runnable") {
		t.Fatalf("planned profile error = %v", err)
	}
	if _, err := client.ModelRDKMatrix(context.Background(), "drobotics/"); err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("invalid matrix selection error = %v", err)
	}
}

func validSDKRDKMatrix(t *testing.T) ModelRDKMatrix {
	t.Helper()
	now := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	profiles := make([]ModelRDKProfileStatus, 0, len(sdkRDKProfileSpecs))
	for index, spec := range sdkRDKProfileSpecs {
		status := ModelRDKProfileStatus{
			ID: spec.id, Name: spec.name, Workflow: spec.workflow, EvidenceClass: spec.evidenceClass, Description: spec.description,
			Availability: "available", EvidenceState: "untested", Targets: []string{"x5", "s100", "s600"},
			NotCovered: append([]string(nil), qualificationRDKProfiles[spec.id]...), StaleReasons: []string{},
		}
		if index == 0 {
			probe := validSDKRDKProbe(spec.id, now)
			status.EvidenceState = "current"
			status.Result = &probe
		}
		if !qualificationRDKProfileRunnable[spec.id] {
			status.Availability = "planned"
		}
		profiles = append(profiles, status)
	}
	return ModelRDKMatrix{SchemaVersion: 1, Provider: "drobotics", Model: "kimi-k3", BoardID: "s600", RDKOSVersion: "5.1.0", Architecture: "arm64", CapturedAt: now, Profiles: profiles}
}

func validSDKRDKProbe(profile string, now time.Time) ModelRDKProbe {
	checks := make([]ModelRDKProbeCheck, 0, len(qualificationRDKChecks))
	for _, name := range qualificationRDKChecks {
		checks = append(checks, ModelRDKProbeCheck{Name: name, Status: "passed", Message: "passed"})
	}
	return ModelRDKProbe{
		SchemaVersion: 1, Scope: "rdk-task-profile", Profile: profile, Provider: "drobotics", Model: "kimi-k3", Status: "passed",
		Message: "passed", CheckedAt: now, Checks: checks, Sources: []string{"https://developer.d-robotics.cc/rdk_doc/"},
		NotCovered: append([]string(nil), qualificationRDKProfiles[profile]...),
		Binding:    ModelRDKProbeBinding{ProductVersion: "0.26.0", BuildStatus: "unavailable", ExpertPromptSHA256: strings.Repeat("a", 64), Board: "D-Robotics RDK S600", BoardID: "s600", RDKOSVersion: "5.1.0", Architecture: "arm64"},
	}
}
