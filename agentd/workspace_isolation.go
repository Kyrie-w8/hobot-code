package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	workspaceModeShared        = "shared"
	workspaceModeWorktree      = "worktree"
	worktreeManifestSchema     = 1
	workspaceIsolationTimeout  = 10 * time.Second
	workspaceWorktreeTimeout   = 2 * time.Minute
	maximumWorktreeManifestB   = 64 * 1024
	maximumManagedWorktreeList = 200
	maximumDeliveryPatchBytes  = 32 * 1024 * 1024
)

var workspaceMutationSlots = make(chan struct{}, 1)

type workspaceIsolationParams struct {
	Path string `json:"path"`
}

type workspaceIsolation struct {
	CapturedAt      time.Time `json:"capturedAt"`
	Available       bool      `json:"available"`
	Repository      bool      `json:"repository"`
	Eligible        bool      `json:"eligible"`
	RecommendedMode string    `json:"recommendedMode"`
	RepositoryRoot  string    `json:"repositoryRoot,omitempty"`
	Scope           string    `json:"scope,omitempty"`
	Head            string    `json:"head,omitempty"`
	Clean           bool      `json:"clean"`
	Reason          string    `json:"reason"`
}

type taskWorktreeManifest struct {
	Schema         int        `json:"schema"`
	TaskID         string     `json:"taskId"`
	ProjectCwd     string     `json:"projectCwd"`
	RepositoryRoot string     `json:"repositoryRoot"`
	WorktreePath   string     `json:"worktreePath"`
	GitCommonDir   string     `json:"gitCommonDir"`
	GitDir         string     `json:"gitDir"`
	Scope          string     `json:"scope"`
	BaseRevision   string     `json:"baseRevision"`
	CreatedAt      time.Time  `json:"createdAt"`
	AppliedAt      *time.Time `json:"appliedAt,omitempty"`
	AppliedDigest  string     `json:"appliedDigest,omitempty"`
}

type taskWorktree struct {
	Cwd          string
	WorktreePath string
	BaseRevision string
}

type workspaceCleanupParams struct {
	TaskID string `json:"taskId"`
}

type workspaceCleanupResult struct {
	TaskID  string `json:"taskId"`
	Cleaned bool   `json:"cleaned"`
}

type workspaceDeliveryParams struct {
	TaskID string `json:"taskId"`
}

type workspaceApplyParams struct {
	TaskID         string `json:"taskId"`
	ExpectedDigest string `json:"expectedDigest"`
}

type workspaceDelivery struct {
	TaskID         string `json:"taskId"`
	Ready          bool   `json:"ready"`
	Reason         string `json:"reason"`
	PatchBytes     int    `json:"patchBytes,omitempty"`
	Digest         string `json:"digest,omitempty"`
	AlreadyApplied bool   `json:"alreadyApplied,omitempty"`
}

type workspaceApplyResult struct {
	TaskID     string    `json:"taskId"`
	Applied    bool      `json:"applied"`
	Staged     bool      `json:"staged"`
	PatchBytes int       `json:"patchBytes"`
	Digest     string    `json:"digest"`
	AppliedAt  time.Time `json:"appliedAt"`
}

type managedWorktree struct {
	TaskID       string    `json:"taskId"`
	ProjectCwd   string    `json:"projectCwd"`
	Path         string    `json:"path"`
	BaseRevision string    `json:"baseRevision"`
	CreatedAt    time.Time `json:"createdAt"`
	InUse        bool      `json:"inUse"`
}

type managedWorktreeList struct {
	Worktrees []managedWorktree `json:"worktrees"`
	Truncated bool              `json:"truncated,omitempty"`
}

func normalizeWorkspaceMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", workspaceModeShared:
		return workspaceModeShared, nil
	case workspaceModeWorktree:
		return workspaceModeWorktree, nil
	default:
		return "", fmt.Errorf("workspace mode must be shared or worktree")
	}
}

