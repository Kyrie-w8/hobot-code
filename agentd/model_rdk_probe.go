package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	rdkProbeSchema         = 1
	rdkProbeProfile        = "read-only-rdk-diagnostic-v1"
	rdkProbeTimeout        = 5 * time.Minute
	rdkProbePromptID       = "rdk-diagnostic"
	rdkProbeKnowledgeQuery = "system version logs temperature memory diagnostics"
)

var rdkProbeRequiredChecks = []string{
	"model-selection",
	"target-identity",
	"live-board-evidence",
	"versioned-knowledge",
	"evidence-synthesis",
	"tool-isolation",
	"settled",
	"resource-stability",
}

var rdkProbeEvidenceChecks = rdkProbeRequiredChecks[:len(rdkProbeRequiredChecks)-1]

var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

type modelRDKProbeParams struct {
	Model   string `json:"model,omitempty"`
	Profile string `json:"profile,omitempty"`
}

type modelRDKProbeCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type modelRDKProbeBinding struct {
	ProductVersion        string `json:"productVersion"`
	BuildStatus           string `json:"buildStatus"`
	Commit                string `json:"commit,omitempty"`
	Dirty                 *bool  `json:"dirty,omitempty"`
	BuildTarget           string `json:"buildTarget,omitempty"`
	AgentdBinarySHA256    string `json:"agentdBinarySha256,omitempty"`
	PiVersion             string `json:"piVersion,omitempty"`
	PiCommit              string `json:"piCommit,omitempty"`
	PiCompatibilitySHA256 string `json:"piCompatibilitySha256,omitempty"`
	ExpertPromptSHA256    string `json:"expertPromptSha256"`
	RDKExtensionSHA256    string `json:"rdkExtensionSha256"`
	KnowledgePackSHA256   string `json:"knowledgePackSha256"`
	KnowledgeVersion      string `json:"knowledgeVersion"`
	KnowledgeUpdatedAt    string `json:"knowledgeUpdatedAt"`
	Board                 string `json:"board"`
	BoardID               string `json:"boardId"`
	RDKOSVersion          string `json:"rdkOsVersion"`
	Architecture          string `json:"architecture"`
}

type modelRDKProbeResult struct {
	SchemaVersion   int                  `json:"schemaVersion"`
	Scope           string               `json:"scope"`
	Profile         string               `json:"profile"`
	Provider        string               `json:"provider"`
	Model           string               `json:"model"`
	Status          string               `json:"status"`
	ReleaseEligible bool                 `json:"releaseEligible"`
	Category        string               `json:"category,omitempty"`
	Message         string               `json:"message"`
	CheckedAt       time.Time            `json:"checkedAt"`
	DurationMS      int64                `json:"durationMs,omitempty"`
	Binding         modelRDKProbeBinding `json:"binding"`
	Sources         []string             `json:"sources,omitempty"`
	Checks          []modelRDKProbeCheck `json:"checks"`
	NotCovered      []string             `json:"notCovered"`
}

type rdkProbeManifest struct {
	SchemaVersion    int    `json:"schemaVersion"`
	KnowledgeVersion string `json:"knowledgeVersion"`
	UpdatedAt        string `json:"updatedAt"`
	Documents        []struct {
		File    string `json:"file"`
		Sources []struct {
			URL string `json:"url"`
		} `json:"sources"`
	} `json:"documents"`
}

type rdkProbeToolCall struct {
	CallID string
	Name   string
	Args   map[string]any
	Index  int
}

type rdkProbeToolResult struct {
	CallID string
	Name   string
	Text   string
	Error  bool
	Index  int
}

type rdkProbeObservation struct {
	eventIndex       int
	model            string
	promptAccepted   bool
	promptAcceptedAt int
	agentStarted     bool
	agentStartedAt   int
	agentStartCount  int
	settled          bool
	settledAt        int
	settledCount     int
	extensionError   bool
	toolStarts       []rdkProbeToolCall
	toolEnds         []rdkProbeToolResult
	lastAssistant    string
	lastAssistantAt  int
	finalTextCount   int
}

type rdkProbeToolSnapshot struct {
	Board        string          `json:"board"`
	BoardID      string          `json:"boardId"`
	RDKOSVersion string          `json:"rdkOsVersion"`
	Architecture string          `json:"architecture"`
	BPUDevices   []string        `json:"bpuDevices"`
	RDKUtilities map[string]bool `json:"rdkUtilities"`
}

