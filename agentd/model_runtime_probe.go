package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	modelRuntimeProbeSchema     = 1
	modelRuntimeProbeTimeout    = 15 * time.Minute
	runtimeProbeToolName        = "hobot_runtime_probe"
	runtimeProbeNonce           = "hobot-runtime-probe-v1"
	runtimeProbeFinalText       = "HOBOT_RUNTIME_PROBE_COMPLETE"
	runtimeProbeParallelText    = "HOBOT_RUNTIME_PARALLEL_COMPLETE"
	runtimeProbeRecoveryText    = "HOBOT_RUNTIME_RECOVERY_COMPLETE"
	runtimeProbeThinkingText    = "HOBOT_RUNTIME_THINKING_COMPLETE"
	runtimeProbeApprovalText    = "HOBOT_RUNTIME_APPROVAL_COMPLETE"
	runtimeProbeApprovalTitle   = "Hobot Code runtime probe"
	runtimeProbeApprovalMessage = "Allow this isolated read-only approval probe?"
	runtimeProbeImageText       = "red green blue yellow"
	runtimeProbeMemoryStored    = "HOBOT_RUNTIME_MEMORY_STORED"
	runtimeProbeFillerText      = "HOBOT_RUNTIME_FILLER_STORED"
	runtimeProbeCompactionToken = "HOBOT_COMPACTION_CANARY_7F3C9A2D"
	runtimeProbeRecoveryToken   = "HOBOT_RECOVERY_CANARY_4B8E1D6A"
	runtimeProbeImageData       = "iVBORw0KGgoAAAANSUhEUgAAAGAAAABgCAIAAABt+uBvAAAAvUlEQVR4nO3QQQ2DQBRAQUT0vHIqAokVgZJ1gIm64L+ESUbBHHutlPX7phzjI4IECRIUMj4iSJAgQSGCBAkSJEiQIEGCIsZHBAkSJChEkCBBggQJEiRIUMT4iCBBggSFCBIkSJAgQYIECYoYHxEkSNCjQevcKff1SREkSJAgQYIECRIUMT4iSJAgQSGCBAkSJEiQIEGCIsZHBAkSJChEkCBBggQJEiRIUMT4iCBBggSFCBIkSJAgQYIEvTXoD4Qz8bM/JxjsAAAAAElFTkSuQmCC"
)

var runtimeProbeRequiredChecks = []string{
	"rpc-lifecycle",
	"model-selection",
	"tool-call",
	"tool-result",
	"continuation",
	"settled",
	"parallel-tools",
	"invalid-argument-recovery",
	"thinking-stream",
	"approval-flow",
	"image-input",
	"context-compaction",
	"interrupted-session-recovery",
}

var runtimeProbePendingChecks = []string{
	"rdk-task-suite",
}

type modelRuntimeProbeParams struct {
	Model string `json:"model,omitempty"`
}

type modelRuntimeProbeCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type modelRuntimeProbeResult struct {
	SchemaVersion      int                      `json:"schemaVersion"`
	Scope              string                   `json:"scope"`
	Provider           string                   `json:"provider"`
	Model              string                   `json:"model"`
	Status             string                   `json:"status"`
	Category           string                   `json:"category,omitempty"`
	Message            string                   `json:"message"`
	ReasoningDeclared  bool                     `json:"reasoningDeclared"`
	ImageInputDeclared bool                     `json:"imageInputDeclared"`
	CheckedAt          time.Time                `json:"checkedAt"`
	DurationMS         int64                    `json:"durationMs,omitempty"`
	Checks             []modelRuntimeProbeCheck `json:"checks"`
	Pending            []string                 `json:"pending"`
}

type runtimeProbeObservation struct {
	promptID             string
	promptAccepted       bool
	agentStarted         bool
	toolStarts           []runtimeProbeToolEvent
	toolEnds             []runtimeProbeToolEvent
	lastAssistantText    string
	lastAssistantIndex   int
	thinkingLevelSet     bool
	thinkingLevelState   bool
	thinkingStarts       int
	thinkingDeltas       int
	thinkingEnds         int
	thinkingBlock        int
	thinkingStartIndex   int
	thinkingEndIndex     int
	textStarts           int
	textBlock            int
	firstTextIndex       int
	finalThinking        int
	finalText            int
	finalOther           int
	finalShape           string
	invalidThinking      bool
	approvalRequests     int
	approvalRequestID    string
	approvalRequestIndex int
	approvalResponded    bool
	invalidApproval      bool
	extensionErr         bool
	settled              bool
	eventIndex           int
}

type runtimeProbeToolEvent struct {
	CallID string
	Stage  string
	Nonce  string
	Result string
	Error  bool
	Index  int
	Args   int
}

type runtimeProbeSuiteObservation struct {
	model    string
	basic    runtimeProbeObservation
	parallel runtimeProbeObservation
	recovery runtimeProbeObservation
	thinking runtimeProbeObservation
	approval runtimeProbeObservation
	image    runtimeProbeObservation
	persist  runtimeProbePersistentObservation
}

type runtimeProbePersistentObservation struct {
	seed                    runtimeProbeObservation
	filler                  runtimeProbeObservation
	postCompaction          runtimeProbeObservation
	interrupt               runtimeProbeObservation
	recovered               runtimeProbeObservation
	compaction              runtimeProbeCompactionObservation
	sessionFile             string
	sessionID               string
	sessionModel            string
	resumedSessionFile      string
	resumedSessionID        string
	resumedSessionModel     string
	sessionValidated        bool
	interruptedStateDurable bool
	processKilled           bool
	resumeStateReceived     bool
	replayedTool            bool
}

type runtimeProbeCompactionObservation struct {
	eventIndex           int
	startIndex           int
	endIndex             int
	responseIndex        int
	startReason          string
	endReason            string
	responseSuccess      bool
	aborted              bool
	willRetry            bool
	resultPresent        bool
	summaryPresent       bool
	firstKeptEntryID     string
	tokensBefore         int64
	estimatedTokensAfter int64
	invalid              bool
}

type runtimeProbeProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	lines   <-chan runtimeProbeLine
	wait    <-chan error
}

type runtimeProbeLine struct {
	raw json.RawMessage
	err error
}

func (manager *taskManager) runModelRuntimeProbe(params modelRuntimeProbeParams) (modelRuntimeProbeResult, error) {
	manager.runtimeProbeMu.Lock()
	if manager.runtimeProbeRunning {
		manager.runtimeProbeMu.Unlock()
		return modelRuntimeProbeResult{}, fmt.Errorf("a model runtime probe is already running")
	}
	manager.runtimeProbeRunning = true
	manager.runtimeProbeMu.Unlock()
	defer func() {
		manager.runtimeProbeMu.Lock()
		manager.runtimeProbeRunning = false
		manager.runtimeProbeMu.Unlock()
	}()

	models, err := manager.availableModels()
	if err != nil {
		return modelRuntimeProbeResult{}, fmt.Errorf("discover models: %w", err)
	}
	selection := normalizeModelSelection(params.Model)
	if strings.TrimSpace(params.Model) != "" && selection == "" {
		return modelRuntimeProbeResult{}, fmt.Errorf("model must use provider/model format")
	}
	if selection == "" {
		for key, model := range models {
			if model.Default {
				selection = key
				break
			}
		}
	}
	model, ok := models[selection]
	if !ok {
		return modelRuntimeProbeResult{}, fmt.Errorf("model is not available: %s", selection)
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelRuntimeProbeTimeout)
	defer cancel()
	result := probeModelThroughPiRuntime(ctx, manager.cfg, model)
	result.Provider = model.Provider
	result.Model = model.ID
	result.ReasoningDeclared = model.Capabilities.Reasoning
	result.ImageInputDeclared = model.Capabilities.ImageInput
	result.CheckedAt = time.Now().UTC()
	return normalizeModelRuntimeProbeResult(result), nil
}