func (manager *taskManager) validateManagedTaskWorkspace(metadata taskMetadata) error {
	mode, err := normalizeWorkspaceMode(metadata.WorkspaceMode)
	if err != nil {
		return err
	}
	if mode == workspaceModeShared {
		return nil
	}
	if !taskIDPattern.MatchString(metadata.WorkspaceID) {
		return fmt.Errorf("isolated workspace id is invalid")
	}
	manifest, err := manager.readWorktreeManifest(metadata.WorkspaceID)
	if err != nil {
		return err
	}
	if metadata.ProjectCwd != manifest.ProjectCwd || metadata.WorktreePath != manifest.WorktreePath || metadata.WorktreeBase != manifest.BaseRevision {
		return fmt.Errorf("isolated workspace no longer matches task metadata")
	}
	physicalCwd, err := normalizeWorkingDirectory(metadata.Cwd)
	if err != nil || physicalCwd != metadata.Cwd || !pathWithin(manifest.WorktreePath, physicalCwd) {
		return fmt.Errorf("isolated task directory is unavailable")
	}
	git := trustedGitBinary()
	if git == "" {
		return fmt.Errorf("a trusted system Git installation is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), workspaceIsolationTimeout)
	defer cancel()
	commonDir, err := resolveGitDirectory(ctx, git, manifest.WorktreePath, "--git-common-dir")
	if err != nil || commonDir != manifest.GitCommonDir {
		return fmt.Errorf("isolated workspace git metadata no longer matches its creation record")
	}
	gitDir, err := resolveGitDirectory(ctx, git, manifest.WorktreePath, "--git-dir")
	if err != nil || gitDir != manifest.GitDir {
		return fmt.Errorf("isolated workspace git metadata no longer matches its creation record")
	}
	return nil
}

func inspectWorkspaceIsolation(path string) (workspaceIsolation, error) {
	result := workspaceIsolation{
		CapturedAt: time.Now().UTC(), RecommendedMode: workspaceModeShared,
		Reason: "This directory is not eligible for an isolated Git worktree.",
	}
	physicalCwd, err := normalizeWorkingDirectory(path)
	if err != nil {
		return result, fmt.Errorf("workspace is unavailable")
	}
	git := trustedGitBinary()
	if git == "" {
		result.Reason = "A trusted system Git installation is not available on this board."
		return result, nil
	}
	result.Available = true
	ctx, cancel := context.WithTimeout(context.Background(), workspaceIsolationTimeout)
	defer cancel()
	select {
	case workspaceChangeSlots <- struct{}{}:
		defer func() { <-workspaceChangeSlots }()
	case <-ctx.Done():
		return result, fmt.Errorf("workspace inspection is busy")
	}

	rootOutput, _, rootErr := runGitBounded(ctx, git, physicalCwd, 4096, "rev-parse", "--show-toplevel")
	if rootErr != nil {
		if errors.Is(rootErr, errGitNotRepository) {
			result.Reason = "This directory is not inside a Git repository."
			return result, nil
		}
		return result, rootErr
	}
	physicalRoot, err := physicalRepositoryRoot(strings.TrimSpace(string(rootOutput)), physicalCwd)
	if err != nil {
		return result, err
	}
	scope, err := filepath.Rel(physicalRoot, physicalCwd)
	if err != nil || scope == ".." || strings.HasPrefix(scope, ".."+string(filepath.Separator)) {
		return result, fmt.Errorf("workspace is outside the git repository")
	}
	if scope == "" {
		scope = "."
	}
	result.Repository = true
	result.RepositoryRoot = physicalRoot
	result.Scope = filepath.ToSlash(scope)
	filterOutput, filterTruncated, filterErr := runGitBounded(ctx, git, physicalRoot, 64*1024,
		"config", "--includes", "--null", "--list")
	if filterErr != nil {
		return result, filterErr
	}
	if filterTruncated || hasExecutableGitFilter(filterOutput) {
		result.Reason = "Repositories with custom Git clean, smudge, or process filters cannot be isolated safely."
		return result, nil
	}
	if _, _, modulesErr := runGitBounded(ctx, git, physicalRoot, 16, "ls-files", "--error-unmatch", ".gitmodules"); modulesErr == nil {
		result.Reason = "Repositories with Git submodules currently use the shared workspace so submodule contents are not omitted."
		return result, nil
	} else if !errors.Is(modulesErr, errGitCommandFailed) {
		return result, modulesErr
	}

	bareOutput, _, err := runGitBounded(ctx, git, physicalRoot, 16, "rev-parse", "--is-bare-repository")
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(string(bareOutput)) != "false" {
		result.Reason = "Bare repositories cannot be used as task workspaces."
		return result, nil
	}
	headOutput, _, err := runGitBounded(ctx, git, physicalRoot, 128, "rev-parse", "--verify", "HEAD")
	if err != nil {
		result.Reason = "Create an initial commit before using an isolated task workspace."
		return result, nil
	}
	result.Head = strings.TrimSpace(string(headOutput))
	statusOutput, statusTruncated, err := runGitBounded(ctx, git, physicalRoot, maximumGitStatusBytes,
		"status", "--porcelain=v2", "-z", "--untracked-files=normal", "--ignore-submodules=all")
	if err != nil {
		return result, err
	}
	result.Clean = len(statusOutput) == 0 && !statusTruncated
	if !result.Clean {
		result.Reason = "Commit or remove all tracked and untracked changes before creating an isolated task workspace."
		return result, nil
	}
	result.Eligible = true
	result.RecommendedMode = workspaceModeWorktree
	result.Reason = "A clean Git repository can be isolated from other tasks."
	return result, nil
}

func (manager *taskManager) inspectWorkspaceIsolation(path string) (workspaceIsolation, error) {
	result, err := inspectWorkspaceIsolation(path)
	if err != nil || !result.Eligible {
		return result, err
	}
	physicalState, err := filepath.EvalSymlinks(manager.cfg.WorktreesRoot)
	if err != nil {
		return result, fmt.Errorf("isolated workspace state is unavailable")
	}
	if pathWithin(result.RepositoryRoot, physicalState) || pathWithin(physicalState, result.RepositoryRoot) {
		result.Eligible = false
		result.RecommendedMode = workspaceModeShared
		result.Reason = "The configured Hobot Code state directory overlaps this repository; choose the shared workspace or move the state directory."
	}
	return result, nil
}

func hasExecutableGitFilter(content []byte) bool {
	for _, record := range strings.Split(string(content), "\x00") {
		key := record
		if index := strings.IndexAny(key, "\n="); index >= 0 {
			key = key[:index]
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if !strings.HasPrefix(key, "filter.") {
			continue
		}
		for _, suffix := range []string{".clean", ".smudge", ".process", ".required"} {
			if strings.HasSuffix(key, suffix) {
				return true
			}
		}
	}
	return false
}

func physicalRepositoryRoot(root, cwd string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("git returned an invalid repository root")
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !pathWithin(physicalRoot, cwd) {
		return "", fmt.Errorf("git repository root is outside the workspace ancestry")
	}
	return physicalRoot, nil
}

func (manager *taskManager) createTaskWorktree(projectCwd, taskID string) (taskWorktree, error) {
	if !taskIDPattern.MatchString(taskID) {
		return taskWorktree{}, fmt.Errorf("task id is invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), workspaceWorktreeTimeout)
	defer cancel()
	select {
	case workspaceMutationSlots <- struct{}{}:
		defer func() { <-workspaceMutationSlots }()
	case <-ctx.Done():
		return taskWorktree{}, fmt.Errorf("workspace creation is busy")
	}
	inspection, err := manager.inspectWorkspaceIsolation(projectCwd)
	if err != nil {
		return taskWorktree{}, err
	}
	if !inspection.Eligible {
		return taskWorktree{}, fmt.Errorf("cannot create isolated workspace: %s", inspection.Reason)
	}
	container := manager.worktreeContainer(taskID)
	if err := os.Mkdir(container, 0o700); err != nil {
		return taskWorktree{}, fmt.Errorf("create isolated workspace state: %w", err)
	}
	worktreePath := filepath.Join(container, "workspace")
	git := trustedGitBinary()
	if git == "" {
		_ = os.Remove(container)
		return taskWorktree{}, fmt.Errorf("trusted system Git became unavailable")
	}
	if err := verifyWorkspaceIsolationBase(ctx, git, inspection.RepositoryRoot, inspection.Head); err != nil {
		_ = os.Remove(container)
		return taskWorktree{}, err
	}
	if _, _, err := runGitBounded(ctx, git, inspection.RepositoryRoot, 64*1024,
		"worktree", "add", "--detach", worktreePath, inspection.Head); err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, fmt.Errorf("create isolated git worktree: %w", err)
	}
	if err := os.Chmod(worktreePath, 0o700); err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, fmt.Errorf("protect isolated workspace: %w", err)
	}
	physicalWorktreePath, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, fmt.Errorf("resolve isolated workspace path")
	}
	worktreePath = physicalWorktreePath
	gitCommonDir, err := resolveGitDirectory(ctx, git, worktreePath, "--git-common-dir")
	if err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, err
	}
	gitDir, err := resolveGitDirectory(ctx, git, worktreePath, "--git-dir")
	if err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, err
	}
	manifest := taskWorktreeManifest{
		Schema: worktreeManifestSchema, TaskID: taskID, ProjectCwd: projectCwd,
		RepositoryRoot: inspection.RepositoryRoot, WorktreePath: worktreePath,
		GitCommonDir: gitCommonDir, GitDir: gitDir, Scope: inspection.Scope,
		BaseRevision: inspection.Head, CreatedAt: time.Now().UTC(),
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, err
	}
	if err := writePrivateFile(filepath.Join(container, "manifest.json"), append(encoded, '\n')); err != nil {
		manager.rollbackNewWorktree(inspection.RepositoryRoot, worktreePath, container)
		return taskWorktree{}, err
	}
	actualCwd := worktreePath
	if inspection.Scope != "." {
		actualCwd = filepath.Join(worktreePath, filepath.FromSlash(inspection.Scope))
	}
	physicalCwd, err := normalizeWorkingDirectory(actualCwd)
	if err != nil || !pathWithin(worktreePath, physicalCwd) {
		manager.rollbackTaskWorktree(taskID)
		return taskWorktree{}, fmt.Errorf("isolated workspace does not contain the selected project directory")
	}
	return taskWorktree{Cwd: physicalCwd, WorktreePath: worktreePath, BaseRevision: inspection.Head}, nil
}