type rdkProbeKnowledge struct {
	KnowledgeVersion string `json:"knowledgeVersion"`
	UpdatedAt        string `json:"updatedAt"`
	DetectedBoard    string `json:"detectedBoard"`
	DetectedRDKOS    string `json:"detectedRdkOs"`
	Results          []struct {
		VersionMatch bool `json:"versionMatch"`
		Sources      []struct {
			URL string `json:"url"`
		} `json:"sources"`
	} `json:"results"`
}

type rdkProbeSynthesis struct {
	BoardID          string   `json:"boardId"`
	RDKOSVersion     string   `json:"rdkOsVersion"`
	Architecture     string   `json:"architecture"`
	KnowledgeVersion string   `json:"knowledgeVersion"`
	SourceURL        string   `json:"sourceUrl"`
	EvidenceStatus   string   `json:"evidenceStatus"`
	Signals          []string `json:"signals"`
}

func (manager *taskManager) runModelRDKProbe(params modelRDKProbeParams) (modelRDKProbeResult, error) {
	manager.runtimeProbeMu.Lock()
	if manager.runtimeProbeRunning {
		manager.runtimeProbeMu.Unlock()
		return modelRDKProbeResult{}, fmt.Errorf("a model qualification probe is already running")
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
		return modelRDKProbeResult{}, fmt.Errorf("discover models: %w", err)
	}
	selection := normalizeModelSelection(params.Model)
	if strings.TrimSpace(params.Model) != "" && selection == "" {
		return modelRDKProbeResult{}, fmt.Errorf("model must use provider/model format")
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
		return modelRDKProbeResult{}, fmt.Errorf("model is not available: %s", selection)
	}
	profile, ok := rdkProbeProfileByID(strings.TrimSpace(params.Profile))
	if !ok {
		return modelRDKProbeResult{}, fmt.Errorf("RDK profile is not supported: %s", params.Profile)
	}
	if !profile.Runnable {
		return modelRDKProbeResult{}, fmt.Errorf("RDK profile is not runnable yet: %s", profile.ID)
	}
	live := collectSystemSnapshot(manager.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), rdkProbeTimeout)
	defer cancel()
	result := probeModelOnRDK(ctx, manager.cfg, model, live, currentBuildIdentity(), profile)
	result.Provider = model.Provider
	result.Model = model.ID
	result.CheckedAt = time.Now().UTC()
	return normalizeModelRDKProbeResult(result), nil
}