func normalizeModelRuntimeProbeResult(result modelRuntimeProbeResult) modelRuntimeProbeResult {
	result.SchemaVersion = modelRuntimeProbeSchema
	result.Scope = "agent-runtime-partial"
	result.Pending = append([]string(nil), runtimeProbePendingChecks...)
	seen := make(map[string]bool, len(result.Checks))
	passed := true
	for index := range result.Checks {
		check := &result.Checks[index]
		if seen[check.Name] || !runtimeProbeCheckRequired(check.Name) {
			check.Status = "failed"
			check.Message = "The runtime probe returned an invalid check set."
			passed = false
		}
		seen[check.Name] = true
		if check.Name == "thinking-stream" {
			if (result.ReasoningDeclared && check.Status != "passed") || (!result.ReasoningDeclared && check.Status != "not-applicable") {
				passed = false
			}
		} else if check.Name == "image-input" {
			if (result.ImageInputDeclared && check.Status != "passed") || (!result.ImageInputDeclared && check.Status != "not-applicable") {
				passed = false
			}
		} else if check.Status != "passed" {
			passed = false
		}
	}
	for _, name := range runtimeProbeRequiredChecks {
		if !seen[name] {
			result.Checks = append(result.Checks, modelRuntimeProbeCheck{Name: name, Status: "failed", Message: runtimeProbeCheckMessage(name, false)})
			passed = false
		}
	}
	if passed {
		result.Status = "partial"
		result.Category = ""
		result.Message = "The model completed the bounded read-only Pi RPC runtime suite. Additional Agent runtime and RDK task checks remain untested."
	} else {
		result.Status = "failed"
		switch result.Category {
		case "preparation", "configuration", "process", "protocol", "timeout":
		default:
			result.Category = "protocol"
		}
		result.Message = runtimeProbeFailureMessage(result.Category)
	}
	return result
}

func runtimeProbeCheckRequired(name string) bool {
	for _, required := range runtimeProbeRequiredChecks {
		if name == required {
			return true
		}
	}
	return false
}

func runtimeProbeFailureMessage(category string) string {
	return map[string]string{
		"preparation":   "The isolated runtime probe could not be prepared. No Agent runtime qualification was granted.",
		"configuration": "The model credential or isolated runtime configuration was unavailable. No Agent runtime qualification was granted.",
		"process":       "The pinned Pi runtime could not be started. No Agent runtime qualification was granted.",
		"protocol":      "The Pi runtime did not complete the bounded read-only runtime suite. No Agent runtime qualification was granted.",
		"timeout":       "The bounded Pi runtime suite timed out. No Agent runtime qualification was granted.",
	}[category]
}

func runtimeProbeCheckMessage(name string, passed bool) string {
	if !passed {
		return map[string]string{
			"rpc-lifecycle":                "Pi RPC did not accept and start the bounded prompt.",
			"model-selection":              "Pi did not report the requested provider and model.",
			"tool-call":                    "The model did not call the only active read-only probe tool exactly once with valid arguments.",
			"tool-result":                  "Pi did not complete the read-only probe tool successfully.",
			"continuation":                 "The model did not produce the required continuation after the tool result.",
			"settled":                      "Pi did not emit the final settled barrier.",
			"parallel-tools":               "The model did not issue and complete both read-only tool calls in one parallel batch.",
			"invalid-argument-recovery":    "The model did not observe the intentional semantic argument error and repair the next tool call.",
			"thinking-stream":              "Pi did not preserve a non-empty structured thinking stream separately from final text.",
			"approval-flow":                "Pi did not complete the exact correlated read-only confirmation and continue the tool turn.",
			"image-input":                  "Pi did not deliver the bounded image prompt to the declared vision model and preserve its exact response.",
			"context-compaction":           "Pi did not compact a private persisted session and preserve its pre-compaction semantic token.",
			"interrupted-session-recovery": "Pi did not durably resume the exact private session after a forced mid-tool interruption without replaying the tool.",
		}[name]
	}
	return map[string]string{
		"rpc-lifecycle":                "Pi RPC accepted the prompt and began an Agent run.",
		"model-selection":              "Pi selected the exact requested provider and model.",
		"tool-call":                    "The model called the only active read-only probe tool exactly once with valid arguments.",
		"tool-result":                  "Pi completed the read-only probe tool successfully.",
		"continuation":                 "The model consumed the tool result and completed the required continuation.",
		"settled":                      "Pi emitted a final settled barrier after every runtime probe stage.",
		"parallel-tools":               "The model issued two read-only calls before either result and consumed both results.",
		"invalid-argument-recovery":    "The model consumed the intentional semantic argument error, repaired the call, and continued.",
		"thinking-stream":              "Pi preserved the requested non-empty structured thinking stream separately from exact final text.",
		"approval-flow":                "Pi correlated the exact confirmation response, resumed the read-only tool, and completed the turn.",
		"image-input":                  "Pi delivered the bounded image prompt to the declared vision model and preserved its exact response.",
		"context-compaction":           "Pi compacted a private persisted session and preserved its pre-compaction semantic token.",
		"interrupted-session-recovery": "Pi durably resumed the exact private session after a forced mid-tool interruption without replaying the tool.",
	}[name]
}