func verifyWorkspaceIsolationBase(ctx context.Context, git, repositoryRoot, expectedHead string) error {
	head, _, err := runGitBounded(ctx, git, repositoryRoot, 128, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != expectedHead {
		return fmt.Errorf("repository changed while the isolated workspace was being created; inspect it again")
	}
	status, truncated, err := runGitBounded(ctx, git, repositoryRoot, maximumGitStatusBytes,
		"status", "--porcelain=v2", "-z", "--untracked-files=normal", "--ignore-submodules=all")
	if err != nil {
		return fmt.Errorf("recheck repository before creating isolated workspace: %w", err)
	}
	if len(status) > 0 || truncated {
		return fmt.Errorf("repository changed while the isolated workspace was being created; inspect it again")
	}
	filters, filtersTruncated, err := runGitBounded(ctx, git, repositoryRoot, 64*1024,
		"config", "--includes", "--null", "--list")
	if err != nil {
		return fmt.Errorf("recheck git configuration before creating isolated workspace: %w", err)
	}
	if filtersTruncated || hasExecutableGitFilter(filters) {
		return fmt.Errorf("git filter configuration changed while the isolated workspace was being created")
	}
	return nil
}

func resolveGitDirectory(ctx context.Context, git, worktreePath, argument string) (string, error) {
	output, _, err := runGitBounded(ctx, git, worktreePath, 4096, "rev-parse", argument)
	if err != nil {
		return "", fmt.Errorf("inspect isolated git metadata: %w", err)
	}
	value := strings.TrimSpace(string(output))
	if !filepath.IsAbs(value) {
		value = filepath.Join(worktreePath, value)
	}
	physical, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", fmt.Errorf("resolve isolated git metadata")
	}
	return physical, nil
}