func probeModelOnRDK(ctx context.Context, cfg config, model modelOption, live systemSnapshot, build buildIdentity, profiles ...rdkProbeProfileDefinition) modelRDKProbeResult {
	started := time.Now()
	profile := defaultRDKProbeProfile()
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	result := modelRDKProbeResult{
		Profile: profile.ID,
		Binding: modelRDKProbeBinding{
			ProductVersion: version, BuildStatus: build.Status, Commit: build.Commit, Dirty: build.Dirty,
			BuildTarget: build.Target, AgentdBinarySHA256: build.BinarySHA256,
			PiVersion: build.PiVersion, PiCommit: build.PiCommit, PiCompatibilitySHA256: build.PiCompatibilitySHA256,
			Board: live.Board, BoardID: live.BoardID, RDKOSVersion: live.RDKOSVersion, Architecture: live.Architecture,
		},
	}
	if !rdkProbeContains([]string{"x5", "s100", "s600"}, live.BoardID) || live.RDKOSVersion == "" || live.RDKOSVersion == "unknown" || live.Architecture != "arm64" {
		return failedModelRDKProbeResult(result, started, "target")
	}
	credentialPayload, credentialErr := selectedModelCredentialPayload(cfg, model)
	if credentialErr != nil || strings.TrimSpace(credentialPayload) == "" {
		return failedModelRDKProbeResult(result, started, "configuration")
	}
	probeConfig := cfg
	probeConfig.gatewayCredential = credentialPayload
	probeConfig.gatewayToken = ""
	rdkExtension, knowledgeRoot, promptPath, manifest, extensionDigest, promptDigest, knowledgeDigest, err := rdkProbeResources(cfg)
	if err != nil {
		return failedModelRDKProbeResult(result, started, "preparation")
	}
	result.Binding.ExpertPromptSHA256 = promptDigest
	result.Binding.RDKExtensionSHA256 = extensionDigest
	result.Binding.KnowledgePackSHA256 = knowledgeDigest
	result.Binding.KnowledgeVersion = manifest.KnowledgeVersion
	result.Binding.KnowledgeUpdatedAt = manifest.UpdatedAt

	temporaryRoot, err := os.MkdirTemp(cfg.AgentdRoot, ".model-rdk-probe-")
	if err != nil {
		return failedModelRDKProbeResult(result, started, "preparation")
	}
	_ = os.Chmod(temporaryRoot, 0o700)
	defer os.RemoveAll(temporaryRoot)
	agentDir := filepath.Join(temporaryRoot, "agent")
	stateRoot := filepath.Join(temporaryRoot, "state")
	workspace := filepath.Join(temporaryRoot, "workspace")
	home := filepath.Join(temporaryRoot, "home")
	temporaryDirectory := filepath.Join(temporaryRoot, "tmp")
	for _, directory := range []string{agentDir, stateRoot, workspace, home, temporaryDirectory, filepath.Join(stateRoot, "sessions")} {
		if os.MkdirAll(directory, 0o700) != nil {
			return failedModelRDKProbeResult(result, started, "preparation")
		}
	}
	if writeRDKProbeConfiguration(agentDir, model, probeConfig) != nil {
		return failedModelRDKProbeResult(result, started, "preparation")
	}
	command := exec.CommandContext(ctx, cfg.AgentBinary,
		"--mode", "rpc", "--no-session", "--no-builtin-tools", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files",
		"--extension", rdkExtension, "--model", joinModel(model.Provider, model.ID), "--no-approve",
	)
	command.Dir = workspace
	command.Env = append(runtimeProbeEnvironment(os.Environ()),
		runtimeProbeProcessEnvironment(model, temporaryRoot, agentDir, stateRoot, home, temporaryDirectory, "rdk")...,
	)
	command.Env = append(command.Env,
		"HOBOT_CODE_RDK_KNOWLEDGE_DIR="+knowledgeRoot,
		"HOBOT_CODE_RDK_EXPERT_PROMPT="+promptPath,
	)
	closeCredential, err := attachGatewayCredential(command, gatewayCredentialPayload(probeConfig))
	if err != nil {
		return failedModelRDKProbeResult(result, started, "configuration")
	}
	command.SysProcAttr = workerSysProcAttr()
	stdin, err := command.StdinPipe()
	if err != nil {
		closeCredential()
		return failedModelRDKProbeResult(result, started, "process")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		closeCredential()
		_ = stdin.Close()
		return failedModelRDKProbeResult(result, started, "process")
	}
	var stderr bytes.Buffer
	command.Stderr = &boundedLogWriter{writer: &stderr, remaining: 64 * 1024}
	if command.Start() != nil {
		closeCredential()
		return failedModelRDKProbeResult(result, started, "process")
	}
	closeCredential()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	lines := make(chan runtimeProbeLine, 32)
	go scanRuntimeProbeLines(ctx, stdout, lines)
	defer cancelRuntimeProbe(command, stdin, wait)

	if !writeRuntimeProbeRPC(stdin, map[string]any{"id": "rdk-state", "type": "get_state"}) {
		return failedModelRDKProbeResult(result, started, "protocol")
	}
	observation := rdkProbeObservation{}
	for observation.model == "" {
		raw, category := nextRuntimeProbeEvent(ctx, lines)
		if category != "" {
			return failedModelRDKProbeResult(result, started, category)
		}
		observeRDKProbeEvent(&observation, raw)
	}
	prompt := "Run the read-only RDK profile " + strconvQuote(profile.ID) + " for " + strconvQuote(profile.Workflow) + ". First call system_snapshot exactly once with includeProcesses false. After that result, call rdk_docs_search exactly once with query " +
		strconvQuote(profile.Query) + ", board set to the exact boardId from the snapshot, topic " + strconvQuote(profile.Topic) + ", and limit 3. Do not call any other tool. Then reply with one JSON object and no markdown using exactly these keys: boardId, rdkOsVersion, architecture, knowledgeVersion, sourceUrl, evidenceStatus, signals. sourceUrl must be copied from a returned knowledge source. evidenceStatus must be complete only when board identity and version-matched knowledge are present, otherwise limited. signals must be ordered as board-identified or board-unidentified; bpu-visible or bpu-not-visible; runtime-tool-visible or runtime-tool-not-visible; docs-version-match or docs-version-mismatch."
	if !writeRuntimeProbeRPC(stdin, map[string]any{"id": rdkProbePromptID, "type": "prompt", "message": prompt}) {
		return failedModelRDKProbeResult(result, started, "protocol")
	}
	for !observation.settled {
		raw, category := nextRuntimeProbeEvent(ctx, lines)
		if category != "" {
			return failedModelRDKProbeResult(result, started, category)
		}
		observeRDKProbeEvent(&observation, raw)
	}
	checks, sources := evaluateRDKProbe(observation, live, model, manifest, profile)
	_, _, _, finalManifest, finalExtensionDigest, finalPromptDigest, finalKnowledgeDigest, finalResourceError := rdkProbeResources(cfg)
	resourcesStable := finalResourceError == nil && extensionDigest == finalExtensionDigest && promptDigest == finalPromptDigest &&
		knowledgeDigest == finalKnowledgeDigest && manifest.KnowledgeVersion == finalManifest.KnowledgeVersion && manifest.UpdatedAt == finalManifest.UpdatedAt
	checks = append(checks, modelRDKProbeCheck{Name: "resource-stability", Status: passedOrFailed(resourcesStable), Message: rdkProbeCheckMessage("resource-stability", resourcesStable)})
	result.Checks = checks
	result.Sources = sources
	result.DurationMS = durationMilliseconds(time.Since(started))
	return result
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func normalizeModelRDKProbeResult(result modelRDKProbeResult) modelRDKProbeResult {
	profile, ok := rdkProbeProfileByID(result.Profile)
	if !ok || !profile.Runnable {
		profile = defaultRDKProbeProfile()
	}
	result.Profile = profile.ID
	result.SchemaVersion = rdkProbeSchema
	result.Scope = "rdk-task-profile"
	result.NotCovered = append([]string(nil), profile.NotCovered...)
	seen := make(map[string]bool, len(result.Checks))
	passed := true
	for index := range result.Checks {
		check := &result.Checks[index]
		if seen[check.Name] || !rdkProbeContains(rdkProbeRequiredChecks, check.Name) || check.Status != "passed" {
			passed = false
		}
		seen[check.Name] = true
	}
	for _, name := range rdkProbeRequiredChecks {
		if !seen[name] {
			result.Checks = append(result.Checks, modelRDKProbeCheck{Name: name, Status: "failed", Message: rdkProbeCheckMessage(name, false)})
			passed = false
		}
	}
	cleanBuild := releaseVersionPattern.MatchString(result.Binding.ProductVersion) && result.Binding.BuildStatus == "verified" && result.Binding.Dirty != nil && !*result.Binding.Dirty &&
		isLowerHex(result.Binding.Commit, 40) && result.Binding.BuildTarget == "linux-arm64" &&
		isLowerHex(result.Binding.AgentdBinarySHA256, 64) && isLowerHex(result.Binding.PiCommit, 40) &&
		result.Binding.PiVersion != "" && isLowerHex(result.Binding.PiCompatibilitySHA256, 64) &&
		isLowerHex(result.Binding.ExpertPromptSHA256, 64) && isLowerHex(result.Binding.RDKExtensionSHA256, 64) && isLowerHex(result.Binding.KnowledgePackSHA256, 64) &&
		result.Binding.KnowledgeVersion != "" && result.Binding.KnowledgeUpdatedAt != "" &&
		rdkProbeContains([]string{"x5", "s100", "s600"}, result.Binding.BoardID) && result.Binding.RDKOSVersion != "" && result.Binding.Architecture == "arm64"
	if passed {
		result.Status = "passed"
		result.Category = ""
		result.ReleaseEligible = cleanBuild
		if cleanBuild {
			result.Message = "The model passed the bound " + profile.Name + " profile on this board and release build. The listed execution outcomes remain unqualified."
		} else {
			result.Message = "The model passed the " + profile.Name + " profile, but this build is not a clean verified release and cannot produce public qualification evidence."
		}
	} else {
		result.Status = "failed"
		result.ReleaseEligible = false
		switch result.Category {
		case "preparation", "configuration", "target", "process", "protocol", "timeout":
		default:
			result.Category = "protocol"
		}
		result.Message = map[string]string{
			"preparation":   "The isolated RDK profile could not be prepared.",
			"configuration": "The model credential or isolated RDK profile configuration was unavailable.",
			"target":        "The current host is not a recognized supported RDK X5, S100, or S600 target.",
			"process":       "The pinned Pi runtime could not start the RDK profile.",
			"protocol":      "The model did not complete the bound " + profile.Name + " profile.",
			"timeout":       "The bound " + profile.Name + " profile timed out.",
		}[result.Category]
	}
	return result
}

func failedModelRDKProbeResult(result modelRDKProbeResult, started time.Time, category string) modelRDKProbeResult {
	result.Category = category
	result.DurationMS = durationMilliseconds(time.Since(started))
	result.Checks = make([]modelRDKProbeCheck, 0, len(rdkProbeRequiredChecks))
	for _, name := range rdkProbeRequiredChecks {
		result.Checks = append(result.Checks, modelRDKProbeCheck{Name: name, Status: "failed", Message: rdkProbeCheckMessage(name, false)})
	}
	return result
}

func rdkProbeResources(cfg config) (string, string, string, rdkProbeManifest, string, string, string, error) {
	resolvedCatalog, err := filepath.EvalSymlinks(cfg.ExtensionCatalog)
	if err != nil {
		return "", "", "", rdkProbeManifest{}, "", "", "", err
	}
	productRoot := filepath.Dir(filepath.Dir(resolvedCatalog))
	rdkExtensionRoot := filepath.Join(filepath.Dir(resolvedCatalog), "rdk")
	rdkExtension := filepath.Join(rdkExtensionRoot, "index.ts")
	knowledgeRoot := filepath.Join(productRoot, "knowledge")
	promptPath := filepath.Join(productRoot, "prompts", "rdk-expert.md")
	manifestPath := filepath.Join(knowledgeRoot, "manifest.json")
	for _, path := range []string{rdkExtension, promptPath, manifestPath} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !pathIsWithinProductRoot(productRoot, resolved) {
			return "", "", "", rdkProbeManifest{}, "", "", "", errors.New("RDK probe resource escaped the product root")
		}
		if _, err := digestRegularFile(resolved, 16*1024*1024); err != nil {
			return "", "", "", rdkProbeManifest{}, "", "", "", err
		}
	}
	promptDigest, err := digestRegularFile(promptPath, 1024*1024)
	if err != nil {
		return "", "", "", rdkProbeManifest{}, "", "", "", err
	}
	extensionDigest, err := digestRDKExtensionBundle(productRoot, rdkExtensionRoot)
	if err != nil {
		return "", "", "", rdkProbeManifest{}, "", "", "", err
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", "", "", rdkProbeManifest{}, "", "", "", err
	}
	var manifest rdkProbeManifest
	if rejectDuplicateJSONKeys(string(content)) != nil || json.Unmarshal(content, &manifest) != nil || manifest.SchemaVersion != 1 || manifest.KnowledgeVersion == "" || manifest.UpdatedAt == "" || len(manifest.Documents) == 0 {
		return "", "", "", rdkProbeManifest{}, "", "", "", errors.New("invalid RDK knowledge manifest")
	}
	knowledgeDigest, err := digestRDKKnowledgePack(productRoot, knowledgeRoot, manifestPath, manifest)
	if err != nil {
		return "", "", "", rdkProbeManifest{}, "", "", "", err
	}
	return rdkExtension, knowledgeRoot, promptPath, manifest, extensionDigest, promptDigest, knowledgeDigest, nil
}

