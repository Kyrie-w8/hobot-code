package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runtimeProbeReasoningModel() modelOption {
	return modelOption{Provider: "drobotics", ID: "kimi-k3", Capabilities: modelCapabilities{Reasoning: true}}
}

func TestModelRuntimeProbeCompletesBoundedPiLoopAndCleansTemporaryState(t *testing.T) {
	cfg := testConfig(t)
	cfg.gatewayToken = "test-token"
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := filepath.Glob(filepath.Join(cfg.AgentdRoot, ".model-runtime-probe-*"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.runModelRuntimeProbe(modelRuntimeProbeParams{Model: "drobotics/kimi-k3"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Scope != "agent-runtime-partial" || result.Status != "partial" || result.Provider != "drobotics" || result.Model != "kimi-k3" || !result.ReasoningDeclared || !result.ImageInputDeclared {
		t.Fatalf("unexpected runtime probe result: %+v", result)
	}
	if len(result.Checks) != len(runtimeProbeRequiredChecks) || len(result.Pending) != len(runtimeProbePendingChecks) {
		t.Fatalf("runtime probe evidence is incomplete: %+v", result)
	}
	for _, check := range result.Checks {
		if check.Status != "passed" || check.Message == "" {
			t.Fatalf("runtime probe check failed: %+v", check)
		}
	}
	after, err := filepath.Glob(filepath.Join(cfg.AgentdRoot, ".model-runtime-probe-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("runtime probe temporary state was retained: before=%v after=%v", before, after)
	}
}

func TestPersistentRuntimeProbeCompactsAndRecoversInterruptedSession(t *testing.T) {
	cfg := testConfig(t)
	cfg.gatewayToken = "test-token"
	temporaryRoot := t.TempDir()
	agentDir := filepath.Join(temporaryRoot, "agent")
	stateRoot := filepath.Join(temporaryRoot, "state")
	workspace := filepath.Join(temporaryRoot, "workspace")
	home := filepath.Join(temporaryRoot, "home")
	temporaryDirectory := filepath.Join(temporaryRoot, "tmp")
	for _, directory := range []string{agentDir, stateRoot, workspace, home, temporaryDirectory, filepath.Join(stateRoot, "sessions")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	model := runtimeProbeReasoningModel()
	if err := writeRuntimeProbeConfiguration(agentDir, model); err != nil {
		t.Fatal(err)
	}
	rdkExtension, probeExtension, err := runtimeProbeExtensionPaths(cfg)
	if err != nil {
		t.Fatal(err)
	}
	observation, category := probePersistentRuntime(context.Background(), cfg, model, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension)
	if category != "" {
		t.Fatalf("persistent runtime probe failed in category %s: %+v", category, observation)
	}
	if !runtimeProbeCompactionPassed(observation) || !runtimeProbeInterruptedRecoveryPassed(observation, model) {
		t.Fatalf("persistent runtime evidence was incomplete: %+v", observation)
	}
}

func TestRuntimeProbeCopiesOnlySelectedManagedProviderMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"schemaVersion":1,"providers":[{"id":"acme","baseUrl":"https://models.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ACME","models":[{"id":"coder"}]},{"id":"other","baseUrl":"https://other.example/v1","api":"anthropic-messages","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_OTHER","models":[{"id":"chat"}]}]}`
	if err := os.WriteFile(filepath.Join(source, "providers.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRuntimeProbeConfiguration(target, modelOption{Provider: "acme", ID: "coder"}, config{AgentDir: source}); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(target, "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"id":"acme"`) || strings.Contains(string(written), `"id":"other"`) || strings.Contains(string(written), "secret") {
		t.Fatalf("managed probe config is overbroad or secret-bearing: %s", written)
	}
}

func TestManagedProviderConfigurationRejectsSecretFieldsAndUnsafeEndpoints(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "providers.json")
	invalid := []string{
		`{"schemaVersion":1,"providers":[{"id":"acme","baseUrl":"https://models.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ACME","apiKey":"secret","models":[{"id":"coder"}]}]}`,
		`{"schemaVersion":1,"providers":[{"id":"acme","baseUrl":"http://models.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ACME","models":[{"id":"coder"}]}]}`,
		`{"schemaVersion":1,"providers":[{"id":"acme","baseUrl":"https://models.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ACME","models":[{"id":"coder","thinkingLevelMap":{"unknown":"high"}}]}]}`,
	}
	for index, content := range invalid {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadManagedProviderDefinitions(path); err == nil {
			t.Fatalf("invalid managed provider fixture %d was accepted", index)
		}
	}
}

func TestModelProbeCredentialPayloadContainsOnlySelectedProvider(t *testing.T) {
	root := t.TempDir()
	providerConfig := filepath.Join(root, "providers.json")
	content := `{"schemaVersion":1,"providers":[{"id":"acme","baseUrl":"https://models.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_ACME","models":[{"id":"coder"}]},{"id":"other","baseUrl":"https://other.example/v1","api":"anthropic-messages","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_OTHER","models":[{"id":"chat"}]}]}`
	if err := os.WriteFile(providerConfig, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	full, err := encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, DRobotics: "drobotics-secret", ProviderKeys: map[string]string{
		"HOBOT_CODE_PROVIDER_KEY_ACME": "acme-secret", "HOBOT_CODE_PROVIDER_KEY_OTHER": "other-secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{AgentDir: root, ManagedProviderConfig: providerConfig, gatewayCredential: full}
	payload, err := selectedModelCredentialPayload(cfg, modelOption{Provider: "acme", ID: "coder"})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := decodeGatewayCredentialBundle([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.DRobotics != "" || len(bundle.ProviderKeys) != 1 || bundle.ProviderKeys["HOBOT_CODE_PROVIDER_KEY_ACME"] != "acme-secret" {
		t.Fatalf("model probe retained unrelated credentials: %+v", bundle)
	}

	droboticsPayload, err := selectedModelCredentialPayload(cfg, modelOption{Provider: "drobotics", ID: "kimi-k3"})
	if err != nil {
		t.Fatal(err)
	}
	droboticsBundle, err := decodeGatewayCredentialBundle([]byte(droboticsPayload))
	if err != nil {
		t.Fatal(err)
	}
	if droboticsBundle.DRobotics != "drobotics-secret" || len(droboticsBundle.ProviderKeys) != 0 {
		t.Fatalf("D-Robotics probe retained unrelated credentials: %+v", droboticsBundle)
	}
}

func TestModelRuntimeProbeNormalizationNeverGrantsFullQualification(t *testing.T) {
	checks := make([]modelRuntimeProbeCheck, 0, len(runtimeProbeRequiredChecks))
	for _, name := range runtimeProbeRequiredChecks {
		checks = append(checks, modelRuntimeProbeCheck{Name: name, Status: "passed"})
	}
	result := normalizeModelRuntimeProbeResult(modelRuntimeProbeResult{Checks: checks, ReasoningDeclared: true, ImageInputDeclared: true})
	if result.Status != "partial" || result.Scope != "agent-runtime-partial" || len(result.Pending) == 0 {
		t.Fatalf("bounded probe incorrectly granted full qualification: %+v", result)
	}
	for name, invalid := range map[string][]modelRuntimeProbeCheck{
		"missing":   checks[:len(checks)-1],
		"duplicate": append(append([]modelRuntimeProbeCheck{}, checks...), checks[0]),
		"unknown":   append(append([]modelRuntimeProbeCheck{}, checks...), modelRuntimeProbeCheck{Name: "future", Status: "passed"}),
	} {
		if got := normalizeModelRuntimeProbeResult(modelRuntimeProbeResult{Checks: invalid, ReasoningDeclared: true, ImageInputDeclared: true}); got.Status != "failed" {
			t.Fatalf("%s runtime probe set was accepted: %+v", name, got)
		}
	}
}

func TestRuntimeProbeEnvironmentRemovesCredentialAndProcessInjection(t *testing.T) {
	result := runtimeProbeEnvironment([]string{
		"PATH=/usr/bin", "HOME=/tmp/host-home", "ANTHROPIC_BASE_URL=https://gateway.example", "ANTHROPIC_AUTH_TOKEN=secret",
		"HOBOT_CODE_GATEWAY_TOKEN_FD=9", "HOBOT_CODE_STATE_DIR=/tmp/host-state", "NODE_OPTIONS=--require=/tmp/evil.js",
		"NODE_PATH=/tmp/modules", "BASH_ENV=/tmp/evil", "LD_PRELOAD=/tmp/evil.so", "DYLD_INSERT_LIBRARIES=/tmp/evil.dylib",
	})
	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=https://gateway.example") {
		t.Fatalf("runtime probe dropped required environment: %v", result)
	}
	for _, forbidden := range []string{"PATH=", "host-home", "host-state", "secret", "GATEWAY_TOKEN", "NODE_OPTIONS", "NODE_PATH", "BASH_ENV", "LD_PRELOAD", "DYLD_INSERT"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("runtime probe retained unsafe environment %q: %v", forbidden, result)
		}
	}
}

func TestModelRuntimeProbeFailureCategoryIsBoundedAndSanitized(t *testing.T) {
	result := normalizeModelRuntimeProbeResult(modelRuntimeProbeResult{Category: "raw secret detail"})
	if result.Category != "protocol" || result.Message != runtimeProbeFailureMessage("protocol") || strings.Contains(result.Message, "secret") {
		t.Fatalf("runtime failure was not sanitized: %+v", result)
	}
}

func TestRuntimeProbeConfigurationUsesSelectedModelAndDisablesAmbientResources(t *testing.T) {
	directory := t.TempDir()
	if err := writeRuntimeProbeConfiguration(directory, modelOption{Provider: "drobotics", ID: "glm-5.2"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Provider   string   `json:"defaultProvider"`
		Model      string   `json:"defaultModel"`
		Thinking   string   `json:"defaultThinkingLevel"`
		Extensions []string `json:"extensions"`
		Skills     []string `json:"skills"`
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Provider != "drobotics" || settings.Model != "glm-5.2" || settings.Thinking != "off" || len(settings.Extensions) != 0 || len(settings.Skills) != 0 {
		t.Fatalf("runtime probe configuration inherited ambient resources: %+v", settings)
	}
}

func TestModelRuntimeProbeRejectsConcurrentRuns(t *testing.T) {
	manager := &taskManager{}
	manager.runtimeProbeMu.Lock()
	manager.runtimeProbeRunning = true
	manager.runtimeProbeMu.Unlock()
	if _, err := manager.runModelRuntimeProbe(modelRuntimeProbeParams{Model: "drobotics/kimi-k3"}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("concurrent runtime probe was accepted: %v", err)
	}
}

func TestObserveRuntimeProbeEventRequiresExactToolAndFinalText(t *testing.T) {
	observation := runtimeProbeObservation{promptID: "runtime-basic"}
	for _, event := range []string{
		`{"type":"response","id":"runtime-basic","command":"prompt","success":true}`,
		`{"type":"agent_start"}`,
		`{"type":"tool_execution_start","toolCallId":"wrong-1","toolName":"hobot_runtime_probe","args":{"stage":"basic","nonce":"wrong"}}`,
		`{"type":"tool_execution_end","toolCallId":"wrong-1","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_OK"}]},"isError":false}`,
		`{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"wrong"}]}}`,
		`{"type":"agent_settled"}`,
	} {
		observeRuntimeProbeEvent(&observation, []byte(event))
	}
	suite := validRuntimeProbeSuite()
	suite.basic = observation
	checks := runtimeProbeChecks(suite, runtimeProbeReasoningModel())
	for _, check := range checks {
		if check.Name == "tool-call" || check.Name == "tool-result" || check.Name == "continuation" {
			if check.Status != "failed" {
				t.Fatalf("invalid runtime event passed %s: %+v", check.Name, checks)
			}
		}
	}
}

func TestObserveRuntimeProbeEventUsesPostToolAssistantMessageAndCorrelatedResult(t *testing.T) {
	observation := runtimeProbeObservation{promptID: "runtime-basic"}
	for _, event := range []string{
		`{"type":"response","id":"runtime-basic","command":"prompt","success":true}`,
		`{"type":"agent_start"}`,
		`{"type":"message_end","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"text","text":"I will run the probe."}]}}`,
		`{"type":"tool_execution_start","toolCallId":"probe-1","toolName":"hobot_runtime_probe","args":{"stage":"basic","nonce":"hobot-runtime-probe-v1"}}`,
		`{"type":"tool_execution_end","toolCallId":"other","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_OK"}]},"isError":false}`,
		`{"type":"tool_execution_end","toolCallId":"probe-1","toolName":"hobot_runtime_probe","result":{"content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_OK"}]},"isError":false}`,
		`{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"HOBOT_RUNTIME_PROBE_COMPLETE"}]}}`,
		`{"type":"agent_settled"}`,
	} {
		observeRuntimeProbeEvent(&observation, []byte(event))
	}
	suite := validRuntimeProbeSuite()
	suite.basic = observation
	checks := runtimeProbeChecks(suite, runtimeProbeReasoningModel())
	for _, check := range checks {
		if check.Name == "tool-result" {
			if check.Status != "failed" {
				t.Fatalf("uncorrelated duplicate tool result was accepted: %+v", checks)
			}
			continue
		}
		if check.Status != "passed" && !(check.Name == "image-input" && check.Status == "not-applicable") {
			t.Fatalf("valid runtime event failed %s: %+v", check.Name, checks)
		}
	}
}

func TestRuntimeProbeChecksRequireSettledBarrier(t *testing.T) {
	suite := validRuntimeProbeSuite()
	suite.basic.settled = false
	for _, check := range runtimeProbeChecks(suite, runtimeProbeReasoningModel()) {
		if check.Name == "settled" && check.Status != "failed" {
			t.Fatalf("missing settled barrier was accepted: %+v", check)
		}
	}
}

func TestRuntimeProbeParallelRequiresTwoStartsBeforeEitherResult(t *testing.T) {
	suite := validRuntimeProbeSuite()
	suite.parallel.toolStarts[1].Index = 4
	suite.parallel.toolEnds[0].Index = 3
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "parallel-tools") != "failed" {
		t.Fatalf("serial tool calls were accepted as parallel: %+v", suite.parallel)
	}
	suite = validRuntimeProbeSuite()
	suite.parallel.toolStarts[1].Nonce = "parallel-a"
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "parallel-tools") != "failed" {
		t.Fatalf("duplicate parallel arguments were accepted: %+v", suite.parallel)
	}
}

func TestRuntimeProbeRecoveryRequiresErrorBeforeCorrectedCall(t *testing.T) {
	suite := validRuntimeProbeSuite()
	suite.recovery.toolEnds[0].Index = 4
	suite.recovery.toolStarts[1].Index = 3
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "invalid-argument-recovery") != "failed" {
		t.Fatalf("preemptive retry was accepted as error recovery: %+v", suite.recovery)
	}
	suite = validRuntimeProbeSuite()
	suite.recovery.toolEnds[0].Error = false
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "invalid-argument-recovery") != "failed" {
		t.Fatalf("recovery without an observed error was accepted: %+v", suite.recovery)
	}
}

func TestRuntimeProbeRequiresResultsAndFinalTextInCausalOrder(t *testing.T) {
	suite := validRuntimeProbeSuite()
	suite.basic.toolEnds[0].Index = suite.basic.toolStarts[0].Index
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "tool-result") != "failed" {
		t.Fatalf("a tool result at or before its call was accepted: %+v", suite.basic)
	}
	suite = validRuntimeProbeSuite()
	suite.basic.lastAssistantIndex = suite.basic.toolEnds[0].Index
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "continuation") != "failed" {
		t.Fatalf("a final response before the tool result was accepted: %+v", suite.basic)
	}
}