func (manager *taskManager) rollbackNewWorktree(repositoryRoot, worktreePath, container string) {
	if git := trustedGitBinary(); git != "" {
		ctx, cancel := context.WithTimeout(context.Background(), workspaceWorktreeTimeout)
		_, _, _ = runGitBounded(ctx, git, repositoryRoot, 4096, "worktree", "remove", "--force", worktreePath)
		cancel()
	}
	if pathWithin(manager.cfg.WorktreesRoot, container) && container != manager.cfg.WorktreesRoot {
		_ = os.RemoveAll(container)
	}
}

func (manager *taskManager) rollbackTaskWorktree(taskID string) {
	manifest, err := manager.readWorktreeManifest(taskID)
	if err != nil {
		return
	}
	if git := trustedGitBinary(); git != "" {
		ctx, cancel := context.WithTimeout(context.Background(), workspaceWorktreeTimeout)
		_, _, _ = runGitBounded(ctx, git, manifest.RepositoryRoot, 4096,
			"worktree", "remove", "--force", manifest.WorktreePath)
		cancel()
	}
	container := manager.worktreeContainer(taskID)
	if validWorktreeContainer(manager.cfg.WorktreesRoot, container, taskID) == nil {
		_ = os.RemoveAll(container)
	}
}

func (manager *taskManager) cleanupTaskWorktree(params workspaceCleanupParams) (workspaceCleanupResult, error) {
	if !taskIDPattern.MatchString(params.TaskID) {
		return workspaceCleanupResult{}, fmt.Errorf("task id is invalid")
	}
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	manifest, err := manager.readWorktreeManifest(params.TaskID)
	if err != nil {
		return workspaceCleanupResult{}, err
	}
	manager.mu.RLock()
	tasks := make([]*task, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		tasks = append(tasks, current)
	}
	manager.mu.RUnlock()
	for _, current := range tasks {
		metadata := current.snapshot()
		if metadata.WorktreePath == manifest.WorktreePath {
			return workspaceCleanupResult{}, fmt.Errorf("delete every conversation using this workspace before cleaning it up")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), workspaceWorktreeTimeout)
	defer cancel()
	select {
	case workspaceMutationSlots <- struct{}{}:
		defer func() { <-workspaceMutationSlots }()
	case <-ctx.Done():
		return workspaceCleanupResult{}, fmt.Errorf("workspace cleanup is busy")
	}
	git := trustedGitBinary()
	if git == "" {
		return workspaceCleanupResult{}, fmt.Errorf("a trusted system Git installation is required for cleanup")
	}
	commonDir, err := resolveGitDirectory(ctx, git, manifest.WorktreePath, "--git-common-dir")
	if err != nil || commonDir != manifest.GitCommonDir {
		return workspaceCleanupResult{}, fmt.Errorf("isolated workspace git metadata no longer matches its creation record")
	}
	gitDir, err := resolveGitDirectory(ctx, git, manifest.WorktreePath, "--git-dir")
	if err != nil || gitDir != manifest.GitDir {
		return workspaceCleanupResult{}, fmt.Errorf("isolated workspace git metadata no longer matches its creation record")
	}
	head, _, err := runGitBounded(ctx, git, manifest.WorktreePath, 128, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return workspaceCleanupResult{}, fmt.Errorf("inspect isolated workspace revision before cleanup: %w", err)
	}
	if strings.TrimSpace(string(head)) != manifest.BaseRevision {
		return workspaceCleanupResult{}, fmt.Errorf("isolated workspace contains commits not present at creation; preserve or merge them before cleanup")
	}
	forceRemove := false
	if manifest.AppliedAt != nil && manifest.AppliedDigest != "" {
		ignored, ignoredTruncated, ignoredErr := runGitBounded(ctx, git, manifest.WorktreePath, maximumGitStatusBytes,
			"ls-files", "--others", "--ignored", "--exclude-standard", "-z")
		if ignoredErr != nil || ignoredTruncated || len(ignored) > 0 {
			return workspaceCleanupResult{}, fmt.Errorf("isolated workspace has ignored artifacts; preserve or remove them before cleanup")
		}
		patch, digest, patchErr := manager.snapshotWorkspaceDeliveryPatch(ctx, manifest)
		if patchErr != nil || len(patch) == 0 || digest != manifest.AppliedDigest {
			return workspaceCleanupResult{}, fmt.Errorf("isolated workspace changed after delivery; preserve its new changes before cleanup")
		}
		forceRemove = true
	} else {
		status, truncated, statusErr := runGitBounded(ctx, git, manifest.WorktreePath, maximumGitStatusBytes,
			"status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignored=matching", "--ignore-submodules=none")
		if statusErr != nil {
			return workspaceCleanupResult{}, fmt.Errorf("inspect isolated workspace before cleanup: %w", statusErr)
		}
		if len(status) > 0 || truncated {
			return workspaceCleanupResult{}, fmt.Errorf("isolated workspace has uncommitted, untracked, ignored, or submodule files; preserve them before cleanup")
		}
	}
	removeArguments := []string{"worktree", "remove"}
	if forceRemove {
		removeArguments = append(removeArguments, "--force")
	}
	removeArguments = append(removeArguments, manifest.WorktreePath)
	if _, _, err := runGitBounded(ctx, git, manifest.RepositoryRoot, 4096,
		removeArguments...); err != nil {
		return workspaceCleanupResult{}, fmt.Errorf("remove isolated git worktree: %w", err)
	}
	container := manager.worktreeContainer(params.TaskID)
	if err := os.Remove(filepath.Join(container, "manifest.json")); err != nil {
		return workspaceCleanupResult{}, fmt.Errorf("remove isolated workspace manifest: %w", err)
	}
	if err := os.Remove(container); err != nil {
		return workspaceCleanupResult{}, fmt.Errorf("remove isolated workspace state: %w", err)
	}
	return workspaceCleanupResult{TaskID: params.TaskID, Cleaned: true}, nil
}