func digestRDKExtensionBundle(productRoot, extensionRoot string) (string, error) {
	physicalRoot, err := filepath.EvalSymlinks(extensionRoot)
	if err != nil || !pathIsWithinProductRoot(productRoot, physicalRoot) {
		return "", errors.New("RDK extension root escaped the product root")
	}
	entries := make([]string, 0, 32)
	totalBytes := int64(0)
	err = filepath.WalkDir(physicalRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("RDK extension bundle contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("RDK extension bundle contains a non-regular file")
		}
		if len(entries) >= 128 {
			return errors.New("RDK extension bundle contains too many files")
		}
		info, err := entry.Info()
		if err != nil || info.Size() <= 0 || totalBytes+info.Size() > 32*1024*1024 {
			return errors.New("RDK extension bundle exceeds its evidence boundary")
		}
		totalBytes += info.Size()
		digest, err := digestRegularFile(path, 4*1024*1024)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(physicalRoot, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("RDK extension file escaped its root")
		}
		entries = append(entries, filepath.ToSlash(relative)+"\x00"+digest)
		return nil
	})
	if err != nil || len(entries) == 0 {
		if err == nil {
			err = errors.New("RDK extension bundle is empty")
		}
		return "", err
	}
	sort.Strings(entries)
	digest := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(digest[:]), nil
}