func probeModelThroughPiRuntime(ctx context.Context, cfg config, model modelOption) modelRuntimeProbeResult {
	started := time.Now()
	result := modelRuntimeProbeResult{Checks: make([]modelRuntimeProbeCheck, 0, len(runtimeProbeRequiredChecks))}
	credentialPayload, credentialErr := selectedModelCredentialPayload(cfg, model)
	if credentialErr != nil || strings.TrimSpace(credentialPayload) == "" {
		return failedRuntimeProbeResult(started, "configuration")
	}
	probeConfig := cfg
	probeConfig.gatewayCredential = credentialPayload
	probeConfig.gatewayToken = ""
	temporaryRoot, err := os.MkdirTemp(cfg.AgentdRoot, ".model-runtime-probe-")
	if err != nil {
		return failedRuntimeProbeResult(started, "preparation")
	}
	_ = os.Chmod(temporaryRoot, 0o700)
	defer os.RemoveAll(temporaryRoot)

	agentDir := filepath.Join(temporaryRoot, "agent")
	stateRoot := filepath.Join(temporaryRoot, "state")
	workspace := filepath.Join(temporaryRoot, "workspace")
	home := filepath.Join(temporaryRoot, "home")
	temporaryDirectory := filepath.Join(temporaryRoot, "tmp")
	for _, directory := range []string{agentDir, stateRoot, workspace, home, temporaryDirectory, filepath.Join(stateRoot, "sessions")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return failedRuntimeProbeResult(started, "preparation")
		}
	}
	if err := writeRuntimeProbeConfiguration(agentDir, model, probeConfig); err != nil {
		return failedRuntimeProbeResult(started, "preparation")
	}
	rdkExtension, probeExtension, err := runtimeProbeExtensionPaths(cfg)
	if err != nil {
		return failedRuntimeProbeResult(started, "preparation")
	}

	process, category := startRuntimeProbeProcess(ctx, probeConfig, model, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension, []string{"--no-session"}, "ephemeral")
	if category != "" {
		return failedRuntimeProbeResult(started, category)
	}
	command, stdin, lines, wait := process.command, process.stdin, process.lines, process.wait

	stateCommand, _ := json.Marshal(map[string]any{"id": "runtime-state", "type": "get_state"})
	if _, err := stdin.Write(append(stateCommand, '\n')); err != nil {
		cancelRuntimeProbe(command, stdin, wait)
		return failedRuntimeProbeResult(started, "protocol")
	}

	suite := runtimeProbeSuiteObservation{}
	consumeEvent := func(observation *runtimeProbeObservation) string {
		select {
		case <-ctx.Done():
			return "timeout"
		case event, ok := <-lines:
			if !ok || event.err != nil {
				if ctx.Err() != nil {
					return "timeout"
				}
				return "protocol"
			}
			if modelSelection := runtimeProbeModelSelection(event.raw); modelSelection != "" {
				suite.model = modelSelection
			}
			observeRuntimeProbeEvent(observation, event.raw)
			if request, ok := runtimeProbeApprovalRequest(event.raw); ok {
				observation.approvalRequests++
				observation.approvalRequestIndex = observation.eventIndex
				valid := observation.promptID == "runtime-approval" && observation.approvalRequests == 1 &&
					modelProviderPattern.MatchString(request.ID) && request.Method == "confirm" &&
					request.Title == runtimeProbeApprovalTitle && request.Message == runtimeProbeApprovalMessage
				if !valid {
					observation.invalidApproval = true
					return "protocol"
				}
				observation.approvalRequestID = request.ID
				response, _ := json.Marshal(map[string]any{"type": "extension_ui_response", "id": request.ID, "confirmed": true})
				if _, err := stdin.Write(append(response, '\n')); err != nil {
					return "protocol"
				}
				observation.approvalResponded = true
			}
			return ""
		}
	}
	stages := []struct {
		id          string
		instruction string
		observation *runtimeProbeObservation
	}{
		{
			id:          "runtime-basic",
			instruction: "Call hobot_runtime_probe exactly once with stage basic and nonce hobot-runtime-probe-v1. After receiving the tool result, reply exactly HOBOT_RUNTIME_PROBE_COMPLETE.",
			observation: &suite.basic,
		},
		{
			id:          "runtime-parallel",
			instruction: "In one assistant response, call hobot_runtime_probe exactly twice: once with stage parallel and nonce parallel-a, and once with stage parallel and nonce parallel-b. Emit both calls before receiving either result. After both results, reply exactly HOBOT_RUNTIME_PARALLEL_COMPLETE.",
			observation: &suite.parallel,
		},
		{
			id:          "runtime-recovery",
			instruction: "Call hobot_runtime_probe with stage recovery and nonce invalid-on-purpose. It will return an intentional semantic argument error. After receiving that error, repair the call by invoking the same tool exactly once with stage recovery and nonce repaired-after-error. Then reply exactly HOBOT_RUNTIME_RECOVERY_COMPLETE.",
			observation: &suite.recovery,
		},
		{
			id:          "runtime-thinking",
			instruction: "Do not call any tool. Use the model's structured reasoning channel to determine 17 multiplied by 19, then reply with exactly HOBOT_RUNTIME_THINKING_COMPLETE and no other visible text.",
			observation: &suite.thinking,
		},
		{
			id:          "runtime-approval",
			instruction: "Call hobot_runtime_probe exactly once with stage approval and nonce confirm-read-only. Wait for the confirmation and tool result, then reply exactly HOBOT_RUNTIME_APPROVAL_COMPLETE.",
			observation: &suite.approval,
		},
		{
			id:          "runtime-image",
			instruction: "Inspect the attached four-quadrant image. Reply with exactly four lowercase color words in this order: top-left, top-right, bottom-left, bottom-right. Use only red, green, blue, or yellow. Do not call a tool or add punctuation or other text.",
			observation: &suite.image,
		},
	}
	for _, stage := range stages {
		stage.observation.promptID = stage.id
		if stage.id == "runtime-thinking" {
			if !model.Capabilities.Reasoning {
				stage.observation.settled = true
				continue
			}
			thinkingCommand, _ := json.Marshal(map[string]any{"id": "runtime-thinking-level", "type": "set_thinking_level", "level": "high"})
			if _, err := stdin.Write(append(thinkingCommand, '\n')); err != nil {
				cancelRuntimeProbe(command, stdin, wait)
				return failedRuntimeProbeResult(started, "protocol")
			}
			for !stage.observation.thinkingLevelSet && !stage.observation.invalidThinking {
				if category := consumeEvent(stage.observation); category != "" {
					cancelRuntimeProbe(command, stdin, wait)
					return failedRuntimeProbeResult(started, category)
				}
			}
			if stage.observation.invalidThinking {
				cancelRuntimeProbe(command, stdin, wait)
				return failedRuntimeProbeResult(started, "protocol")
			}
			thinkingStateCommand, _ := json.Marshal(map[string]any{"id": "runtime-thinking-state", "type": "get_state"})
			if _, err := stdin.Write(append(thinkingStateCommand, '\n')); err != nil {
				cancelRuntimeProbe(command, stdin, wait)
				return failedRuntimeProbeResult(started, "protocol")
			}
			for !stage.observation.thinkingLevelState && !stage.observation.invalidThinking {
				if category := consumeEvent(stage.observation); category != "" {
					cancelRuntimeProbe(command, stdin, wait)
					return failedRuntimeProbeResult(started, category)
				}
			}
			if stage.observation.invalidThinking {
				cancelRuntimeProbe(command, stdin, wait)
				return failedRuntimeProbeResult(started, "protocol")
			}
		}
		prompt := map[string]any{"id": stage.id, "type": "prompt", "message": stage.instruction}
		if stage.id == "runtime-image" {
			if !model.Capabilities.ImageInput {
				stage.observation.settled = true
				continue
			}
			images := []imageContent{{Type: "image", Data: runtimeProbeImageData, MimeType: "image/png"}}
			if validateImages(images) != nil {
				cancelRuntimeProbe(command, stdin, wait)
				return failedRuntimeProbeResult(started, "preparation")
			}
			prompt["images"] = images
		}
		promptCommand, _ := json.Marshal(prompt)
		if _, err := stdin.Write(append(promptCommand, '\n')); err != nil {
			cancelRuntimeProbe(command, stdin, wait)
			return failedRuntimeProbeResult(started, "protocol")
		}
		for !stage.observation.settled {
			if category := consumeEvent(stage.observation); category != "" {
				cancelRuntimeProbe(command, stdin, wait)
				return failedRuntimeProbeResult(started, category)
			}
		}
	}
	cancelRuntimeProbe(command, stdin, wait)
	persistent, category := probePersistentRuntime(ctx, probeConfig, model, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension)
	if category != "" {
		return failedRuntimeProbeResult(started, category)
	}
	suite.persist = persistent
	result.DurationMS = durationMilliseconds(time.Since(started))
	result.Checks = runtimeProbeChecks(suite, model)
	return result
}

func scanRuntimeProbeLines(ctx context.Context, reader io.Reader, output chan<- runtimeProbeLine) {
	defer close(output)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxEventRecordBytes)
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if !json.Valid(line) {
			select {
			case output <- runtimeProbeLine{err: errors.New("invalid Pi RPC event")}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case output <- runtimeProbeLine{raw: append(json.RawMessage(nil), line...)}:
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case output <- runtimeProbeLine{err: err}:
		case <-ctx.Done():
		}
	}
}

func startRuntimeProbeProcess(ctx context.Context, cfg config, model modelOption, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension string, sessionArgs []string, phase string) (runtimeProbeProcess, string) {
	args := []string{"--mode", "rpc"}
	args = append(args, sessionArgs...)
	args = append(args,
		"--no-builtin-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files",
		"--extension", rdkExtension, "--extension", probeExtension, "--model", joinModel(model.Provider, model.ID), "--no-approve",
	)
	command := exec.CommandContext(ctx, cfg.AgentBinary, args...)
	command.Dir = workspace
	command.Env = append(runtimeProbeEnvironment(os.Environ()), runtimeProbeProcessEnvironment(model, temporaryRoot, agentDir, stateRoot, home, temporaryDirectory, phase)...)
	closeCredential, err := attachGatewayCredential(command, gatewayCredentialPayload(cfg))
	if err != nil {
		return runtimeProbeProcess{}, "configuration"
	}
	command.SysProcAttr = workerSysProcAttr()
	stdin, err := command.StdinPipe()
	if err != nil {
		closeCredential()
		return runtimeProbeProcess{}, "process"
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		closeCredential()
		_ = stdin.Close()
		return runtimeProbeProcess{}, "process"
	}
	var stderr bytes.Buffer
	command.Stderr = &boundedLogWriter{writer: &stderr, remaining: 64 * 1024}
	if err := command.Start(); err != nil {
		closeCredential()
		_ = stdin.Close()
		_ = stdout.Close()
		return runtimeProbeProcess{}, "process"
	}
	closeCredential()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	lines := make(chan runtimeProbeLine, 32)
	go scanRuntimeProbeLines(ctx, stdout, lines)
	return runtimeProbeProcess{command: command, stdin: stdin, lines: lines, wait: wait}, ""
}