func (manager *taskManager) inspectWorkspaceDelivery(params workspaceDeliveryParams) (workspaceDelivery, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), workspaceWorktreeTimeout)
	defer cancel()
	select {
	case workspaceMutationSlots <- struct{}{}:
		defer func() { <-workspaceMutationSlots }()
	case <-ctx.Done():
		return workspaceDelivery{}, fmt.Errorf("workspace delivery inspection is busy")
	}
	manifest, patch, digest, err := manager.prepareWorkspaceDelivery(ctx, params.TaskID, false)
	if err != nil {
		return workspaceDelivery{TaskID: params.TaskID, Reason: err.Error()}, nil
	}
	if manifest.AppliedAt != nil {
		return workspaceDelivery{TaskID: params.TaskID, Reason: "These isolated changes have already been applied to the project.", AlreadyApplied: true, Digest: manifest.AppliedDigest}, nil
	}
	if len(patch) == 0 {
		return workspaceDelivery{TaskID: params.TaskID, Reason: "The isolated workspace has no changes to apply."}, nil
	}
	return workspaceDelivery{
		TaskID: params.TaskID, Ready: true, Reason: "Changes can be applied to the original project as staged Git changes.",
		PatchBytes: len(patch), Digest: digest,
	}, nil
}

func (manager *taskManager) applyWorkspaceDelivery(params workspaceApplyParams) (workspaceApplyResult, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if !validSHA256Digest(params.ExpectedDigest) {
		return workspaceApplyResult{}, fmt.Errorf("reviewed workspace digest is required before applying changes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), workspaceWorktreeTimeout)
	defer cancel()
	select {
	case workspaceMutationSlots <- struct{}{}:
		defer func() { <-workspaceMutationSlots }()
	case <-ctx.Done():
		return workspaceApplyResult{}, fmt.Errorf("workspace delivery is busy")
	}
	manifest, patch, digest, err := manager.prepareWorkspaceDelivery(ctx, params.TaskID, false)
	if err != nil {
		return workspaceApplyResult{}, err
	}
	if manifest.AppliedAt != nil {
		return workspaceApplyResult{}, fmt.Errorf("isolated workspace changes were already applied")
	}
	if len(patch) == 0 {
		return workspaceApplyResult{}, fmt.Errorf("isolated workspace has no changes to apply")
	}
	if digest != params.ExpectedDigest {
		return workspaceApplyResult{}, fmt.Errorf("isolated changes differ from the reviewed snapshot; review them again before applying")
	}
	worktreeLease, err := acquireWorkspaceWriteLease(manager.cfg.StateRoot, manifest.WorktreePath, "workspace-delivery:"+manifest.TaskID)
	if err != nil {
		return workspaceApplyResult{}, fmt.Errorf("lock the isolated workspace for delivery: %w", err)
	}
	worktreeReleased := false
	defer func() {
		if !worktreeReleased {
			_ = worktreeLease.release()
		}
	}()
	projectLease, err := acquireWorkspaceWriteLease(manager.cfg.StateRoot, manifest.RepositoryRoot, "workspace-delivery:"+manifest.TaskID)
	if err != nil {
		return workspaceApplyResult{}, fmt.Errorf("lock the original project for delivery: %w", err)
	}
	projectReleased := false
	defer func() {
		if !projectReleased {
			_ = projectLease.release()
		}
	}()
	if err := manager.ensureWorkspaceDeliveryIdle(manifest, true); err != nil {
		return workspaceApplyResult{}, err
	}
	manifest, patch, digest, err = manager.prepareWorkspaceDelivery(ctx, params.TaskID, false)
	if err != nil {
		return workspaceApplyResult{}, err
	}
	if len(patch) == 0 || digest != params.ExpectedDigest {
		return workspaceApplyResult{}, fmt.Errorf("isolated changes differ from the reviewed snapshot; review them again before applying")
	}
	git := trustedGitBinary()
	if err := verifyWorkspaceIsolationBase(ctx, git, manifest.RepositoryRoot, manifest.BaseRevision); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("the original project changed while delivery was being prepared; preserve or commit those changes before retrying")
	}
	patchPath := filepath.Join(manager.worktreeContainer(manifest.TaskID), "delivery.patch")
	if err := writePrivateFile(patchPath, patch); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("stage workspace delivery patch: %w", err)
	}
	defer os.Remove(patchPath)
	if _, _, err := runGitBounded(ctx, git, manifest.RepositoryRoot, 4096,
		"apply", "--check", "--index", "--binary", patchPath); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("project changed or the isolated changes conflict; review both workspaces before retrying")
	}
	if _, _, err := runGitBounded(ctx, git, manifest.RepositoryRoot, 4096,
		"apply", "--index", "--binary", patchPath); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("apply isolated changes failed; inspect the original project before retrying")
	}
	now := time.Now().UTC()
	manifest.AppliedAt = &now
	manifest.AppliedDigest = digest
	if err := manager.writeWorktreeManifest(manifest); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("changes were staged, but Hobot Code could not record delivery; do not retry before inspecting the project")
	}
	if err := projectLease.release(); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("changes were staged and recorded, but the project delivery lock could not be released; run diagnostics before another write")
	}
	projectReleased = true
	if err := worktreeLease.release(); err != nil {
		return workspaceApplyResult{}, fmt.Errorf("changes were staged and recorded, but the isolated workspace lock could not be released; run diagnostics before another write")
	}
	worktreeReleased = true
	return workspaceApplyResult{TaskID: params.TaskID, Applied: true, Staged: true, PatchBytes: len(patch), Digest: digest, AppliedAt: now}, nil
}

