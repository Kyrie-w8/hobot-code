package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type scheduleTestClock struct {
	mu    sync.Mutex
	value time.Time
}

func (clock *scheduleTestClock) now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.value
}
func (clock *scheduleTestClock) add(value time.Duration) {
	clock.mu.Lock()
	clock.value = clock.value.Add(value)
	clock.mu.Unlock()
}

func newScheduleTestManager(t *testing.T) (*taskManager, *scheduleManager, config, *scheduleTestClock) {
	t.Helper()
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clock := &scheduleTestClock{value: time.Date(2026, 8, 17, 4, 0, 0, 0, time.UTC)}
	schedules, err := newScheduleManagerWithClock(cfg, manager, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(schedules.shutdown)
	manager.schedules = schedules
	return manager, schedules, cfg, clock
}

func addScheduleTarget(t *testing.T, cfg config, manager *taskManager) *task {
	t.Helper()
	return addSettledSourceTask(t, manager, cfg)
}

func addScheduleBranchTarget(t *testing.T, cfg config, manager *taskManager, id, kind, parentID string) *task {
	t.Helper()
	dir := filepath.Join(cfg.TasksRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(dir, "events.jsonl")
	stderr := filepath.Join(dir, "worker.stderr.log")
	if err := os.WriteFile(events, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderr, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	current := &task{
		manager: manager, dir: dir, events: events, stderr: stderr,
		metadata:    taskMetadata{ID: id, Name: kind, Cwd: cfg.StateRoot, Status: statusStopped, CreatedAt: now, UpdatedAt: now, BranchKind: kind, ParentTaskID: parentID},
		subscribers: make(map[uint64]chan taskEvent),
	}
	manager.mu.Lock()
	manager.tasks[id] = current
	manager.mu.Unlock()
	return current
}

func TestScheduleCreateRedactsAndRejectsBranchTargets(t *testing.T) {
	manager, schedules, cfg, _ := newScheduleTestManager(t)
	target := addScheduleTarget(t, cfg, manager)
	created, err := schedules.create(createScheduleParams{Name: "status", TaskID: target.snapshot().ID, Every: "1m", Prompt: "report only the result"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Prompt != "" || created.Every != "1m0s" || !created.Enabled {
		t.Fatalf("unexpected redacted schedule: %+v", created)
	}
	shown, err := schedules.show(scheduleIDParams{ID: created.ID, Details: true})
	if err != nil || shown.Prompt != "report only the result" {
		t.Fatalf("details did not reveal bounded prompt: %+v %v", shown, err)
	}
	edited := addScheduleBranchTarget(t, cfg, manager, "111122223333444455556666", "edit", target.snapshot().ID)
	if _, err := schedules.create(createScheduleParams{TaskID: edited.snapshot().ID, Every: "1m", Prompt: "edited main"}); err != nil {
		t.Fatalf("edited main timeline was not schedule eligible: %v", err)
	}
	side := addScheduleBranchTarget(t, cfg, manager, "222233334444555566667777", "side", target.snapshot().ID)
	if _, err := schedules.create(createScheduleParams{TaskID: side.snapshot().ID, Every: "1m", Prompt: "no"}); err == nil || !strings.Contains(err.Error(), "main task") {
		t.Fatalf("side task schedule accepted: %v", err)
	}
	sideEdit := addScheduleBranchTarget(t, cfg, manager, "333344445555666677778888", "edit", side.snapshot().ID)
	if _, err := schedules.create(createScheduleParams{TaskID: sideEdit.snapshot().ID, Every: "1m", Prompt: "no"}); err == nil || !strings.Contains(err.Error(), "main task") {
		t.Fatalf("edited side task schedule accepted: %v", err)
	}
}

func TestSchedulePauseResumeDeleteAndLifecycleReferences(t *testing.T) {
	manager, schedules, cfg, _ := newScheduleTestManager(t)
	target := addScheduleTarget(t, cfg, manager)
	created, err := schedules.create(createScheduleParams{TaskID: target.snapshot().ID, Every: "1m", Prompt: "check"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schedules.pause(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := schedules.resume(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.archive(archiveTaskParams{TaskID: target.snapshot().ID, Archive: true}); err == nil || !strings.Contains(err.Error(), "delete associated schedules") {
		t.Fatalf("archive should preserve schedule references: %v", err)
	}
	if err := manager.delete(target.snapshot().ID); err == nil || !strings.Contains(err.Error(), "delete associated schedules") {
		t.Fatalf("delete should preserve schedule references: %v", err)
	}
	if err := schedules.delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.archive(archiveTaskParams{TaskID: target.snapshot().ID, Archive: true}); err != nil {
		t.Fatal(err)
	}
}

func TestScheduleOneShotAndBusyCoalescing(t *testing.T) {
	manager, schedules, cfg, clock := newScheduleTestManager(t)
	target := addScheduleTarget(t, cfg, manager)
	at := clock.now().Add(time.Minute).Format(time.RFC3339)
	one, err := schedules.create(createScheduleParams{TaskID: target.snapshot().ID, At: at, Prompt: "once"})
	if err != nil {
		t.Fatal(err)
	}
	target.mu.Lock()
	target.metadata.Status = statusRunning
	target.mu.Unlock()
	clock.add(time.Minute)
	schedules.processDue(clock.now())
	entry, err := schedules.show(scheduleIDParams{ID: one.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.InFlight && !entry.Pending {
		t.Fatalf("due one-shot was neither claimed nor coalesced: %+v", entry)
	}
	// Completed one-shots cannot be revived into an active schedule without a
	// future cadence; this avoids a UI state that appears enabled but cannot run.
	schedules.mu.Lock()
	stored := schedules.entries[one.ID]
	stored.Enabled = false
	stored.Status = "completed"
	stored.Pending = false
	stored.InFlight = false
	stored.NextRun = nil
	schedules.entries[one.ID] = stored
	schedules.mu.Unlock()
	if _, err := schedules.resume(one.ID); err == nil || !strings.Contains(err.Error(), "completed or failed") {
		t.Fatalf("completed one-shot resumed: %v", err)
	}
	if _, err := schedules.runNow(one.ID); err == nil {
		t.Fatal("completed one-shot run-now accepted")
	}
}

func TestScheduleStateFailsClosedForUnknownAndUnsafeFiles(t *testing.T) {
	manager, schedules, cfg, _ := newScheduleTestManager(t)
	_ = manager
	schedules.shutdown()
	// shutdown is intentionally idempotent for daemon teardown paths.
	schedules.shutdown()
	unsafe := []byte(`{"schema":1,"schedules":[],"unexpected":true}`)
	if err := os.WriteFile(filepath.Join(cfg.SchedulesRoot, "schedules.json"), unsafe, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newScheduleManagerWithClock(cfg, manager, time.Now); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("unknown schedule state fields accepted: %v", err)
	}
}

func TestScheduleCrashWindowsAndPausedInFlightRoundTrip(t *testing.T) {
	manager, schedules, cfg, clock := newScheduleTestManager(t)
	target := addScheduleTarget(t, cfg, manager)
	one, err := schedules.create(createScheduleParams{TaskID: target.snapshot().ID, At: clock.now().Add(time.Minute).Format(time.RFC3339), Prompt: "once"})
	if err != nil {
		t.Fatal(err)
	}
	repeating, err := schedules.create(createScheduleParams{TaskID: target.snapshot().ID, Every: "1m", Prompt: "repeat"})
	if err != nil {
		t.Fatal(err)
	}
	// Freeze the injected scheduler loop before constructing exact persisted
	// crash-window records; no real timer is involved in this test.
	schedules.shutdown()
	schedules.mu.Lock()
	// A one-shot remains active with its due timestamp while it is pending; the
	// timestamp is only removed inside the durable claim transition.
	due := clock.now()
	entry := schedules.entries[one.ID]
	entry.NextRun = &due
	entry.Pending = true
	schedules.entries[one.ID] = entry
	paused := schedules.entries[repeating.ID]
	paused.Enabled = false
	paused.Status = "paused"
	paused.InFlight = true
	paused.Pending = false
	schedules.entries[repeating.ID] = paused
	if err := schedules.persistLocked(); err != nil {
		schedules.mu.Unlock()
		t.Fatal(err)
	}
	schedules.mu.Unlock()
	reloaded, err := newScheduleManagerWithClock(cfg, manager, clock.now)
	if err != nil {
		t.Fatalf("valid persisted scheduler windows failed to reload: %v", err)
	}
	defer reloaded.shutdown()
	if got, err := reloaded.show(scheduleIDParams{ID: one.ID}); err != nil || !got.Pending || got.NextRun == nil {
		t.Fatalf("pending one-shot did not survive restart: %+v %v", got, err)
	}
	if got, err := reloaded.show(scheduleIDParams{ID: repeating.ID}); err != nil || got.Status != "paused" || got.InFlight {
		t.Fatalf("paused in-flight state was not recovered safely: %+v %v", got, err)
	}
}