func digestRDKKnowledgePack(productRoot, knowledgeRoot, manifestPath string, manifest rdkProbeManifest) (string, error) {
	entries := make([]string, 0, len(manifest.Documents)+1)
	manifestDigest, err := digestRegularFile(manifestPath, 4*1024*1024)
	if err != nil {
		return "", err
	}
	entries = append(entries, "manifest.json\x00"+manifestDigest)
	seen := map[string]bool{}
	for _, document := range manifest.Documents {
		if document.File == "" || filepath.IsAbs(document.File) || seen[document.File] {
			return "", errors.New("invalid or duplicate RDK knowledge document")
		}
		seen[document.File] = true
		path := filepath.Join(knowledgeRoot, filepath.Clean(document.File))
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !pathIsWithinProductRoot(productRoot, resolved) || !runtimeProbePathWithin(knowledgeRoot, resolved) {
			return "", errors.New("RDK knowledge document escaped its product root")
		}
		digest, err := digestRegularFile(resolved, 4*1024*1024)
		if err != nil {
			return "", err
		}
		entries = append(entries, document.File+"\x00"+digest)
	}
	sort.Strings(entries)
	digest := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(digest[:]), nil
}

func writeRDKProbeConfiguration(agentDir string, model modelOption, configs ...config) error {
	if err := writeRuntimeProbeConfiguration(agentDir, model, configs...); err != nil {
		return err
	}
	policy := map[string]any{
		"schemaVersion": 2, "rootMode": "policy", "default": "deny",
		"rules": []map[string]string{{"tool": "system_snapshot", "action": "allow"}, {"tool": "rdk_docs_search", "action": "allow"}},
	}
	content, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(agentDir, "permissions.json"), append(content, '\n'), 0o600)
}