func (manager *taskManager) prepareWorkspaceDelivery(ctx context.Context, taskID string, stopIdle bool) (taskWorktreeManifest, []byte, string, error) {
	if !taskIDPattern.MatchString(taskID) {
		return taskWorktreeManifest{}, nil, "", fmt.Errorf("task id is invalid")
	}
	workspaceID := taskID
	if current, err := manager.get(taskID); err == nil {
		metadata := current.snapshot()
		if metadata.WorkspaceMode != workspaceModeWorktree || !taskIDPattern.MatchString(metadata.WorkspaceID) {
			return taskWorktreeManifest{}, nil, "", fmt.Errorf("task does not use an isolated workspace")
		}
		workspaceID = metadata.WorkspaceID
	}
	manifest, err := manager.readWorktreeManifest(workspaceID)
	if err != nil {
		return taskWorktreeManifest{}, nil, "", err
	}
	if manifest.AppliedAt != nil {
		return manifest, nil, manifest.AppliedDigest, nil
	}
	if err := manager.ensureWorkspaceDeliveryIdle(manifest, stopIdle); err != nil {
		return taskWorktreeManifest{}, nil, "", err
	}
	git := trustedGitBinary()
	if git == "" {
		return taskWorktreeManifest{}, nil, "", fmt.Errorf("a trusted system Git installation is required")
	}
	if err := verifyWorkspaceIsolationBase(ctx, git, manifest.RepositoryRoot, manifest.BaseRevision); err != nil {
		return taskWorktreeManifest{}, nil, "", fmt.Errorf("the original project changed after this isolated task started; preserve or commit those changes before delivery")
	}
	head, _, err := runGitBounded(ctx, git, manifest.WorktreePath, 128, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != manifest.BaseRevision {
		return taskWorktreeManifest{}, nil, "", fmt.Errorf("the isolated workspace contains commits; merge or export them explicitly instead of applying an uncommitted patch")
	}
	commonDir, err := resolveGitDirectory(ctx, git, manifest.WorktreePath, "--git-common-dir")
	if err != nil || commonDir != manifest.GitCommonDir {
		return taskWorktreeManifest{}, nil, "", fmt.Errorf("isolated workspace git metadata no longer matches its creation record")
	}
	patch, digest, err := manager.snapshotWorkspaceDeliveryPatch(ctx, manifest)
	if err != nil {
		return taskWorktreeManifest{}, nil, "", err
	}
	return manifest, patch, digest, nil
}

func (manager *taskManager) snapshotWorkspaceDeliveryPatch(ctx context.Context, manifest taskWorktreeManifest) ([]byte, string, error) {
	git := trustedGitBinary()
	if git == "" {
		return nil, "", fmt.Errorf("a trusted system Git installation is required")
	}
	indexFile, err := os.CreateTemp(manager.worktreeContainer(manifest.TaskID), ".delivery-index.*")
	if err != nil {
		return nil, "", fmt.Errorf("prepare isolated delivery index: %w", err)
	}
	indexPath := indexFile.Name()
	if err := indexFile.Close(); err != nil {
		_ = os.Remove(indexPath)
		return nil, "", err
	}
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	environment := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, _, err := runGitBoundedEnv(ctx, git, manifest.WorktreePath, 4096, environment, "read-tree", "HEAD"); err != nil {
		return nil, "", fmt.Errorf("prepare isolated delivery snapshot: %w", err)
	}
	if _, _, err := runGitBoundedEnv(ctx, git, manifest.WorktreePath, 4096, environment, "add", "--all", "--", "."); err != nil {
		return nil, "", fmt.Errorf("index isolated workspace changes: %w", err)
	}
	patch, truncated, err := runGitBoundedEnv(ctx, git, manifest.WorktreePath, maximumDeliveryPatchBytes, environment,
		"diff", "--cached", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "HEAD", "--")
	if err != nil {
		return nil, "", fmt.Errorf("create isolated delivery patch: %w", err)
	}
	if truncated {
		return nil, "", fmt.Errorf("isolated changes exceed the %d-byte delivery limit; export or commit them manually", maximumDeliveryPatchBytes)
	}
	digestBytes := sha256.Sum256(patch)
	return patch, fmt.Sprintf("%x", digestBytes), nil
}

func (manager *taskManager) ensureWorkspaceDeliveryIdle(manifest taskWorktreeManifest, stopIdle bool) error {
	manager.mu.RLock()
	tasks := make([]*task, 0, len(manager.tasks))
	for _, current := range manager.tasks {
		tasks = append(tasks, current)
	}
	manager.mu.RUnlock()
	matching := make([]*task, 0, len(tasks))
	for _, current := range tasks {
		metadata := current.snapshot()
		affected := metadata.WorktreePath == manifest.WorktreePath
		if !affected && metadata.WorkspaceMode != workspaceModeWorktree {
			root, err := workspaceLeaseRoot(metadata.Cwd)
			affected = err == nil && pathsOverlap(root, manifest.RepositoryRoot)
		}
		if !affected {
			continue
		}
		matching = append(matching, current)
		if isLiveStatus(metadata.Status) && metadata.Status != statusIdle {
			return fmt.Errorf("wait for every Agent using the isolated workspace or original project to finish its current turn before applying changes")
		}
	}
	if !stopIdle {
		return nil
	}
	for _, current := range matching {
		stopped, err := current.stopIfIdle()
		if err != nil {
			return fmt.Errorf("stop idle Agent before applying changes: %w", err)
		}
		if stopped {
			continue
		}
		if isLiveStatus(current.snapshot().Status) {
			return fmt.Errorf("an Agent started another turn while changes were being prepared; wait for it to finish and retry")
		}
	}
	return nil
}

func (manager *taskManager) writeWorktreeManifest(manifest taskWorktreeManifest) error {
	container := manager.worktreeContainer(manifest.TaskID)
	if err := validWorktreeContainer(manager.cfg.WorktreesRoot, container, manifest.TaskID); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(container, "manifest.json"), append(encoded, '\n'))
}

