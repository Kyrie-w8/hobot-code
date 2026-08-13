package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceIsolationRequiresCleanCommittedRepository(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	root := t.TempDir()
	runGitTest(t, git, root, "init", "-q")
	runGitTest(t, git, root, "config", "user.name", "Hobot Test")
	runGitTest(t, git, root, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("board project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, root, "add", "README.md")
	runGitTest(t, git, root, "commit", "-qm", "initial")

	clean, err := inspectWorkspaceIsolation(root)
	if err != nil || !clean.Available || !clean.Repository || !clean.Clean || !clean.Eligible || clean.RecommendedMode != workspaceModeWorktree || clean.Head == "" {
		t.Fatalf("clean repository inspection=%+v err=%v", clean, err)
	}
	if err := os.WriteFile(filepath.Join(root, "local.bin"), []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirty, err := inspectWorkspaceIsolation(root)
	if err != nil || dirty.Clean || dirty.Eligible || dirty.RecommendedMode != workspaceModeShared || !strings.Contains(dirty.Reason, "untracked") {
		t.Fatalf("dirty repository inspection=%+v err=%v", dirty, err)
	}
	if err := verifyWorkspaceIsolationBase(context.Background(), git, root, clean.Head); err == nil {
		t.Fatal("repository mutation after inspection was not detected")
	}

	nonRepository, err := inspectWorkspaceIsolation(t.TempDir())
	if err != nil || nonRepository.Repository || nonRepository.Eligible || nonRepository.RecommendedMode != workspaceModeShared {
		t.Fatalf("non-repository inspection=%+v err=%v", nonRepository, err)
	}
}

func TestManagedTaskWorktreeIsIsolatedRetainedAndExplicitlyCleaned(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	project := filepath.Join(repository, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(project, "main.txt")
	if err := os.WriteFile(tracked, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".gitignore"), []byte("cache.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")

	taskID := "11223344556677889900aabb"
	workspace, err := manager.createTaskWorktree(project, taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.rollbackTaskWorktree(taskID) })
	physicalWorktreesRoot, err := filepath.EvalSymlinks(cfg.WorktreesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !pathWithin(physicalWorktreesRoot, workspace.Cwd) || workspace.WorktreePath == "" || workspace.BaseRevision == "" {
		t.Fatalf("invalid managed worktree: %+v", workspace)
	}
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "main.txt"), []byte("isolated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(tracked)
	if err != nil || string(original) != "original\n" {
		t.Fatalf("isolated write changed source project: %q err=%v", original, err)
	}
	if _, err := manager.cleanupTaskWorktree(workspaceCleanupParams{TaskID: taskID}); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("dirty workspace cleanup was accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "main.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(workspace.Cwd, "cache.bin")
	if err := os.WriteFile(ignored, []byte("generated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.cleanupTaskWorktree(workspaceCleanupParams{TaskID: taskID}); err == nil || !strings.Contains(err.Error(), "ignored") {
		t.Fatalf("ignored workspace artifact was discarded: %v", err)
	}
	if err := os.Remove(ignored); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.tasks[taskID] = &task{metadata: taskMetadata{ID: taskID, WorktreePath: workspace.WorktreePath}}
	manager.mu.Unlock()
	if _, err := manager.cleanupTaskWorktree(workspaceCleanupParams{TaskID: taskID}); err == nil || !strings.Contains(err.Error(), "delete every conversation") {
		t.Fatalf("in-use workspace cleanup was accepted: %v", err)
	}
	manager.mu.Lock()
	delete(manager.tasks, taskID)
	manager.mu.Unlock()
	result, err := manager.cleanupTaskWorktree(workspaceCleanupParams{TaskID: taskID})
	if err != nil || !result.Cleaned {
		t.Fatalf("clean retained workspace was not removed: result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(manager.worktreeContainer(taskID)); !os.IsNotExist(err) {
		t.Fatalf("workspace state remains after cleanup: %v", err)
	}
}

func TestManagedTaskWorktreeCleanupPreservesNewCommits(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")

	taskID := "aabbccddeeff001122334455"
	workspace, err := manager.createTaskWorktree(repository, taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.rollbackTaskWorktree(taskID) })
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "main.txt"), []byte("committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, workspace.Cwd, "add", "main.txt")
	runGitTest(t, git, workspace.Cwd, "commit", "-qm", "task result")
	if _, err := manager.cleanupTaskWorktree(workspaceCleanupParams{TaskID: taskID}); err == nil || !strings.Contains(err.Error(), "contains commits") {
		t.Fatalf("workspace commit was discarded: %v", err)
	}
}

func TestWorkspaceDeliveryAppliesStagedChangesAndThenAllowsCleanup(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "modify.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "delete.txt"), []byte("remove me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "binary.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")

	taskID := "3344556677889900aabbccdd"
	workspace, err := manager.createTaskWorktree(repository, taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.rollbackTaskWorktree(taskID) })
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "modify.txt"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace.Cwd, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "new.txt"), []byte("new file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "binary.bin"), []byte{0, 9, 8, 7, 6}, 0o600); err != nil {
		t.Fatal(err)
	}

	inspection, err := manager.inspectWorkspaceDelivery(workspaceDeliveryParams{TaskID: taskID})
	if err != nil || !inspection.Ready || inspection.PatchBytes == 0 || !validSHA256Digest(inspection.Digest) {
		t.Fatalf("delivery was not ready: inspection=%+v err=%v", inspection, err)
	}
	result, err := manager.applyWorkspaceDelivery(workspaceApplyParams{TaskID: taskID, ExpectedDigest: inspection.Digest})
	if err != nil || !result.Applied || !result.Staged || result.Digest != inspection.Digest {
		t.Fatalf("delivery failed: result=%+v err=%v", result, err)
	}
	for path, want := range map[string][]byte{
		"modify.txt": []byte("after\n"), "new.txt": []byte("new file\n"), "binary.bin": {0, 9, 8, 7, 6},
	} {
		got, readErr := os.ReadFile(filepath.Join(repository, path))
		if readErr != nil || string(got) != string(want) {
			t.Fatalf("delivered %s = %v err=%v, want %v", path, got, readErr, want)
		}
	}
	if _, err := os.Stat(filepath.Join(repository, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file remains after delivery: %v", err)
	}
	stagedCommand := exec.CommandContext(context.Background(), git, "diff", "--cached", "--name-status")
	stagedCommand.Dir = repository
	stagedBytes, err := stagedCommand.Output()
	if err != nil {
		t.Fatal(err)
	}
	staged := string(stagedBytes)
	for _, expected := range []string{"M\tbinary.bin", "D\tdelete.txt", "M\tmodify.txt", "A\tnew.txt"} {
		if !strings.Contains(staged, expected) {
			t.Fatalf("delivered changes are not staged: %q missing %q", staged, expected)
		}
	}
	repeated, err := manager.inspectWorkspaceDelivery(workspaceDeliveryParams{TaskID: taskID})
	if err != nil || repeated.Ready || !repeated.AlreadyApplied {
		t.Fatalf("repeated delivery inspection=%+v err=%v", repeated, err)
	}
	if _, err := manager.applyWorkspaceDelivery(workspaceApplyParams{TaskID: taskID, ExpectedDigest: inspection.Digest}); err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Fatalf("repeated delivery was accepted: %v", err)
	}
	cleanup, err := manager.cleanupTaskWorktree(workspaceCleanupParams{TaskID: taskID})
	if err != nil || !cleanup.Cleaned {
		t.Fatalf("delivered worktree could not be cleaned: cleanup=%+v err=%v", cleanup, err)
	}
}

func TestWorkspaceDeliveryRejectsLiveTasksAndChangedSource(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")
	taskID := "44556677889900aabbccddee"
	workspace, err := manager.createTaskWorktree(repository, taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.rollbackTaskWorktree(taskID) })
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "main.txt"), []byte("isolated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.tasks[taskID] = &task{metadata: taskMetadata{ID: taskID, WorkspaceMode: workspaceModeWorktree, WorkspaceID: taskID, WorktreePath: workspace.WorktreePath, Status: statusRunning}}
	manager.mu.Unlock()
	live, err := manager.inspectWorkspaceDelivery(workspaceDeliveryParams{TaskID: taskID})
	if err != nil || live.Ready || !strings.Contains(live.Reason, "finish its current turn") {
		t.Fatalf("live task delivery inspection=%+v err=%v", live, err)
	}
	manager.mu.Lock()
	manager.tasks[taskID].metadata.Status = statusStopped
	manager.mu.Unlock()
	if err := os.WriteFile(filepath.Join(repository, "outside.txt"), []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := manager.inspectWorkspaceDelivery(workspaceDeliveryParams{TaskID: taskID})
	if err != nil || changed.Ready || !strings.Contains(changed.Reason, "original project changed") {
		t.Fatalf("changed source delivery inspection=%+v err=%v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(repository, "main.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceDeliveryStopsIdleAgentsOnlyWhenApplying(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")
	workspaceID := "556677889900aabbccddeeff"
	taskID := "6677889900aabbccddeeff00"
	workspace, err := manager.createTaskWorktree(repository, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.rollbackTaskWorktree(workspaceID) })
	if err := os.WriteFile(filepath.Join(workspace.Cwd, "main.txt"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{manager: manager, metadata: taskMetadata{
		ID: taskID, WorkspaceMode: workspaceModeWorktree, WorkspaceID: workspaceID,
		WorktreePath: workspace.WorktreePath, Status: statusIdle,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}}
	sharedTaskID := "77889900aabbccddeeff0011"
	shared := &task{manager: manager, metadata: taskMetadata{
		ID: sharedTaskID, Cwd: repository, ProjectCwd: repository, WorkspaceMode: workspaceModeShared,
		Status: statusIdle, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}}
	manager.mu.Lock()
	manager.tasks[taskID] = current
	manager.tasks[sharedTaskID] = shared
	manager.mu.Unlock()
	inspection, err := manager.inspectWorkspaceDelivery(workspaceDeliveryParams{TaskID: taskID})
	if err != nil || !inspection.Ready || current.snapshot().Status != statusIdle || shared.snapshot().Status != statusIdle {
		t.Fatalf("inspection changed idle tasks: inspection=%+v isolated=%s shared=%s err=%v", inspection, current.snapshot().Status, shared.snapshot().Status, err)
	}
	if _, err := manager.applyWorkspaceDelivery(workspaceApplyParams{TaskID: taskID, ExpectedDigest: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "reviewed snapshot") {
		t.Fatalf("stale reviewed digest was accepted: %v", err)
	}
	if current.snapshot().Status != statusIdle || shared.snapshot().Status != statusIdle {
		t.Fatalf("digest mismatch stopped tasks: isolated=%s shared=%s", current.snapshot().Status, shared.snapshot().Status)
	}
	result, err := manager.applyWorkspaceDelivery(workspaceApplyParams{TaskID: taskID, ExpectedDigest: inspection.Digest})
	if err != nil || !result.Applied || current.snapshot().Status != statusStopped || shared.snapshot().Status != statusStopped {
		t.Fatalf("apply did not stop idle tasks: result=%+v isolated=%s shared=%s err=%v", result, current.snapshot().Status, shared.snapshot().Status, err)
	}
}

func TestTaskStartCreatesWorktreeAndForkInheritsIt(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")

	metadata, err := manager.start(startTaskParams{
		Name: "isolated", Cwd: repository, Prompt: "test", WorkspaceMode: workspaceModeWorktree,
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := waitForStatus(t, current, statusIdle)
	physicalRepository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	if state.WorkspaceMode != workspaceModeWorktree || state.ProjectCwd != physicalRepository || state.WorktreePath == "" || state.Cwd == physicalRepository || !pathWithin(state.WorktreePath, state.Cwd) {
		t.Fatalf("task did not persist isolated workspace identity: %+v", state)
	}
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)

	// A real Pi session contains the fork graph. Reuse the settled test fixture to
	// verify that child conversations inherit the workspace instead of creating
	// another worktree or falling back to the source directory.
	source := addSettledSourceTask(t, manager, cfg)
	source.mu.Lock()
	source.metadata.Cwd = state.Cwd
	source.metadata.ProjectCwd = state.ProjectCwd
	source.metadata.WorkspaceMode = state.WorkspaceMode
	source.metadata.WorkspaceID = state.WorkspaceID
	source.metadata.WorktreePath = state.WorktreePath
	source.metadata.WorktreeBase = state.WorktreeBase
	source.mu.Unlock()
	if err := source.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	child, err := manager.fork(forkTaskParams{TaskID: source.metadata.ID, Kind: "side"})
	if err != nil {
		t.Fatal(err)
	}
	if child.Cwd != state.Cwd || child.ProjectCwd != state.ProjectCwd || child.WorkspaceMode != workspaceModeWorktree || child.WorkspaceID != state.WorkspaceID || child.WorktreePath != state.WorktreePath || child.WorktreeBase != state.WorktreeBase {
		t.Fatalf("side task did not inherit workspace identity: parent=%+v child=%+v", state, child)
	}
	reloaded, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reloaded.get(metadata.ID)
	if err != nil || recovered.snapshot().WorkspaceID != metadata.ID {
		t.Fatalf("valid isolated task was not recovered: task=%+v err=%v", recovered, err)
	}
	manager.rollbackTaskWorktree(metadata.ID)
}

func TestTaskReloadPreservesConversationWhenIsolatedWorkspaceIsUnavailable(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")

	metadata, err := manager.start(startTaskParams{
		Name: "preserved", Cwd: repository, Prompt: "test", WorkspaceMode: workspaceModeWorktree,
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.get(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := waitForStatus(t, current, statusIdle)
	if err := current.stop(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusStopped)
	moved := state.WorktreePath + ".unavailable"
	if err := os.Rename(state.WorktreePath, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Rename(moved, state.WorktreePath)
		manager.rollbackTaskWorktree(metadata.ID)
	})

	reloaded, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reloaded.get(metadata.ID)
	if err != nil {
		t.Fatalf("conversation disappeared when its workspace became unavailable: %v", err)
	}
	recoveredState := recovered.snapshot()
	if recoveredState.Status != statusFailed || recoveredState.Failure == nil || recoveredState.Failure.Code != "workspace-unavailable" || recoveredState.Failure.Recovery != "diagnose" {
		t.Fatalf("unexpected unavailable workspace recovery: %+v", recoveredState)
	}
}

func TestWorkspaceModeValidation(t *testing.T) {
	for _, valid := range []string{"", "shared", "WORKTREE"} {
		if _, err := normalizeWorkspaceMode(valid); err != nil {
			t.Fatalf("valid mode %q rejected: %v", valid, err)
		}
	}
	if _, err := normalizeWorkspaceMode("unsafe"); err == nil {
		t.Fatal("invalid workspace mode was accepted")
	}
}

func TestExecutableGitFilterParser(t *testing.T) {
	if !hasExecutableGitFilter([]byte("core.repositoryformatversion\n0\x00filter.demo.smudge\n/bin/filter\x00")) {
		t.Fatal("newline-delimited git filter was missed")
	}
	if !hasExecutableGitFilter([]byte("filter.demo.process=/bin/filter\x00")) {
		t.Fatal("equals-delimited git filter was missed")
	}
	if hasExecutableGitFilter([]byte("core.autocrlf\nfalse\x00remote.origin.url\nhttps://example.invalid/repo\x00")) {
		t.Fatal("ordinary git configuration was treated as an executable filter")
	}
}

func TestManagerIsolationRejectsOverlappingStateRoot(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	repository := t.TempDir()
	runGitTest(t, git, repository, "init", "-q")
	runGitTest(t, git, repository, "config", "user.name", "Hobot Test")
	runGitTest(t, git, repository, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "main.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, repository, "add", ".")
	runGitTest(t, git, repository, "commit", "-qm", "initial")
	worktreesRoot := filepath.Join(repository, ".hobot-state", "worktrees")
	if err := os.MkdirAll(worktreesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &taskManager{cfg: config{WorktreesRoot: worktreesRoot}}
	inspection, err := manager.inspectWorkspaceIsolation(repository)
	if err != nil || inspection.Eligible || inspection.RecommendedMode != workspaceModeShared || !strings.Contains(inspection.Reason, "overlaps") {
		t.Fatalf("overlapping state root was accepted: inspection=%+v err=%v", inspection, err)
	}
}

func TestWorkspaceIsolationRejectsExecutableGitFilters(t *testing.T) {
	git := trustedGitBinary()
	if git == "" {
		t.Skip("trusted system Git is unavailable")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "filter-ran")
	filter := filepath.Join(root, "filter.sh")
	if err := os.WriteFile(filter, []byte("#!/bin/sh\ntouch \"$HOBOT_FILTER_MARKER\"\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, root, "init", "-q")
	runGitTest(t, git, root, "config", "user.name", "Hobot Test")
	runGitTest(t, git, root, "config", "user.email", "hobot@example.invalid")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.txt filter=hobot-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, root, "add", ".")
	runGitTest(t, git, root, "commit", "-qm", "initial")
	runGitTest(t, git, root, "config", "filter.hobot-test.smudge", filter)
	t.Setenv("HOBOT_FILTER_MARKER", marker)

	inspection, err := inspectWorkspaceIsolation(root)
	if err != nil || inspection.Eligible || !strings.Contains(inspection.Reason, "custom Git") {
		t.Fatalf("executable filter was not rejected: inspection=%+v err=%v", inspection, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("git filter executed during isolation inspection: %v", err)
	}
}
