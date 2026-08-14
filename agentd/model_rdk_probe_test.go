package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const testRDKSourceURL = "https://developer.d-robotics.cc/rdk_s_doc/RDK"

func TestModelRDKProbeCompletesBoundReadOnlyDiagnosticAndCleansState(t *testing.T) {
	cfg := testConfig(t)
	cfg.gatewayToken = "test-token"
	before, err := filepath.Glob(filepath.Join(cfg.AgentdRoot, ".model-rdk-probe-*"))
	if err != nil {
		t.Fatal(err)
	}
	dirty := false
	result := probeModelOnRDK(context.Background(), cfg, runtimeProbeReasoningModel(), testRDKLiveSnapshot(), buildIdentity{
		Status: "verified", Commit: strings.Repeat("a", 40), Dirty: &dirty,
		Target: "linux-arm64", BinarySHA256: strings.Repeat("f", 64),
		PiVersion: "0.84.1", PiCommit: strings.Repeat("c", 40), PiCompatibilitySHA256: strings.Repeat("b", 64),
	})
	result.Provider = "drobotics"
	result.Model = "kimi-k3"
	result.Binding.ProductVersion = "0.26.0"
	result = normalizeModelRDKProbeResult(result)
	if result.SchemaVersion != 1 || result.Scope != "rdk-task-profile" || result.Profile != rdkProbeProfile || result.Status != "passed" || !result.ReleaseEligible {
		t.Fatalf("unexpected RDK probe result: %+v", result)
	}
	if len(result.Checks) != len(rdkProbeRequiredChecks) || len(result.Sources) != 1 || result.Sources[0] != testRDKSourceURL {
		t.Fatalf("RDK probe evidence is incomplete: %+v", result)
	}
	for _, check := range result.Checks {
		if check.Status != "passed" || check.Message == "" {
			t.Fatalf("RDK probe check failed: %+v", check)
		}
	}
	for name, digest := range map[string]string{
		"expert prompt":  result.Binding.ExpertPromptSHA256,
		"RDK extension":  result.Binding.RDKExtensionSHA256,
		"knowledge pack": result.Binding.KnowledgePackSHA256,
	} {
		if len(digest) != 64 {
			t.Fatalf("%s was not bound into the result: %+v", name, result.Binding)
		}
	}
	after, err := filepath.Glob(filepath.Join(cfg.AgentdRoot, ".model-rdk-probe-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("RDK probe temporary state was retained: before=%v after=%v", before, after)
	}
}

func TestRDKProbeAcceptsRichToolEvidenceButRequiresStrictFinalJSON(t *testing.T) {
	observation, live, model, manifest := validRDKProbeEvidence()
	checks, sources := evaluateRDKProbe(observation, live, model, manifest)
	for _, check := range checks {
		if check.Status != "passed" {
			t.Fatalf("valid rich RDK evidence failed: %+v", checks)
		}
	}
	if len(sources) != 1 || sources[0] != testRDKSourceURL {
		t.Fatalf("official source was not retained: %v", sources)
	}

	observation.lastAssistant = strings.TrimSuffix(observation.lastAssistant, "}") + `,"unsupported":"value"}`
	checks, _ = evaluateRDKProbe(observation, live, model, manifest)
	if rdkProbeCheckStatus(checks, "evidence-synthesis") != "failed" {
		t.Fatalf("unknown model output fields were accepted: %+v", checks)
	}

	observation, live, model, manifest = validRDKProbeEvidence()
	observation.lastAssistant = strings.Replace(observation.lastAssistant, `"boardId":"s600"`, `"boardId":"x5","boardId":"s600"`, 1)
	checks, _ = evaluateRDKProbe(observation, live, model, manifest)
	if rdkProbeCheckStatus(checks, "evidence-synthesis") != "failed" {
		t.Fatalf("duplicate model output keys were accepted: %+v", checks)
	}
}

func TestRDKProbeRejectsUnboundOrNonCausalEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*rdkProbeObservation, *systemSnapshot, *rdkProbeManifest){
		"wrong-live-board": func(_ *rdkProbeObservation, live *systemSnapshot, _ *rdkProbeManifest) {
			live.Board = "D-Robotics RDK S100"
		},
		"extra-tool": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.toolStarts = append(observation.toolStarts, rdkProbeToolCall{CallID: "extra", Name: "bash", Index: 7})
		},
		"unofficial-source": func(observation *rdkProbeObservation, _ *systemSnapshot, manifest *rdkProbeManifest) {
			manifest.Documents[0].Sources[0].URL = "https://example.invalid/rdk"
			observation.toolEnds[1].Text = strings.ReplaceAll(observation.toolEnds[1].Text, testRDKSourceURL, "https://example.invalid/rdk")
			observation.lastAssistant = strings.ReplaceAll(observation.lastAssistant, testRDKSourceURL, "https://example.invalid/rdk")
		},
		"source-only-on-version-mismatch": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.toolEnds[1].Text = strings.Replace(observation.toolEnds[1].Text, `"versionMatch":true`, `"versionMatch":false`, 1)
			observation.toolEnds[1].Text = strings.Replace(observation.toolEnds[1].Text, `]}`, `]},{"versionMatch":true,"sources":[]}]}`, 1)
		},
		"answer-before-tools": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.lastAssistantAt = observation.toolEnds[1].Index
		},
		"duplicate-final-answer": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.finalTextCount = 2
		},
		"wrong-query": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.toolStarts[1].Args["query"] = "generic docs"
		},
		"parallel-instead-of-causal": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.toolStarts[1].Index = observation.toolStarts[0].Index + 1
		},
		"model-mismatch": func(observation *rdkProbeObservation, _ *systemSnapshot, _ *rdkProbeManifest) {
			observation.model = "drobotics/glm-5.2"
		},
	} {
		t.Run(name, func(t *testing.T) {
			observation, live, model, manifest := validRDKProbeEvidence()
			mutate(&observation, &live, &manifest)
			checks, _ := evaluateRDKProbe(observation, live, model, manifest)
			allPassed := true
			for _, check := range checks {
				allPassed = allPassed && check.Status == "passed"
			}
			if allPassed {
				t.Fatalf("mutated RDK evidence was accepted: %+v", checks)
			}
		})
	}
}