func (manager *taskManager) listTaskWorktrees() (managedWorktreeList, error) {
	entries, err := os.ReadDir(manager.cfg.WorktreesRoot)
	if err != nil {
		return managedWorktreeList{}, err
	}
	result := managedWorktreeList{Worktrees: make([]managedWorktree, 0)}
	for _, entry := range entries {
		if !entry.IsDir() || !taskIDPattern.MatchString(entry.Name()) {
			continue
		}
		if len(result.Worktrees) >= maximumManagedWorktreeList {
			result.Truncated = true
			break
		}
		manifest, err := manager.readWorktreeManifest(entry.Name())
		if err != nil {
			continue
		}
		item := managedWorktree{
			TaskID: manifest.TaskID, ProjectCwd: manifest.ProjectCwd, Path: manifest.WorktreePath,
			BaseRevision: manifest.BaseRevision, CreatedAt: manifest.CreatedAt,
		}
		manager.mu.RLock()
		tasks := make([]*task, 0, len(manager.tasks))
		for _, current := range manager.tasks {
			tasks = append(tasks, current)
		}
		manager.mu.RUnlock()
		for _, current := range tasks {
			if current.snapshot().WorktreePath == manifest.WorktreePath {
				item.InUse = true
				break
			}
		}
		result.Worktrees = append(result.Worktrees, item)
	}
	return result, nil
}

