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
	statusStarting    taskStatus = "starting"
	statusIdle        taskStatus = "idle"
	statusRunning     taskStatus = "running"
	statusWaiting     taskStatus = "waiting"
	statusStopping    taskStatus = "stopping"
	statusStopped     taskStatus = "stopped"
	statusFailed      taskStatus = "failed"
	statusInterrupted taskStatus = "interrupted"
)

var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

type taskMetadata struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Cwd            string            `json:"cwd"`
	Status         taskStatus        `json:"status"`
	PID            int               `json:"pid,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
	LastSequence   uint64            `json:"lastSequence"`
	LogTruncated   bool              `json:"logTruncated,omitempty"`
	LastError      string            `json:"lastError,omitempty"`
	SessionFile    string            `json:"sessionFile,omitempty"`
	SessionID      string            `json:"sessionId,omitempty"`
	Approved       bool              `json:"approved,omitempty"`
	ResumeCount    int               `json:"resumeCount,omitempty"`
	RestartCount   int               `json:"restartCount,omitempty"`
	Model          string            `json:"model,omitempty"`
	PermissionMode string            `json:"permissionMode,omitempty"`
	ParentTaskID   string            `json:"parentTaskId,omitempty"`
	ForkSequence   uint64            `json:"forkSequence,omitempty"`
	BranchKind     string            `json:"branchKind,omitempty"`
	AwaitingPrompt bool              `json:"awaitingPrompt,omitempty"`
	ArchivedAt     *time.Time        `json:"archivedAt,omitempty"`
	Approvals      []pendingApproval `json:"pendingApprovals,omitempty"`
	Deployment     *deploymentRecord `json:"deployment,omitempty"`
}

type task struct {
	manager *taskManager
	dir     string
	events  string
	stderr  string

	mu                 sync.Mutex
	metadata           taskMetadata
	command            *exec.Cmd
	stdin              io.WriteCloser
	workerDone         chan struct{}
	writeMu            sync.Mutex
	streamWG           sync.WaitGroup
	stopping           bool
	interrupted        bool
	eventBytes         int64
	subscribers        map[uint64]chan taskEvent
	nextSubID          uint64
	pendingSessionFile string
	pendingSessionID   string
}

type taskManager struct {
	cfg          config
	mu           sync.RWMutex
	startMu      sync.Mutex
	tasks        map[string]*task
	modelsOnce   sync.Once
	models       map[string]modelOption
	modelListErr error
}

type startTaskParams struct {
	Name           string            `json:"name,omitempty"`
	Cwd            string            `json:"cwd"`
	Prompt         string            `json:"prompt"`
	Images         []imageContent    `json:"images,omitempty"`
	Approve        bool              `json:"approve,omitempty"`
	Model          string            `json:"model,omitempty"`
	PermissionMode string            `json:"permissionMode,omitempty"`
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
	Events    []taskEvent `json:"events"`
	NextAfter uint64      `json:"nextAfter,omitempty"`
	HasMore   bool        `json:"hasMore"`
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
		legacyMetadataCleared := false
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
		if metadata.AwaitingPrompt && (metadata.Status != statusStopped || metadata.SessionFile == "") {
			metadata.AwaitingPrompt = false
			legacyMetadataCleared = true
		}
		if isLiveStatus(metadata.Status) {
			metadata.Status = statusInterrupted
			metadata.PID = 0
			metadata.UpdatedAt = time.Now().UTC()
			metadata.LastError = "agentd restarted; an in-flight worker was not replayed"
			for index := range metadata.Approvals {
				metadata.Approvals[index].Active = false
			}
		}
		current := &task{
			manager:     manager,
			dir:         dir,
			events:      filepath.Join(dir, "events.jsonl"),
			stderr:      filepath.Join(dir, "worker.stderr.log"),
			metadata:    metadata,
			subscribers: make(map[uint64]chan taskEvent),
		}
		eventBytes, lastSequence, repaired, err := recoverEventLog(current.events, metadata.ID, cfg.MaxEventSize)
		if err != nil {
			continue
		}
		current.eventBytes = eventBytes
		sequenceRecovered := !metadata.LogTruncated && metadata.LastSequence != lastSequence
		if !metadata.LogTruncated {
			metadata.LastSequence = lastSequence
			current.metadata.LastSequence = lastSequence
		}
		manager.tasks[metadata.ID] = current
		if metadata.Status == statusInterrupted || repaired || sequenceRecovered || legacyMetadataCleared {
			_ = current.saveMetadata()
		}
	}
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

func isLiveStatus(status taskStatus) bool {
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
		live := isLiveStatus(current.metadata.Status)
		current.mu.Unlock()
		if live {
			count++
		}
	}
	return count
}

func (manager *taskManager) ensureActiveSlot() error {
	if manager.activeCount() < manager.cfg.MaxTasks {
		return nil
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
			return err
		}
		if !stopped {
			continue
		}
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			if isTerminalStatus(candidate.snapshot().Status) && manager.activeCount() < manager.cfg.MaxTasks {
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return fmt.Errorf("timed out while suspending an idle background task")
	}
	return fmt.Errorf("background task limit reached (%d); all agents are currently working", manager.cfg.MaxTasks)
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
	if err := manager.ensureActiveSlot(); err != nil {
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
			ID: id, Name: name, Cwd: cwd, Status: statusStarting,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Approved: params.Approve,
			Model: normalizeModelSelection(params.Model), PermissionMode: permissionMode, Deployment: params.Deployment,
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
	if err := current.launch(params.Prompt, params.Images, params.Approve, ""); err != nil {
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
	sessionFile, err := validateSessionFile(manager.cfg.SessionDir, parent.SessionFile)
	if err != nil {
		return taskMetadata{}, err
	}
	_, lines, err := readSessionLines(sessionFile)
	if err != nil {
		return taskMetadata{}, fmt.Errorf("read source session: %w", err)
	}
	leafID := ""
	if params.Sequence > 0 {
		leafID, err = source.sessionLeafBeforePrompt(params.Sequence, lines)
		if err != nil {
			return taskMetadata{}, err
		}
	} else {
		leafID = safeSessionLeaf(lines)
		if leafID == "" {
			return taskMetadata{}, fmt.Errorf("source task has no settled context to fork")
		}
	}
	forkFile, err := writeSessionFork(manager.cfg.SessionDir, sessionFile, parent.Cwd, leafID, lines)
	if err != nil {
		return taskMetadata{}, fmt.Errorf("create session fork: %w", err)
	}
	keepSession := false
	defer func() {
		if !keepSession {
			_ = os.Remove(forkFile)
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
	if params.Kind == "edit" || params.Prompt != "" {
		if err := manager.ensureActiveSlot(); err != nil {
			return taskMetadata{}, err
		}
	}
	id, err := newTaskID()
	if err != nil {
		return taskMetadata{}, err
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
	parentTaskID := parent.ID
	if params.Kind == "side" {
		parentTaskID = manager.rootTaskID(parent)
	}
	status := statusStarting
	awaitingPrompt := false
	if params.Kind == "side" && params.Prompt == "" {
		status = statusStopped
		awaitingPrompt = true
	}
	current := &task{
		manager: manager,
		dir:     dir,
		events:  filepath.Join(dir, "events.jsonl"),
		stderr:  filepath.Join(dir, "worker.stderr.log"),
		metadata: taskMetadata{
			ID: id, Name: name, Cwd: parent.Cwd, Status: status,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Approved: parent.Approved,
			SessionFile: forkFile, Model: model, PermissionMode: permissionMode, ParentTaskID: parentTaskID,
			ForkSequence: params.Sequence, BranchKind: params.Kind, AwaitingPrompt: awaitingPrompt,
		},
		subscribers: make(map[uint64]chan taskEvent),
	}
	if params.Kind == "edit" {
		eventBytes, lastSequence, err := writeEditEventHistory(current.events, source.events, parent.ID, id, params.Sequence, manager.cfg.MaxEventSize)
		if err != nil {
			return taskMetadata{}, fmt.Errorf("copy edited conversation history: %w", err)
		}
		current.eventBytes = eventBytes
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
		if err := current.launch(params.Prompt, params.Images, parent.Approved, forkFile); err != nil {
			current.setTerminal(statusFailed, err.Error())
			return current.snapshot(), err
		}
	}
	return current.snapshot(), nil
}

// Editing creates a new session branch internally, but it remains the same
// conversation to the user. Preserve the visible timeline before the selected
// prompt and let launch() append the replacement prompt as the next event.
func writeEditEventHistory(targetPath, sourcePath, sourceTaskID, targetTaskID string, before uint64, maximumBytes int64) (int64, uint64, error) {
	events, err := readEvents(sourcePath, sourceTaskID, 0)
	if err != nil {
		return 0, 0, err
	}
	prefix := make([]taskEvent, 0)
	foundBoundary := false
	for _, event := range events {
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
	events, err := readEvents(current.events, current.metadata.ID, 0)
	if err != nil {
		return "", err
	}
	prompt := ""
	for _, event := range events {
		if event.Sequence == sequence {
			if event.Normalized == nil || event.Normalized.Type != "user.message" {
				break
			}
			prompt, _ = event.Normalized.Data["text"].(string)
			break
		}
	}
	if prompt == "" {
		return "", fmt.Errorf("fork sequence is not a user message: %d", sequence)
	}
	parentID := ""
	matches := 0
	for _, line := range lines {
		if line.Type == "message" && line.Role == "user" && line.Text == prompt {
			matches++
			parentID = line.ParentID
		}
	}
	if matches == 0 {
		return "", fmt.Errorf("selected message was not found in the current source session")
	}
	if matches > 1 {
		return "", fmt.Errorf("selected message is ambiguous in the current source session")
	}
	if parentID == "" {
		return "", fmt.Errorf("selected message has no parent context")
	}
	return parentID, nil
}

func (current *task) launch(prompt string, images []imageContent, approve bool, sessionFile string) error {
	if err := current.ensurePermissionPolicy(current.metadata.PermissionMode); err != nil {
		return fmt.Errorf("write task permission policy: %w", err)
	}
	args := []string{"--mode", "rpc", "--session-dir", current.manager.cfg.SessionDir, "--name", current.metadata.Name}
	if sessionFile != "" {
		args = append(args, "--session", sessionFile)
	}
	if current.metadata.Model != "" {
		args = append(args, "--model", current.metadata.Model)
	}
	if approve {
		args = append(args, "--approve")
	} else {
		args = append(args, "--no-approve")
	}
	command := exec.Command(current.manager.cfg.AgentBinary, args...)
	command.Dir = current.metadata.Cwd
	command.Env = append(os.Environ(),
		"HOBOT_CODE_BACKGROUND_TASK=1",
		"HOBOT_CODE_BACKGROUND_TASK_ID="+current.metadata.ID,
		"HOBOT_CODE_PERMISSION_POLICY="+current.permissionPolicyPath(),
	)
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
	if err := command.Start(); err != nil {
		return err
	}

	current.mu.Lock()
	current.command = command
	current.stdin = stdin
	current.workerDone = make(chan struct{})
	current.metadata.PID = command.Process.Pid
	current.metadata.UpdatedAt = time.Now().UTC()
	current.stopping = false
	current.interrupted = false
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
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
		return err
	}
	if prompt != "" {
		startCommand, _ := json.Marshal(map[string]any{
			"id": "agentd-start", "type": "prompt", "message": prompt, "images": images,
		})
		if err := current.sendCommand(startCommand); err != nil {
			_ = terminateProcessGroup(command.Process.Pid, syscall.SIGKILL)
			return err
		}
	}
	return nil
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
	_, _ = io.Copy(&boundedLogWriter{writer: file, remaining: 1024 * 1024}, reader)
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
		current.setTerminal(statusStopped, "")
	}
}

func (current *task) failWorker(message string) {
	current.mu.Lock()
	pid := current.metadata.PID
	current.mu.Unlock()
	current.setTerminal(statusFailed, message)
	if pid > 0 {
		_ = terminateProcessGroup(pid, syscall.SIGKILL)
	}
}

func (current *task) recordEvent(raw json.RawMessage) {
	var header struct {
		Type    string `json:"type"`
		Method  string `json:"method"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		ID      string `json:"id"`
		Data    struct {
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

	current.mu.Lock()
	current.metadata.LastSequence++
	current.metadata.UpdatedAt = time.Now().UTC()
	acceptWorkerTransition := current.metadata.Status != statusStopping && isLiveStatus(current.metadata.Status)
	switch header.Type {
	case "agent_start":
		if acceptWorkerTransition {
			current.metadata.Status = statusRunning
		}
	case "agent_settled":
		if acceptWorkerTransition {
			current.metadata.Status = statusIdle
		}
	case "extension_ui_request":
		if approval, ok := approvalFromEvent(raw); ok && acceptWorkerTransition {
			current.metadata.Status = statusWaiting
			current.upsertApprovalLocked(approval)
		}
	case "response":
		if header.Success && header.Command == "get_state" {
			current.pendingSessionFile = header.Data.SessionFile
			current.pendingSessionID = header.Data.SessionID
			if current.metadata.Status == statusStarting {
				current.metadata.Status = statusIdle
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
	persisted := false
	if !current.metadata.LogTruncated && current.eventBytes+int64(len(encoded)+1) <= current.manager.cfg.MaxEventSize {
		if file, err := os.OpenFile(current.events, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			if _, err := file.Write(append(encoded, '\n')); err == nil {
				current.eventBytes += int64(len(encoded) + 1)
				persisted = true
			}
			if err := file.Close(); err != nil {
				persisted = false
			}
		}
	}
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
	if sessionBound || logBecameTruncated || header.Type == "hobot_user_prompt" || header.Type == "agent_start" || header.Type == "agent_settled" || header.Type == "extension_ui_request" || header.Type == "response" {
		_ = current.saveMetadata()
	}
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
	if header.Type == "prompt" {
		current.mu.Lock()
		if current.metadata.Status != statusStarting && current.metadata.Status != statusIdle {
			current.mu.Unlock()
			return fmt.Errorf("task must be idle before accepting another prompt")
		}
		current.metadata.Status = statusRunning
		current.metadata.UpdatedAt = time.Now().UTC()
		current.mu.Unlock()
		_ = current.saveMetadata()
		attachments := make([]map[string]string, 0, len(header.Images))
		for _, image := range header.Images {
			attachments = append(attachments, map[string]string{"name": image.Name, "mimeType": image.MimeType})
		}
		promptEvent, _ := json.Marshal(map[string]any{"type": "hobot_user_prompt", "message": header.Message, "attachments": attachments})
		current.recordEvent(promptEvent)
		workerCommand := map[string]any{}
		if err := json.Unmarshal(command, &workerCommand); err != nil {
			return fmt.Errorf("decode prompt command: %w", err)
		}
		workerCommand["images"] = imagePayload(header.Images)
		command, _ = json.Marshal(workerCommand)
	}
	if err := current.writeWorkerCommand(command); err != nil {
		if header.Type == "prompt" {
			current.mu.Lock()
			if current.metadata.Status == statusRunning {
				current.metadata.Status = status
				current.metadata.UpdatedAt = time.Now().UTC()
			}
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
	if current.metadata.Status == statusStopped && status != statusStopped {
		current.mu.Unlock()
		return
	}
	current.metadata.Status = status
	current.metadata.UpdatedAt = time.Now().UTC()
	if message != "" {
		current.metadata.LastError = message
	}
	for index := range current.metadata.Approvals {
		current.metadata.Approvals[index].Active = false
	}
	for id, subscriber := range current.subscribers {
		close(subscriber)
		delete(current.subscribers, id)
	}
	current.mu.Unlock()
	_ = current.saveMetadata()
}

func (current *task) snapshot() taskMetadata {
	current.mu.Lock()
	defer current.mu.Unlock()
	return cloneMetadata(current.metadata)
}

func (current *task) saveMetadata() error {
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
	current.mu.Lock()
	status := current.metadata.Status
	if isTerminalStatus(status) {
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
	if current.metadata.Status != statusIdle && !isTerminalStatus(current.metadata.Status) {
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
	if err := manager.ensureActiveSlot(); err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	current.metadata.Status = statusStarting
	current.metadata.PID = 0
	current.metadata.LastError = ""
	current.metadata.AwaitingPrompt = false
	current.metadata.ResumeCount++
	for index := range current.metadata.Approvals {
		current.metadata.Approvals[index].Active = false
	}
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		return taskMetadata{}, err
	}
	if err := current.launch(params.Prompt, params.Images, metadata.Approved, sessionFile); err != nil {
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
	if err := manager.ensureActiveSlot(); err != nil {
		return taskMetadata{}, err
	}
	current.mu.Lock()
	current.metadata.Status = statusStarting
	current.metadata.PID = 0
	current.metadata.LastError = ""
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
		return taskMetadata{}, err
	}
	if err := current.launch(params.Prompt, params.Images, metadata.Approved, ""); err != nil {
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

func (current *task) subscribe(after uint64, follow bool) ([]taskEvent, <-chan taskEvent, func(), error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	replayed, err := readEvents(current.events, current.metadata.ID, after)
	if err != nil {
		return nil, nil, nil, err
	}
	if !follow || !isLiveStatus(current.metadata.Status) {
		return replayed, nil, func() {}, nil
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
	return replayed, channel, cancel, nil
}

func (current *task) eventPage(after uint64, limit int) (eventPage, error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	return readEventPage(current.events, current.metadata.ID, after, limit)
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
func recoverEventLog(path, taskID string, maximumBytes int64) (int64, uint64, bool, error) {
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

func readEventPage(path, taskID string, after uint64, limit int) (eventPage, error) {
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
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxEventRecordBytes)
	for scanner.Scan() {
		var event taskEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return eventPage{}, fmt.Errorf("corrupt task event log: %w", err)
		}
		if event.Protocol != protocolVersion || event.Kind != "event" || event.TaskID != taskID || event.Sequence != lastSequence+1 {
			return eventPage{}, fmt.Errorf("corrupt task event envelope at sequence %d", event.Sequence)
		}
		lastSequence = event.Sequence
		eventBytes := len(scanner.Bytes()) + 1
		pageBudgetReached := limit > 0 && len(result) > 0 && resultBytes+eventBytes > maxResponseBytes-64*1024
		if event.Sequence > after && !pageBudgetReached && (limit == 0 || len(result) < limit) {
			result = append(result, event)
			resultBytes += eventBytes
		} else if event.Sequence > after && limit > 0 {
			page := eventPage{Events: result, HasMore: true}
			if len(result) > 0 {
				page.NextAfter = result[len(result)-1].Sequence
			}
			return page, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return eventPage{}, err
	}
	page := eventPage{Events: result}
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
		live := isLiveStatus(current.metadata.Status)
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
		live := isLiveStatus(current.metadata.Status)
		active.live = live
		if live {
			current.interrupted = true
			current.metadata.Status = statusInterrupted
			current.metadata.LastError = "agentd stopped; worker was not replayed"
			current.metadata.PID = 0
			current.metadata.UpdatedAt = time.Now().UTC()
		}
		current.mu.Unlock()
		if live {
			_ = current.saveMetadata()
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
