package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

type taskStatus string

const (
	statusQueued      taskStatus = "queued"
	statusStarting    taskStatus = "starting"
	statusIdle        taskStatus = "idle"
	statusRunning     taskStatus = "running"
	statusWaiting     taskStatus = "waiting"
	statusStopping    taskStatus = "stopping"
	statusStopped     taskStatus = "stopped"
	statusFailed      taskStatus = "failed"
	statusInterrupted taskStatus = "interrupted"
)

var errTaskLaunchCancelled = errors.New("task launch was cancelled")

var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)
var taskToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,80}$`)

const maximumWorkerStderrBytes int64 = 1024 * 1024

type taskMetadata struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Cwd             string             `json:"cwd"`
	ProjectCwd      string             `json:"projectCwd,omitempty"`
	WorkspaceMode   string             `json:"workspaceMode"`
	WorkspaceID     string             `json:"workspaceId,omitempty"`
	WorktreePath    string             `json:"worktreePath,omitempty"`
	WorktreeBase    string             `json:"worktreeBase,omitempty"`
	Status          taskStatus         `json:"status"`
	PID             int                `json:"pid,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
	LastSequence    uint64             `json:"lastSequence"`
	LogTruncated    bool               `json:"logTruncated,omitempty"`
	LastError       string             `json:"lastError,omitempty"`
	Failure         *taskFailure       `json:"failure,omitempty"`
	SessionFile     string             `json:"sessionFile,omitempty"`
	SessionID       string             `json:"sessionId,omitempty"`
	Approved        bool               `json:"approved,omitempty"`
	ResumeCount     int                `json:"resumeCount,omitempty"`
	RestartCount    int                `json:"restartCount,omitempty"`
	Model           string             `json:"model,omitempty"`
	PermissionMode  string             `json:"permissionMode,omitempty"`
	SandboxMode     string             `json:"sandboxMode"`
	NetworkMode     string             `json:"networkMode"`
	Sandbox         taskSandboxStatus  `json:"sandbox"`
	ParentTaskID    string             `json:"parentTaskId,omitempty"`
	SourceTaskID    string             `json:"sourceTaskId,omitempty"`
	ForkSequence    uint64             `json:"forkSequence,omitempty"`
	BranchKind      string             `json:"branchKind,omitempty"`
	CurrentActivity string             `json:"currentActivity,omitempty"`
	AwaitingPrompt  bool               `json:"awaitingPrompt,omitempty"`
	QueuedAt        *time.Time         `json:"queuedAt,omitempty"`
	QueueOperation  string             `json:"queueOperation,omitempty"`
	ArchivedAt      *time.Time         `json:"archivedAt,omitempty"`
	Approvals       []pendingApproval  `json:"pendingApprovals,omitempty"`
	Deployment      *deploymentRecord  `json:"deployment,omitempty"`
	TurnEvidence    []taskTurnEvidence `json:"turnEvidence,omitempty"`
}

type taskFailure struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Recovery string `json:"recovery"`
}

type task struct {
	manager *taskManager
	dir     string
	events  string
	stderr  string

	mu                    sync.Mutex
	metadata              taskMetadata
	command               *exec.Cmd
	stdin                 io.WriteCloser
	workerDone            chan struct{}
	writeMu               sync.Mutex
	stderrMu              sync.Mutex
	streamWG              sync.WaitGroup
	persistMu             sync.Mutex
	turnCaptureMu         sync.Mutex
	stopping              bool
	interrupted           bool
	eventBytes            int64
	eventLastSequence     uint64
	subscribers           map[uint64]chan taskEvent
	nextSubID             uint64
	pendingSessionFile    string
	pendingSessionID      string
	pendingLaunch         *queuedLaunch
	stderrBytes           int64
	openToolCalls         map[string]struct{}
	openAnonymousTools    int
	terminalCaptureUnsafe bool
}

type taskManager struct {
	cfg                 config
	mu                  sync.RWMutex
	startMu             sync.Mutex
	runtimeProbeMu      sync.Mutex
	runtimeProbeRunning bool
	tasks               map[string]*task
	modelsOnce          sync.Once
	models              map[string]modelOption
	modelListErr        error
}

type queuedLaunch struct {
	Schema      int            `json:"schema"`
	State       string         `json:"state"`
	Operation   string         `json:"operation"`
	Prompt      string         `json:"prompt"`
	Images      []imageContent `json:"images,omitempty"`
	Approve     bool           `json:"approve,omitempty"`
	SessionFile string         `json:"sessionFile,omitempty"`
	QueuedAt    time.Time      `json:"queuedAt"`
}

type startTaskParams struct {
	Name           string            `json:"name,omitempty"`
	Cwd            string            `json:"cwd"`
	Prompt         string            `json:"prompt"`
	Images         []imageContent    `json:"images,omitempty"`
	Approve        bool              `json:"approve,omitempty"`
	Model          string            `json:"model,omitempty"`
	PermissionMode string            `json:"permissionMode,omitempty"`
	WorkspaceMode  string            `json:"workspaceMode,omitempty"`
	SandboxMode    string            `json:"sandboxMode,omitempty"`
	NetworkMode    string            `json:"networkMode,omitempty"`
	Deployment     *deploymentRecord `json:"-"`
}

type forkTaskParams struct {
	TaskID         string         `json:"taskId"`
	Sequence       uint64         `json:"sequence,omitempty"`
	Prompt         string         `json:"prompt"`
	Images         []imageContent `json:"images,omitempty"`
	Name           string         `json:"name,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Model          string         `json:"model,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
	SandboxMode    string         `json:"sandboxMode,omitempty"`
	NetworkMode    string         `json:"networkMode,omitempty"`
}

type resumeTaskParams struct {
	TaskID string         `json:"taskId"`
	Prompt string         `json:"prompt,omitempty"`
	Images []imageContent `json:"images,omitempty"`
}

type imageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	Name     string `json:"name,omitempty"`
}

type renameTaskParams struct {
	TaskID string `json:"taskId"`
	Name   string `json:"name"`
}

type archiveTaskParams struct {
	TaskID  string `json:"taskId"`
	Archive bool   `json:"archive"`
}