func runtimeProbeProcessEnvironment(model modelOption, temporaryRoot, agentDir, stateRoot, home, temporaryDirectory, phase string) []string {
	runtimeProbe, rdkProbe := "1", "0"
	if phase == "rdk" {
		runtimeProbe, rdkProbe = "0", "1"
	}
	return []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + home,
		"TMPDIR=" + temporaryDirectory,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"HOBOT_CODE_RUNTIME_PROBE=" + runtimeProbe,
		"HOBOT_CODE_RDK_PROBE=" + rdkProbe,
		"HOBOT_CODE_RUNTIME_PROBE_PHASE=" + phase,
		"ANTHROPIC_MODEL=" + model.ID,
		"HOBOT_CODE_CONFIG_DIR=" + temporaryRoot,
		"HOBOT_CODING_AGENT_DIR=" + agentDir,
		"HOBOT_CODE_STATE_DIR=" + stateRoot,
		"HOBOT_CODING_AGENT_SESSION_DIR=" + filepath.Join(stateRoot, "sessions"),
		"HOBOT_CODE_PERMISSION_POLICY=" + filepath.Join(agentDir, "permissions.json"),
		"HOBOT_CODE_MEMORY_CONFIG=" + filepath.Join(agentDir, "memory.json"),
		"HOBOT_CODE_MEMORY_DB=" + filepath.Join(stateRoot, "memory.db"),
		"HOBOT_CODE_GOAL_CONFIG=" + filepath.Join(agentDir, "goals.json"),
		"HOBOT_CODE_GOAL_DB=" + filepath.Join(stateRoot, "goals.db"),
		"HOBOT_CODE_HOOK_CONFIG=" + filepath.Join(agentDir, "hooks.json"),
		"HOBOT_CODE_HOOK_AUDIT=" + filepath.Join(stateRoot, "hooks.jsonl"),
		"HOBOT_CODE_NOTIFICATION_CONFIG=" + filepath.Join(agentDir, "notifications.json"),
		"HOBOT_CODE_LSP_CONFIG=" + filepath.Join(agentDir, "lsp.json"),
		"HOBOT_CODE_SANDBOX_MODE=review",
		"HOBOT_CODE_SANDBOX_BACKEND=runtime-probe",
	}
}

func writeRuntimeProbeRPC(stdin io.Writer, command map[string]any) bool {
	encoded, err := json.Marshal(command)
	if err != nil {
		return false
	}
	_, err = stdin.Write(append(encoded, '\n'))
	return err == nil
}

func nextRuntimeProbeEvent(ctx context.Context, lines <-chan runtimeProbeLine) (json.RawMessage, string) {
	select {
	case <-ctx.Done():
		return nil, "timeout"
	case event, ok := <-lines:
		if !ok || event.err != nil {
			if ctx.Err() != nil {
				return nil, "timeout"
			}
			return nil, "protocol"
		}
		return event.raw, ""
	}
}

func probePersistentRuntime(ctx context.Context, cfg config, model modelOption, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension string) (runtimeProbePersistentObservation, string) {
	observation := runtimeProbePersistentObservation{}
	sessionRoot := filepath.Join(stateRoot, "sessions")
	process, category := startRuntimeProbeProcess(ctx, cfg, model, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension, []string{"--session-dir", sessionRoot, "--name", "Hobot runtime recovery probe"}, "persistent")
	if category != "" {
		return observation, category
	}
	stopped := false
	defer func() {
		if !stopped {
			cancelRuntimeProbe(process.command, process.stdin, process.wait)
		}
	}()

	if !writeRuntimeProbeRPC(process.stdin, map[string]any{"id": "persistent-state", "type": "get_state"}) {
		return observation, "protocol"
	}
	for observation.sessionFile == "" {
		raw, category := nextRuntimeProbeEvent(ctx, process.lines)
		if category != "" {
			return observation, category
		}
		state, ok := runtimeProbeStateResponse(raw, "persistent-state")
		if !ok {
			continue
		}
		observation.sessionFile, observation.sessionID, observation.sessionModel = state.SessionFile, state.SessionID, state.Model
	}
	if validated, err := validateRuntimeProbeSessionFile(sessionRoot, observation.sessionFile, false); err == nil {
		observation.sessionFile = validated
		observation.sessionValidated = true
	} else {
		return observation, "protocol"
	}

	stages := []struct {
		id          string
		instruction string
		result      string
		observation *runtimeProbeObservation
	}{
		{
			id:          "persistent-seed",
			instruction: "Remember the opaque session token " + runtimeProbeCompactionToken + ". Treat the following as bounded older context: " + strings.Repeat("historical-board-note ", 800) + "Do not call a tool. Reply exactly " + runtimeProbeMemoryStored + ".",
			result:      runtimeProbeMemoryStored,
			observation: &observation.seed,
		},
		{
			id:          "persistent-filler",
			instruction: "Store this unrelated padding as the newest turn and do not repeat any earlier token: " + strings.Repeat("bounded-context-padding ", 20) + "Reply exactly " + runtimeProbeFillerText + ".",
			result:      runtimeProbeFillerText,
			observation: &observation.filler,
		},
	}
	for _, stage := range stages {
		stage.observation.promptID = stage.id
		if !writeRuntimeProbeRPC(process.stdin, map[string]any{"id": stage.id, "type": "prompt", "message": stage.instruction}) {
			return observation, "protocol"
		}
		for !stage.observation.settled {
			raw, category := nextRuntimeProbeEvent(ctx, process.lines)
			if category != "" {
				return observation, category
			}
			observeRuntimeProbeEvent(stage.observation, raw)
		}
		if !runtimeProbePlainTextPassed(*stage.observation, stage.result) {
			return observation, "protocol"
		}
	}

	if !writeRuntimeProbeRPC(process.stdin, map[string]any{
		"id": "persistent-compact", "type": "compact",
		"customInstructions": "Preserve exact opaque identifiers and user constraints. Do not continue the conversation.",
	}) {
		return observation, "protocol"
	}
	for !observation.compaction.responseSuccess || observation.compaction.endIndex == 0 {
		raw, category := nextRuntimeProbeEvent(ctx, process.lines)
		if category != "" {
			return observation, category
		}
		observeRuntimeProbeCompaction(&observation.compaction, raw)
		if observation.compaction.invalid {
			return observation, "protocol"
		}
	}

	observation.postCompaction.promptID = "persistent-after-compact"
	if !writeRuntimeProbeRPC(process.stdin, map[string]any{
		"id": observation.postCompaction.promptID, "type": "prompt",
		"message": "Without calling a tool, reply with only the exact opaque session token I asked you to remember before the padding turn.",
	}) {
		return observation, "protocol"
	}
	for !observation.postCompaction.settled {
		raw, category := nextRuntimeProbeEvent(ctx, process.lines)
		if category != "" {
			return observation, category
		}
		observeRuntimeProbeEvent(&observation.postCompaction, raw)
	}
	if !runtimeProbePlainTextPassed(observation.postCompaction, runtimeProbeCompactionToken) {
		return observation, "protocol"
	}

	observation.interrupt.promptID = "persistent-interrupt"
	if !writeRuntimeProbeRPC(process.stdin, map[string]any{
		"id": observation.interrupt.promptID, "type": "prompt",
		"message": "Remember the opaque recovery token " + runtimeProbeRecoveryToken + ", then call hobot_runtime_probe exactly once with stage interrupt and nonce wait-for-termination. Do not produce final text before the tool completes.",
	}) {
		return observation, "protocol"
	}
	for len(observation.interrupt.toolStarts) == 0 {
		raw, category := nextRuntimeProbeEvent(ctx, process.lines)
		if category != "" {
			return observation, category
		}
		observeRuntimeProbeEvent(&observation.interrupt, raw)
		if len(observation.interrupt.toolEnds) != 0 || observation.interrupt.settled || observation.interrupt.extensionErr {
			return observation, "protocol"
		}
	}
	if !runtimeProbeInterruptStarted(observation.interrupt) {
		return observation, "protocol"
	}
	if err := validateInterruptedRuntimeProbeSession(observation.sessionFile, observation.interrupt.toolStarts[0].CallID); err != nil {
		return observation, "protocol"
	}
	observation.interruptedStateDurable = true
	if process.command.Process == nil || terminateProcessGroup(process.command.Process.Pid, syscall.SIGKILL) != nil {
		return observation, "process"
	}
	_ = process.stdin.Close()
	select {
	case <-process.wait:
		observation.processKilled = true
		stopped = true
	case <-ctx.Done():
		return observation, "timeout"
	case <-time.After(3 * time.Second):
		return observation, "process"
	}

	validatedSession, err := validateRuntimeProbeSessionFile(sessionRoot, observation.sessionFile, true)
	if err != nil {
		return observation, "protocol"
	}
	resumed, category := startRuntimeProbeProcess(ctx, cfg, model, temporaryRoot, agentDir, stateRoot, workspace, home, temporaryDirectory, rdkExtension, probeExtension, []string{"--session-dir", sessionRoot, "--session", validatedSession}, "resume")
	if category != "" {
		return observation, category
	}
	defer cancelRuntimeProbe(resumed.command, resumed.stdin, resumed.wait)
	if !writeRuntimeProbeRPC(resumed.stdin, map[string]any{"id": "resumed-state", "type": "get_state"}) {
		return observation, "protocol"
	}
	for !observation.resumeStateReceived {
		raw, category := nextRuntimeProbeEvent(ctx, resumed.lines)
		if category != "" {
			return observation, category
		}
		state, ok := runtimeProbeStateResponse(raw, "resumed-state")
		if !ok {
			continue
		}
		observation.resumeStateReceived = true
		observation.resumedSessionFile, observation.resumedSessionID, observation.resumedSessionModel = state.SessionFile, state.SessionID, state.Model
	}
	observation.recovered.promptID = "resumed-prompt"
	if !writeRuntimeProbeRPC(resumed.stdin, map[string]any{
		"id": observation.recovered.promptID, "type": "prompt",
		"message": "The previous tool was interrupted. Do not retry or call any tool. Reply with only the exact opaque recovery token from the interrupted user turn.",
	}) {
		return observation, "protocol"
	}
	for !observation.recovered.settled {
		raw, category := nextRuntimeProbeEvent(ctx, resumed.lines)
		if category != "" {
			return observation, category
		}
		before := len(observation.recovered.toolStarts)
		observeRuntimeProbeEvent(&observation.recovered, raw)
		if len(observation.recovered.toolStarts) > before {
			observation.replayedTool = true
			return observation, "protocol"
		}
	}
	return observation, ""
}