func TestRDKProbeNormalizationNeverMakesDirtyOrIncompleteEvidenceReleaseEligible(t *testing.T) {
	dirty := true
	checks := make([]modelRDKProbeCheck, 0, len(rdkProbeRequiredChecks))
	for _, name := range rdkProbeRequiredChecks {
		checks = append(checks, modelRDKProbeCheck{Name: name, Status: "passed"})
	}
	binding := modelRDKProbeBinding{
		BuildStatus: "verified", Commit: strings.Repeat("a", 40), Dirty: &dirty,
		BuildTarget: "linux-arm64", AgentdBinarySHA256: strings.Repeat("f", 64), PiCommit: strings.Repeat("c", 40),
		PiCompatibilitySHA256: strings.Repeat("b", 64), ExpertPromptSHA256: strings.Repeat("c", 64),
		RDKExtensionSHA256: strings.Repeat("d", 64), KnowledgePackSHA256: strings.Repeat("e", 64),
	}
	result := normalizeModelRDKProbeResult(modelRDKProbeResult{Checks: checks, Binding: binding})
	if result.Status != "passed" || result.ReleaseEligible {
		t.Fatalf("dirty build was allowed to produce public evidence: %+v", result)
	}
	result = normalizeModelRDKProbeResult(modelRDKProbeResult{Checks: checks[:len(checks)-1], Binding: binding})
	if result.Status != "failed" || result.ReleaseEligible {
		t.Fatalf("incomplete evidence was accepted: %+v", result)
	}
}

func TestRDKProbeRejectsUnsupportedTargetBeforeLaunchingPi(t *testing.T) {
	cfg := testConfig(t)
	cfg.gatewayToken = "test-token"
	result := normalizeModelRDKProbeResult(probeModelOnRDK(context.Background(), cfg, runtimeProbeReasoningModel(), systemSnapshot{
		Board: "Unknown Linux board", BoardID: "unknown", RDKOSVersion: "unknown", Architecture: "arm64",
	}, buildIdentity{}))
	if result.Status != "failed" || result.Category != "target" || result.ReleaseEligible {
		t.Fatalf("unsupported host was not rejected: %+v", result)
	}
}

