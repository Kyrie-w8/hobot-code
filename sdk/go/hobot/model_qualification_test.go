package hobot

import (
	"strings"
	"testing"
	"time"
)

func TestDecodeModelQualificationValidatesOneBoundedEvidenceObject(t *testing.T) {
	valid := `{"schemaVersion":1,"provider":"drobotics","model":"kimi-k3","state":"current","level":"route","outcome":"passed","updatedAt":"2026-08-14T03:04:05Z","staleReasons":[],"staleLayers":[],"expiredLayers":[],"health":{"provider":"drobotics","model":"kimi-k3","status":"available","category":"ok","message":"The model gateway completed a minimal response successfully.","transport":"sse","checkedAt":"2026-08-14T03:04:05Z","expiresAt":"2026-08-14T03:09:05Z","attempts":1,"cached":false}}`
	result, err := decodeModelQualification([]byte(valid))
	if err != nil || result.State != "current" || result.Health == nil || result.Health.Status != "available" {
		t.Fatalf("valid qualification = %+v err=%v", result, err)
	}
	for name, raw := range map[string]string{
		"unknown-field":        strings.Replace(valid, `"expiredLayers":[]`, `"expiredLayers":[],"secret":"leak"`, 1),
		"trailing-json":        valid + `{}`,
		"stale-without-reason": strings.Replace(strings.Replace(valid, `"state":"current"`, `"state":"stale"`, 1), `"staleLayers":[]`, `"staleLayers":["route"]`, 1),
		"cached":               strings.Replace(valid, `"cached":false`, `"cached":true`, 1),
		"unsafe-message":       strings.Replace(valid, `successfully.`, `successfully.\nsecret`, 1),
		"inflated-level":       strings.Replace(valid, `"level":"route"`, `"level":"protocol"`, 1),
	} {
		if _, err := decodeModelQualification([]byte(raw)); err == nil {
			t.Fatalf("%s qualification was accepted", name)
		}
	}
}

func TestDecodeModelQualificationAcceptsExplicitUntestedState(t *testing.T) {
	result, err := decodeModelQualification([]byte(`{"schemaVersion":1,"provider":"acme","model":"coder-v2","state":"untested","level":"untested","outcome":"unknown","staleReasons":[],"staleLayers":[],"expiredLayers":[]}`))
	if err != nil || result.State != "untested" || !result.UpdatedAt.IsZero() {
		t.Fatalf("untested qualification = %+v err=%v", result, err)
	}
}

func TestQualificationRuntimeRequiresTheExactProbeCheckSet(t *testing.T) {
	checks := make([]ModelRuntimeProbeCheck, 0, len(qualificationRuntimeChecks))
	for _, name := range qualificationRuntimeChecks {
		checks = append(checks, ModelRuntimeProbeCheck{Name: name, Status: "passed", Message: "passed"})
	}
	probe := ModelRuntimeProbe{
		SchemaVersion: 1, Scope: "agent-runtime-partial", Provider: "acme", Model: "coder", Status: "partial",
		Message: "passed", ReasoningDeclared: true, ImageInputDeclared: true, CheckedAt: time.Now(), Checks: checks, Pending: []string{"rdk-task-suite"},
	}
	if !validQualificationRuntime(probe, "acme", "coder") {
		t.Fatal("canonical runtime checks were rejected")
	}
	probe.Checks[len(probe.Checks)-1].Name = probe.Checks[0].Name
	if validQualificationRuntime(probe, "acme", "coder") {
		t.Fatal("duplicate runtime checks were accepted")
	}
}

func TestQualificationRDKRequiresExactChecksOfficialSourcesAndValidDigests(t *testing.T) {
	checks := make([]ModelRDKProbeCheck, 0, len(qualificationRDKChecks))
	for _, name := range qualificationRDKChecks {
		checks = append(checks, ModelRDKProbeCheck{Name: name, Status: "passed", Message: "passed"})
	}
	probe := ModelRDKProbe{
		SchemaVersion: 1, Scope: "rdk-task-profile", Profile: "read-only-rdk-diagnostic-v1", Provider: "acme", Model: "coder", Status: "passed",
		Message: "passed", CheckedAt: time.Now(), Checks: checks, Sources: []string{"https://developer.d-robotics.cc/rdk_doc/"},
		NotCovered: append([]string(nil), qualificationRDKNotCovered...),
		Binding:    ModelRDKProbeBinding{ProductVersion: "0.26.0", BuildStatus: "unavailable", ExpertPromptSHA256: strings.Repeat("a", 64)},
	}
	if !validQualificationRDK(probe, "acme", "coder") {
		t.Fatal("canonical RDK evidence was rejected")
	}
	probe.Sources[0] = "https://example.com/looks-official"
	if validQualificationRDK(probe, "acme", "coder") {
		t.Fatal("unofficial RDK evidence source was accepted")
	}
	probe.Sources[0] = "https://github.com/D-Robotics/rdk_doc"
	probe.Binding.ExpertPromptSHA256 = strings.Repeat("A", 64)
	if validQualificationRDK(probe, "acme", "coder") {
		t.Fatal("non-canonical RDK resource digest was accepted")
	}
}