func TestRuntimeProbeThinkingRequiresStructuredSeparatedReasoning(t *testing.T) {
	for name, mutate := range map[string]func(*runtimeProbeObservation){
		"level-not-applied": func(observation *runtimeProbeObservation) { observation.thinkingLevelState = false },
		"no-delta":          func(observation *runtimeProbeObservation) { observation.thinkingDeltas = 0 },
		"text-before-end":   func(observation *runtimeProbeObservation) { observation.firstTextIndex = observation.thinkingEndIndex },
		"thinking-in-text":  func(observation *runtimeProbeObservation) { observation.finalThinking = 0 },
		"shared-block":      func(observation *runtimeProbeObservation) { observation.textBlock = observation.thinkingBlock },
		"reordered-final":   func(observation *runtimeProbeObservation) { observation.finalShape = "text,thinking" },
		"tool-call": func(observation *runtimeProbeObservation) {
			observation.toolStarts = []runtimeProbeToolEvent{{CallID: "unexpected"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			suite := validRuntimeProbeSuite()
			mutate(&suite.thinking)
			if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "thinking-stream") != "failed" {
				t.Fatalf("invalid thinking stream was accepted: %+v", suite.thinking)
			}
		})
	}
}

func TestRuntimeProbeApprovalRequiresOneCorrelatedConfirmation(t *testing.T) {
	for name, mutate := range map[string]func(*runtimeProbeObservation){
		"not-responded": func(observation *runtimeProbeObservation) { observation.approvalResponded = false },
		"multiple":      func(observation *runtimeProbeObservation) { observation.approvalRequests = 2 },
		"before-tool": func(observation *runtimeProbeObservation) {
			observation.approvalRequestIndex = observation.toolStarts[0].Index
		},
		"after-tool": func(observation *runtimeProbeObservation) {
			observation.approvalRequestIndex = observation.toolEnds[0].Index
		},
		"invalid-request": func(observation *runtimeProbeObservation) { observation.invalidApproval = true },
	} {
		t.Run(name, func(t *testing.T) {
			suite := validRuntimeProbeSuite()
			mutate(&suite.approval)
			if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "approval-flow") != "failed" {
				t.Fatalf("invalid approval flow was accepted: %+v", suite.approval)
			}
		})
	}
}