func (manager *taskManager) worktreeContainer(taskID string) string {
	return filepath.Join(manager.cfg.WorktreesRoot, taskID)
}

func (manager *taskManager) readWorktreeManifest(taskID string) (taskWorktreeManifest, error) {
	container := manager.worktreeContainer(taskID)
	if err := validWorktreeContainer(manager.cfg.WorktreesRoot, container, taskID); err != nil {
		return taskWorktreeManifest{}, err
	}
	content, err := readPrivateRegularFile(filepath.Join(container, "manifest.json"), maximumWorktreeManifestB)
	if err != nil {
		return taskWorktreeManifest{}, fmt.Errorf("read isolated workspace manifest: %w", err)
	}
	var manifest taskWorktreeManifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return taskWorktreeManifest{}, fmt.Errorf("invalid isolated workspace manifest")
	}
	expectedPath, err := filepath.EvalSymlinks(filepath.Join(container, "workspace"))
	if err != nil {
		return taskWorktreeManifest{}, fmt.Errorf("resolve isolated workspace path")
	}
	if manifest.Schema != worktreeManifestSchema || manifest.TaskID != taskID || manifest.WorktreePath != expectedPath ||
		!filepath.IsAbs(manifest.ProjectCwd) || !filepath.IsAbs(manifest.RepositoryRoot) ||
		!filepath.IsAbs(manifest.GitCommonDir) || !filepath.IsAbs(manifest.GitDir) || manifest.BaseRevision == "" {
		return taskWorktreeManifest{}, fmt.Errorf("invalid isolated workspace manifest")
	}
	if (manifest.AppliedAt == nil) != (manifest.AppliedDigest == "") ||
		(manifest.AppliedDigest != "" && !validSHA256Digest(manifest.AppliedDigest)) {
		return taskWorktreeManifest{}, fmt.Errorf("invalid isolated workspace delivery state")
	}
	return manifest, nil
}

func validSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validWorktreeContainer(root, container, taskID string) error {
	if !taskIDPattern.MatchString(taskID) || container != filepath.Join(root, taskID) {
		return fmt.Errorf("refusing to use an invalid isolated workspace path")
	}
	info, err := os.Lstat(container)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("isolated workspace state is not a real directory")
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return fmt.Errorf("isolated workspace state has an unexpected owner")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("isolated workspace state is accessible by another user")
	}
	return nil
}