func TestRDKProbeRejectsDuplicateRPCEventKeys(t *testing.T) {
	observation := rdkProbeObservation{}
	observeRDKProbeEvent(&observation, []byte(`{"type":"agent_start","type":"agent_settled"}`))
	if !observation.extensionError || observation.agentStarted || observation.settled {
		t.Fatalf("duplicate RPC keys were accepted: %+v", observation)
	}
}

func TestNormalizeRDKOSReleaseVersionMatchesNodeSnapshotFallback(t *testing.T) {
	for input, expected := range map[string]string{"V4.0.5": "4.0.5", "v5.1.0": "5.1.0", "3.4.1": "3.4.1"} {
		if got := normalizeRDKOSReleaseVersion(input); got != expected {
			t.Fatalf("normalizeRDKOSReleaseVersion(%q) = %q, want %q", input, got, expected)
		}
	}
}

func testRDKLiveSnapshot() systemSnapshot {
	return systemSnapshot{
		Board: "D-Robotics RDK S600", BoardID: "s600", RDKOSVersion: "5.0.0", Architecture: "arm64",
		BPUDevices: []string{"/dev/bpu0"}, RDKUtilities: map[string]bool{"hrt_model_exec": true},
	}
}

func validRDKProbeEvidence() (rdkProbeObservation, systemSnapshot, modelOption, rdkProbeManifest) {
	manifest := rdkProbeManifest{SchemaVersion: 1, KnowledgeVersion: "2026.08.3", UpdatedAt: "2026-08-10"}
	manifest.Documents = append(manifest.Documents, struct {
		File    string `json:"file"`
		Sources []struct {
			URL string `json:"url"`
		} `json:"sources"`
	}{File: "common/system-diagnostics.md", Sources: []struct {
		URL string `json:"url"`
	}{{URL: testRDKSourceURL}}})
	observation := rdkProbeObservation{
		model: "drobotics/kimi-k3", promptAccepted: true, promptAcceptedAt: 1,
		agentStarted: true, agentStartedAt: 2, agentStartCount: 1,
		toolStarts: []rdkProbeToolCall{
			{CallID: "snapshot", Name: "system_snapshot", Args: map[string]any{"includeProcesses": false}, Index: 3},
			{CallID: "knowledge", Name: "rdk_docs_search", Args: map[string]any{"query": rdkProbeKnowledgeQuery, "board": "s600", "topic": "diagnostics", "limit": float64(3)}, Index: 5},
		},
		toolEnds: []rdkProbeToolResult{
			{CallID: "snapshot", Name: "system_snapshot", Text: `{"board":"D-Robotics RDK S600","boardId":"s600","rdkOsVersion":"5.0.0","architecture":"arm64","hostname":"rdk-s600","bpuDevices":["/dev/bpu0"],"rdkUtilities":{"hrt_model_exec":true,"hrut_somstatus":true},"memoryTotalMiB":16384}`, Index: 4},
			{CallID: "knowledge", Name: "rdk_docs_search", Text: `{"knowledgeVersion":"2026.08.3","updatedAt":"2026-08-10","detectedBoard":"s600","detectedRdkOs":"5.0.0","query":"system version logs temperature memory diagnostics","results":[{"id":"system-diagnostics","versionMatch":true,"excerpt":"bounded","sources":[{"title":"RDK S","url":"https://developer.d-robotics.cc/rdk_s_doc/RDK"}]}]}`, Index: 6},
		},
		lastAssistant:   `{"boardId":"s600","rdkOsVersion":"5.0.0","architecture":"arm64","knowledgeVersion":"2026.08.3","sourceUrl":"https://developer.d-robotics.cc/rdk_s_doc/RDK","evidenceStatus":"complete","signals":["board-identified","bpu-visible","runtime-tool-visible","docs-version-match"]}`,
		lastAssistantAt: 7, finalTextCount: 1, settled: true, settledAt: 8, settledCount: 1,
	}
	return observation, testRDKLiveSnapshot(), runtimeProbeReasoningModel(), manifest
}

func rdkProbeCheckStatus(checks []modelRDKProbeCheck, name string) string {
	for _, check := range checks {
		if check.Name == name {
			return check.Status
		}
	}
	return "missing"
}