func TestRuntimeProbeApprovalRequestRequiresExactDialog(t *testing.T) {
	valid := `{"type":"extension_ui_request","id":"approval-1","method":"confirm","title":"Hobot Code runtime probe","message":"Allow this isolated read-only approval probe?"}`
	request, ok := runtimeProbeApprovalRequest([]byte(valid))
	if !ok || request.ID != "approval-1" || request.Method != "confirm" || request.Title != runtimeProbeApprovalTitle || request.Message != runtimeProbeApprovalMessage {
		t.Fatalf("valid approval request was not parsed: %+v %v", request, ok)
	}
	if _, ok := runtimeProbeApprovalRequest([]byte(`{"type":"message_update"}`)); ok {
		t.Fatal("a non-approval event was parsed as an approval")
	}
}

func TestRuntimeProbeApprovalRequestIgnoresNonConfirmUIEvents(t *testing.T) {
	for _, event := range []string{
		`{"type":"extension_ui_request","id":"status-1","method":"setStatus","statusKey":"hobot-gates","statusText":"gates: disabled"}`,
		`{"type":"extension_ui_request","id":"notify-1","method":"notify","message":"completed"}`,
	} {
		if _, ok := runtimeProbeApprovalRequest([]byte(event)); ok {
			t.Fatalf("non-confirm UI event was treated as an approval: %s", event)
		}
	}
}