type runtimeProbeState struct {
	SessionFile string
	SessionID   string
	Model       string
}

func runtimeProbeStateResponse(raw json.RawMessage, id string) (runtimeProbeState, bool) {
	var response struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Data    struct {
			SessionFile string `json:"sessionFile"`
			SessionID   string `json:"sessionId"`
			Model       *struct {
				Provider string `json:"provider"`
				ID       string `json:"id"`
			} `json:"model"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || response.ID != id || response.Type != "response" || response.Command != "get_state" || !response.Success || response.Data.Model == nil {
		return runtimeProbeState{}, false
	}
	return runtimeProbeState{
		SessionFile: response.Data.SessionFile,
		SessionID:   response.Data.SessionID,
		Model:       joinModel(response.Data.Model.Provider, response.Data.Model.ID),
	}, true
}

func validateRuntimeProbeSessionFile(sessionRoot, value string, requireContent bool) (string, error) {
	if !filepath.IsAbs(value) || strings.TrimSpace(value) == "" {
		return "", errors.New("runtime probe session path is unavailable")
	}
	root, err := filepath.EvalSymlinks(sessionRoot)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(value))
	if err != nil || !runtimeProbePathWithin(root, parent) {
		return "", errors.New("runtime probe session path escaped its private root")
	}
	if !requireContent {
		if info, err := os.Lstat(value); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("runtime probe session is a symbolic link")
		}
		return filepath.Join(parent, filepath.Base(value)), nil
	}
	physical, err := filepath.EvalSymlinks(value)
	if err != nil || !runtimeProbePathWithin(root, physical) {
		return "", errors.New("runtime probe session escaped its private root")
	}
	if _, err := privateRegularFileInfo(physical, maxRequestBytes*32); err != nil {
		return "", err
	}
	return physical, nil
}

func runtimeProbePathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateInterruptedRuntimeProbeSession(path, toolCallID string) error {
	content, err := readPrivateRegularFile(path, maxRequestBytes*32)
	if err != nil {
		return err
	}
	if strings.TrimSpace(toolCallID) == "" || !bytes.Contains(content, []byte(toolCallID)) || !bytes.Contains(content, []byte(runtimeProbeRecoveryToken)) || !bytes.Contains(content, []byte(`"stage":"interrupt"`)) {
		return errors.New("interrupted runtime state was not durably recorded")
	}
	return nil
}

func observeRuntimeProbeCompaction(observation *runtimeProbeCompactionObservation, raw json.RawMessage) {
	var event struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		Command   string `json:"command"`
		Success   bool   `json:"success"`
		Reason    string `json:"reason"`
		Aborted   bool   `json:"aborted"`
		WillRetry bool   `json:"willRetry"`
		Data      *struct {
			Summary              string `json:"summary"`
			FirstKeptEntryID     string `json:"firstKeptEntryId"`
			TokensBefore         int64  `json:"tokensBefore"`
			EstimatedTokensAfter int64  `json:"estimatedTokensAfter"`
		} `json:"data"`
		Result *struct {
			Summary              string `json:"summary"`
			FirstKeptEntryID     string `json:"firstKeptEntryId"`
			TokensBefore         int64  `json:"tokensBefore"`
			EstimatedTokensAfter int64  `json:"estimatedTokensAfter"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &event) != nil {
		observation.invalid = true
		return
	}
	observation.eventIndex++
	switch event.Type {
	case "compaction_start":
		if observation.startIndex != 0 || event.Reason != "manual" {
			observation.invalid = true
		}
		observation.startIndex, observation.startReason = observation.eventIndex, event.Reason
	case "compaction_end":
		if observation.startIndex == 0 || observation.endIndex != 0 || event.Reason != "manual" || event.Result == nil {
			observation.invalid = true
		}
		observation.endIndex, observation.endReason = observation.eventIndex, event.Reason
		observation.aborted, observation.willRetry = event.Aborted, event.WillRetry
		if event.Result != nil {
			observation.resultPresent = true
			observation.summaryPresent = strings.TrimSpace(event.Result.Summary) != ""
			observation.firstKeptEntryID = event.Result.FirstKeptEntryID
			observation.tokensBefore = event.Result.TokensBefore
			observation.estimatedTokensAfter = event.Result.EstimatedTokensAfter
		}
	case "response":
		if event.ID == "persistent-compact" && event.Command == "compact" {
			if observation.responseIndex != 0 || !event.Success || event.Data == nil {
				observation.invalid = true
			}
			observation.responseIndex, observation.responseSuccess = observation.eventIndex, event.Success
		}
	}
}

func runtimeProbePlainTextPassed(observation runtimeProbeObservation, expected string) bool {
	return observation.promptAccepted && observation.agentStarted && observation.settled && !observation.extensionErr &&
		len(observation.toolStarts) == 0 && len(observation.toolEnds) == 0 &&
		strings.TrimSpace(observation.lastAssistantText) == expected && observation.lastAssistantIndex > 0
}

func runtimeProbeInterruptStarted(observation runtimeProbeObservation) bool {
	if !observation.promptAccepted || !observation.agentStarted || observation.settled || observation.extensionErr ||
		len(observation.toolStarts) != 1 || len(observation.toolEnds) != 0 {
		return false
	}
	start := observation.toolStarts[0]
	return start.CallID != "" && start.Stage == "interrupt" && start.Nonce == "wait-for-termination" && start.Args == 2
}

