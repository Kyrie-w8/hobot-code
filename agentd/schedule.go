package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	scheduleSchemaVersion = 1
	maximumSchedules      = 100
	maximumScheduleBytes  = 2 * 1024 * 1024
	maximumSchedulePrompt = 16 * 1024
	maximumScheduleName   = 96
	maximumScheduleResult = 240
	minimumScheduleEvery  = time.Minute
	maximumScheduleEvery  = 30 * 24 * time.Hour
)

var scheduleAtPattern = regexp.MustCompile(`(?:Z|[+-][0-9]{2}:[0-9]{2})$`)

type scheduleRecord struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	TaskID           string     `json:"taskId"`
	Prompt           string     `json:"prompt,omitempty"`
	At               *time.Time `json:"at,omitempty"`
	Every            string     `json:"every,omitempty"`
	Enabled          bool       `json:"enabled"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	NextRun          *time.Time `json:"nextRun,omitempty"`
	LastRun          *time.Time `json:"lastRun,omitempty"`
	RunCount         int        `json:"runCount"`
	LastResult       string     `json:"lastResult,omitempty"`
	Pending          bool       `json:"pending,omitempty"`
	InFlight         bool       `json:"inFlight,omitempty"`
	DispatchState    string     `json:"dispatchState,omitempty"`
	DispatchSequence uint64     `json:"dispatchSequence,omitempty"`
}

type scheduleState struct {
	Schema    int              `json:"schema"`
	Schedules []scheduleRecord `json:"schedules"`
}

type createScheduleParams struct {
	Name   string `json:"name"`
	TaskID string `json:"taskId"`
	Prompt string `json:"prompt"`
	At     string `json:"at,omitempty"`
	Every  string `json:"every,omitempty"`
}

type scheduleIDParams struct {
	ID      string `json:"id"`
	Details bool   `json:"details,omitempty"`
}

type listScheduleParams struct {
	All bool `json:"all,omitempty"`
}

type scheduleManager struct {
	cfg      config
	tasks    *taskManager
	now      func() time.Time
	mu       sync.Mutex
	entries  map[string]scheduleRecord
	wake     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newScheduleManager(cfg config, tasks *taskManager) (*scheduleManager, error) {
	return newScheduleManagerWithClock(cfg, tasks, func() time.Time { return time.Now().UTC() })
}

func newScheduleManagerWithClock(cfg config, tasks *taskManager, now func() time.Time) (*scheduleManager, error) {
	if tasks == nil || now == nil {
		return nil, fmt.Errorf("schedule manager requires tasks and a clock")
	}
	manager := &scheduleManager{cfg: cfg, tasks: tasks, now: now, entries: make(map[string]scheduleRecord), wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	if err := manager.load(); err != nil {
		return nil, err
	}
	go manager.loop()
	manager.signal()
	return manager, nil
}

func (manager *scheduleManager) path() string {
	return filepath.Join(manager.cfg.SchedulesRoot, "schedules.json")
}

func (manager *scheduleManager) load() error {
	if err := ensurePrivateDir(manager.cfg.SchedulesRoot); err != nil {
		return fmt.Errorf("prepare private schedule state: %w", err)
	}
	content, err := readPrivateRegularFile(manager.path(), maximumScheduleBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read private schedule state: %w", err)
	}
	var state scheduleState
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode private schedule state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode private schedule state: unexpected trailing data")
	}
	if state.Schema != scheduleSchemaVersion || len(state.Schedules) > maximumSchedules {
		return fmt.Errorf("private schedule state has an unsupported schema or too many schedules")
	}
	changed := false
	for _, entry := range state.Schedules {
		if err := validateSchedule(entry); err != nil {
			return fmt.Errorf("invalid private schedule state: %w", err)
		}
		if _, exists := manager.entries[entry.ID]; exists {
			return fmt.Errorf("invalid private schedule state: duplicate schedule ID")
		}
		if entry.InFlight {
			// A durable claim may have reached the worker just before power loss.
			// Do not replay it; future recurring occurrences remain eligible.
			entry.InFlight = false
			entry.DispatchState = "uncertain"
			entry.LastResult = "A scheduled run was claimed before the board restarted and was not replayed automatically."
			entry.UpdatedAt = manager.now().UTC()
			changed = true
		}
		if _, err := manager.tasks.get(entry.TaskID); err != nil {
			entry.Enabled = false
			entry.Pending = false
			entry.Status = "failed"
			entry.LastResult = "The target task is unavailable. Delete this schedule or create one for an existing main task."
			entry.UpdatedAt = manager.now().UTC()
			changed = true
		}
		manager.entries[entry.ID] = entry
	}
	if changed {
		return manager.persistLocked()
	}
	return nil
}

func validateSchedule(entry scheduleRecord) error {
	if !taskIDPattern.MatchString(entry.ID) || !taskIDPattern.MatchString(entry.TaskID) {
		return fmt.Errorf("schedule ID or task ID is invalid")
	}
	if len(entry.Name) == 0 || len(entry.Name) > maximumScheduleName || !utf8.ValidString(entry.Name) || strings.ContainsAny(entry.Name, "\r\n") {
		return fmt.Errorf("schedule name is invalid")
	}
	if len(entry.Prompt) == 0 || len(entry.Prompt) > maximumSchedulePrompt || !utf8.ValidString(entry.Prompt) {
		return fmt.Errorf("schedule prompt is invalid")
	}
	if (entry.At != nil) == (entry.Every != "") {
		return fmt.Errorf("schedule must contain exactly one cadence")
	}
	if entry.At != nil && (entry.At.IsZero() || entry.At.Location() == nil) {
		return fmt.Errorf("schedule time zone is invalid")
	}
	if entry.Every != "" {
		duration, err := time.ParseDuration(entry.Every)
		if err != nil || duration < minimumScheduleEvery || duration > maximumScheduleEvery {
			return fmt.Errorf("schedule interval is invalid")
		}
	}
	if entry.Status != "active" && entry.Status != "paused" && entry.Status != "completed" && entry.Status != "failed" {
		return fmt.Errorf("schedule status is invalid")
	}
	if entry.DispatchState != "" && entry.DispatchState != "claimed" && entry.DispatchState != "dispatched" && entry.DispatchState != "completed" && entry.DispatchState != "failed" && entry.DispatchState != "stopped" && entry.DispatchState != "uncertain" {
		return fmt.Errorf("schedule dispatch state is invalid")
	}
	if entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() || entry.RunCount < 0 || len(entry.LastResult) > maximumScheduleResult || !utf8.ValidString(entry.LastResult) {
		return fmt.Errorf("schedule metadata is invalid")
	}
	if entry.NextRun != nil && entry.NextRun.IsZero() {
		return fmt.Errorf("schedule next run is invalid")
	}
	if entry.LastRun != nil && entry.LastRun.IsZero() {
		return fmt.Errorf("schedule last run is invalid")
	}
	if entry.InFlight && !entry.Enabled && entry.Status != "completed" && entry.Status != "paused" {
		return fmt.Errorf("disabled schedule cannot be in flight")
	}
	if entry.Pending && (!entry.Enabled || entry.Status != "active") {
		return fmt.Errorf("only active schedules can be pending")
	}
	switch entry.Status {
	case "active":
		if !entry.Enabled || entry.NextRun == nil {
			return fmt.Errorf("active schedule is invalid")
		}
	case "paused":
		if entry.Enabled || entry.Pending {
			return fmt.Errorf("paused schedule is invalid")
		}
	case "completed":
		if entry.At == nil || entry.Enabled || entry.Pending {
			return fmt.Errorf("completed schedule is invalid")
		}
	case "failed":
		if entry.Enabled || entry.Pending || entry.InFlight {
			return fmt.Errorf("failed schedule is invalid")
		}
	}
	return nil
}

func (manager *scheduleManager) persistLocked() error {
	entries := make([]scheduleRecord, 0, len(manager.entries))
	for _, entry := range manager.entries {
		entries = append(entries, cloneSchedule(entry))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].CreatedAt.Before(entries[j].CreatedAt) })
	encoded, err := json.MarshalIndent(scheduleState{Schema: scheduleSchemaVersion, Schedules: entries}, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded) > maximumScheduleBytes {
		return fmt.Errorf("private schedule state exceeds %d bytes", maximumScheduleBytes)
	}
	return writePrivateFileDurable(manager.path(), append(encoded, '\n'))
}

func cloneSchedule(entry scheduleRecord) scheduleRecord {
	copy := entry
	if entry.At != nil {
		value := *entry.At
		copy.At = &value
	}
	if entry.NextRun != nil {
		value := *entry.NextRun
		copy.NextRun = &value
	}
	if entry.LastRun != nil {
		value := *entry.LastRun
		copy.LastRun = &value
	}
	return copy
}

func (manager *scheduleManager) signal() {
	select {
	case manager.wake <- struct{}{}:
	default:
	}
}

func (manager *scheduleManager) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer close(manager.done)
	for {
		select {
		case <-manager.stop:
			return
		case <-manager.wake:
			manager.processDue(manager.now().UTC())
		case now := <-ticker.C:
			manager.processDue(now.UTC())
		}
	}
}

func (manager *scheduleManager) shutdown() {
	manager.stopOnce.Do(func() { close(manager.stop) })
	<-manager.done
}

func (manager *scheduleManager) create(params createScheduleParams) (scheduleRecord, error) {
	if !taskIDPattern.MatchString(params.TaskID) || len(params.Prompt) == 0 || len(params.Prompt) > maximumSchedulePrompt {
		return scheduleRecord{}, fmt.Errorf("schedule task ID or prompt is invalid")
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = deriveTaskTitle(params.Prompt)
	}
	if len(name) > maximumScheduleName || strings.ContainsAny(name, "\r\n") {
		return scheduleRecord{}, fmt.Errorf("schedule name is invalid")
	}
	current, err := manager.tasks.get(params.TaskID)
	if err != nil {
		return scheduleRecord{}, fmt.Errorf("target task does not exist")
	}
	metadata := current.snapshot()
	if metadata.BranchKind != "" {
		return scheduleRecord{}, fmt.Errorf("schedules can only target a main task")
	}
	if metadata.ArchivedAt != nil {
		return scheduleRecord{}, fmt.Errorf("unarchive the target task before scheduling it")
	}
	if strings.TrimSpace(params.At) == "" == (strings.TrimSpace(params.Every) == "") {
		return scheduleRecord{}, fmt.Errorf("provide exactly one of at or every")
	}
	now := manager.now().UTC()
	entry := scheduleRecord{ID: "", Name: name, TaskID: params.TaskID, Prompt: params.Prompt, Enabled: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	if params.At != "" {
		if !scheduleAtPattern.MatchString(params.At) {
			return scheduleRecord{}, fmt.Errorf("schedule time must be RFC3339 with a time zone offset")
		}
		at, err := time.Parse(time.RFC3339, params.At)
		if err != nil || !at.After(now) {
			return scheduleRecord{}, fmt.Errorf("schedule time must be a future RFC3339 time with a time zone offset")
		}
		entry.At, entry.NextRun = &at, &at
	} else {
		duration, err := time.ParseDuration(params.Every)
		if err != nil || duration < minimumScheduleEvery || duration > maximumScheduleEvery {
			return scheduleRecord{}, fmt.Errorf("schedule interval must be between 1 minute and 30 days")
		}
		entry.Every = duration.String()
		next := now.Add(duration)
		entry.NextRun = &next
	}
	id, err := newTaskID()
	if err != nil {
		return scheduleRecord{}, err
	}
	entry.ID = id
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.entries) >= maximumSchedules {
		return scheduleRecord{}, fmt.Errorf("schedule limit reached (%d)", maximumSchedules)
	}
	manager.entries[entry.ID] = entry
	if err := manager.persistLocked(); err != nil {
		delete(manager.entries, entry.ID)
		return scheduleRecord{}, err
	}
	manager.signal()
	return redactSchedule(entry, false), nil
}

func (manager *scheduleManager) list(includeAll bool) []scheduleRecord {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	result := make([]scheduleRecord, 0, len(manager.entries))
	for _, entry := range manager.entries {
		if includeAll || entry.Enabled {
			result = append(result, redactSchedule(entry, false))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (manager *scheduleManager) show(params scheduleIDParams) (scheduleRecord, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	entry, ok := manager.entries[params.ID]
	if !ok {
		return scheduleRecord{}, fmt.Errorf("schedule does not exist")
	}
	return redactSchedule(entry, params.Details), nil
}

func redactSchedule(entry scheduleRecord, details bool) scheduleRecord {
	copy := cloneSchedule(entry)
	if !details {
		copy.Prompt = ""
	}
	return copy
}

func (manager *scheduleManager) pause(id string) (scheduleRecord, error) {
	return manager.setEnabled(id, false)
}
func (manager *scheduleManager) resume(id string) (scheduleRecord, error) {
	return manager.setEnabled(id, true)
}

func (manager *scheduleManager) setEnabled(id string, enabled bool) (scheduleRecord, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	entry, ok := manager.entries[id]
	if !ok {
		return scheduleRecord{}, fmt.Errorf("schedule does not exist")
	}
	if entry.Status == "completed" || entry.Status == "failed" {
		return scheduleRecord{}, fmt.Errorf("completed or failed schedules cannot be changed; create a new schedule")
	}
	if enabled && entry.Status == "active" && entry.Enabled {
		return redactSchedule(entry, false), nil
	}
	if !enabled && entry.Status == "paused" && !entry.Enabled {
		return redactSchedule(entry, false), nil
	}
	entry.Enabled = enabled
	entry.Pending = false
	if enabled {
		entry.Status = "active"
		if entry.NextRun == nil && entry.Every != "" {
			duration, _ := time.ParseDuration(entry.Every)
			next := manager.now().UTC().Add(duration)
			entry.NextRun = &next
		}
	} else {
		entry.Status = "paused"
	}
	entry.UpdatedAt = manager.now().UTC()
	previous := manager.entries[id]
	manager.entries[id] = entry
	if err := manager.persistLocked(); err != nil {
		manager.entries[id] = previous
		return scheduleRecord{}, err
	}
	manager.signal()
	return redactSchedule(entry, false), nil
}

func (manager *scheduleManager) delete(id string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	entry, ok := manager.entries[id]
	if !ok {
		return fmt.Errorf("schedule does not exist")
	}
	delete(manager.entries, id)
	if err := manager.persistLocked(); err != nil {
		manager.entries[id] = entry
		return err
	}
	return nil
}

func (manager *scheduleManager) runNow(id string) (scheduleRecord, error) {
	manager.mu.Lock()
	entry, ok := manager.entries[id]
	if !ok {
		manager.mu.Unlock()
		return scheduleRecord{}, fmt.Errorf("schedule does not exist")
	}
	if entry.Status == "completed" || entry.Status == "failed" {
		manager.mu.Unlock()
		return scheduleRecord{}, fmt.Errorf("completed or failed schedules cannot be run; create a new schedule")
	}
	if !entry.Enabled {
		manager.mu.Unlock()
		return scheduleRecord{}, fmt.Errorf("resume the schedule before running it")
	}
	if entry.InFlight {
		manager.mu.Unlock()
		return redactSchedule(entry, false), nil
	}
	entry.Pending = true
	entry.LastResult = "A run is waiting for the target task to become available."
	entry.UpdatedAt = manager.now().UTC()
	previous := manager.entries[id]
	manager.entries[id] = entry
	err := manager.persistLocked()
	if err != nil {
		manager.entries[id] = previous
	}
	manager.mu.Unlock()
	if err != nil {
		return scheduleRecord{}, err
	}
	manager.signal()
	return redactSchedule(entry, false), nil
}

func (manager *scheduleManager) blocksTaskLifecycle(id string) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, entry := range manager.entries {
		if entry.TaskID == id {
			return true
		}
	}
	return false
}

func (manager *scheduleManager) processDue(now time.Time) {
	manager.mu.Lock()
	before := cloneScheduleEntries(manager.entries)
	ids := make([]string, 0, len(manager.entries))
	changed := false
	for id, entry := range manager.entries {
		if entry.InFlight {
			ids = append(ids, id)
			continue
		}
		if !entry.Enabled {
			continue
		}
		if entry.Pending || (entry.NextRun != nil && !entry.NextRun.After(now)) {
			if entry.NextRun != nil && !entry.NextRun.After(now) {
				if entry.Every != "" {
					duration, _ := time.ParseDuration(entry.Every)
					next := now.Add(duration)
					entry.NextRun = &next
				}
				entry.Pending = true
				entry.LastResult = "A scheduled run is waiting for the target task to become available."
				entry.UpdatedAt = now
				manager.entries[id] = entry
				changed = true
			}
			ids = append(ids, id)
		}
	}
	if changed {
		if err := manager.persistLocked(); err != nil {
			// A due occurrence is never dispatched unless its new cadence and claim
			// can survive a restart. Leave the old durable state eligible for retry.
			manager.entries = before
			manager.mu.Unlock()
			return
		}
	}
	manager.mu.Unlock()
	for _, id := range ids {
		manager.advance(id, now)
	}
}

func cloneScheduleEntries(entries map[string]scheduleRecord) map[string]scheduleRecord {
	copy := make(map[string]scheduleRecord, len(entries))
	for id, entry := range entries {
		copy[id] = cloneSchedule(entry)
	}
	return copy
}

func (manager *scheduleManager) advance(id string, now time.Time) {
	manager.mu.Lock()
	entry, ok := manager.entries[id]
	if !ok {
		manager.mu.Unlock()
		return
	}
	if entry.InFlight {
		manager.mu.Unlock()
		manager.finishInFlight(id, now)
		return
	}
	if !entry.Pending || !entry.Enabled || entry.Status != "active" {
		manager.mu.Unlock()
		return
	}
	current, err := manager.tasks.get(entry.TaskID)
	if err != nil {
		manager.failLocked(id, "The target task is unavailable. Delete this schedule or choose an existing main task.", now)
		manager.mu.Unlock()
		return
	}
	metadata := current.snapshot()
	if metadata.BranchKind != "" || metadata.ArchivedAt != nil {
		manager.failLocked(id, "The target must be an available main task before this schedule can run.", now)
		manager.mu.Unlock()
		return
	}
	if metadata.Status != statusIdle && isLiveStatus(metadata.Status) {
		entry.Pending = true
		entry.Status = "active"
		entry.LastResult = "Run coalesced while the target task is busy."
		entry.UpdatedAt = now
		previous := manager.entries[id]
		manager.entries[id] = entry
		if err := manager.persistLocked(); err != nil {
			manager.entries[id] = previous
		}
		manager.mu.Unlock()
		return
	}
	if _, err := validateSessionFile(manager.cfg.SessionDir, metadata.SessionFile); err != nil {
		manager.failLocked(id, "The target has no safe resumable Pi session. Open the task and resume it manually before scheduling.", now)
		manager.mu.Unlock()
		return
	}
	previousClaim := entry
	entry.Pending, entry.InFlight, entry.DispatchState = false, true, "claimed"
	if entry.At != nil {
		// A one-shot has now been durably claimed. It cannot be replayed after
		// power loss and cannot create a second occurrence while it is running.
		entry.Enabled = false
		entry.Status = "completed"
		entry.NextRun = nil
	}
	entry.DispatchSequence = metadata.LastSequence
	entry.LastRun = &now
	entry.RunCount++
	entry.LastResult = "Run claimed and dispatched to the target task."
	entry.UpdatedAt = now
	manager.entries[id] = entry
	if err := manager.persistLocked(); err != nil {
		// Restore the pre-claim entry. It remains pending and will be retried,
		// rather than risking a worker dispatch with no durable claim.
		manager.entries[id] = previousClaim
		manager.mu.Unlock()
		return
	}
	manager.mu.Unlock()

	var dispatchErr error
	if metadata.Status == statusIdle {
		command, _ := json.Marshal(map[string]any{"id": "schedule-" + id, "type": "prompt", "message": entry.Prompt})
		dispatchErr = current.sendCommandWithPromptEvent(command, true, promptEventOrigin{Source: "schedule", ScheduleID: id})
	} else {
		_, dispatchErr = manager.tasks.resume(resumeTaskParams{TaskID: metadata.ID, Prompt: entry.Prompt, PromptSource: "schedule", ScheduleID: id})
	}
	manager.mu.Lock()
	entry, ok = manager.entries[id]
	if !ok {
		manager.mu.Unlock()
		return
	}
	if dispatchErr != nil {
		previous := entry
		entry.InFlight = false
		entry.DispatchState = "failed"
		entry.LastResult = "The board could not dispatch this run safely. Review the target task before the next occurrence."
		entry.UpdatedAt = now
		manager.entries[id] = entry
		if err := manager.persistLocked(); err != nil {
			manager.entries[id] = previous
		}
		manager.mu.Unlock()
		return
	}
	previous := entry
	entry.DispatchState = "dispatched"
	entry.LastResult = "Run dispatched to the target task."
	entry.UpdatedAt = now
	manager.entries[id] = entry
	if err := manager.persistLocked(); err != nil {
		manager.entries[id] = previous
	}
	manager.mu.Unlock()
}

func (manager *scheduleManager) finishInFlight(id string, now time.Time) {
	manager.mu.Lock()
	entry, ok := manager.entries[id]
	if !ok || !entry.InFlight {
		manager.mu.Unlock()
		return
	}
	manager.mu.Unlock()
	current, err := manager.tasks.get(entry.TaskID)
	if err != nil {
		manager.mu.Lock()
		manager.failLocked(id, "The target task is unavailable. Delete this schedule or choose an existing main task.", now)
		manager.mu.Unlock()
		return
	}
	metadata := current.snapshot()
	if metadata.Status == statusStopped {
		manager.mu.Lock()
		entry = manager.entries[id]
		entry.InFlight = false
		entry.DispatchState = "stopped"
		entry.LastResult = "The most recent run was stopped before completion."
		entry.UpdatedAt = now
		previous := manager.entries[id]
		manager.entries[id] = entry
		if err := manager.persistLocked(); err != nil {
			manager.entries[id] = previous
		}
		manager.mu.Unlock()
		return
	}
	if metadata.Status == statusIdle && metadata.LastSequence > entry.DispatchSequence {
		manager.mu.Lock()
		entry = manager.entries[id]
		entry.InFlight = false
		entry.DispatchState = "completed"
		entry.LastResult = "The most recent run completed."
		entry.UpdatedAt = now
		previous := manager.entries[id]
		manager.entries[id] = entry
		if err := manager.persistLocked(); err != nil {
			manager.entries[id] = previous
		}
		manager.mu.Unlock()
		return
	}
	if metadata.Status == statusFailed || metadata.Status == statusInterrupted {
		manager.mu.Lock()
		entry = manager.entries[id]
		entry.InFlight = false
		entry.DispatchState = "failed"
		entry.LastResult = "The most recent run did not complete. Open the target task to review it."
		entry.UpdatedAt = now
		previous := manager.entries[id]
		manager.entries[id] = entry
		if err := manager.persistLocked(); err != nil {
			manager.entries[id] = previous
		}
		manager.mu.Unlock()
	}
}

func (manager *scheduleManager) failLocked(id, message string, now time.Time) {
	entry := manager.entries[id]
	previous := entry
	entry.Enabled = false
	entry.Pending = false
	entry.InFlight = false
	entry.Status = "failed"
	entry.DispatchState = "failed"
	entry.LastResult = truncateScheduleResult(message)
	entry.UpdatedAt = now
	manager.entries[id] = entry
	if err := manager.persistLocked(); err != nil {
		manager.entries[id] = previous
	}
}

func truncateScheduleResult(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " "))
	if len(value) > maximumScheduleResult {
		return value[:maximumScheduleResult]
	}
	return value
}