func TestRuntimeProbeImageRequiresExactVisionResponse(t *testing.T) {
	suite := validRuntimeProbeSuite()
	visionModel := runtimeProbeReasoningModel()
	visionModel.Capabilities.ImageInput = true
	if runtimeProbeCheckStatus(runtimeProbeChecks(suite, visionModel), "image-input") != "passed" {
		t.Fatalf("valid image flow failed: %+v", suite.image)
	}
	for name, mutate := range map[string]func(*runtimeProbeObservation){
		"wrong-answer": func(observation *runtimeProbeObservation) { observation.lastAssistantText = "red top-left" },
		"tool-call": func(observation *runtimeProbeObservation) {
			observation.toolStarts = []runtimeProbeToolEvent{{CallID: "unexpected"}}
		},
		"not-settled": func(observation *runtimeProbeObservation) { observation.settled = false },
	} {
		t.Run(name, func(t *testing.T) {
			suite := validRuntimeProbeSuite()
			mutate(&suite.image)
			if runtimeProbeCheckStatus(runtimeProbeChecks(suite, visionModel), "image-input") != "failed" {
				t.Fatalf("invalid image flow was accepted: %+v", suite.image)
			}
		})
	}
}

func TestRuntimeProbeImageIsNotApplicableForTextOnlyModels(t *testing.T) {
	suite := validRuntimeProbeSuite()
	suite.model = "drobotics/text-only"
	suite.persist.sessionModel = suite.model
	suite.persist.resumedSessionModel = suite.model
	suite.image = runtimeProbeObservation{settled: true}
	checks := runtimeProbeChecks(suite, modelOption{Provider: "drobotics", ID: "text-only", Capabilities: modelCapabilities{Reasoning: true}})
	if runtimeProbeCheckStatus(checks, "image-input") != "not-applicable" {
		t.Fatalf("text-only image stage was not marked inapplicable: %+v", checks)
	}
	result := normalizeModelRuntimeProbeResult(modelRuntimeProbeResult{Checks: checks, ReasoningDeclared: true, ImageInputDeclared: false})
	if result.Status != "partial" {
		t.Fatalf("a valid text-only runtime probe was rejected: %+v", result)
	}
}