func runtimeProbeCompactionPassed(observation runtimeProbePersistentObservation) bool {
	compact := observation.compaction
	return observation.sessionValidated && runtimeProbePlainTextPassed(observation.seed, runtimeProbeMemoryStored) &&
		runtimeProbePlainTextPassed(observation.filler, runtimeProbeFillerText) &&
		compact.startIndex > 0 && compact.endIndex > compact.startIndex && compact.responseIndex > compact.endIndex &&
		compact.startReason == "manual" && compact.endReason == "manual" && compact.responseSuccess && compact.resultPresent &&
		!compact.aborted && !compact.willRetry && !compact.invalid && compact.summaryPresent && compact.firstKeptEntryID != "" &&
		compact.tokensBefore > 0 && compact.estimatedTokensAfter > 0 && compact.estimatedTokensAfter < compact.tokensBefore &&
		runtimeProbePlainTextPassed(observation.postCompaction, runtimeProbeCompactionToken)
}

func runtimeProbeInterruptedRecoveryPassed(observation runtimeProbePersistentObservation, model modelOption) bool {
	return observation.sessionValidated && observation.interruptedStateDurable && observation.processKilled && observation.resumeStateReceived &&
		observation.sessionFile != "" && observation.sessionFile == observation.resumedSessionFile && observation.sessionID != "" &&
		observation.sessionID == observation.resumedSessionID && observation.sessionModel == joinModel(model.Provider, model.ID) &&
		observation.resumedSessionModel == observation.sessionModel && runtimeProbeInterruptStarted(observation.interrupt) &&
		!observation.replayedTool && runtimeProbePlainTextPassed(observation.recovered, runtimeProbeRecoveryToken)
}