type pageTaskParams struct {
	Cursor          string `json:"cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	IncludeArchived bool   `json:"includeArchived,omitempty"`
}

type taskPage struct {
	Tasks      []taskMetadata `json:"tasks"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type commandTaskParams struct {
	TaskID  string          `json:"taskId"`
	Command json.RawMessage `json:"command"`
}

type setTaskModelParams struct {
	TaskID   string `json:"taskId"`
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

type setTaskPermissionParams struct {
	TaskID string `json:"taskId"`
	Mode   string `json:"mode"`
}

type setTaskSandboxParams struct {
	TaskID string `json:"taskId"`
	Mode   string `json:"mode"`
}

type setTaskNetworkParams struct {
	TaskID string `json:"taskId"`
	Mode   string `json:"mode"`
}

type taskIDParams struct {
	TaskID string `json:"taskId"`
}

type subscribeParams struct {
	TaskID string `json:"taskId"`
	After  uint64 `json:"after,omitempty"`
	Follow bool   `json:"follow,omitempty"`
}

type eventPageParams struct {
	TaskID string `json:"taskId"`
	After  uint64 `json:"after,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type eventPage struct {
	Events           []taskEvent `json:"events"`
	NextAfter        uint64      `json:"nextAfter,omitempty"`
	HasMore          bool        `json:"hasMore"`
	RetainedFrom     uint64      `json:"retainedFrom,omitempty"`
	RetainedThrough  uint64      `json:"retainedThrough,omitempty"`
	LatestSequence   uint64      `json:"latestSequence,omitempty"`
	HistoryTruncated bool        `json:"historyTruncated"`
	CursorExpired    bool        `json:"cursorExpired"`
}

type subscriptionResult struct {
	Replayed         int    `json:"replayed"`
	Following        bool   `json:"following"`
	RetainedFrom     uint64 `json:"retainedFrom,omitempty"`
	RetainedThrough  uint64 `json:"retainedThrough,omitempty"`
	LatestSequence   uint64 `json:"latestSequence,omitempty"`
	HistoryTruncated bool   `json:"historyTruncated"`
	CursorExpired    bool   `json:"cursorExpired"`
}

func newTaskManager(cfg config) (*taskManager, error) {
	manager := &taskManager{cfg: cfg, tasks: make(map[string]*task)}
	entries, err := os.ReadDir(cfg.TasksRoot)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !taskIDPattern.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(cfg.TasksRoot, entry.Name())
		content, err := readPrivateRegularFile(filepath.Join(dir, "metadata.json"), maxRequestBytes)
		if err != nil {
			continue
		}
		var metadata taskMetadata
		if json.Unmarshal(content, &metadata) != nil || metadata.ID != entry.Name() {
			continue
		}
		metadata.PermissionMode, _ = normalizePermissionMode(metadata.PermissionMode)
		previousSandboxMode, previousSandbox := metadata.SandboxMode, metadata.Sandbox
		previousNetworkMode := metadata.NetworkMode
		metadata.SandboxMode, metadata.Sandbox = normalizePersistedSandbox(metadata.SandboxMode, metadata.Sandbox, metadata.PermissionMode, metadata.Deployment != nil)
		metadata.NetworkMode, metadata.Sandbox = normalizePersistedNetwork(metadata.NetworkMode, metadata.SandboxMode, metadata.Sandbox)
		legacyMetadataCleared := previousSandboxMode != metadata.SandboxMode || previousSandbox != metadata.Sandbox || previousNetworkMode != metadata.NetworkMode
		failureDetail := ""
		workspaceMode, workspaceErr := normalizeWorkspaceMode(metadata.WorkspaceMode)
		workspaceInvalid := workspaceErr != nil || (workspaceMode == workspaceModeWorktree && metadata.WorktreePath == "")
		if workspaceErr == nil && metadata.WorkspaceMode != workspaceMode {
			metadata.WorkspaceMode = workspaceMode
			legacyMetadataCleared = true
		}
		if metadata.ProjectCwd == "" {
			metadata.ProjectCwd = metadata.Cwd
			legacyMetadataCleared = true
		}
		if workspaceInvalid {
			failureDetail = "persisted task workspace metadata is invalid"
		} else if workspaceMode == workspaceModeWorktree {
			if err := manager.validateManagedTaskWorkspace(metadata); err != nil {
				failureDetail = err.Error()
			}
		}
		if failureDetail != "" {
			metadata.Status = statusFailed
			metadata.PID = 0
			metadata.QueuedAt = nil
			metadata.QueueOperation = ""
			metadata.UpdatedAt = time.Now().UTC()
			metadata.Failure = taskFailureFor("workspace-unavailable", metadata.SessionFile != "")
			metadata.LastError = metadata.Failure.Message
			for index := range metadata.Approvals {
				metadata.Approvals[index].Active = false
			}
			legacyMetadataCleared = true
		}
		if metadata.Model != "" && validPersistedModel(metadata.Model) == "" {
			metadata.Model = ""
			legacyMetadataCleared = true
		}
		if metadata.SessionFile != "" {
			if _, err := validateSessionFile(cfg.SessionDir, metadata.SessionFile); err != nil {
				metadata.SessionFile = ""
				metadata.SessionID = ""
				legacyMetadataCleared = true
			}
		}
		previousEvidence, _ := json.Marshal(metadata.TurnEvidence)
		metadata.TurnEvidence = normalizePersistedTurnEvidence(metadata.TurnEvidence, metadata.SessionFile != "")
		normalizedEvidence, _ := json.Marshal(metadata.TurnEvidence)
		legacyMetadataCleared = legacyMetadataCleared || !bytes.Equal(previousEvidence, normalizedEvidence)
		if metadata.AwaitingPrompt && (metadata.Status != statusStopped || metadata.SessionFile == "") {
			metadata.AwaitingPrompt = false
			legacyMetadataCleared = true
		}
		if isTerminalStatus(metadata.Status) && metadata.Status != statusStopped && (metadata.LastError != "" || metadata.Failure != nil) {
			if metadata.Failure == nil {
				failureDetail = metadata.LastError
			}
			failure := normalizeTaskFailure(metadata.Status, metadata.Failure, metadata.LastError, metadata.SessionFile != "")
			if metadata.Failure == nil || *metadata.Failure != *failure || metadata.LastError != failure.Message {
				metadata.Failure = failure
				metadata.LastError = failure.Message
				legacyMetadataCleared = true
			}
		}
		var pendingLaunch *queuedLaunch
		discardRecoveredQueue := metadata.Status != statusQueued
		if metadata.Status == statusQueued {
			queued, queueErr := readQueuedLaunch(filepath.Join(dir, "queue.json"), cfg.SessionDir)
			if queueErr != nil {
				failureDetail = queueErr.Error()
				metadata.Status = statusFailed
				metadata.QueuedAt = nil
				metadata.QueueOperation = ""
				metadata.Failure = taskFailureFor("queue-recovery-failed", false)
				metadata.LastError = metadata.Failure.Message
				legacyMetadataCleared = true
				discardRecoveredQueue = true
			} else if queued.State == "launching" {
				metadata.Status = statusInterrupted
				metadata.PID = 0
				metadata.QueuedAt = nil
				metadata.QueueOperation = ""
				metadata.Failure = taskFailureFor("handoff-uncertain", metadata.SessionFile != "")
				metadata.LastError = metadata.Failure.Message
				legacyMetadataCleared = true
				discardRecoveredQueue = true
			} else {
				pendingLaunch = &queued
				metadata.QueuedAt = &queued.QueuedAt
				metadata.QueueOperation = queued.Operation
			}
		}
		if occupiesActiveSlot(metadata.Status) {
			metadata.Status = statusInterrupted
			metadata.PID = 0
			metadata.UpdatedAt = time.Now().UTC()
			metadata.Failure = taskFailureFor("service-restarted", metadata.SessionFile != "")
			metadata.LastError = metadata.Failure.Message
			for index := range metadata.Approvals {
				metadata.Approvals[index].Active = false
			}
		}
		current := &task{
			manager:       manager,
			dir:           dir,
			events:        filepath.Join(dir, "events.jsonl"),
			stderr:        filepath.Join(dir, "worker.stderr.log"),
			metadata:      metadata,
			subscribers:   make(map[uint64]chan taskEvent),
			pendingLaunch: pendingLaunch,
		}
		eventBytes, lastSequence, repaired, err := recoverEventLog(current.events, metadata.ID, cfg.MaxEventSize, metadata.LogTruncated)
		if err != nil {
			continue
		}
		current.eventBytes = eventBytes
		current.eventLastSequence = lastSequence
		if info, statErr := os.Stat(current.stderr); statErr == nil && info.Mode().IsRegular() {
			current.stderrBytes = info.Size()
		}
		current.appendFailureDetail(failureDetail)
		sequenceRecovered := false
		if !metadata.LogTruncated || metadata.LastSequence < lastSequence {
			sequenceRecovered = metadata.LastSequence != lastSequence
			metadata.LastSequence = lastSequence
			current.metadata.LastSequence = lastSequence
		}
		if isTerminalStatus(current.metadata.Status) && finalizeRunningTurnAfterRestart(&current.metadata, lastSequence) {
			legacyMetadataCleared = true
		}
		manager.tasks[metadata.ID] = current
		metadataSaved := true
		if metadata.Status == statusInterrupted || repaired || sequenceRecovered || legacyMetadataCleared {
			metadataSaved = current.saveMetadata() == nil
		}
		if discardRecoveredQueue && metadataSaved {
			_ = os.Remove(current.queuePath())
		}
	}
	manager.scheduleQueued()
	return manager, nil
}

func (manager *taskManager) availableModels() (map[string]modelOption, error) {
	manager.modelsOnce.Do(func() {
		models, err := listModels(manager.cfg)
		if err != nil {
			manager.modelListErr = err
			return
		}
		manager.models = make(map[string]modelOption, len(models))
		for _, model := range models {
			manager.models[joinModel(model.Provider, model.ID)] = model
		}
	})
	return manager.models, manager.modelListErr
}

func (manager *taskManager) validateImagesForModel(selection string, images []imageContent) error {
	if err := validateImages(images); err != nil || len(images) == 0 {
		return err
	}
	models, err := manager.availableModels()
	if err != nil {
		return fmt.Errorf("cannot verify image input support: %w", err)
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
		return fmt.Errorf("cannot attach images because model %q is not available", selection)
	}
	if !model.Capabilities.ImageInput {
		return fmt.Errorf("model %s does not declare image input support", selection)
	}
	return nil
}

func (manager *taskManager) validateNetworkModel(mode, selection string) error {
	if mode != networkModeModelOnly {
		return nil
	}
	if !modelEgressAvailable(manager.cfg) {
		return fmt.Errorf("model-only network mode requires a configured supported model provider and model egress broker")
	}
	models, err := manager.availableModels()
	if err != nil {
		return fmt.Errorf("cannot verify model-only compatibility: %w", err)
	}
	selection = normalizeModelSelection(selection)
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
		return fmt.Errorf("model-only network mode cannot resolve model %q", selection)
	}
	if !modelEgressProviderAvailable(manager.cfg, model.Provider, model.ID) {
		return fmt.Errorf("model-only network mode does not support %s; use shared networking or configure a supported managed provider", selection)
	}
	return nil
}

func isLiveStatus(status taskStatus) bool {
	switch status {
	case statusQueued, statusStarting, statusIdle, statusRunning, statusWaiting, statusStopping:
		return true
	default:
		return false
	}
}

func occupiesActiveSlot(status taskStatus) bool {
	switch status {
	case statusStarting, statusIdle, statusRunning, statusWaiting, statusStopping:
		return true
	default:
		return false
	}
}

func isTerminalStatus(status taskStatus) bool {
	switch status {
	case statusStopped, statusFailed, statusInterrupted:
		return true
	default:
		return false
	}
}

func (manager *taskManager) activeCount() int {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	count := 0
	for _, current := range manager.tasks {
		current.mu.Lock()
		live := occupiesActiveSlot(current.metadata.Status)
		current.mu.Unlock()
		if live {
			count++
		}
	}
	return count
}

func (manager *taskManager) hasQueuedTasks() bool {
	return manager.queuedCount() > 0
}

func (manager *taskManager) queuedCount() int {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	count := 0
	for _, current := range manager.tasks {
		if current.snapshot().Status == statusQueued {
			count++
		}
	}
	return count
}

func (manager *taskManager) sideTaskLimit() int {
	if manager.cfg.MaxSideTasks >= 1 && manager.cfg.MaxSideTasks <= maximumMaxSideTasks {
		return manager.cfg.MaxSideTasks
	}
	return defaultMaxSideTasks
}

func (manager *taskManager) liveSideTaskCount(rootTaskID string) int {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	count := 0
	for _, current := range manager.tasks {
		metadata := current.snapshot()
		if metadata.BranchKind == "side" && metadata.ParentTaskID == rootTaskID &&
			(metadata.AwaitingPrompt || isLiveStatus(metadata.Status)) {
			count++
		}
	}
	return count
}

func (manager *taskManager) claimSubmissionSlot() (bool, error) {
	if manager.hasQueuedTasks() {
		return false, nil
	}
	return manager.claimActiveSlot()
}

func (manager *taskManager) claimActiveSlot() (bool, error) {
	if manager.activeCount() < manager.cfg.MaxTasks {
		return true, nil
	}
	manager.mu.RLock()
	candidates := make([]*task, 0)
	for _, current := range manager.tasks {
		if current.snapshot().Status == statusIdle {
			candidates = append(candidates, current)
		}
	}
	manager.mu.RUnlock()
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].snapshot().UpdatedAt.Before(candidates[j].snapshot().UpdatedAt)
	})
	for _, candidate := range candidates {
		stopped, err := candidate.stopIfIdle()
		if err != nil {
			return false, err
		}
		if !stopped {
			continue
		}
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			if isTerminalStatus(candidate.snapshot().Status) && manager.activeCount() < manager.cfg.MaxTasks {
				return true, nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false, fmt.Errorf("timed out while suspending an idle background task")
	}
	return false, nil
}

func (current *task) queuePath() string {
	return filepath.Join(current.dir, "queue.json")
}

func writeQueuedLaunch(path string, queued queuedLaunch) error {
	queued.Schema = 1
	if queued.State == "" {
		queued.State = "queued"
	}
	encoded, err := json.MarshalIndent(queued, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(encoded, '\n'))
}

func readQueuedLaunch(path, sessionRoot string) (queuedLaunch, error) {
	content, err := readPrivateRegularFile(path, maxRequestBytes)
	if err != nil {
		return queuedLaunch{}, err
	}
	var queued queuedLaunch
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&queued); err != nil {
		return queuedLaunch{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return queuedLaunch{}, fmt.Errorf("queued launch must contain exactly one JSON object")
	}
	if queued.Schema != 1 || queued.QueuedAt.IsZero() || len(queued.Prompt) > maxPromptBytes || (queued.State != "queued" && queued.State != "launching") {
		return queuedLaunch{}, fmt.Errorf("queued launch metadata is invalid")
	}
	switch queued.Operation {
	case "start", "fork", "restart":
		if queued.Prompt == "" {
			return queuedLaunch{}, fmt.Errorf("queued launch prompt is missing")
		}
	case "resume":
	default:
		return queuedLaunch{}, fmt.Errorf("queued launch operation is invalid")
	}
	if err := validateImages(queued.Images); err != nil {
		return queuedLaunch{}, err
	}
	if queued.Prompt == "" && len(queued.Images) > 0 {
		return queuedLaunch{}, fmt.Errorf("queued images require a prompt")
	}
	if (queued.Operation == "fork" || queued.Operation == "resume") && queued.SessionFile == "" {
		return queuedLaunch{}, fmt.Errorf("queued %s session is missing", queued.Operation)
	}
	if (queued.Operation == "start" || queued.Operation == "restart") && queued.SessionFile != "" {
		return queuedLaunch{}, fmt.Errorf("queued %s must not bind an existing session", queued.Operation)
	}
	if queued.SessionFile != "" {
		physical, err := validateSessionFile(sessionRoot, queued.SessionFile)
		if err != nil {
			return queuedLaunch{}, err
		}
		queued.SessionFile = physical
	}
	return queued, nil
}

func (current *task) queue(queued queuedLaunch) error {
	queued.Schema = 1
	queued.State = "queued"
	if queued.QueuedAt.IsZero() {
		queued.QueuedAt = time.Now().UTC()
	}
	if err := writeQueuedLaunch(current.queuePath(), queued); err != nil {
		current.mu.Lock()
		current.metadata.Status = statusStarting
		current.mu.Unlock()
		current.setTerminal(statusFailed, "persist queued task: "+err.Error())
		return err
	}
	current.mu.Lock()
	current.pendingLaunch = &queued
	current.metadata.Status = statusQueued
	current.metadata.PID = 0
	current.metadata.LastError = ""
	current.metadata.Failure = nil
	current.metadata.AwaitingPrompt = false
	current.metadata.QueuedAt = &queued.QueuedAt
	current.metadata.QueueOperation = queued.Operation
	current.metadata.UpdatedAt = queued.QueuedAt
	for index := range current.metadata.Approvals {
		current.metadata.Approvals[index].Active = false
	}
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		current.setTerminal(statusFailed, "persist queued task: "+err.Error())
		_ = os.Remove(current.queuePath())
		return err
	}
	raw, _ := json.Marshal(map[string]any{
		"type": "hobot_task_queued", "queuedAt": queued.QueuedAt, "operation": queued.Operation,
	})
	current.recordEvent(raw)
	if queued.Prompt != "" {
		current.recordQueuedPrompt(queued)
	}
	go current.manager.scheduleQueued()
	return nil
}

func (manager *taskManager) scheduleQueued() {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	for {
		manager.mu.RLock()
		candidates := make([]*task, 0)
		for _, current := range manager.tasks {
			if current.snapshot().Status == statusQueued {
				candidates = append(candidates, current)
			}
		}
		manager.mu.RUnlock()
		if len(candidates) == 0 {
			return
		}
		sort.Slice(candidates, func(i, j int) bool {
			left, right := candidates[i].snapshot(), candidates[j].snapshot()
			if left.QueuedAt != nil && right.QueuedAt != nil && !left.QueuedAt.Equal(*right.QueuedAt) {
				return left.QueuedAt.Before(*right.QueuedAt)
			}
			return left.CreatedAt.Before(right.CreatedAt)
		})
		available, err := manager.claimActiveSlot()
		if err != nil || !available {
			return
		}
		current := candidates[0]
		current.mu.Lock()
		if current.metadata.Status != statusQueued || current.pendingLaunch == nil {
			current.mu.Unlock()
			continue
		}
		queued := *current.pendingLaunch
		queued.Images = append([]imageContent(nil), current.pendingLaunch.Images...)
		promptRecorded := current.queuedPromptRecorded(queued)
		queued.State = "launching"
		if err := writeQueuedLaunch(current.queuePath(), queued); err != nil {
			current.mu.Unlock()
			current.setTerminal(statusFailed, "persist queue handoff: "+err.Error())
			_ = os.Remove(current.queuePath())
			continue
		}
		current.metadata.Status = statusStarting
		current.metadata.QueuedAt = nil
		current.metadata.QueueOperation = ""
		current.metadata.LastError = ""
		current.metadata.Failure = nil
		if queued.Operation == "restart" {
			current.metadata.SessionFile = ""
			current.metadata.SessionID = ""
			current.pendingSessionFile = ""
			current.pendingSessionID = ""
		}
		current.metadata.UpdatedAt = time.Now().UTC()
		current.mu.Unlock()
		if err := current.saveMetadata(); err != nil {
			current.setTerminal(statusFailed, err.Error())
			_ = os.Remove(current.queuePath())
			continue
		}
		raw, _ := json.Marshal(map[string]any{
			"type": "hobot_task_dequeued", "queuedAt": queued.QueuedAt, "operation": queued.Operation,
		})
		current.recordEvent(raw)
		if err := current.launch(queued.Prompt, queued.Images, queued.Approve, queued.SessionFile, promptRecorded); err != nil {
			_ = os.Remove(current.queuePath())
			if errors.Is(err, errTaskLaunchCancelled) {
				current.setTerminal(statusStopped, "")
				continue
			}
			current.setTerminal(statusFailed, err.Error())
			continue
		}
		current.mu.Lock()
		current.pendingLaunch = nil
		current.mu.Unlock()
		_ = os.Remove(current.queuePath())
	}
}

func (current *task) recordQueuedPrompt(queued queuedLaunch) {
	attachments := make([]map[string]string, 0, len(queued.Images))
	for _, image := range queued.Images {
		attachments = append(attachments, map[string]string{"name": image.Name, "mimeType": image.MimeType})
	}
	promptEvent, _ := json.Marshal(map[string]any{
		"type": "hobot_user_prompt", "message": queued.Prompt, "attachments": attachments,
		"queuedAt": queued.QueuedAt,
	})
	current.recordEvent(promptEvent)
}

func (current *task) queuedPromptRecorded(queued queuedLaunch) bool {
	if queued.Prompt == "" {
		return true
	}
	page, err := readEventPageWithRetention(current.events, current.metadata.ID, 0, 0, current.metadata.LogTruncated)
	if err != nil {
		return false
	}
	marker := false
	for _, event := range page.Events {
		var raw struct {
			Type      string    `json:"type"`
			Message   string    `json:"message"`
			QueuedAt  time.Time `json:"queuedAt"`
			Operation string    `json:"operation"`
		}
		if json.Unmarshal(event.Event, &raw) != nil || !raw.QueuedAt.Equal(queued.QueuedAt) {
			continue
		}
		if raw.Type == "hobot_task_queued" && raw.Operation == queued.Operation {
			marker = true
			continue
		}
		if marker && raw.Type == "hobot_user_prompt" && raw.Message == queued.Prompt {
			return true
		}
	}
	return false
}

func newTaskID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func validateTaskName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) {
		return "", fmt.Errorf("task title is required")
	}
	runes := []rune(value)
	if len(runes) > 64 {
		return "", fmt.Errorf("task title must contain at most 64 characters")
	}
	for _, char := range runes {
		if char == '/' || char == '\\' || unicode.IsControl(char) || unicode.In(char, unicode.Cf, unicode.Zl, unicode.Zp) {
			return "", fmt.Errorf("task title cannot contain path separators or control characters")
		}
	}
	return value, nil
}

func validateImages(images []imageContent) error {
	const maximumImageBytes = 1024 * 1024
	if len(images) > 4 {
		return fmt.Errorf("a prompt can contain at most 4 images")
	}
	total := 0
	allowed := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}
	for index, image := range images {
		if image.Type != "image" || !allowed[image.MimeType] || image.Data == "" {
			return fmt.Errorf("image %d must be a supported image content block", index+1)
		}
		decoded, err := base64.StdEncoding.DecodeString(image.Data)
		if err != nil {
			return fmt.Errorf("image %d contains invalid base64 data", index+1)
		}
		total += len(decoded)
		if total > maximumImageBytes {
			return fmt.Errorf("prompt images exceed the %d byte limit", maximumImageBytes)
		}
		if len([]rune(image.Name)) > 128 || strings.ContainsAny(image.Name, "\r\n") {
			return fmt.Errorf("image %d has an invalid name", index+1)
		}
	}
	return nil
}

func imagePayload(images []imageContent) []map[string]string {
	result := make([]map[string]string, 0, len(images))
	for _, image := range images {
		result = append(result, map[string]string{"type": image.Type, "data": image.Data, "mimeType": image.MimeType})
	}
	return result
}

func deriveTaskTitle(prompt string) string {
	value := strings.Join(strings.Fields(prompt), " ")
	value = strings.TrimLeft(value, "#>*-` ")
	for _, prefix := range []string{"请帮我", "帮我", "请", "麻烦", "Please ", "Can you ", "Could you "} {
		if strings.HasPrefix(value, prefix) {
			value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
			break
		}
	}
	value = strings.Map(func(char rune) rune {
		if char == '/' || char == '\\' || unicode.IsControl(char) || unicode.In(char, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return char
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	limit := 36
	if len(runes) > limit {
		runes = runes[:limit]
	}
	value = strings.TrimSpace(string(runes))
	value = strings.TrimRight(value, "，。！？；,.!?;:- ")
	if value == "" {
		return "New task"
	}
	return value
}

func truncateTaskTitle(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}

func normalizeWorkingDirectory(value string) (string, error) {
	if value == "" {
		value = "."
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("task working directory is not a directory: %s", physical)
	}
	return physical, nil
}

func (manager *taskManager) start(params startTaskParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if len(params.Prompt) == 0 || len(params.Prompt) > maxPromptBytes {
		return taskMetadata{}, fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
	}
	if params.Model != "" && normalizeModelSelection(params.Model) == "" {
		return taskMetadata{}, fmt.Errorf("model must use provider/model format")
	}
	if err := manager.validateImagesForModel(normalizeModelSelection(params.Model), params.Images); err != nil {
		return taskMetadata{}, err
	}
	permissionMode, err := normalizePermissionMode(params.PermissionMode)
	if err != nil {
		return taskMetadata{}, err
	}
	workspaceMode, err := normalizeWorkspaceMode(params.WorkspaceMode)
	if err != nil {
		return taskMetadata{}, err
	}
	sandboxMode, sandbox, err := manager.resolveTaskSandbox(params.SandboxMode, permissionMode, params.Deployment != nil)
	if err != nil {
		return taskMetadata{}, err
	}
	networkMode, sandbox, err := manager.resolveTaskNetworkMode(params.NetworkMode, sandboxMode, sandbox)
	if err != nil {
		return taskMetadata{}, err
	}
	if err := manager.validateNetworkModel(networkMode, params.Model); err != nil {
		return taskMetadata{}, err
	}
	manager.mu.RLock()
	retained := len(manager.tasks)
	manager.mu.RUnlock()
	if retained >= manager.cfg.MaxRetainedTasks {
		return taskMetadata{}, fmt.Errorf("retained task limit reached (%d); archive and delete old tasks", manager.cfg.MaxRetainedTasks)
	}
	cwd, err := normalizeWorkingDirectory(params.Cwd)
	if err != nil {
		return taskMetadata{}, err
	}
	available, err := manager.claimSubmissionSlot()
	if err != nil {
		return taskMetadata{}, err
	}
	id, err := newTaskID()
	if err != nil {
		return taskMetadata{}, err
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = deriveTaskTitle(params.Prompt)
	}
	name, err = validateTaskName(name)
	if err != nil {
		return taskMetadata{}, err
	}

	projectCwd := cwd
	worktreePath := ""
	worktreeBase := ""
	if workspaceMode == workspaceModeWorktree {
		workspace, workspaceErr := manager.createTaskWorktree(projectCwd, id)
		if workspaceErr != nil {
			return taskMetadata{}, workspaceErr
		}
		cwd = workspace.Cwd
		worktreePath = workspace.WorktreePath
		worktreeBase = workspace.BaseRevision
	}
	keepWorktree := false
	defer func() {
		if workspaceMode == workspaceModeWorktree && !keepWorktree {
			manager.rollbackTaskWorktree(id)
		}
	}()

	dir := filepath.Join(manager.cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return taskMetadata{}, err
	}
	keepDirectory := false
	defer func() {
		if !keepDirectory {
			_ = os.RemoveAll(dir)
		}
	}()
	current := &task{
		manager: manager,
		dir:     dir,
		events:  filepath.Join(dir, "events.jsonl"),
		stderr:  filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: name, Cwd: cwd, ProjectCwd: projectCwd, WorkspaceMode: workspaceMode,
			WorkspaceID: id, WorktreePath: worktreePath, WorktreeBase: worktreeBase, Status: statusStopped,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Approved: params.Approve,
			Model: normalizeModelSelection(params.Model), PermissionMode: permissionMode,
			SandboxMode: sandboxMode, NetworkMode: networkMode, Sandbox: sandbox, Deployment: params.Deployment,
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		return taskMetadata{}, err
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		return taskMetadata{}, err
	}
	if err := current.saveMetadata(); err != nil {
		return taskMetadata{}, err
	}

	manager.mu.Lock()
	manager.tasks[id] = current
	manager.mu.Unlock()
	keepDirectory = true
	keepWorktree = true
	if !available {
		queued := queuedLaunch{Operation: "start", Prompt: params.Prompt, Images: params.Images, Approve: params.Approve, QueuedAt: time.Now().UTC()}
		if err := current.queue(queued); err != nil {
			current.setTerminal(statusFailed, err.Error())
			return current.snapshot(), err
		}
		return current.snapshot(), nil
	}
	current.mu.Lock()
	current.metadata.Status = statusStarting
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		current.setTerminal(statusFailed, err.Error())
		return current.snapshot(), err
	}
	if err := current.launch(params.Prompt, params.Images, params.Approve, "", false); err != nil {
		if errors.Is(err, errTaskLaunchCancelled) {
			current.setTerminal(statusStopped, "")
			return current.snapshot(), nil
		}
		current.setTerminal(statusFailed, err.Error())
		return current.snapshot(), err
	}
	return current.snapshot(), nil
}

func (manager *taskManager) fork(params forkTaskParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if len(params.Prompt) > maxPromptBytes {
		return taskMetadata{}, fmt.Errorf("task prompt must contain at most %d bytes", maxPromptBytes)
	}
	if err := validateImages(params.Images); err != nil {
		return taskMetadata{}, err
	}
	if params.Kind == "" {
		params.Kind = "side"
	}
	if params.Kind != "side" && params.Kind != "edit" {
		return taskMetadata{}, fmt.Errorf("task fork kind must be side or edit")
	}
	if params.Kind == "edit" && len(params.Prompt) == 0 {
		return taskMetadata{}, fmt.Errorf("edited task fork requires a replacement prompt")
	}
	if params.Kind == "edit" && params.Sequence == 0 {
		return taskMetadata{}, fmt.Errorf("edited task fork requires a user message sequence")
	}
	if len(params.Prompt) == 0 && len(params.Images) > 0 {
		return taskMetadata{}, fmt.Errorf("images require a task prompt")
	}
	if params.Model != "" && normalizeModelSelection(params.Model) == "" {
		return taskMetadata{}, fmt.Errorf("model must use provider/model format")
	}
	if params.PermissionMode != "" {
		if _, err := normalizePermissionMode(params.PermissionMode); err != nil {
			return taskMetadata{}, err
		}
	}
	if params.SandboxMode != "" {
		if _, err := normalizeSandboxMode(params.SandboxMode); err != nil {
			return taskMetadata{}, err
		}
	}
	if params.NetworkMode != "" {
		if _, err := normalizeNetworkMode(params.NetworkMode); err != nil {
			return taskMetadata{}, err
		}
	}
	manager.mu.RLock()
	retained := len(manager.tasks)
	manager.mu.RUnlock()
	if retained >= manager.cfg.MaxRetainedTasks {
		return taskMetadata{}, fmt.Errorf("retained task limit reached (%d); archive and delete old tasks", manager.cfg.MaxRetainedTasks)
	}
	source, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	parent := source.snapshot()
	if params.Kind == "side" {
		rootTaskID := manager.rootTaskID(parent)
		limit := manager.sideTaskLimit()
		if manager.liveSideTaskCount(rootTaskID) >= limit {
			return taskMetadata{}, fmt.Errorf("side agent limit reached (%d) for this main task; stop or close a Side Agent before opening another", limit)
		}
	}
	id, err := newTaskID()
	if err != nil {
		return taskMetadata{}, err
	}
	forkSource, sessionFile, lines, forkSequence, err := manager.resolveForkSource(source, params.Sequence)
	if err != nil {
		return taskMetadata{}, fmt.Errorf("read source session: %w", err)
	}
	leafID := ""
	if params.Sequence > 0 {
		leafID, err = forkSource.sessionLeafBeforePrompt(forkSequence, lines)
		if err != nil {
			return taskMetadata{}, err
		}
	} else {
		leafID = safeSessionLeaf(lines)
		if leafID == "" {
			return taskMetadata{}, fmt.Errorf("source task has no settled context to fork")
		}
	}
	forkSessionDir := filepath.Join(manager.cfg.SessionDir, id)
	if err := ensurePrivateDir(forkSessionDir); err != nil {
		return taskMetadata{}, fmt.Errorf("prepare fork session directory: %w", err)
	}
	forkFile, err := writeSessionFork(forkSessionDir, sessionFile, parent.Cwd, leafID, lines)
	if err != nil {
		_ = os.RemoveAll(forkSessionDir)
		return taskMetadata{}, fmt.Errorf("create session fork: %w", err)
	}
	keepSession := false
	defer func() {
		if !keepSession {
			_ = os.RemoveAll(forkSessionDir)
		}
	}()
	if params.Kind == "edit" {
		if _, err := source.stopIfIdle(); err != nil {
			return taskMetadata{}, fmt.Errorf("stop edited task: %w", err)
		}
		if isLiveStatus(source.snapshot().Status) {
			return taskMetadata{}, fmt.Errorf("task must be idle or stopped before editing its history")
		}
	}
	available := true
	if params.Kind == "edit" || params.Prompt != "" {
		available, err = manager.claimSubmissionSlot()
		if err != nil {
			return taskMetadata{}, err
		}
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		if params.Kind == "edit" {
			name = parent.Name
		} else {
			base := truncateTaskTitle(parent.Name, 54)
			name = base + "-side"
		}
	}
	name, err = validateTaskName(name)
	if err != nil {
		return taskMetadata{}, err
	}
	dir := filepath.Join(manager.cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return taskMetadata{}, err
	}
	keepDirectory := false
	defer func() {
		if !keepDirectory {
			_ = os.RemoveAll(dir)
		}
	}()
	model := normalizeModelSelection(params.Model)
	if model == "" {
		model = parent.Model
	}
	if err := manager.validateImagesForModel(model, params.Images); err != nil {
		return taskMetadata{}, err
	}
	permissionMode := params.PermissionMode
	if permissionMode == "" {
		permissionMode = parent.PermissionMode
	}
	permissionMode, _ = normalizePermissionMode(permissionMode)
	sandboxMode := params.SandboxMode
	if sandboxMode == "" {
		sandboxMode = parent.SandboxMode
	}
	sandboxMode, sandbox, err := manager.resolveTaskSandbox(sandboxMode, permissionMode, parent.Deployment != nil)
	if err != nil {
		return taskMetadata{}, err
	}
	networkMode := params.NetworkMode
	if networkMode == "" {
		networkMode = parent.NetworkMode
	}
	networkMode, sandbox, err = manager.resolveTaskNetworkMode(networkMode, sandboxMode, sandbox)
	if err != nil {
		return taskMetadata{}, err
	}
	if err := manager.validateNetworkModel(networkMode, model); err != nil {
		return taskMetadata{}, err
	}
	projectCwd := parent.ProjectCwd
	if projectCwd == "" {
		projectCwd = parent.Cwd
	}
	workspaceMode, workspaceErr := normalizeWorkspaceMode(parent.WorkspaceMode)
	if workspaceErr != nil {
		return taskMetadata{}, workspaceErr
	}
	parentTaskID := parent.ID
	if params.Kind == "side" {
		parentTaskID = manager.rootTaskID(parent)
	}
	status := statusStarting
	awaitingPrompt := false
	if params.Kind == "side" && params.Prompt == "" {
		status = statusStopped
		awaitingPrompt = true
	} else if !available {
		status = statusStopped
	}
	current := &task{
		manager: manager,
		dir:     dir,
		events:  filepath.Join(dir, "events.jsonl"),
		stderr:  filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: name, Cwd: parent.Cwd, ProjectCwd: projectCwd,
			WorkspaceMode: workspaceMode, WorkspaceID: parent.WorkspaceID,
			WorktreePath: parent.WorktreePath, WorktreeBase: parent.WorktreeBase, Status: status,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Approved: parent.Approved,
			SessionFile: forkFile, Model: model, PermissionMode: permissionMode,
			SandboxMode: sandboxMode, NetworkMode: networkMode, Sandbox: sandbox, ParentTaskID: parentTaskID,
			SourceTaskID: parent.ID,
			ForkSequence: params.Sequence, BranchKind: params.Kind, AwaitingPrompt: awaitingPrompt,
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if params.Kind == "edit" {
		eventBytes, lastSequence, err := writeEditEventHistory(current.events, source.events, parent.ID, id, source.metadata.LogTruncated, params.Sequence, manager.cfg.MaxEventSize)
		if err != nil {
			return taskMetadata{}, fmt.Errorf("copy edited conversation history: %w", err)
		}
		current.eventBytes = eventBytes
		current.eventLastSequence = lastSequence
		current.metadata.LastSequence = lastSequence
	} else if err := os.WriteFile(current.events, nil, 0o600); err != nil {
		return taskMetadata{}, err
	}
	if err := os.WriteFile(current.stderr, nil, 0o600); err != nil {
		return taskMetadata{}, err
	}
	if err := current.saveMetadata(); err != nil {
		return taskMetadata{}, err
	}
	manager.mu.Lock()
	manager.tasks[id] = current
	manager.mu.Unlock()
	keepDirectory = true
	keepSession = true
	if params.Prompt != "" {
		if !available {
			queued := queuedLaunch{Operation: "fork", Prompt: params.Prompt, Images: params.Images, Approve: parent.Approved, SessionFile: forkFile, QueuedAt: time.Now().UTC()}
			if err := current.queue(queued); err != nil {
				current.setTerminal(statusFailed, err.Error())
				return current.snapshot(), err
			}
			return current.snapshot(), nil
		}
		if err := current.launch(params.Prompt, params.Images, parent.Approved, forkFile, false); err != nil {
			if errors.Is(err, errTaskLaunchCancelled) {
				current.setTerminal(statusStopped, "")
				return current.snapshot(), nil
			}
			current.setTerminal(statusFailed, err.Error())
			return current.snapshot(), err
		}
	}
	return current.snapshot(), nil
}

// Editing creates a new session branch internally, but it remains the same
// conversation to the user. Preserve the visible timeline before the selected
// prompt and let launch() append the replacement prompt as the next event.
func writeEditEventHistory(targetPath, sourcePath, sourceTaskID, targetTaskID string, sourceTruncated bool, before uint64, maximumBytes int64) (int64, uint64, error) {
	events, err := readEventPageWithRetention(sourcePath, sourceTaskID, 0, 0, sourceTruncated)
	if err != nil {
		return 0, 0, err
	}
	prefix := make([]taskEvent, 0)
	foundBoundary := false
	for _, event := range events.Events {
		if event.Sequence == before {
			foundBoundary = true
			break
		}
		if event.Sequence > before {
			break
		}
		prefix = append(prefix, event)
	}
	if !foundBoundary {
		return 0, 0, fmt.Errorf("selected message event is missing: %d", before)
	}
	// Keep the newest half of the prior timeline so the edited conversation has
	// enough durable-log capacity for its replacement turn and later follow-ups.
	historyBudget := maximumBytes / 2
	start := len(prefix)
	used := int64(0)
	for index := len(prefix) - 1; index >= 0; index-- {
		event := prefix[index]
		event.TaskID = targetTaskID
		event.Sequence = 1
		encoded, err := json.Marshal(event)
		if err != nil {
			return 0, 0, err
		}
		if used+int64(len(encoded)+1) > historyBudget {
			break
		}
		used += int64(len(encoded) + 1)
		start = index
	}
	for start < len(prefix) && (prefix[start].Normalized == nil || prefix[start].Normalized.Type != "user.message") {
		start++
	}
	var output bytes.Buffer
	lastSequence := uint64(0)
	for _, event := range prefix[start:] {
		event.TaskID = targetTaskID
		lastSequence++
		event.Sequence = lastSequence
		encoded, err := json.Marshal(event)
		if err != nil {
			return 0, 0, err
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	if int64(output.Len()) > historyBudget {
		return 0, 0, fmt.Errorf("edited conversation history exceeds %d bytes", historyBudget)
	}
	if err := os.WriteFile(targetPath, output.Bytes(), 0o600); err != nil {
		return 0, 0, err
	}
	return int64(output.Len()), lastSequence, nil
}

func (current *task) sessionLeafBeforePrompt(sequence uint64, lines []sessionLine) (string, error) {
	page, err := readEventPageWithRetention(current.events, current.metadata.ID, 0, 0, current.metadata.LogTruncated)
	if err != nil {
		return "", err
	}
	prompts := make([]string, 0)
	selected := -1
	for _, event := range page.Events {
		if event.Normalized == nil || event.Normalized.Type != "user.message" {
			continue
		}
		text, _ := event.Normalized.Data["text"].(string)
		prompts = append(prompts, text)
		if event.Sequence == sequence {
			selected = len(prompts) - 1
		}
	}
	if selected < 0 || prompts[selected] == "" {
		return "", fmt.Errorf("fork sequence is not a user message: %d", sequence)
	}
	if len(lines) == 0 || lines[len(lines)-1].ID == "" {
		return "", fmt.Errorf("current source session has no active branch")
	}
	branch, err := sessionBranch(lines, lines[len(lines)-1].ID)
	if err != nil {
		return "", err
	}
	sessionPrompts := make([]sessionLine, 0)
	for _, line := range branch {
		if line.Type == "message" && line.Role == "user" {
			sessionPrompts = append(sessionPrompts, line)
		}
	}

	// Agentd can retain events from an older Pi session, while event retention
	// can also remove the oldest events from the current session. The current
	// session prompts and retained task prompts must therefore share a suffix.
	// Match that suffix from newest to oldest so repeated prompts such as
	// "continue" keep their identity by position instead of by text alone.
	eventIndex := len(prompts) - 1
	sessionIndex := len(sessionPrompts) - 1
	mappedSessionIndex := -1
	for eventIndex >= 0 && sessionIndex >= 0 && prompts[eventIndex] == sessionPrompts[sessionIndex].Text {
		if eventIndex == selected {
			mappedSessionIndex = sessionIndex
		}
		eventIndex--
		sessionIndex--
	}
	if mappedSessionIndex < 0 {
		return "", fmt.Errorf("selected message was not found in the current source session")
	}
	parentID := sessionPrompts[mappedSessionIndex].ParentID
	if parentID == "" {
		return "", fmt.Errorf("selected message has no parent context")
	}
	return parentID, nil
}

type retainedUserPrompt struct {
	sequence uint64
	text     string
}

func (current *task) retainedUserPrompts() ([]retainedUserPrompt, error) {
	page, err := readEventPageWithRetention(current.events, current.metadata.ID, 0, 0, current.metadata.LogTruncated)
	if err != nil {
		return nil, err
	}
	prompts := make([]retainedUserPrompt, 0)
	for _, event := range page.Events {
		if event.Normalized == nil || event.Normalized.Type != "user.message" {
			continue
		}
		text, _ := event.Normalized.Data["text"].(string)
		prompts = append(prompts, retainedUserPrompt{sequence: event.Sequence, text: text})
	}
	return prompts, nil
}

// resolveForkSource keeps historical editing usable when a previously edited
// Pi session was damaged after it was created. Events copied into an edit task
// retain their order, so a prompt at or before the edit anchor can be mapped
// back to the healthy parent session without guessing from duplicate text.
func (manager *taskManager) resolveForkSource(source *task, sequence uint64) (*task, string, []sessionLine, uint64, error) {
	current := source
	currentSequence := sequence
	seen := make(map[string]bool)
	var sourceErr error
	for {
		metadata := current.snapshot()
		if metadata.ID == "" || seen[metadata.ID] {
			break
		}
		seen[metadata.ID] = true
		sessionFile, err := validateSessionFile(manager.cfg.SessionDir, metadata.SessionFile)
		if err == nil {
			_, lines, readErr := readSessionLines(sessionFile)
			if readErr == nil {
				return current, sessionFile, lines, currentSequence, nil
			}
			err = readErr
		}
		if sourceErr == nil {
			sourceErr = err
		}
		if currentSequence == 0 || metadata.BranchKind != "edit" || metadata.ParentTaskID == "" || metadata.ForkSequence == 0 {
			break
		}
		parent, parentErr := manager.get(metadata.ParentTaskID)
		if parentErr != nil {
			break
		}
		mapped, mapErr := mapEditPromptToParent(current, parent, currentSequence, metadata.ForkSequence)
		if mapErr != nil {
			break
		}
		current = parent
		currentSequence = mapped
	}
	if sourceErr == nil {
		sourceErr = fmt.Errorf("session does not contain a resumable branch")
	}
	return nil, "", nil, 0, sourceErr
}

func mapEditPromptToParent(current, parent *task, sequence, parentForkSequence uint64) (uint64, error) {
	currentPrompts, err := current.retainedUserPrompts()
	if err != nil {
		return 0, err
	}
	parentPrompts, err := parent.retainedUserPrompts()
	if err != nil {
		return 0, err
	}
	selected := -1
	for index, prompt := range currentPrompts {
		if prompt.sequence == sequence {
			selected = index
			break
		}
	}
	anchor := -1
	for index, prompt := range parentPrompts {
		if prompt.sequence == parentForkSequence {
			anchor = index
			break
		}
	}
	if selected < 0 || anchor < 0 || selected > anchor || selected >= len(parentPrompts) {
		return 0, fmt.Errorf("selected message cannot be recovered from the parent edit context")
	}
	for index := 0; index < selected; index++ {
		if index >= len(parentPrompts) || currentPrompts[index].text != parentPrompts[index].text {
			return 0, fmt.Errorf("edited conversation no longer matches its parent context")
		}
	}
	if selected < anchor && currentPrompts[selected].text != parentPrompts[selected].text {
		return 0, fmt.Errorf("selected historical message no longer matches its parent context")
	}
	return parentPrompts[selected].sequence, nil
}

func (current *task) launch(prompt string, images []imageContent, approve bool, sessionFile string, promptRecorded bool) error {
	metadata := current.snapshot()
	if err := current.manager.validateNetworkModel(metadata.NetworkMode, metadata.Model); err != nil {
		return err
	}
	if err := current.manager.validateManagedTaskWorkspace(metadata); err != nil {
		return fmt.Errorf("validate task workspace: %w", err)
	}
	if err := current.ensurePermissionPolicy(metadata.PermissionMode); err != nil {
		return fmt.Errorf("write task permission policy: %w", err)
	}
	taskSessionDir, sessionFile, err := current.prepareTaskSession(sessionFile)
	if err != nil {
		return fmt.Errorf("prepare private task session: %w", err)
	}
	taskAgentDir, err := current.prepareTaskAgentConfiguration(metadata.NetworkMode)
	if err != nil {
		return fmt.Errorf("prepare private task agent configuration: %w", err)
	}
	args := []string{"--mode", "rpc", "--session-dir", taskSessionDir, "--name", metadata.Name}
	if sessionFile != "" {
		args = append(args, "--session", sessionFile)
	}
	if metadata.Model != "" {
		args = append(args, "--model", metadata.Model)
	}
	if approve {
		args = append(args, "--approve")
	} else {
		args = append(args, "--no-approve")
	}
	commandName, commandArgs, sandbox, err := current.manager.sandboxCommand(metadata, args)
	if err != nil {
		return err
	}
	command := exec.Command(commandName, commandArgs...)
	command.Dir = metadata.Cwd
	commandEnvironment := append(safeChildEnvironment(os.Environ()),
		"HOBOT_CODE_BACKGROUND_TASK=1",
		"HOBOT_CODE_BACKGROUND_TASK_ID="+metadata.ID,
		"HOBOT_CODE_AGENT_ROLE="+taskAgentRole(metadata),
		"HOBOT_CODE_PARENT_TASK_ID="+metadata.ParentTaskID,
		"HOBOT_CODE_SOURCE_TASK_ID="+metadata.SourceTaskID,
		"HOBOT_CODING_AGENT_DIR="+taskAgentDir,
		"HOBOT_CODE_PERMISSION_POLICY="+current.permissionPolicyPath(),
		"HOBOT_CODE_SANDBOX_MODE="+metadata.SandboxMode,
		"HOBOT_CODE_SANDBOX_BACKEND="+sandbox.Backend,
		"HOBOT_CODE_NETWORK_MODE="+metadata.NetworkMode,
	)
	if _, skillsRoot, skillsErr := configuredOpenExplorerSkillPaths(); skillsErr != nil {
		return fmt.Errorf("prepare OpenExplorer LLM Skill runtime: %w", skillsErr)
	} else if skillsRoot != "" {
		commandEnvironment = append(commandEnvironment, "HOBOT_CODE_OPENEXPLORER_SKILLS_ROOT="+skillsRoot)
	}
	command.Env = commandEnvironment
	credentialPayload := ""
	if metadata.NetworkMode == networkModeShared {
		credentialPayload = gatewayCredentialPayload(current.manager.cfg)
	}
	closeCredential, err := attachGatewayCredential(command, credentialPayload)
	if err != nil {
		return fmt.Errorf("prepare worker model credential: %w", err)
	}
	defer closeCredential()
	command.SysProcAttr = workerSysProcAttr()
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	current.mu.Lock()
	if current.metadata.Status != statusStarting {
		current.mu.Unlock()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return errTaskLaunchCancelled
	}
	if err := command.Start(); err != nil {
		current.mu.Unlock()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	current.command = command
	current.stdin = stdin
	current.workerDone = make(chan struct{})
	current.metadata.PID = command.Process.Pid
	current.metadata.Sandbox = sandbox
	current.metadata.UpdatedAt = time.Now().UTC()
	current.stopping = false
	current.interrupted = false
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		current.mu.Lock()
		done := current.workerDone
		current.command = nil
		current.stdin = nil
		current.workerDone = nil
		current.metadata.PID = 0
		current.mu.Unlock()
		if done != nil {
			close(done)
		}
		return err
	}

	current.streamWG.Add(2)
	go func() {
		defer current.streamWG.Done()
		current.consumeStdout(stdout)
	}()
	go func() {
		defer current.streamWG.Done()
		current.consumeStderr(stderr)
	}()
	go current.wait()
	stateCommand, _ := json.Marshal(map[string]any{"id": "agentd-state", "type": "get_state"})
	if err := current.writeWorkerCommand(stateCommand); err != nil {
		_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
		if current.snapshot().Status == statusStopping || current.snapshot().Status == statusStopped {
			return errTaskLaunchCancelled
		}
		return err
	}
	if prompt != "" {
		startCommand, _ := json.Marshal(map[string]any{
			"id": "agentd-start", "type": "prompt", "message": prompt, "images": images,
		})
		if err := current.sendCommandWithPromptEvent(startCommand, !promptRecorded); err != nil {
			_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
			if current.snapshot().Status == statusStopping || current.snapshot().Status == statusStopped {
				return errTaskLaunchCancelled
			}
			return err
		}
	}
	return nil
}

func taskAgentRole(metadata taskMetadata) string {
	if metadata.BranchKind == "side" {
		return "side"
	}
	return "main"
}

func taskToolActivity(name string) string {
	if !taskToolNamePattern.MatchString(name) {
		return "thinking"
	}
	return "using " + name
}

func (current *task) taskSessionDirectory() string {
	return filepath.Join(current.manager.cfg.SessionDir, current.metadata.ID)
}

func (current *task) taskAgentDirectory() string {
	return filepath.Join(current.taskSessionDirectory(), "agent")
}

// Pi uses a short-lived settings.json.lock directory even when it only reads
// settings. Give every background worker a private runtime snapshot instead of
// making the user's canonical configuration writable inside the sandbox.
func (current *task) prepareTaskAgentConfiguration(networkMode string) (string, error) {
	if !taskIDPattern.MatchString(current.metadata.ID) {
		return "", fmt.Errorf("task id is invalid")
	}
	directory := current.taskAgentDirectory()
	if err := ensurePrivateDir(directory); err != nil {
		return "", err
	}
	names := []string{"settings.json", "models.json"}
	if networkMode == networkModeShared {
		// Pi login providers own auth.json and require it only with shared
		// networking. Restricted workers must never receive this credential.
		names = append(names, "auth.json")
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
		destination := filepath.Join(directory, name)
		if err := os.RemoveAll(destination); err != nil {
			return "", err
		}
		source := filepath.Join(current.manager.cfg.AgentDir, name)
		content, err := readPrivateRegularFile(source, maxRequestBytes)
		if errors.Is(err, os.ErrNotExist) {
			if name != "settings.json" {
				continue
			}
			content = nil
			err = nil
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		if name == "settings.json" {
			content, err = mergeOpenExplorerSkillsIntoSettings(content)
			if err != nil {
				return "", err
			}
		}
		if len(content) == 0 {
			continue
		}
		if !json.Valid(content) {
			return "", fmt.Errorf("%s is not valid JSON", name)
		}
		if err := writePrivateFile(destination, content); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}
	for _, name := range []string{"settings.json", "models.json", "auth.json"} {
		if !allowed[name] {
			if err := os.RemoveAll(filepath.Join(directory, name)); err != nil {
				return "", err
			}
		}
	}
	if err := os.RemoveAll(filepath.Join(directory, "settings.json.lock")); err != nil {
		return "", err
	}
	return directory, nil
}

func (current *task) prepareTaskSession(sessionFile string) (string, string, error) {
	directory := current.taskSessionDirectory()
	if err := ensurePrivateDir(directory); err != nil {
		return "", "", err
	}
	if sessionFile == "" {
		return directory, "", nil
	}
	source, err := validateSessionFile(current.manager.cfg.SessionDir, sessionFile)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(directory, source)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return directory, source, nil
	}
	input, err := os.Open(source)
	if err != nil {
		return "", "", err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(directory, ".resume-*.jsonl")
	if err != nil {
		return "", "", err
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", "", err
	}
	written, err := io.CopyN(temporary, input, int64(maxRequestBytes*32)+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", "", err
	}
	if written > int64(maxRequestBytes*32) {
		return "", "", fmt.Errorf("session exceeds the private migration limit")
	}
	if err := temporary.Sync(); err != nil {
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	target := filepath.Join(directory, "resume-"+current.metadata.ID+".jsonl")
	if err := os.Rename(temporaryPath, target); err != nil {
		return "", "", err
	}
	keep = true
	return directory, target, nil
}

func (current *task) consumeStdout(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxRequestBytes)
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if !json.Valid(line) {
			current.failWorker("worker emitted invalid JSON")
			return
		}
		current.recordEvent(append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		current.mu.Lock()
		expected := current.stopping || current.interrupted || current.metadata.Status == statusStopped || current.metadata.Status == statusInterrupted
		current.mu.Unlock()
		if !expected {
			current.failWorker("worker event stream failed: " + err.Error())
		}
	}
}

func (current *task) consumeStderr(reader io.Reader) {
	file, err := os.OpenFile(current.stderr, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = io.Copy(&taskStderrWriter{task: current, file: file}, reader)
}

type taskStderrWriter struct {
	task *task
	file *os.File
}

func (writer *taskStderrWriter) Write(value []byte) (int, error) {
	originalLength := len(value)
	writer.task.stderrMu.Lock()
	defer writer.task.stderrMu.Unlock()
	remaining := maximumWorkerStderrBytes - writer.task.stderrBytes
	if remaining <= 0 {
		return originalLength, nil
	}
	writeLength := int64(len(value))
	if writeLength > remaining {
		writeLength = remaining
	}
	written, err := writer.file.Write(value[:writeLength])
	writer.task.stderrBytes += int64(written)
	if err != nil {
		return written, err
	}
	if written != int(writeLength) {
		return written, io.ErrShortWrite
	}
	return originalLength, nil
}

type boundedLogWriter struct {
	writer    io.Writer
	remaining int64
}

func (writer *boundedLogWriter) Write(value []byte) (int, error) {
	originalLength := len(value)
	if writer.remaining > 0 {
		writeLength := int64(len(value))
		if writeLength > writer.remaining {
			writeLength = writer.remaining
		}
		if _, err := writer.writer.Write(value[:writeLength]); err != nil {
			return 0, err
		}
		writer.remaining -= writeLength
	}
	return originalLength, nil
}

func (current *task) wait() {
	current.mu.Lock()
	command := current.command
	workerDone := current.workerDone
	current.mu.Unlock()
	if command == nil {
		return
	}
	if workerDone != nil {
		defer close(workerDone)
		defer func() { go current.manager.scheduleQueued() }()
	}
	err := command.Wait()
	current.streamWG.Wait()
	current.mu.Lock()
	stopping := current.stopping
	interrupted := current.interrupted
	status := current.metadata.Status
	current.command = nil
	current.stdin = nil
	current.metadata.PID = 0
	current.mu.Unlock()
	if interrupted || status == statusInterrupted {
		current.setTerminal(statusInterrupted, "")
	} else if stopping || status == statusStopping {
		current.setTerminal(statusStopped, "")
	} else if err != nil {
		current.setTerminal(statusFailed, err.Error())
	} else {
		current.setTerminal(statusFailed, "worker exited before the task completed")
	}
}

func (current *task) failWorker(message string) {
	current.mu.Lock()
	pid := current.metadata.PID
	if pid > 0 {
		current.terminalCaptureUnsafe = true
	}
	current.mu.Unlock()
	current.setTerminal(statusFailed, message)
	if pid > 0 {
		_ = terminateProcessGroup(pid, syscall.SIGKILL)
	}
}

func (current *task) recordEvent(raw json.RawMessage) {
	raw = redactEventImagePayloads(raw)
	var header struct {
		Type       string `json:"type"`
		Method     string `json:"method"`
		Command    string `json:"command"`
		Success    bool   `json:"success"`
		IsError    bool   `json:"isError"`
		ID         string `json:"id"`
		ToolCallID string `json:"toolCallId"`
		ToolName   string `json:"toolName"`
		Data       struct {
			SessionFile string `json:"sessionFile"`
			SessionID   string `json:"sessionId"`
			Provider    string `json:"provider"`
			ID          string `json:"id"`
			Model       *struct {
				Provider string `json:"provider"`
				ID       string `json:"id"`
			} `json:"model"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &header)
	turnBoundary := header.Type == "agent_settled" || header.Type == "hobot_task_failed" || header.Type == "hobot_task_interrupted" || header.Type == "hobot_task_stopped"
	var boundaryWorkspace *turnWorkspaceEvidence
	if turnBoundary {
		current.turnCaptureMu.Lock()
		defer current.turnCaptureMu.Unlock()
		current.mu.Lock()
		cwd := current.metadata.Cwd
		unsafe := current.terminalCaptureUnsafe
		current.terminalCaptureUnsafe = false
		current.mu.Unlock()
		if !unsafe {
			boundaryWorkspace = captureTurnWorkspaceEvidence(cwd)
		}
	}

	current.mu.Lock()
	current.metadata.LastSequence++
	current.metadata.UpdatedAt = time.Now().UTC()
	acceptWorkerTransition := current.metadata.Status != statusStopping && isLiveStatus(current.metadata.Status)
	switch header.Type {
	case "agent_start":
		if acceptWorkerTransition {
			current.metadata.Status = statusRunning
			current.metadata.CurrentActivity = "thinking"
		}
		if current.currentRunningTurnLocked() == 0 {
			beginTurnEvidenceLocked(&current.metadata, nil)
			current.openToolCalls = nil
			current.openAnonymousTools = 0
			fallback := &current.metadata.TurnEvidence[len(current.metadata.TurnEvidence)-1]
			fallback.StartSequence = current.metadata.LastSequence
			fallback.Evidence = "partial"
		}
	case "agent_settled":
		if acceptWorkerTransition {
			current.metadata.Status = statusIdle
			current.metadata.CurrentActivity = ""
		}
		finalizeTurnEvidenceLocked(&current.metadata, "completed", boundaryWorkspace, current.metadata.LastSequence)
	case "tool_execution_start", "tool_execution_end":
		current.updateTurnToolEvidenceLocked(header.Type, header.ToolCallID, header.IsError)
		if acceptWorkerTransition {
			if header.Type == "tool_execution_start" {
				current.metadata.CurrentActivity = taskToolActivity(header.ToolName)
			} else if len(current.openToolCalls) > 0 || current.openAnonymousTools > 0 {
				current.metadata.CurrentActivity = "using tools"
			} else {
				current.metadata.CurrentActivity = "thinking"
			}
		}
	case "hobot_task_failed":
		finalizeTurnEvidenceLocked(&current.metadata, "failed", boundaryWorkspace, current.metadata.LastSequence)
	case "hobot_task_interrupted":
		finalizeTurnEvidenceLocked(&current.metadata, "interrupted", boundaryWorkspace, current.metadata.LastSequence)
	case "hobot_task_stopped":
		finalizeTurnEvidenceLocked(&current.metadata, "stopped", boundaryWorkspace, current.metadata.LastSequence)
	case "extension_ui_request":
		if approval, ok := approvalFromEvent(raw); ok && acceptWorkerTransition {
			current.metadata.Status = statusWaiting
			current.metadata.CurrentActivity = "waiting for approval"
			current.upsertApprovalLocked(approval)
		}
	case "response":
		if header.Success && header.Command == "get_state" {
			current.pendingSessionFile = header.Data.SessionFile
			current.pendingSessionID = header.Data.SessionID
			if current.metadata.Status == statusStarting {
				current.metadata.Status = statusIdle
				current.metadata.CurrentActivity = ""
			}
			if header.Data.Model != nil {
				if model := workerModelSelection(header.Data.Model.Provider, header.Data.Model.ID); model != "" {
					current.metadata.Model = model
				}
			}
		} else if header.Success && header.Command == "set_model" {
			if model := workerModelSelection(header.Data.Provider, header.Data.ID); model != "" {
				current.metadata.Model = model
			}
		}
	}
	sessionBound := current.bindPendingSessionLocked()
	event := taskEvent{
		Protocol:   protocolVersion,
		Kind:       "event",
		TaskID:     current.metadata.ID,
		Sequence:   current.metadata.LastSequence,
		Time:       time.Now().UTC(),
		Event:      raw,
		Normalized: normalizeWorkerEvent(raw),
	}
	encoded, _ := json.Marshal(event)
	wasTruncated := current.metadata.LogTruncated
	persisted := current.persistEventLocked(event, encoded)
	if !persisted {
		current.metadata.LogTruncated = true
	}
	logBecameTruncated := !wasTruncated && current.metadata.LogTruncated
	for id, subscriber := range current.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(current.subscribers, id)
		}
	}
	current.mu.Unlock()
	if sessionBound || logBecameTruncated || header.Type == "hobot_user_prompt" || header.Type == "hobot_task_queued" || header.Type == "hobot_task_dequeued" || header.Type == "hobot_task_queue_cancelled" || header.Type == "hobot_task_failed" || header.Type == "hobot_task_interrupted" || header.Type == "hobot_task_stopped" || header.Type == "agent_start" || header.Type == "agent_settled" || header.Type == "tool_execution_start" || header.Type == "tool_execution_end" || header.Type == "extension_ui_request" || header.Type == "response" {
		_ = current.saveMetadata()
	}
	if header.Type == "agent_settled" {
		go current.manager.scheduleQueued()
	}
}

func (current *task) persistEventLocked(event taskEvent, encoded []byte) bool {
	recordBytes := int64(len(encoded) + 1)
	maximumBytes := current.manager.cfg.MaxEventSize
	if recordBytes > maximumBytes {
		return false
	}
	continuous := (current.eventLastSequence == 0 && event.Sequence == 1) || event.Sequence == current.eventLastSequence+1
	if continuous && current.eventBytes+recordBytes <= maximumBytes {
		file, err := os.OpenFile(current.events, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return false
		}
		written, writeErr := file.Write(append(encoded, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil || written != len(encoded)+1 {
			return false
		}
		current.eventBytes += recordBytes
		current.eventLastSequence = event.Sequence
		return true
	}

	eventBytes, lastSequence, err := rewriteRetainedEventLog(
		current.events, current.metadata.ID, event, maximumBytes, current.metadata.LogTruncated,
	)
	if err != nil {
		return false
	}
	current.eventBytes = eventBytes
	current.eventLastSequence = lastSequence
	current.metadata.LogTruncated = true
	return true
}

func workerModelSelection(provider, id string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "unknown") || strings.EqualFold(strings.TrimSpace(id), "unknown") {
		return ""
	}
	return joinModel(provider, id)
}

func validPersistedModel(value string) string {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return workerModelSelection(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
}

func (current *task) bindPendingSessionLocked() bool {
	if current.pendingSessionFile == "" || current.pendingSessionID == "" {
		return false
	}
	physical, err := validateSessionFile(current.manager.cfg.SessionDir, current.pendingSessionFile)
	if err != nil {
		return false
	}
	current.metadata.SessionFile = physical
	current.metadata.SessionID = current.pendingSessionID
	current.pendingSessionFile = ""
	current.pendingSessionID = ""
	return true
}

func (current *task) upsertApprovalLocked(approval pendingApproval) {
	for index := range current.metadata.Approvals {
		if current.metadata.Approvals[index].ID == approval.ID {
			current.metadata.Approvals[index] = approval
			return
		}
	}
	if len(current.metadata.Approvals) >= maximumPendingApprovals {
		current.metadata.Approvals = current.metadata.Approvals[1:]
	}
	current.metadata.Approvals = append(current.metadata.Approvals, approval)
}

func (current *task) sendCommand(command json.RawMessage) error {
	return current.sendCommandWithPromptEvent(command, true)
}

func (current *task) sendCommandWithPromptEvent(command json.RawMessage, recordPrompt bool) error {
	if len(command) == 0 || len(command) > maxRequestBytes || !json.Valid(command) {
		return fmt.Errorf("worker command must be valid JSON no larger than %d bytes", maxRequestBytes)
	}
	var header struct {
		Type     string         `json:"type"`
		ID       string         `json:"id"`
		Message  string         `json:"message"`
		Provider string         `json:"provider"`
		ModelID  string         `json:"modelId"`
		Images   []imageContent `json:"images"`
	}
	if err := json.Unmarshal(command, &header); err != nil || header.Type == "" {
		return fmt.Errorf("worker command must contain a type")
	}
	current.mu.Lock()
	stdin := current.stdin
	status := current.metadata.Status
	model := current.metadata.Model
	current.mu.Unlock()
	if stdin == nil || !isLiveStatus(status) {
		return fmt.Errorf("task worker is not running")
	}
	switch header.Type {
	case "prompt":
		if len(header.Message) == 0 || len(header.Message) > maxPromptBytes {
			return fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
		}
		if status != statusStarting && status != statusIdle {
			return fmt.Errorf("task must be idle before accepting another prompt")
		}
		if err := current.manager.validateImagesForModel(model, header.Images); err != nil {
			return err
		}
	case "abort":
	case "set_model":
		if status != statusIdle {
			return fmt.Errorf("task must be idle before changing models")
		}
		if !modelProviderPattern.MatchString(header.Provider) || !modelIDPattern.MatchString(header.ModelID) {
			return fmt.Errorf("model provider and ID are invalid")
		}
	case "extension_ui_response":
		if header.ID == "" || status != statusWaiting || !current.hasActiveApproval(header.ID) {
			return fmt.Errorf("task is not waiting for an approval response")
		}
	default:
		return fmt.Errorf("unsupported worker command: %s", header.Type)
	}
	promptCwd := ""
	if header.Type == "prompt" {
		current.turnCaptureMu.Lock()
		defer current.turnCaptureMu.Unlock()
		current.mu.Lock()
		if current.metadata.Status != statusStarting && current.metadata.Status != statusIdle {
			current.mu.Unlock()
			return fmt.Errorf("task must be idle before accepting another prompt")
		}
		current.metadata.Status = statusRunning
		current.metadata.CurrentActivity = "thinking"
		current.metadata.UpdatedAt = time.Now().UTC()
		beginTurnEvidenceLocked(&current.metadata, nil)
		current.openToolCalls = nil
		current.openAnonymousTools = 0
		current.terminalCaptureUnsafe = false
		turn := current.currentRunningTurnLocked()
		promptCwd = current.metadata.Cwd
		current.mu.Unlock()
		_ = current.saveMetadata()
		if recordPrompt {
			attachments := make([]map[string]string, 0, len(header.Images))
			for _, image := range header.Images {
				attachments = append(attachments, map[string]string{"name": image.Name, "mimeType": image.MimeType})
			}
			promptEvent, _ := json.Marshal(map[string]any{"type": "hobot_user_prompt", "message": header.Message, "attachments": attachments})
			current.recordEvent(promptEvent)
		}
		current.applyTurnWorkspaceEvidence(turn, true, captureTurnWorkspaceEvidence(promptCwd))
		workerCommand := map[string]any{}
		if err := json.Unmarshal(command, &workerCommand); err != nil {
			return fmt.Errorf("decode prompt command: %w", err)
		}
		workerCommand["images"] = imagePayload(header.Images)
		command, _ = json.Marshal(workerCommand)
	}
	if err := current.writeWorkerCommand(command); err != nil {
		if header.Type == "prompt" {
			after := captureTurnWorkspaceEvidence(promptCwd)
			current.mu.Lock()
			if current.metadata.Status == statusRunning {
				current.metadata.Status = status
				current.metadata.UpdatedAt = time.Now().UTC()
			}
			finalizeTurnEvidenceLocked(&current.metadata, "failed", after, current.metadata.LastSequence)
			current.mu.Unlock()
			_ = current.saveMetadata()
		}
		return err
	}
	if header.Type == "extension_ui_response" {
		current.mu.Lock()
		for index := range current.metadata.Approvals {
			if current.metadata.Approvals[index].ID == header.ID {
				current.metadata.Approvals[index].Active = false
			}
		}
		if current.metadata.Status == statusWaiting && !current.hasActiveApprovalLocked("") {
			current.metadata.Status = statusRunning
			current.metadata.CurrentActivity = "thinking"
			current.metadata.UpdatedAt = time.Now().UTC()
		}
		current.mu.Unlock()
		_ = current.saveMetadata()
		resolved, _ := json.Marshal(map[string]any{"type": "hobot_approval_resolved", "id": header.ID})
		current.recordEvent(resolved)
	}
	return nil
}

func (current *task) writeWorkerCommand(command json.RawMessage) error {
	current.mu.Lock()
	stdin := current.stdin
	current.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("task worker is not running")
	}
	current.writeMu.Lock()
	defer current.writeMu.Unlock()
	_, err := stdin.Write(append(append([]byte(nil), command...), '\n'))
	return err
}

func (current *task) hasActiveApproval(id string) bool {
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.hasActiveApprovalLocked(id)
}

func (current *task) hasActiveApprovalLocked(id string) bool {
	now := time.Now()
	for index := range current.metadata.Approvals {
		approval := &current.metadata.Approvals[index]
		if approval.Active && approval.TimeoutMS > 0 && now.After(approval.RequestedAt.Add(time.Duration(approval.TimeoutMS)*time.Millisecond)) {
			approval.Active = false
		}
		if approval.Active && (id == "" || approval.ID == id) {
			return true
		}
	}
	return false
}

func (current *task) stop() error {
	current.mu.Lock()
	if current.metadata.Status == statusQueued {
		queuedAt := current.metadata.QueuedAt
		operation := current.metadata.QueueOperation
		current.pendingLaunch = nil
		current.metadata.QueuedAt = nil
		current.metadata.QueueOperation = ""
		current.mu.Unlock()
		_ = os.Remove(current.queuePath())
		raw, _ := json.Marshal(map[string]any{"type": "hobot_task_queue_cancelled", "queuedAt": queuedAt, "operation": operation})
		current.recordEvent(raw)
		current.setTerminal(statusStopped, "")
		return nil
	}
	pid := current.metadata.PID
	workerDone := current.workerDone
	if !isLiveStatus(current.metadata.Status) && pid <= 0 {
		current.mu.Unlock()
		return nil
	}
	current.stopping = true
	current.metadata.Status = statusStopping
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	_ = current.saveMetadata()
	if pid <= 0 {
		current.setTerminal(statusStopped, "")
		return nil
	}
	if err := terminateProcessGroup(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		if !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	return current.waitForWorkerExit(pid, workerDone)
}

func (current *task) stopIfIdle() (bool, error) {
	current.mu.Lock()
	if current.metadata.Status != statusIdle {
		current.mu.Unlock()
		return false, nil
	}
	current.stopping = true
	current.metadata.Status = statusStopping
	current.metadata.UpdatedAt = time.Now().UTC()
	pid := current.metadata.PID
	workerDone := current.workerDone
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		return false, err
	}
	if pid <= 0 {
		current.setTerminal(statusStopped, "")
		return true, nil
	}
	if err := terminateProcessGroup(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		if !errors.Is(err, syscall.ESRCH) {
			return false, err
		}
	}
	return true, current.waitForWorkerExit(pid, workerDone)
}

func (current *task) waitForWorkerExit(pid int, workerDone <-chan struct{}) error {
	if waitForWorkerDone(workerDone, 5*time.Second) {
		return nil
	}
	if err := terminateProcessGroup(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if waitForWorkerDone(workerDone, time.Second) {
		return nil
	}
	return fmt.Errorf("timed out waiting for task worker %d to exit", pid)
}

func waitForWorkerDone(workerDone <-chan struct{}, timeout time.Duration) bool {
	if workerDone == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-workerDone:
		return true
	case <-timer.C:
		return false
	}
}

func terminateProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, signal)
}

func (current *task) setTerminal(status taskStatus, message string) {
	current.mu.Lock()
	if isTerminalStatus(current.metadata.Status) {
		current.mu.Unlock()
		return
	}
	current.metadata.Status = status
	current.metadata.CurrentActivity = ""
	current.metadata.UpdatedAt = time.Now().UTC()
	removeQueuedLaunch := false
	if isTerminalStatus(status) {
		removeQueuedLaunch = current.pendingLaunch != nil || current.metadata.QueuedAt != nil
		current.pendingLaunch = nil
		current.metadata.QueuedAt = nil
		current.metadata.QueueOperation = ""
	}
	if status == statusStopped {
		current.metadata.LastError = ""
		current.metadata.Failure = nil
	} else if status == statusFailed || status == statusInterrupted {
		current.metadata.Failure = classifyTaskFailure(status, message, current.metadata.SessionFile != "")
		current.metadata.LastError = current.metadata.Failure.Message
	}
	for index := range current.metadata.Approvals {
		current.metadata.Approvals[index].Active = false
	}
	failure := current.metadata.Failure
	current.mu.Unlock()
	if status == statusFailed || status == statusInterrupted {
		current.appendFailureDetail(message)
	}
	event := map[string]any{"type": "hobot_task_" + string(status)}
	if failure != nil {
		event["code"] = failure.Code
		event["message"] = failure.Message
		event["recovery"] = failure.Recovery
	}
	raw, _ := json.Marshal(event)
	current.recordEvent(raw)
	current.mu.Lock()
	for id, subscriber := range current.subscribers {
		close(subscriber)
		delete(current.subscribers, id)
	}
	current.mu.Unlock()
	if removeQueuedLaunch {
		_ = os.Remove(current.queuePath())
	}
}

func (current *task) appendFailureDetail(detail string) {
	detail = strings.TrimSpace(detail)
	if detail == "" || current.stderr == "" {
		return
	}
	detail = boundedValue(detail, maximumAssistantErrorText)
	prefix := "[agentd failure " + time.Now().UTC().Format(time.RFC3339) + "] "
	current.stderrMu.Lock()
	defer current.stderrMu.Unlock()
	remaining := maximumWorkerStderrBytes - current.stderrBytes
	maximumDetail := remaining - int64(len(prefix)+1)
	if maximumDetail <= 0 {
		return
	}
	if int64(len(detail)) > maximumDetail {
		detail = boundedValue(detail, int(maximumDetail))
	}
	file, err := os.OpenFile(current.stderr, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	written, _ := fmt.Fprintf(file, "%s%s\n", prefix, detail)
	current.stderrBytes += int64(written)
	_ = file.Close()
}

func classifyTaskFailure(status taskStatus, detail string, hasSession bool) *taskFailure {
	normalized := strings.ToLower(detail)
	switch {
	case strings.Contains(normalized, "queued task handoff") || strings.Contains(normalized, "duplicate execution"):
		return taskFailureFor("handoff-uncertain", hasSession)
	case status == statusInterrupted:
		return taskFailureFor("service-restarted", hasSession)
	case strings.Contains(normalized, "validate task workspace") || strings.Contains(normalized, "isolated workspace"):
		return taskFailureFor("workspace-unavailable", hasSession)
	case strings.Contains(normalized, "persist") || strings.Contains(normalized, "metadata") || strings.Contains(normalized, "queue"):
		return taskFailureFor("state-persistence-failed", hasSession)
	case strings.Contains(normalized, "unsupported model") || strings.Contains(normalized, "authentication") || strings.Contains(normalized, "unauthorized") || strings.Contains(normalized, "forbidden") || strings.Contains(normalized, "gateway") || strings.Contains(normalized, "message_stop") || strings.Contains(normalized, "stream ended") || strings.Contains(normalized, "http 400") || strings.Contains(normalized, "http 401") || strings.Contains(normalized, "http 403") || strings.Contains(normalized, "http 429") || strings.Contains(normalized, "invalidparameter") || strings.Contains(normalized, "rate limit"):
		return taskFailureFor("model-unavailable", hasSession)
	case strings.Contains(normalized, "invalid json") || strings.Contains(normalized, "event stream") || strings.Contains(normalized, "worker emitted"):
		return taskFailureFor("worker-protocol-failed", hasSession)
	default:
		return taskFailureFor("worker-exited", hasSession)
	}
}

func normalizeTaskFailure(status taskStatus, existing *taskFailure, legacyDetail string, hasSession bool) *taskFailure {
	if existing != nil {
		for _, code := range []string{"queue-recovery-failed", "handoff-uncertain", "service-restarted", "workspace-unavailable", "state-persistence-failed", "model-unavailable", "worker-protocol-failed", "worker-exited"} {
			if existing.Code == code {
				return taskFailureFor(code, hasSession)
			}
		}
	}
	return classifyTaskFailure(status, legacyDetail, hasSession)
}

func taskFailureFor(code string, hasSession bool) *taskFailure {
	recovery := "restart"
	if hasSession {
		recovery = "resume"
	}
	switch code {
	case "queue-recovery-failed":
		return &taskFailure{Code: code, Message: "Queued work could not be recovered safely.", Recovery: "restart"}
	case "handoff-uncertain":
		return &taskFailure{Code: code, Message: "The board service restarted while this task was starting. Review the last output before continuing.", Recovery: recovery}
	case "service-restarted":
		return &taskFailure{Code: code, Message: "This task was interrupted when the board service stopped or restarted.", Recovery: recovery}
	case "workspace-unavailable":
		return &taskFailure{Code: code, Message: "This task's isolated workspace is unavailable or no longer trusted.", Recovery: "diagnose"}
	case "state-persistence-failed":
		return &taskFailure{Code: code, Message: "Hobot Code could not save this task state safely.", Recovery: "diagnose"}
	case "model-unavailable":
		return &taskFailure{Code: code, Message: "The selected model route could not complete this task.", Recovery: "check-model"}
	case "worker-protocol-failed":
		return &taskFailure{Code: code, Message: "The Agent worker returned an invalid response.", Recovery: "diagnose"}
	default:
		return &taskFailure{Code: "worker-exited", Message: "The Agent worker exited before the task completed.", Recovery: recovery}
	}
}

func (current *task) snapshot() taskMetadata {
	current.mu.Lock()
	defer current.mu.Unlock()
	return cloneMetadata(current.metadata)
}

func (current *task) saveMetadata() error {
	current.persistMu.Lock()
	defer current.persistMu.Unlock()
	current.mu.Lock()
	metadata := cloneMetadata(current.metadata)
	current.mu.Unlock()
	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(current.dir, ".metadata.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(current.dir, "metadata.json"))
}

func (manager *taskManager) get(id string) (*task, error) {
	manager.mu.RLock()
	current := manager.tasks[id]
	manager.mu.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("task does not exist: %s", id)
	}
	return current, nil
}

func (manager *taskManager) rootTaskID(metadata taskMetadata) string {
	rootID := metadata.ID
	parentID := metadata.ParentTaskID
	seen := map[string]bool{rootID: true}
	for parentID != "" && !seen[parentID] {
		seen[parentID] = true
		parent, err := manager.get(parentID)
		if err != nil {
			break
		}
		metadata = parent.snapshot()
		rootID = metadata.ID
		parentID = metadata.ParentTaskID
	}
	return rootID
}

func (manager *taskManager) setModel(params setTaskModelParams) (taskMetadata, error) {
	if !modelProviderPattern.MatchString(params.Provider) || !modelIDPattern.MatchString(params.ModelID) {
		return taskMetadata{}, fmt.Errorf("model provider and ID are invalid")
	}
	model := joinModel(params.Provider, params.ModelID)
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	if err := manager.validateNetworkModel(current.snapshot().NetworkMode, model); err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	status := current.metadata.Status
	if isTerminalStatus(status) || status == statusQueued {
		current.metadata.Model = model
		current.metadata.UpdatedAt = time.Now().UTC()
		metadata := current.metadata
		current.mu.Unlock()
		if err := current.saveMetadata(); err != nil {
			return taskMetadata{}, err
		}
		return metadata, nil
	}
	current.mu.Unlock()
	if status != statusIdle {
		return taskMetadata{}, fmt.Errorf("task must be idle or stopped before changing models")
	}
	command, _ := json.Marshal(map[string]string{
		"id": fmt.Sprintf("agentd-model-%d", time.Now().UnixNano()), "type": "set_model",
		"provider": params.Provider, "modelId": params.ModelID,
	})
	if err := current.sendCommand(command); err != nil {
		return taskMetadata{}, err
	}
	return current.snapshot(), nil
}

func (manager *taskManager) setPermissionMode(params setTaskPermissionParams) (taskMetadata, error) {
	mode, err := normalizePermissionMode(params.Mode)
	if err != nil {
		return taskMetadata{}, err
	}
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	if current.metadata.Status != statusIdle && current.metadata.Status != statusQueued && !isTerminalStatus(current.metadata.Status) {
		current.mu.Unlock()
		return taskMetadata{}, fmt.Errorf("task must be idle or stopped before changing permissions")
	}
	previousMode := current.metadata.PermissionMode
	previousUpdatedAt := current.metadata.UpdatedAt
	current.metadata.PermissionMode = mode
	current.metadata.UpdatedAt = time.Now().UTC()
	metadata := current.metadata
	current.mu.Unlock()
	if err := current.writePermissionPolicy(mode); err != nil {
		current.mu.Lock()
		current.metadata.PermissionMode = previousMode
		current.metadata.UpdatedAt = previousUpdatedAt
		current.mu.Unlock()
		return taskMetadata{}, err
	}
	if err := current.saveMetadata(); err != nil {
		current.mu.Lock()
		current.metadata.PermissionMode = previousMode
		current.metadata.UpdatedAt = previousUpdatedAt
		current.mu.Unlock()
		if restoreErr := current.writePermissionPolicy(previousMode); restoreErr != nil {
			return taskMetadata{}, fmt.Errorf("save task metadata: %w; restore permission policy: %v", err, restoreErr)
		}
		return taskMetadata{}, err
	}
	return metadata, nil
}

func (manager *taskManager) setSandboxMode(params setTaskSandboxParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	if current.metadata.Status != statusQueued && !isTerminalStatus(current.metadata.Status) {
		current.mu.Unlock()
		return taskMetadata{}, fmt.Errorf("task must be queued or stopped before changing its OS sandbox")
	}
	permissionMode := current.metadata.PermissionMode
	deployment := current.metadata.Deployment != nil
	networkMode := current.metadata.NetworkMode
	current.mu.Unlock()
	mode, sandbox, err := manager.resolveTaskSandbox(params.Mode, permissionMode, deployment)
	if err != nil {
		return taskMetadata{}, err
	}
	networkMode, sandbox, err = manager.resolveTaskNetworkMode(networkMode, mode, sandbox)
	if err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	previousMode := current.metadata.SandboxMode
	previousStatus := current.metadata.Sandbox
	previousUpdatedAt := current.metadata.UpdatedAt
	current.metadata.SandboxMode = mode
	current.metadata.NetworkMode = networkMode
	current.metadata.Sandbox = sandbox
	current.metadata.UpdatedAt = time.Now().UTC()
	metadata := current.metadata
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		current.mu.Lock()
		current.metadata.SandboxMode = previousMode
		current.metadata.Sandbox = previousStatus
		current.metadata.UpdatedAt = previousUpdatedAt
		current.mu.Unlock()
		return taskMetadata{}, err
	}
	return metadata, nil
}

func (manager *taskManager) setNetworkMode(params setTaskNetworkParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	if current.metadata.Status != statusQueued && !isTerminalStatus(current.metadata.Status) {
		current.mu.Unlock()
		return taskMetadata{}, fmt.Errorf("task must be queued or stopped before changing its network boundary")
	}
	sandboxMode := current.metadata.SandboxMode
	sandbox := current.metadata.Sandbox
	model := current.metadata.Model
	current.mu.Unlock()
	mode, sandbox, err := manager.resolveTaskNetworkMode(params.Mode, sandboxMode, sandbox)
	if err != nil {
		return taskMetadata{}, err
	}
	if err := manager.validateNetworkModel(mode, model); err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	previousMode := current.metadata.NetworkMode
	previousStatus := current.metadata.Sandbox
	previousUpdatedAt := current.metadata.UpdatedAt
	current.metadata.NetworkMode = mode
	current.metadata.Sandbox = sandbox
	current.metadata.UpdatedAt = time.Now().UTC()
	metadata := current.metadata
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		current.mu.Lock()
		current.metadata.NetworkMode = previousMode
		current.metadata.Sandbox = previousStatus
		current.metadata.UpdatedAt = previousUpdatedAt
		current.mu.Unlock()
		return taskMetadata{}, err
	}
	return metadata, nil
}

func (manager *taskManager) list() []taskMetadata {
	manager.mu.RLock()
	result := make([]taskMetadata, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		metadata := current.summary()
		if metadata.ArchivedAt == nil {
			result = append(result, metadata)
		}
	}
	manager.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (current *task) summary() taskMetadata {
	metadata := current.snapshot()
	metadata.Approvals = nil
	return metadata
}

func cloneMetadata(metadata taskMetadata) taskMetadata {
	copy := metadata
	copy.Approvals = append([]pendingApproval(nil), metadata.Approvals...)
	for index := range copy.Approvals {
		copy.Approvals[index].Options = append([]string(nil), metadata.Approvals[index].Options...)
		if copy.Approvals[index].Active && copy.Approvals[index].TimeoutMS > 0 &&
			time.Now().After(copy.Approvals[index].RequestedAt.Add(time.Duration(copy.Approvals[index].TimeoutMS)*time.Millisecond)) {
			copy.Approvals[index].Active = false
		}
	}
	if metadata.ArchivedAt != nil {
		value := *metadata.ArchivedAt
		copy.ArchivedAt = &value
	}
	copy.TurnEvidence = append([]taskTurnEvidence(nil), metadata.TurnEvidence...)
	for index := range copy.TurnEvidence {
		if metadata.TurnEvidence[index].EndedAt != nil {
			value := *metadata.TurnEvidence[index].EndedAt
			copy.TurnEvidence[index].EndedAt = &value
		}
		if metadata.TurnEvidence[index].WorkspaceBefore != nil {
			value := *metadata.TurnEvidence[index].WorkspaceBefore
			copy.TurnEvidence[index].WorkspaceBefore = &value
		}
		if metadata.TurnEvidence[index].WorkspaceAfter != nil {
			value := *metadata.TurnEvidence[index].WorkspaceAfter
			copy.TurnEvidence[index].WorkspaceAfter = &value
		}
		if metadata.TurnEvidence[index].WorkspaceChanged != nil {
			value := *metadata.TurnEvidence[index].WorkspaceChanged
			copy.TurnEvidence[index].WorkspaceChanged = &value
		}
	}
	return copy
}

func (manager *taskManager) page(params pageTaskParams) (taskPage, error) {
	if params.Limit == 0 {
		params.Limit = 50
	}
	if params.Limit < 1 || params.Limit > 200 {
		return taskPage{}, fmt.Errorf("task page limit must be between 1 and 200")
	}
	manager.mu.RLock()
	all := make([]taskMetadata, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		metadata := current.summary()
		if params.IncludeArchived || metadata.ArchivedAt == nil {
			all = append(all, metadata)
		}
	}
	manager.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	start := 0
	if params.Cursor != "" {
		found := false
		for index := range all {
			if all[index].ID == params.Cursor {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			return taskPage{}, fmt.Errorf("task page cursor does not exist: %s", params.Cursor)
		}
	}
	end := start + params.Limit
	if end > len(all) {
		end = len(all)
	}
	page := taskPage{Tasks: all[start:end]}
	if end < len(all) && end > start {
		page.NextCursor = all[end-1].ID
	}
	return page, nil
}

func (manager *taskManager) rename(params renameTaskParams) (taskMetadata, error) {
	name, err := validateTaskName(params.Name)
	if err != nil {
		return taskMetadata{}, err
	}
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	current.metadata.Name = name
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		return taskMetadata{}, err
	}
	return current.snapshot(), nil
}

func (manager *taskManager) archive(params archiveTaskParams) (taskMetadata, error) {
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	if isLiveStatus(current.metadata.Status) {
		current.mu.Unlock()
		return taskMetadata{}, fmt.Errorf("stop the task before changing its archive state")
	}
	if params.Archive {
		now := time.Now().UTC()
		current.metadata.ArchivedAt = &now
	} else {
		current.metadata.ArchivedAt = nil
	}
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		return taskMetadata{}, err
	}
	return current.snapshot(), nil
}

func (manager *taskManager) delete(id string) error {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	current, err := manager.get(id)
	if err != nil {
		return err
	}
	current.mu.Lock()
	if isLiveStatus(current.metadata.Status) {
		current.mu.Unlock()
		return fmt.Errorf("stop the task before deleting it")
	}
	if current.metadata.ArchivedAt == nil {
		current.mu.Unlock()
		return fmt.Errorf("archive the task before deleting it")
	}
	current.mu.Unlock()
	if err := validateTaskDirectory(manager.cfg.TasksRoot, current.dir, id); err != nil {
		return err
	}
	manager.mu.Lock()
	delete(manager.tasks, id)
	manager.mu.Unlock()
	if err := os.RemoveAll(current.dir); err != nil {
		manager.mu.Lock()
		manager.tasks[id] = current
		manager.mu.Unlock()
		return err
	}
	_ = os.RemoveAll(filepath.Join(manager.cfg.SessionDir, id))
	return nil
}

func validateTaskDirectory(root, dir, id string) error {
	expected := filepath.Join(root, id)
	if dir != expected || !taskIDPattern.MatchString(id) {
		return fmt.Errorf("refusing to delete an invalid task directory")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("task directory is not a real directory: %s", dir)
	}
	return nil
}

func (manager *taskManager) resume(params resumeTaskParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if len(params.Prompt) > maxPromptBytes {
		return taskMetadata{}, fmt.Errorf("task prompt must contain at most %d bytes", maxPromptBytes)
	}
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	metadata := current.snapshot()
	if err := manager.validateImagesForModel(metadata.Model, params.Images); err != nil {
		return taskMetadata{}, err
	}
	if isLiveStatus(metadata.Status) {
		return taskMetadata{}, fmt.Errorf("task is already running")
	}
	if metadata.ArchivedAt != nil {
		return taskMetadata{}, fmt.Errorf("unarchive the task before resuming it")
	}
	sessionFile, err := validateSessionFile(manager.cfg.SessionDir, metadata.SessionFile)
	if err != nil {
		return taskMetadata{}, err
	}
	available, err := manager.claimSubmissionSlot()
	if err != nil {
		return taskMetadata{}, err
	}
	if !available {
		current.mu.Lock()
		current.metadata.ResumeCount++
		current.mu.Unlock()
		queued := queuedLaunch{Operation: "resume", Prompt: params.Prompt, Images: params.Images, Approve: metadata.Approved, SessionFile: sessionFile, QueuedAt: time.Now().UTC()}
		if err := current.queue(queued); err != nil {
			return taskMetadata{}, err
		}
		return current.snapshot(), nil
	}
	current.mu.Lock()
	current.metadata.Status = statusStarting
	current.metadata.PID = 0
	current.metadata.LastError = ""
	current.metadata.Failure = nil
	current.metadata.AwaitingPrompt = false
	current.metadata.ResumeCount++
	for index := range current.metadata.Approvals {
		current.metadata.Approvals[index].Active = false
	}
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		current.setTerminal(statusFailed, "persist resumed task: "+err.Error())
		return current.snapshot(), err
	}
	if err := current.launch(params.Prompt, params.Images, metadata.Approved, sessionFile, false); err != nil {
		if errors.Is(err, errTaskLaunchCancelled) {
			current.setTerminal(statusStopped, "")
			return current.snapshot(), nil
		}
		current.setTerminal(statusFailed, err.Error())
		return current.snapshot(), err
	}
	return current.snapshot(), nil
}

func (manager *taskManager) restart(params resumeTaskParams) (taskMetadata, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if len(params.Prompt) == 0 || len(params.Prompt) > maxPromptBytes {
		return taskMetadata{}, fmt.Errorf("task prompt must contain 1 to %d bytes", maxPromptBytes)
	}
	current, err := manager.get(params.TaskID)
	if err != nil {
		return taskMetadata{}, err
	}
	metadata := current.snapshot()
	if err := manager.validateImagesForModel(metadata.Model, params.Images); err != nil {
		return taskMetadata{}, err
	}
	if isLiveStatus(metadata.Status) {
		return taskMetadata{}, fmt.Errorf("task is already running")
	}
	if metadata.ArchivedAt != nil {
		return taskMetadata{}, fmt.Errorf("unarchive the task before restarting it")
	}
	available, err := manager.claimSubmissionSlot()
	if err != nil {
		return taskMetadata{}, err
	}
	if !available {
		current.mu.Lock()
		current.metadata.RestartCount++
		current.mu.Unlock()
		queued := queuedLaunch{Operation: "restart", Prompt: params.Prompt, Images: params.Images, Approve: metadata.Approved, QueuedAt: time.Now().UTC()}
		if err := current.queue(queued); err != nil {
			return taskMetadata{}, err
		}
		return current.snapshot(), nil
	}
	current.mu.Lock()
	current.metadata.Status = statusStarting
	current.metadata.PID = 0
	current.metadata.LastError = ""
	current.metadata.Failure = nil
	current.metadata.SessionFile = ""
	current.metadata.SessionID = ""
	current.metadata.AwaitingPrompt = false
	current.pendingSessionFile = ""
	current.pendingSessionID = ""
	current.metadata.RestartCount++
	for index := range current.metadata.Approvals {
		current.metadata.Approvals[index].Active = false
	}
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		current.setTerminal(statusFailed, "persist restarted task: "+err.Error())
		return current.snapshot(), err
	}
	if err := current.launch(params.Prompt, params.Images, metadata.Approved, "", false); err != nil {
		if errors.Is(err, errTaskLaunchCancelled) {
			current.setTerminal(statusStopped, "")
			return current.snapshot(), nil
		}
		current.setTerminal(statusFailed, err.Error())
		return current.snapshot(), err
	}
	return current.snapshot(), nil
}

func validateSessionFile(sessionRoot, value string) (string, error) {
	if !filepath.IsAbs(value) || value == "" {
		return "", fmt.Errorf("task has no resumable Hobot Code session")
	}
	physicalRoot, err := filepath.EvalSymlinks(sessionRoot)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", fmt.Errorf("Hobot Code session is unavailable: %w", err)
	}
	relative, err := filepath.Rel(physicalRoot, physical)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Hobot Code session is outside the configured session directory")
	}
	if _, err := privateRegularFileInfo(physical, maxRequestBytes*32); err != nil {
		return "", fmt.Errorf("Hobot Code session is unsafe: %w", err)
	}
	return physical, nil
}

func (current *task) subscribe(after uint64, follow bool) (eventPage, <-chan taskEvent, func(), error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	page, err := readEventPageWithRetention(current.events, current.metadata.ID, after, 0, current.metadata.LogTruncated)
	if err != nil {
		return eventPage{}, nil, nil, err
	}
	page.RetainedThrough = current.eventLastSequence
	page.LatestSequence = current.metadata.LastSequence
	page.HistoryTruncated = current.metadata.LogTruncated || page.RetainedFrom > 1
	if !follow || !isLiveStatus(current.metadata.Status) {
		return page, nil, func() {}, nil
	}
	current.nextSubID++
	id := current.nextSubID
	channel := make(chan taskEvent, 128)
	current.subscribers[id] = channel
	cancel := func() {
		current.mu.Lock()
		if registered, ok := current.subscribers[id]; ok {
			close(registered)
			delete(current.subscribers, id)
		}
		current.mu.Unlock()
	}
	return page, channel, cancel, nil
}

func (current *task) eventPage(after uint64, limit int) (eventPage, error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	page, err := readEventPageWithRetention(current.events, current.metadata.ID, after, limit, current.metadata.LogTruncated)
	if err != nil {
		return eventPage{}, err
	}
	page.RetainedThrough = current.eventLastSequence
	page.LatestSequence = current.metadata.LastSequence
	page.HistoryTruncated = current.metadata.LogTruncated || page.RetainedFrom > 1
	return page, nil
}

func readEvents(path, taskID string, after uint64) ([]taskEvent, error) {
	page, err := readEventPage(path, taskID, after, 0)
	return page.Events, err
}

// recoverEventLog makes the append-only event log authoritative after a daemon
// restart. Metadata is intentionally saved less often than streaming deltas, so
// its lastSequence can lag the durable log. Older daemons could then append a
// second, otherwise contiguous sequence epoch. Renumber only that recognizable
// rollback suffix; gaps and malformed envelopes remain hard failures.
func recoverEventLog(path, taskID string, maximumBytes int64, allowRetainedStart bool) (int64, uint64, bool, error) {
	info, err := privateRegularFileInfo(path, maximumBytes)
	if err != nil {
		return 0, 0, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer file.Close()

	events := make([]taskEvent, 0)
	originalSequence := uint64(0)
	fixedSequence := uint64(0)
	rollback := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxEventRecordBytes)
	for scanner.Scan() {
		var event taskEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return 0, 0, false, fmt.Errorf("corrupt task event log: %w", err)
		}
		if event.Protocol != protocolVersion || event.Kind != "event" || event.TaskID != taskID || event.Sequence == 0 {
			return 0, 0, false, fmt.Errorf("corrupt task event envelope at sequence %d", event.Sequence)
		}
		if fixedSequence == 0 && event.Sequence != 1 {
			if !allowRetainedStart {
				return 0, 0, false, fmt.Errorf("corrupt task event envelope at sequence %d", event.Sequence)
			}
			fixedSequence = event.Sequence - 1
			originalSequence = fixedSequence
		}
		if !rollback {
			switch {
			case event.Sequence == fixedSequence+1:
				originalSequence = event.Sequence
			case event.Sequence <= fixedSequence:
				rollback = true
				originalSequence = event.Sequence
				event.Sequence = fixedSequence + 1
			default:
				return 0, 0, false, fmt.Errorf("corrupt task event envelope at sequence %d", event.Sequence)
			}
		} else {
			if event.Sequence != originalSequence+1 {
				return 0, 0, false, fmt.Errorf("corrupt task event rollback suffix at sequence %d", event.Sequence)
			}
			originalSequence = event.Sequence
			event.Sequence = fixedSequence + 1
		}
		fixedSequence = event.Sequence
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, false, err
	}
	if !rollback {
		return info.Size(), fixedSequence, false, nil
	}
	if err := file.Close(); err != nil {
		return 0, 0, false, err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".repair-")
	if err != nil {
		return 0, 0, false, err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return 0, 0, false, err
	}
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}
	written := int64(0)
	writer := bufio.NewWriter(temporary)
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			cleanup()
			return 0, 0, false, err
		}
		encoded = append(encoded, '\n')
		count, err := writer.Write(encoded)
		written += int64(count)
		if err != nil || written > maximumBytes {
			cleanup()
			if err != nil {
				return 0, 0, false, err
			}
			return 0, 0, false, fmt.Errorf("repaired event log exceeds %d bytes", maximumBytes)
		}
	}
	if err := writer.Flush(); err != nil {
		cleanup()
		return 0, 0, false, err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return 0, 0, false, err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporary.Name())
		return 0, 0, false, err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		_ = os.Remove(temporary.Name())
		return 0, 0, false, err
	}
	return written, fixedSequence, true, nil
}

func rewriteRetainedEventLog(path, taskID string, next taskEvent, maximumBytes int64, allowRetainedStart bool) (int64, uint64, error) {
	if _, err := privateRegularFileInfo(path, maximumBytes); err != nil {
		return 0, 0, err
	}
	page, err := readEventPageWithRetention(path, taskID, 0, 0, allowRetainedStart)
	if err != nil {
		return 0, 0, err
	}
	events := page.Events
	if len(events) == 0 {
		if next.Sequence != 1 && !allowRetainedStart {
			return 0, 0, fmt.Errorf("event sequence %d does not follow an empty log", next.Sequence)
		}
	} else if events[len(events)-1].Sequence+1 != next.Sequence {
		if !allowRetainedStart {
			return 0, 0, fmt.Errorf("event sequence %d does not follow retained sequence %d", next.Sequence, events[len(events)-1].Sequence)
		}
		events = nil
	}
	events = append(events, next)

	records := make([][]byte, len(events))
	for index, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return 0, 0, err
		}
		records[index] = append(encoded, '\n')
	}
	if int64(len(records[len(records)-1])) > maximumBytes {
		return 0, 0, fmt.Errorf("event record exceeds the %d byte retention limit", maximumBytes)
	}
	targetBytes := maximumBytes * 3 / 4
	if targetBytes <= 0 {
		targetBytes = maximumBytes
	}
	start := len(records) - 1
	used := int64(len(records[start]))
	for index := start - 1; index >= 0; index-- {
		candidate := int64(len(records[index]))
		if used+candidate > targetBytes {
			break
		}
		used += candidate
		start = index
	}
	// Prefer a complete recent user turn when one exists inside the retained
	// byte window. A single very long active turn still keeps its newest events.
	for index := start; index < len(events)-1; index++ {
		if events[index].Normalized != nil && events[index].Normalized.Type == "user.message" {
			start = index
			break
		}
	}

	var output bytes.Buffer
	for _, record := range records[start:] {
		output.Write(record)
	}
	if int64(output.Len()) > maximumBytes {
		return 0, 0, fmt.Errorf("retained event log exceeds %d bytes", maximumBytes)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".retained-")
	if err != nil {
		return 0, 0, err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return 0, 0, err
	}
	if _, err := temporary.Write(output.Bytes()); err != nil {
		cleanup()
		return 0, 0, err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return 0, 0, err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return 0, 0, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return 0, 0, err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return 0, 0, err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return 0, 0, syncErr
	}
	if closeErr != nil {
		return 0, 0, closeErr
	}
	return int64(output.Len()), next.Sequence, nil
}

func readEventPage(path, taskID string, after uint64, limit int) (eventPage, error) {
	return readEventPageWithRetention(path, taskID, after, limit, false)
}

func readEventPageWithRetention(path, taskID string, after uint64, limit int, allowRetainedStart bool) (eventPage, error) {
	if limit < 0 || limit > 1000 {
		return eventPage{}, fmt.Errorf("event page limit must be between 1 and 1000")
	}
	file, err := os.Open(path)
	if err != nil {
		return eventPage{}, err
	}
	defer file.Close()
	result := make([]taskEvent, 0)
	resultBytes := 0
	lastSequence := uint64(0)
	firstSequence := uint64(0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxEventRecordBytes)
	for scanner.Scan() {
		var event taskEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return eventPage{}, fmt.Errorf("corrupt task event log: %w", err)
		}
		validSequence := event.Sequence == lastSequence+1
		if firstSequence == 0 && allowRetainedStart {
			validSequence = event.Sequence > 0
		}
		if event.Protocol != protocolVersion || event.Kind != "event" || event.TaskID != taskID || !validSequence {
			return eventPage{}, fmt.Errorf("corrupt task event envelope at sequence %d", event.Sequence)
		}
		if firstSequence == 0 {
			firstSequence = event.Sequence
		}
		lastSequence = event.Sequence
		eventBytes := len(scanner.Bytes()) + 1
		pageBudgetReached := limit > 0 && len(result) > 0 && resultBytes+eventBytes > maxResponseBytes-64*1024
		if event.Sequence > after && !pageBudgetReached && (limit == 0 || len(result) < limit) {
			result = append(result, event)
			resultBytes += eventBytes
		} else if event.Sequence > after && limit > 0 {
			page := eventPage{
				Events: result, HasMore: true, RetainedFrom: firstSequence, RetainedThrough: lastSequence,
				HistoryTruncated: firstSequence > 1, CursorExpired: firstSequence > 1 && after < firstSequence-1,
			}
			if len(result) > 0 {
				page.NextAfter = result[len(result)-1].Sequence
			}
			return page, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return eventPage{}, err
	}
	page := eventPage{
		Events: result, RetainedFrom: firstSequence, RetainedThrough: lastSequence,
		HistoryTruncated: firstSequence > 1, CursorExpired: firstSequence > 1 && after < firstSequence-1,
	}
	if len(result) > 0 {
		page.NextAfter = result[len(result)-1].Sequence
	}
	return page, nil
}

func (manager *taskManager) interruptAll() {
	manager.mu.RLock()
	tasks := make([]struct {
		current *task
		pid     int
		live    bool
	}, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		current.mu.Lock()
		pid := current.metadata.PID
		live := occupiesActiveSlot(current.metadata.Status)
		current.mu.Unlock()
		tasks = append(tasks, struct {
			current *task
			pid     int
			live    bool
		}{current: current, pid: pid, live: live})
	}
	manager.mu.RUnlock()
	for index := range tasks {
		active := &tasks[index]
		current := active.current
		current.mu.Lock()
		live := occupiesActiveSlot(current.metadata.Status)
		active.live = live
		if live {
			current.interrupted = true
			current.metadata.PID = 0
			current.terminalCaptureUnsafe = true
		}
		current.mu.Unlock()
		if live {
			current.setTerminal(statusInterrupted, "agentd stopped; worker was not replayed")
			_ = terminateProcessGroup(active.pid, syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, active := range tasks {
		pid := active.pid
		if !active.live || pid <= 0 {
			continue
		}
		for time.Now().Before(deadline) {
			if err := syscall.Kill(-pid, 0); err != nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err := syscall.Kill(-pid, 0); err == nil {
			_ = terminateProcessGroup(pid, syscall.SIGKILL)
		}
	}
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request parameters: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("invalid request parameters: multiple JSON values")
		}
		return fmt.Errorf("invalid request parameters: %w", err)
	}
	return nil
}

func readPrivateRegularFile(path string, maximum int64) ([]byte, error) {
	_, err := privateRegularFileInfo(path, maximum)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func privateRegularFileInfo(path string, maximum int64) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, fmt.Errorf("private state file is not a bounded regular file: %s", path)
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return nil, fmt.Errorf("private state file is owned by uid %d, expected %d: %s", owner, os.Getuid(), path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private state file is accessible by another user: %s", path)
	}
	return info, nil
}