func TestRuntimeProbeOptionalCapabilitiesCanBothBeNotApplicable(t *testing.T) {
	suite := validRuntimeProbeSuite()
	suite.model = "drobotics/basic-text"
	suite.persist.sessionModel = suite.model
	suite.persist.resumedSessionModel = suite.model
	suite.thinking = runtimeProbeObservation{settled: true}
	suite.image = runtimeProbeObservation{settled: true}
	checks := runtimeProbeChecks(suite, modelOption{Provider: "drobotics", ID: "basic-text"})
	if runtimeProbeCheckStatus(checks, "thinking-stream") != "not-applicable" || runtimeProbeCheckStatus(checks, "image-input") != "not-applicable" {
		t.Fatalf("optional model capabilities were not marked inapplicable: %+v", checks)
	}
	result := normalizeModelRuntimeProbeResult(modelRuntimeProbeResult{Checks: checks})
	if result.Status != "partial" {
		t.Fatalf("a basic text model failed otherwise valid runtime checks: %+v", result)
	}
}

func TestRuntimeProbeCompactionRequiresCausalReductionAndSemanticRecall(t *testing.T) {
	for name, mutate := range map[string]func(*runtimeProbePersistentObservation){
		"response-before-end": func(observation *runtimeProbePersistentObservation) {
			observation.compaction.responseIndex = observation.compaction.endIndex
		},
		"no-reduction": func(observation *runtimeProbePersistentObservation) {
			observation.compaction.estimatedTokensAfter = observation.compaction.tokensBefore
		},
		"empty-summary": func(observation *runtimeProbePersistentObservation) {
			observation.compaction.summaryPresent = false
		},
		"wrong-recall": func(observation *runtimeProbePersistentObservation) {
			observation.postCompaction.lastAssistantText = "wrong"
		},
	} {
		t.Run(name, func(t *testing.T) {
			suite := validRuntimeProbeSuite()
			mutate(&suite.persist)
			if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "context-compaction") != "failed" {
				t.Fatalf("invalid compaction evidence was accepted: %+v", suite.persist)
			}
		})
	}
}