func observeRuntimeProbeEvent(observation *runtimeProbeObservation, raw json.RawMessage) bool {
	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Data    struct {
			ThinkingLevel string `json:"thinkingLevel"`
			Model         *struct {
				Provider string `json:"provider"`
				ID       string `json:"id"`
			} `json:"model"`
		} `json:"data"`
		ToolName   string         `json:"toolName"`
		ToolCallID string         `json:"toolCallId"`
		Args       map[string]any `json:"args"`
		IsError    bool           `json:"isError"`
		Result     *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Message               json.RawMessage `json:"message"`
		AssistantMessageEvent *struct {
			Type         string `json:"type"`
			ContentIndex int    `json:"contentIndex"`
			Delta        string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false
	}
	observation.eventIndex++
	switch event.Type {
	case "response":
		if event.Command == "prompt" && event.ID == observation.promptID && event.Success {
			observation.promptAccepted = true
		}
		if event.Command == "set_thinking_level" && event.ID == "runtime-thinking-level" && event.Success {
			observation.thinkingLevelSet = true
		} else if event.Command == "set_thinking_level" && event.ID == "runtime-thinking-level" {
			observation.invalidThinking = true
		}
		if event.Command == "get_state" && event.ID == "runtime-thinking-state" && event.Success && event.Data.ThinkingLevel == "high" {
			observation.thinkingLevelState = true
		} else if event.Command == "get_state" && event.ID == "runtime-thinking-state" {
			observation.invalidThinking = true
		}
	case "agent_start":
		observation.agentStarted = true
	case "tool_execution_start":
		if event.ToolName == runtimeProbeToolName {
			stage, _ := event.Args["stage"].(string)
			nonce, _ := event.Args["nonce"].(string)
			observation.toolStarts = append(observation.toolStarts, runtimeProbeToolEvent{
				CallID: event.ToolCallID, Stage: stage, Nonce: nonce, Index: observation.eventIndex, Args: len(event.Args),
			})
		}
	case "tool_execution_end":
		if event.ToolName == runtimeProbeToolName {
			resultText := ""
			if event.Result != nil && len(event.Result.Content) == 1 && event.Result.Content[0].Type == "text" {
				resultText = strings.TrimSpace(event.Result.Content[0].Text)
			}
			observation.toolEnds = append(observation.toolEnds, runtimeProbeToolEvent{
				CallID: event.ToolCallID, Result: resultText, Error: event.IsError, Index: observation.eventIndex,
			})
		}
	case "message_end":
		var message struct {
			Role       string `json:"role"`
			StopReason string `json:"stopReason"`
			Content    []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"content"`
		}
		if json.Unmarshal(event.Message, &message) == nil && message.Role == "assistant" && message.StopReason != "error" && message.StopReason != "aborted" {
			var text strings.Builder
			for _, content := range message.Content {
				if observation.finalShape != "" {
					observation.finalShape += ","
				}
				observation.finalShape += content.Type
				switch content.Type {
				case "text":
					text.WriteString(content.Text)
					observation.finalText++
				case "thinking":
					observation.finalThinking++
					if strings.TrimSpace(content.Thinking) == "" {
						observation.invalidThinking = true
					}
				default:
					observation.finalOther++
				}
			}
			observation.lastAssistantText = text.String()
			observation.lastAssistantIndex = observation.eventIndex
		}
	case "message_update":
		if event.AssistantMessageEvent == nil {
			break
		}
		update := event.AssistantMessageEvent
		switch update.Type {
		case "thinking_start":
			observation.thinkingStarts++
			if observation.thinkingStarts != 1 || observation.thinkingDeltas != 0 || observation.thinkingEnds != 0 {
				observation.invalidThinking = true
			}
			observation.thinkingBlock = update.ContentIndex
			observation.thinkingStartIndex = observation.eventIndex
		case "thinking_delta":
			observation.thinkingDeltas++
			if observation.thinkingStarts != 1 || observation.thinkingEnds != 0 || update.ContentIndex != observation.thinkingBlock || strings.TrimSpace(update.Delta) == "" {
				observation.invalidThinking = true
			}
		case "thinking_end":
			observation.thinkingEnds++
			if observation.thinkingStarts != 1 || observation.thinkingDeltas == 0 || observation.thinkingEnds != 1 || update.ContentIndex != observation.thinkingBlock {
				observation.invalidThinking = true
			}
			observation.thinkingEndIndex = observation.eventIndex
		case "text_start":
			observation.textStarts++
			if observation.textStarts != 1 || observation.thinkingEnds != 1 || update.ContentIndex == observation.thinkingBlock {
				observation.invalidThinking = true
			}
			observation.textBlock = update.ContentIndex
			if observation.firstTextIndex == 0 {
				observation.firstTextIndex = observation.eventIndex
			}
		}
	case "extension_error":
		observation.extensionErr = true
	case "agent_settled":
		observation.settled = true
		return true
	}
	return false
}

func runtimeProbeModelSelection(raw json.RawMessage) string {
	var event struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Data    struct {
			Model *struct {
				Provider string `json:"provider"`
				ID       string `json:"id"`
			} `json:"model"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Type != "response" || event.Command != "get_state" || !event.Success || event.Data.Model == nil {
		return ""
	}
	return joinModel(event.Data.Model.Provider, event.Data.Model.ID)
}

func runtimeProbeChecks(suite runtimeProbeSuiteObservation, model modelOption) []modelRuntimeProbeCheck {
	basicExpected := []runtimeProbeToolEvent{{Stage: "basic", Nonce: runtimeProbeNonce, Result: "HOBOT_RUNTIME_PROBE_OK"}}
	basicCall := runtimeProbeExactStarts(suite.basic, basicExpected)
	basicResult := runtimeProbeExactCalls(suite.basic, basicExpected)
	values := map[string]bool{
		"rpc-lifecycle":                suite.basic.promptAccepted && suite.basic.agentStarted && !suite.basic.extensionErr,
		"model-selection":              suite.model == joinModel(model.Provider, model.ID),
		"tool-call":                    basicCall,
		"tool-result":                  basicResult,
		"continuation":                 runtimeProbeContinuationAfterCalls(suite.basic, runtimeProbeFinalText),
		"settled":                      suite.basic.settled && suite.parallel.settled && suite.recovery.settled && suite.thinking.settled && suite.approval.settled && suite.image.settled,
		"parallel-tools":               runtimeProbeParallelPassed(suite.parallel),
		"invalid-argument-recovery":    runtimeProbeRecoveryPassed(suite.recovery),
		"thinking-stream":              runtimeProbeThinkingPassed(suite.thinking),
		"approval-flow":                runtimeProbeApprovalPassed(suite.approval),
		"image-input":                  runtimeProbeImagePassed(suite.image),
		"context-compaction":           runtimeProbeCompactionPassed(suite.persist),
		"interrupted-session-recovery": runtimeProbeInterruptedRecoveryPassed(suite.persist, model),
	}
	checks := make([]modelRuntimeProbeCheck, 0, len(runtimeProbeRequiredChecks))
	for _, name := range runtimeProbeRequiredChecks {
		status := "failed"
		message := runtimeProbeCheckMessage(name, values[name])
		if name == "context-compaction" && !values[name] {
			message = runtimeProbeCompactionFailureMessage(suite.persist)
		}
		if name == "thinking-stream" && !model.Capabilities.Reasoning {
			status = "not-applicable"
			message = "The selected model does not declare reasoning; the Pi structured-thinking stage was not run."
		} else if name == "image-input" && !model.Capabilities.ImageInput {
			status = "not-applicable"
			message = "The selected model does not declare image input; the Pi image stage was not run."
		} else if values[name] {
			status = "passed"
		}
		checks = append(checks, modelRuntimeProbeCheck{Name: name, Status: status, Message: message})
	}
	return checks
}

func runtimeProbeCompactionFailureMessage(observation runtimeProbePersistentObservation) string {
	compact := observation.compaction
	failed := make([]string, 0, 12)
	conditions := []struct {
		name string
		ok   bool
	}{
		{"session", observation.sessionValidated},
		{"seed", runtimeProbePlainTextPassed(observation.seed, runtimeProbeMemoryStored)},
		{"filler", runtimeProbePlainTextPassed(observation.filler, runtimeProbeFillerText)},
		{"event-order", compact.startIndex > 0 && compact.endIndex > compact.startIndex && compact.responseIndex > compact.endIndex},
		{"manual-reason", compact.startReason == "manual" && compact.endReason == "manual"},
		{"response", compact.responseSuccess && compact.resultPresent},
		{"completion", !compact.aborted && !compact.willRetry && !compact.invalid},
		{"summary", compact.summaryPresent},
		{"retained-entry", compact.firstKeptEntryID != ""},
		{"token-accounting", compact.tokensBefore > 0 && compact.estimatedTokensAfter > 0},
		{fmt.Sprintf("token-reduction(before=%d,after=%d)", compact.tokensBefore, compact.estimatedTokensAfter), compact.estimatedTokensAfter > 0 && compact.estimatedTokensAfter < compact.tokensBefore},
		{"semantic-recall", runtimeProbePlainTextPassed(observation.postCompaction, runtimeProbeCompactionToken)},
	}
	for _, condition := range conditions {
		if !condition.ok {
			failed = append(failed, condition.name)
		}
	}
	if len(failed) == 0 {
		failed = append(failed, "unknown")
	}
	return "Pi context compaction failed bounded checks: " + strings.Join(failed, ", ") + "."
}

func runtimeProbeExactStarts(observation runtimeProbeObservation, expected []runtimeProbeToolEvent) bool {
	if len(observation.toolStarts) != len(expected) {
		return false
	}
	matched := make(map[string]bool, len(expected))
	callIDs := make(map[string]bool, len(expected))
	for _, start := range observation.toolStarts {
		if strings.TrimSpace(start.CallID) == "" || start.Args != 2 || callIDs[start.CallID] {
			return false
		}
		callIDs[start.CallID] = true
		valid := false
		for _, want := range expected {
			key := want.Stage + "\x00" + want.Nonce
			if !matched[key] && start.Stage == want.Stage && start.Nonce == want.Nonce {
				matched[key] = true
				valid = true
				break
			}
		}
		if !valid {
			return false
		}
	}
	return len(matched) == len(expected)
}

func runtimeProbeExactCalls(observation runtimeProbeObservation, expected []runtimeProbeToolEvent) bool {
	if !runtimeProbeExactStarts(observation, expected) || len(observation.toolEnds) != len(expected) {
		return false
	}
	starts := make(map[string]runtimeProbeToolEvent, len(expected))
	for _, start := range observation.toolStarts {
		starts[start.CallID] = start
	}
	matched := make(map[string]bool, len(expected))
	matchedExpected := make(map[string]bool, len(expected))
	for _, end := range observation.toolEnds {
		start, ok := starts[end.CallID]
		if !ok || matched[end.CallID] || end.Error || end.Index <= start.Index {
			return false
		}
		valid := false
		for _, want := range expected {
			key := want.Stage + "\x00" + want.Nonce
			if !matchedExpected[key] && start.Stage == want.Stage && start.Nonce == want.Nonce && end.Result == want.Result {
				valid = true
				matchedExpected[key] = true
				break
			}
		}
		if !valid {
			return false
		}
		matched[end.CallID] = true
	}
	return len(matched) == len(expected) && len(matchedExpected) == len(expected)
}

func runtimeProbeContinuationAfterCalls(observation runtimeProbeObservation, expected string) bool {
	if strings.TrimSpace(observation.lastAssistantText) != expected || len(observation.toolEnds) == 0 {
		return false
	}
	latestEnd := observation.toolEnds[0].Index
	for _, end := range observation.toolEnds[1:] {
		if end.Index > latestEnd {
			latestEnd = end.Index
		}
	}
	return observation.lastAssistantIndex > latestEnd
}

func runtimeProbeParallelPassed(observation runtimeProbeObservation) bool {
	expected := []runtimeProbeToolEvent{
		{Stage: "parallel", Nonce: "parallel-a", Result: "HOBOT_RUNTIME_PARALLEL_A"},
		{Stage: "parallel", Nonce: "parallel-b", Result: "HOBOT_RUNTIME_PARALLEL_B"},
	}
	if !observation.promptAccepted || !observation.agentStarted || !observation.settled || observation.extensionErr ||
		!runtimeProbeExactCalls(observation, expected) || !runtimeProbeContinuationAfterCalls(observation, runtimeProbeParallelText) {
		return false
	}
	latestStart := observation.toolStarts[0].Index
	for _, start := range observation.toolStarts[1:] {
		if start.Index > latestStart {
			latestStart = start.Index
		}
	}
	earliestEnd := observation.toolEnds[0].Index
	for _, end := range observation.toolEnds[1:] {
		if end.Index < earliestEnd {
			earliestEnd = end.Index
		}
	}
	return latestStart < earliestEnd
}

func runtimeProbeRecoveryPassed(observation runtimeProbeObservation) bool {
	if !observation.promptAccepted || !observation.agentStarted || !observation.settled || observation.extensionErr ||
		len(observation.toolStarts) != 2 || len(observation.toolEnds) != 2 || !runtimeProbeContinuationAfterCalls(observation, runtimeProbeRecoveryText) {
		return false
	}
	firstStart, secondStart := observation.toolStarts[0], observation.toolStarts[1]
	if firstStart.Stage != "recovery" || firstStart.Nonce != "invalid-on-purpose" || secondStart.Stage != "recovery" || secondStart.Nonce != "repaired-after-error" ||
		firstStart.Args != 2 || secondStart.Args != 2 || firstStart.CallID == "" || secondStart.CallID == "" || firstStart.CallID == secondStart.CallID {
		return false
	}
	ends := make(map[string]runtimeProbeToolEvent, 2)
	for _, end := range observation.toolEnds {
		if ends[end.CallID].CallID != "" {
			return false
		}
		ends[end.CallID] = end
	}
	firstEnd, firstOK := ends[firstStart.CallID]
	secondEnd, secondOK := ends[secondStart.CallID]
	return firstOK && secondOK && firstEnd.Index > firstStart.Index && firstEnd.Error &&
		strings.Contains(firstEnd.Result, "HOBOT_RUNTIME_PROBE_EXPECTED_ARGUMENT_ERROR") && firstEnd.Index < secondStart.Index &&
		secondEnd.Index > secondStart.Index && !secondEnd.Error && secondEnd.Result == "HOBOT_RUNTIME_RECOVERY_OK"
}

func runtimeProbeThinkingPassed(observation runtimeProbeObservation) bool {
	return observation.promptAccepted && observation.agentStarted && observation.settled && !observation.extensionErr &&
		observation.thinkingLevelSet && observation.thinkingLevelState && len(observation.toolStarts) == 0 && len(observation.toolEnds) == 0 &&
		!observation.invalidThinking && observation.thinkingStarts == 1 && observation.thinkingDeltas > 0 && observation.thinkingEnds == 1 &&
		observation.thinkingStartIndex > 0 && observation.thinkingStartIndex < observation.thinkingEndIndex &&
		observation.thinkingBlock == 0 && observation.textStarts == 1 && observation.textBlock == 1 && observation.firstTextIndex > observation.thinkingEndIndex &&
		observation.finalThinking == 1 && observation.finalText == 1 &&
		observation.finalOther == 0 && observation.finalShape == "thinking,text" && strings.TrimSpace(observation.lastAssistantText) == runtimeProbeThinkingText &&
		observation.lastAssistantIndex > observation.firstTextIndex
}

type runtimeProbeApproval struct {
	ID      string
	Method  string
	Title   string
	Message string
}

func runtimeProbeApprovalRequest(raw json.RawMessage) (runtimeProbeApproval, bool) {
	var event struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Method  string `json:"method"`
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Type != "extension_ui_request" || event.Method != "confirm" {
		return runtimeProbeApproval{}, false
	}
	return runtimeProbeApproval{ID: event.ID, Method: event.Method, Title: event.Title, Message: event.Message}, true
}

func runtimeProbeApprovalPassed(observation runtimeProbeObservation) bool {
	expected := []runtimeProbeToolEvent{{Stage: "approval", Nonce: "confirm-read-only", Result: "HOBOT_RUNTIME_APPROVAL_OK"}}
	if !observation.promptAccepted || !observation.agentStarted || !observation.settled || observation.extensionErr ||
		observation.invalidApproval || observation.approvalRequests != 1 || !observation.approvalResponded ||
		observation.approvalRequestID == "" || !runtimeProbeExactCalls(observation, expected) ||
		!runtimeProbeContinuationAfterCalls(observation, runtimeProbeApprovalText) {
		return false
	}
	return observation.toolStarts[0].Index < observation.approvalRequestIndex &&
		observation.approvalRequestIndex < observation.toolEnds[0].Index
}

func runtimeProbeImagePassed(observation runtimeProbeObservation) bool {
	return observation.promptAccepted && observation.agentStarted && observation.settled && !observation.extensionErr &&
		len(observation.toolStarts) == 0 && len(observation.toolEnds) == 0 &&
		strings.TrimSpace(observation.lastAssistantText) == runtimeProbeImageText && observation.lastAssistantIndex > 0
}

func failedRuntimeProbeResult(started time.Time, category string) modelRuntimeProbeResult {
	checks := make([]modelRuntimeProbeCheck, 0, len(runtimeProbeRequiredChecks))
	for _, name := range runtimeProbeRequiredChecks {
		checks = append(checks, modelRuntimeProbeCheck{Name: name, Status: "failed", Message: runtimeProbeCheckMessage(name, false)})
	}
	return modelRuntimeProbeResult{Category: category, DurationMS: durationMilliseconds(time.Since(started)), Checks: checks}
}

func cancelRuntimeProbe(command *exec.Cmd, stdin io.Closer, wait <-chan error) {
	_ = stdin.Close()
	if command.Process != nil {
		_ = terminateProcessGroup(command.Process.Pid, syscall.SIGTERM)
	}
	select {
	case <-wait:
	case <-time.After(2 * time.Second):
		if command.Process != nil {
			_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-wait:
		case <-time.After(time.Second):
		}
	}
}

func runtimeProbeExtensionPaths(cfg config) (string, string, error) {
	resolvedCatalog, err := filepath.EvalSymlinks(cfg.ExtensionCatalog)
	if err != nil {
		return "", "", fmt.Errorf("resolve runtime probe product root: %w", err)
	}
	productRoot := filepath.Dir(filepath.Dir(resolvedCatalog))
	paths := []string{
		filepath.Join(filepath.Dir(resolvedCatalog), "rdk", "index.ts"),
		filepath.Join(productRoot, "runtime-probes", "model-runtime.ts"),
	}
	for index, path := range paths {
		if _, err := digestRegularFile(path, 16*1024*1024); err != nil {
			return "", "", fmt.Errorf("runtime probe extension unavailable: %w", err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !pathIsWithinProductRoot(productRoot, resolved) {
			return "", "", fmt.Errorf("runtime probe extension resolves outside the product root")
		}
		paths[index] = resolved
	}
	return paths[0], paths[1], nil
}

func runtimeProbeEnvironment(source []string) []string {
	allowed := map[string]bool{
		"ALL_PROXY": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
		"all_proxy": true, "http_proxy": true, "https_proxy": true, "no_proxy": true,
		"ANTHROPIC_BASE_URL": true, "CURL_CA_BUNDLE": true, "LANG": true, "LANGUAGE": true,
		"LOGNAME": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true,
		"TERM": true, "TZ": true, "USER": true,
		"HOBOT_CODE_MODEL_CONTEXT_WINDOW": true, "HOBOT_CODE_MODEL_MAX_TOKENS": true,
	}
	filtered := make([]string, 0, len(source))
	for _, entry := range safeChildEnvironment(source) {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[name] || strings.HasPrefix(name, "LC_") {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func writeRuntimeProbeConfiguration(agentDir string, model modelOption, configs ...config) error {
	providers := map[string]any{}
	if model.Provider != "drobotics" {
		if len(configs) == 0 {
			return fmt.Errorf("managed provider configuration is unavailable")
		}
		provider, err := selectedManagedProvider(managedProviderConfigPath(configs[0]), model.Provider)
		if err != nil {
			return err
		}
		providers[model.Provider] = provider
	}
	files := map[string]any{
		"settings.json": map[string]any{
			"defaultProvider": model.Provider, "defaultModel": model.ID, "defaultThinkingLevel": "off",
			"extensions": []string{}, "skills": []string{}, "enableSkillCommands": false,
			"retry":      map[string]any{"enabled": false},
			"compaction": map[string]any{"enabled": false, "reserveTokens": 512, "keepRecentTokens": 64},
		},
		"models.json":        map[string]any{"providers": map[string]any{}},
		"providers.json":     map[string]any{"schemaVersion": 1, "providers": sortedManagedProviderValues(providers)},
		"permissions.json":   map[string]any{"schemaVersion": 2, "rootMode": "policy", "default": "deny", "rules": []map[string]string{{"tool": runtimeProbeToolName, "action": "allow"}}},
		"memory.json":        map[string]any{"schemaVersion": 1, "enabled": false, "autoRecall": false, "maxInjected": 0, "maxSearchResults": 1, "maxContentChars": 128, "defaultExpiresDays": nil},
		"goals.json":         map[string]any{"schemaVersion": 1, "enabled": false, "defaultTurnBudget": 1, "defaultTokenBudget": nil},
		"hooks.json":         map[string]any{"schemaVersion": 1, "enabled": false, "failurePolicy": "block", "timeoutMs": 1000, "maxOutputChars": 128, "allowProjectHooks": false, "hooks": []any{}},
		"notifications.json": map[string]any{"schemaVersion": 1, "enabled": false, "allowLocal": false, "bell": false, "protocol": "osc9", "onApproval": false, "onComplete": false, "onFailure": false, "minDurationMs": 60000},
		"lsp.json":           map[string]any{"schemaVersion": 1, "enabled": false, "maxProcesses": 1, "maxMemoryMiB": 64, "idleTimeoutMs": 1000, "requestTimeoutMs": 1000, "diagnosticsWaitMs": 100, "servers": []any{}},
	}
	for name, value := range files {
		content, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(agentDir, name), append(content, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}