func observeRDKProbeEvent(observation *rdkProbeObservation, raw json.RawMessage) {
	var event struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Command    string         `json:"command"`
		Success    bool           `json:"success"`
		ToolName   string         `json:"toolName"`
		ToolCallID string         `json:"toolCallId"`
		Args       map[string]any `json:"args"`
		IsError    bool           `json:"isError"`
		Data       struct {
			Model *struct {
				Provider string `json:"provider"`
				ID       string `json:"id"`
			} `json:"model"`
		} `json:"data"`
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Message json.RawMessage `json:"message"`
	}
	if rejectDuplicateJSONKeys(string(raw)) != nil || json.Unmarshal(raw, &event) != nil {
		observation.extensionError = true
		return
	}
	observation.eventIndex++
	switch event.Type {
	case "response":
		if event.ID == "rdk-state" && event.Command == "get_state" && event.Success && event.Data.Model != nil {
			observation.model = joinModel(event.Data.Model.Provider, event.Data.Model.ID)
		}
		if event.ID == rdkProbePromptID && event.Command == "prompt" && event.Success {
			observation.promptAccepted = true
			observation.promptAcceptedAt = observation.eventIndex
		}
	case "agent_start":
		observation.agentStarted = true
		observation.agentStartedAt = observation.eventIndex
		observation.agentStartCount++
	case "tool_execution_start":
		observation.toolStarts = append(observation.toolStarts, rdkProbeToolCall{CallID: event.ToolCallID, Name: event.ToolName, Args: event.Args, Index: observation.eventIndex})
	case "tool_execution_end":
		text := ""
		if event.Result != nil && len(event.Result.Content) == 1 && event.Result.Content[0].Type == "text" {
			text = event.Result.Content[0].Text
		}
		observation.toolEnds = append(observation.toolEnds, rdkProbeToolResult{CallID: event.ToolCallID, Name: event.ToolName, Text: text, Error: event.IsError, Index: observation.eventIndex})
	case "message_end":
		var message struct {
			Role       string `json:"role"`
			StopReason string `json:"stopReason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(event.Message, &message) == nil && message.Role == "assistant" && message.StopReason != "error" && message.StopReason != "aborted" && len(message.Content) == 1 && message.Content[0].Type == "text" {
			observation.lastAssistant = message.Content[0].Text
			observation.lastAssistantAt = observation.eventIndex
			observation.finalTextCount++
		}
	case "extension_error":
		observation.extensionError = true
	case "agent_settled":
		observation.settled = true
		observation.settledAt = observation.eventIndex
		observation.settledCount++
	}
}

func evaluateRDKProbe(observation rdkProbeObservation, live systemSnapshot, model modelOption, manifest rdkProbeManifest, profiles ...rdkProbeProfileDefinition) ([]modelRDKProbeCheck, []string) {
	profile := defaultRDKProbeProfile()
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	modelOK := observation.model == joinModel(model.Provider, model.ID)
	targetOK := rdkProbeContains([]string{"x5", "s100", "s600"}, live.BoardID) && live.RDKOSVersion != "" && live.RDKOSVersion != "unknown" && live.Architecture == "arm64"
	snapshot, snapshotCall, snapshotOK := rdkProbeSnapshotEvidence(observation, live)
	knowledge, knowledgeCall, sources, knowledgeOK := rdkProbeKnowledgeEvidence(observation, snapshot, manifest, profile)
	synthesisOK := rdkProbeSynthesisPassed(observation, snapshot, knowledge, sources, targetOK && knowledgeOK)
	toolIsolation := snapshotCall && knowledgeCall && len(observation.toolStarts) == 2 && len(observation.toolEnds) == 2
	settled := observation.promptAccepted && observation.agentStarted && observation.settled && !observation.extensionError &&
		observation.agentStartCount == 1 && observation.settledCount == 1 && observation.finalTextCount == 1 &&
		observation.promptAcceptedAt < observation.agentStartedAt && observation.lastAssistantAt < observation.settledAt
	values := map[string]bool{
		"model-selection": modelOK, "target-identity": targetOK, "live-board-evidence": snapshotOK,
		"versioned-knowledge": knowledgeOK, "evidence-synthesis": synthesisOK,
		"tool-isolation": toolIsolation, "settled": settled,
	}
	checks := make([]modelRDKProbeCheck, 0, len(rdkProbeEvidenceChecks))
	for _, name := range rdkProbeEvidenceChecks {
		status := "failed"
		if values[name] {
			status = "passed"
		}
		checks = append(checks, modelRDKProbeCheck{Name: name, Status: status, Message: rdkProbeCheckMessage(name, values[name])})
	}
	return checks, sources
}

func rdkProbeSnapshotEvidence(observation rdkProbeObservation, live systemSnapshot) (rdkProbeToolSnapshot, bool, bool) {
	var zero rdkProbeToolSnapshot
	if len(observation.toolStarts) != 2 || len(observation.toolEnds) != 2 {
		return zero, false, false
	}
	start, end := observation.toolStarts[0], observation.toolEnds[0]
	includeProcesses, ok := start.Args["includeProcesses"].(bool)
	callOK := start.Name == "system_snapshot" && start.CallID != "" && len(start.Args) == 1 && ok && !includeProcesses &&
		observation.agentStartedAt < start.Index && end.Name == start.Name && end.CallID == start.CallID && !end.Error &&
		end.Index > start.Index && observation.toolStarts[1].Index > end.Index
	var snapshot rdkProbeToolSnapshot
	parsed := callOK && decodeSingleJSONAllowUnknown(end.Text, &snapshot) == nil
	evidenceOK := parsed && snapshot.Board == live.Board && snapshot.BoardID == live.BoardID && snapshot.RDKOSVersion == live.RDKOSVersion &&
		snapshot.Architecture == live.Architecture && snapshot.RDKUtilities != nil
	return snapshot, callOK, evidenceOK
}

func rdkProbeKnowledgeEvidence(observation rdkProbeObservation, snapshot rdkProbeToolSnapshot, manifest rdkProbeManifest, profile rdkProbeProfileDefinition) (rdkProbeKnowledge, bool, []string, bool) {
	var zero rdkProbeKnowledge
	if len(observation.toolStarts) != 2 || len(observation.toolEnds) != 2 {
		return zero, false, nil, false
	}
	start, end := observation.toolStarts[1], observation.toolEnds[1]
	query, queryOK := start.Args["query"].(string)
	board, boardOK := start.Args["board"].(string)
	topic, topicOK := start.Args["topic"].(string)
	limit, limitOK := start.Args["limit"].(float64)
	callOK := start.Name == "rdk_docs_search" && start.CallID != "" && len(start.Args) == 4 && queryOK && query == profile.Query &&
		boardOK && board == snapshot.BoardID && topicOK && topic == profile.Topic && limitOK && limit == 3 &&
		end.Name == start.Name && end.CallID == start.CallID && !end.Error && end.Index > start.Index
	var knowledge rdkProbeKnowledge
	if !callOK || decodeSingleJSONAllowUnknown(end.Text, &knowledge) != nil {
		return knowledge, callOK, nil, false
	}
	allowed := make(map[string]bool)
	for _, document := range manifest.Documents {
		for _, source := range document.Sources {
			allowed[source.URL] = true
		}
	}
	sourceSet := make(map[string]bool)
	versionMatch := false
	for _, item := range knowledge.Results {
		if !item.VersionMatch {
			continue
		}
		versionMatch = true
		for _, source := range item.Sources {
			if allowed[source.URL] && officialRDKSource(source.URL) {
				sourceSet[source.URL] = true
			}
		}
	}
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	ok := knowledge.KnowledgeVersion == manifest.KnowledgeVersion && knowledge.UpdatedAt == manifest.UpdatedAt &&
		knowledge.DetectedBoard == snapshot.BoardID && knowledge.DetectedRDKOS == snapshot.RDKOSVersion &&
		len(knowledge.Results) > 0 && versionMatch && len(sources) > 0
	return knowledge, callOK, sources, ok
}

func rdkProbeSynthesisPassed(observation rdkProbeObservation, snapshot rdkProbeToolSnapshot, knowledge rdkProbeKnowledge, sources []string, complete bool) bool {
	var synthesis rdkProbeSynthesis
	if len(observation.toolEnds) != 2 || observation.lastAssistantAt <= observation.toolEnds[1].Index ||
		observation.settledAt <= observation.lastAssistantAt || decodeSingleJSON(observation.lastAssistant, &synthesis) != nil {
		return false
	}
	expectedStatus := "limited"
	if complete {
		expectedStatus = "complete"
	}
	runtimeVisible := snapshot.RDKUtilities["hrt_model_exec"]
	expectedSignals := []string{"board-unidentified", "bpu-not-visible", "runtime-tool-not-visible", "docs-version-mismatch"}
	if rdkProbeContains([]string{"x5", "s100", "s600"}, snapshot.BoardID) {
		expectedSignals[0] = "board-identified"
	}
	if len(snapshot.BPUDevices) > 0 {
		expectedSignals[1] = "bpu-visible"
	}
	if runtimeVisible {
		expectedSignals[2] = "runtime-tool-visible"
	}
	for _, result := range knowledge.Results {
		if result.VersionMatch {
			expectedSignals[3] = "docs-version-match"
			break
		}
	}
	return synthesis.BoardID == snapshot.BoardID && synthesis.RDKOSVersion == snapshot.RDKOSVersion &&
		synthesis.Architecture == snapshot.Architecture && synthesis.KnowledgeVersion == knowledge.KnowledgeVersion &&
		rdkProbeContains(sources, synthesis.SourceURL) && synthesis.EvidenceStatus == expectedStatus && slicesEqual(synthesis.Signals, expectedSignals)
}

func decodeSingleJSON(value string, target any) error {
	if err := rejectDuplicateJSONKeys(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	return decodeSingleJSONValue(decoder, target)
}

func decodeSingleJSONAllowUnknown(value string, target any) error {
	if err := rejectDuplicateJSONKeys(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	return decodeSingleJSONValue(decoder, target)
}

func rejectDuplicateJSONKeys(value string) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("JSON object contains a duplicate or invalid key")
			}
			seen[key] = true
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("JSON object is incomplete")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("JSON array is incomplete")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func decodeSingleJSONValue(decoder *json.Decoder, target any) error {
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func slicesEqual(left, right []string) bool {
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

func rdkProbeContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func passedOrFailed(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}

func officialRDKSource(value string) bool {
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

func rdkProbeCheckMessage(name string, passed bool) string {
	if passed {
		return map[string]string{
			"model-selection":     "Pi selected the exact requested provider and model.",
			"target-identity":     "The host is a recognized ARM64 RDK X5, S100, or S600 with an identified RDK OS.",
			"live-board-evidence": "The model called the read-only board snapshot once and the result matched agentd's live identity.",
			"versioned-knowledge": "The model searched the exact board and RDK OS knowledge track and received a version-matched official source.",
			"evidence-synthesis":  "The final structured diagnosis reproduced live and documented evidence without adding unsupported values.",
			"tool-isolation":      "The model used only the two profile-approved read-only RDK tools in causal order.",
			"settled":             "Pi accepted the prompt, completed the Agent turn, and emitted the settled barrier.",
			"resource-stability":  "The expert prompt, complete RDK extension bundle, and knowledge pack remained unchanged for the full probe.",
		}[name]
	}
	return map[string]string{
		"model-selection":     "Pi did not report the requested provider and model.",
		"target-identity":     "The host did not satisfy the supported RDK board identity boundary.",
		"live-board-evidence": "The live board snapshot was missing, malformed, repeated, or inconsistent with agentd evidence.",
		"versioned-knowledge": "The board-specific knowledge result was missing, mismatched, or lacked an allowed official source.",
		"evidence-synthesis":  "The final structured diagnosis did not faithfully synthesize the tool evidence.",
		"tool-isolation":      "The model called an unapproved, repeated, or causally invalid tool.",
		"settled":             "Pi did not complete the bounded RDK Agent turn with a settled barrier.",
		"resource-stability":  "An RDK probe resource changed or became untrusted while the profile was running.",
	}[name]
}