func TestRuntimeProbeCompactionFailureMessageNamesOnlyFailedEvidence(t *testing.T) {
	observation := validRuntimeProbeSuite().persist
	observation.compaction.estimatedTokensAfter = observation.compaction.tokensBefore
	message := runtimeProbeCompactionFailureMessage(observation)
	if message != "Pi context compaction failed bounded checks: token-reduction(before=200,after=200)." {
		t.Fatalf("unexpected bounded compaction diagnostic: %q", message)
	}
}

func TestRuntimeProbeInterruptedRecoveryRequiresSameSessionWithoutReplay(t *testing.T) {
	for name, mutate := range map[string]func(*runtimeProbePersistentObservation){
		"not-durable": func(observation *runtimeProbePersistentObservation) {
			observation.interruptedStateDurable = false
		},
		"session-changed": func(observation *runtimeProbePersistentObservation) {
			observation.resumedSessionID = "other"
		},
		"model-changed": func(observation *runtimeProbePersistentObservation) {
			observation.resumedSessionModel = "drobotics/other"
		},
		"tool-replayed": func(observation *runtimeProbePersistentObservation) {
			observation.replayedTool = true
		},
		"lost-context": func(observation *runtimeProbePersistentObservation) {
			observation.recovered.lastAssistantText = "unknown"
		},
	} {
		t.Run(name, func(t *testing.T) {
			suite := validRuntimeProbeSuite()
			mutate(&suite.persist)
			if runtimeProbeCheckStatus(runtimeProbeChecks(suite, runtimeProbeReasoningModel()), "interrupted-session-recovery") != "failed" {
				t.Fatalf("invalid interrupted-session evidence was accepted: %+v", suite.persist)
			}
		})
	}
}

func TestRuntimeProbeSessionValidationRejectsEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRuntimeProbeSessionFile(root, outside, true); err == nil {
		t.Fatal("session outside the private runtime root was accepted")
	}
	link := filepath.Join(root, "linked.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRuntimeProbeSessionFile(root, link, false); err == nil {
		t.Fatal("symbolic-link session was accepted before persistence")
	}
}

func validRuntimeProbeSuite() runtimeProbeSuiteObservation {
	return runtimeProbeSuiteObservation{
		model: "drobotics/kimi-k3",
		basic: runtimeProbeObservation{
			promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeFinalText, lastAssistantIndex: 3,
			toolStarts: []runtimeProbeToolEvent{{CallID: "basic", Stage: "basic", Nonce: runtimeProbeNonce, Index: 1, Args: 2}},
			toolEnds:   []runtimeProbeToolEvent{{CallID: "basic", Result: "HOBOT_RUNTIME_PROBE_OK", Index: 2}},
		},
		parallel: runtimeProbeObservation{
			promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeParallelText, lastAssistantIndex: 5,
			toolStarts: []runtimeProbeToolEvent{
				{CallID: "parallel-a", Stage: "parallel", Nonce: "parallel-a", Index: 1, Args: 2},
				{CallID: "parallel-b", Stage: "parallel", Nonce: "parallel-b", Index: 2, Args: 2},
			},
			toolEnds: []runtimeProbeToolEvent{
				{CallID: "parallel-b", Result: "HOBOT_RUNTIME_PARALLEL_B", Index: 3},
				{CallID: "parallel-a", Result: "HOBOT_RUNTIME_PARALLEL_A", Index: 4},
			},
		},
		recovery: runtimeProbeObservation{
			promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeRecoveryText, lastAssistantIndex: 5,
			toolStarts: []runtimeProbeToolEvent{
				{CallID: "recovery-error", Stage: "recovery", Nonce: "invalid-on-purpose", Index: 1, Args: 2},
				{CallID: "recovery-fixed", Stage: "recovery", Nonce: "repaired-after-error", Index: 3, Args: 2},
			},
			toolEnds: []runtimeProbeToolEvent{
				{CallID: "recovery-error", Result: "HOBOT_RUNTIME_PROBE_EXPECTED_ARGUMENT_ERROR", Error: true, Index: 2},
				{CallID: "recovery-fixed", Result: "HOBOT_RUNTIME_RECOVERY_OK", Index: 4},
			},
		},
		thinking: runtimeProbeObservation{
			promptAccepted: true, agentStarted: true, settled: true, thinkingLevelSet: true, thinkingLevelState: true,
			thinkingStarts: 1, thinkingDeltas: 1, thinkingEnds: 1, thinkingStartIndex: 1, thinkingEndIndex: 3,
			textStarts: 1, textBlock: 1, firstTextIndex: 4, finalThinking: 1, finalText: 1,
			finalShape: "thinking,text", lastAssistantText: runtimeProbeThinkingText, lastAssistantIndex: 5,
		},
		approval: runtimeProbeObservation{
			promptAccepted: true, agentStarted: true, settled: true, approvalRequests: 1,
			approvalRequestID: "approval-1", approvalRequestIndex: 2, approvalResponded: true,
			lastAssistantText: runtimeProbeApprovalText, lastAssistantIndex: 4,
			toolStarts: []runtimeProbeToolEvent{{CallID: "approval-call", Stage: "approval", Nonce: "confirm-read-only", Index: 1, Args: 2}},
			toolEnds:   []runtimeProbeToolEvent{{CallID: "approval-call", Result: "HOBOT_RUNTIME_APPROVAL_OK", Index: 3}},
		},
		image: runtimeProbeObservation{
			promptAccepted: true, agentStarted: true, settled: true,
			lastAssistantText: runtimeProbeImageText, lastAssistantIndex: 1,
		},
		persist: runtimeProbePersistentObservation{
			seed:           runtimeProbeObservation{promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeMemoryStored, lastAssistantIndex: 1},
			filler:         runtimeProbeObservation{promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeFillerText, lastAssistantIndex: 1},
			postCompaction: runtimeProbeObservation{promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeCompactionToken, lastAssistantIndex: 1},
			interrupt: runtimeProbeObservation{
				promptAccepted: true, agentStarted: true,
				toolStarts: []runtimeProbeToolEvent{{CallID: "interrupt-call", Stage: "interrupt", Nonce: "wait-for-termination", Index: 1, Args: 2}},
			},
			recovered: runtimeProbeObservation{promptAccepted: true, agentStarted: true, settled: true, lastAssistantText: runtimeProbeRecoveryToken, lastAssistantIndex: 1},
			compaction: runtimeProbeCompactionObservation{
				startIndex: 1, endIndex: 2, responseIndex: 3, startReason: "manual", endReason: "manual", responseSuccess: true,
				resultPresent: true, summaryPresent: true, firstKeptEntryID: "kept", tokensBefore: 200, estimatedTokensAfter: 90,
			},
			sessionFile: "/private/session.jsonl", sessionID: "session", sessionModel: "drobotics/kimi-k3",
			resumedSessionFile: "/private/session.jsonl", resumedSessionID: "session", resumedSessionModel: "drobotics/kimi-k3",
			sessionValidated: true, interruptedStateDurable: true, processKilled: true, resumeStateReceived: true,
		},
	}
}

func runtimeProbeCheckStatus(checks []modelRuntimeProbeCheck, name string) string {
	for _, check := range checks {
		if check.Name == name {
			return check.Status
		}
	}
	return "missing"
}
