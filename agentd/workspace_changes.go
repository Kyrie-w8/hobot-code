package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	maximumWorkspaceChangeFiles = 200
	maximumWorkspacePatchBytes  = 512 * 1024
	maximumGitStatusBytes       = 2 * 1024 * 1024
	maximumGitErrorBytes        = 8 * 1024
	workspaceChangesTimeout     = 5 * time.Second
)

var workspaceChangeSlots = make(chan struct{}, 2)

type workspaceChangesParams struct {
	TaskID string `json:"taskId"`
}

type workspaceChangeFile struct {
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath,omitempty"`
	Status       string `json:"status"`
	Kind         string `json:"kind"`
	Staged       bool   `json:"staged,omitempty"`
	Unstaged     bool   `json:"unstaged,omitempty"`
	Untracked    bool   `json:"untracked,omitempty"`
	Conflict     bool   `json:"conflict,omitempty"`
}

type workspaceChanges struct {
	CapturedAt     time.Time             `json:"capturedAt"`
	Available      bool                  `json:"available"`
	Repository     bool                  `json:"repository"`
	RepositoryRoot string                `json:"repositoryRoot,omitempty"`
	Scope          string                `json:"scope,omitempty"`
	Head           string                `json:"head,omitempty"`
	Files          []workspaceChangeFile `json:"files"`
	Patch          string                `json:"patch,omitempty"`
	FilesTruncated bool                  `json:"filesTruncated,omitempty"`
	PatchTruncated bool                  `json:"patchTruncated,omitempty"`
}

func (manager *taskManager) workspaceChanges(params workspaceChangesParams) (workspaceChanges, error) {
	current, err := manager.get(params.TaskID)
	if err != nil {
		return workspaceChanges{}, err
	}
	metadata := current.snapshot()
	path := metadata.Cwd
	if metadata.WorkspaceMode == workspaceModeWorktree && metadata.WorktreePath != "" {
		path = metadata.WorktreePath
	}
	return inspectWorkspaceChanges(path)
}

func inspectWorkspaceChanges(cwd string) (workspaceChanges, error) {
	result := workspaceChanges{CapturedAt: time.Now().UTC(), Files: make([]workspaceChangeFile, 0)}
	physicalCwd, err := normalizeWorkingDirectory(cwd)
	if err != nil {
		return result, fmt.Errorf("workspace is unavailable")
	}
	git := trustedGitBinary()
	if git == "" {
		return result, nil
	}
	result.Available = true
	ctx, cancel := context.WithTimeout(context.Background(), workspaceChangesTimeout)
	defer cancel()
	select {
	case workspaceChangeSlots <- struct{}{}:
		defer func() { <-workspaceChangeSlots }()
	case <-ctx.Done():
		return result, fmt.Errorf("workspace change inspection is busy")
	}

	rootOutput, _, rootErr := runGitBounded(ctx, git, physicalCwd, 4096, "rev-parse", "--show-toplevel")
	if rootErr != nil {
		if errors.Is(rootErr, errGitNotRepository) {
			return result, nil
		}
		return result, rootErr
	}
	rootValue := strings.TrimSpace(string(rootOutput))
	if !filepath.IsAbs(rootValue) {
		return result, fmt.Errorf("git returned an invalid repository root")
	}
	physicalRoot, err := filepath.EvalSymlinks(rootValue)
	if err != nil || !pathWithin(physicalRoot, physicalCwd) {
		return result, fmt.Errorf("git repository root is outside the task workspace ancestry")
	}
	scope, err := filepath.Rel(physicalRoot, physicalCwd)
	if err != nil || scope == ".." || strings.HasPrefix(scope, ".."+string(filepath.Separator)) {
		return result, fmt.Errorf("task workspace is outside the git repository")
	}
	if scope == "" {
		scope = "."
	}
	result.Repository = true
	result.RepositoryRoot = physicalRoot
	result.Scope = filepath.ToSlash(scope)

	statusOutput, statusTruncated, err := runGitBounded(ctx, git, physicalCwd, maximumGitStatusBytes,
		"status", "--porcelain=v2", "-z", "--untracked-files=normal", "--ignore-submodules=all", "--", ".")
	if err != nil {
		return result, fmt.Errorf("inspect git status: %w", err)
	}
	if statusTruncated {
		if lastRecord := bytes.LastIndexByte(statusOutput, 0); lastRecord >= 0 {
			statusOutput = statusOutput[:lastRecord+1]
		} else {
			statusOutput = nil
		}
	}
	files, parserTruncated, err := parseGitStatusV2(statusOutput, maximumWorkspaceChangeFiles)
	if err != nil {
		return result, err
	}
	result.Files = files
	result.FilesTruncated = statusTruncated || parserTruncated

	headOutput, _, headErr := runGitBounded(ctx, git, physicalCwd, 128, "rev-parse", "--short=12", "HEAD")
	hasHead := headErr == nil
	if hasHead {
		result.Head = strings.TrimSpace(string(headOutput))
	}
	if len(result.Files) == 0 && !result.FilesTruncated {
		return result, nil
	}

	var patch bytes.Buffer
	appendPatch := func(arguments ...string) error {
		remaining := maximumWorkspacePatchBytes - patch.Len()
		if remaining <= 0 {
			result.PatchTruncated = true
			return nil
		}
		content, truncated, commandErr := runGitBounded(ctx, git, physicalCwd, remaining, arguments...)
		if commandErr != nil {
			return commandErr
		}
		patch.Write(content)
		result.PatchTruncated = result.PatchTruncated || truncated
		return nil
	}
	if hasHead {
		err = appendPatch("diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--unified=3", "HEAD", "--", ".")
	} else {
		err = appendPatch("diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--unified=3", "--cached", "--", ".")
		if err == nil {
			err = appendPatch("diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--unified=3", "--", ".")
		}
	}
	if err != nil {
		return result, fmt.Errorf("inspect git diff: %w", err)
	}
	result.Patch = sanitizeWorkspacePatch(patch.String())
	return result, nil
}

func trustedGitBinary() string {
	for _, candidate := range []string{"/usr/bin/git", "/bin/git"} {
		info, err := os.Lstat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			continue
		}
		if owner, ok := fileOwner(info); ok && owner != 0 {
			continue
		}
		return candidate
	}
	return ""
}

var errGitCommandFailed = errors.New("git command failed")
var errGitNotRepository = errors.New("not a git repository")

func runGitBounded(ctx context.Context, git, cwd string, maximum int, arguments ...string) ([]byte, bool, error) {
	return runGitBoundedEnv(ctx, git, cwd, maximum, nil, arguments...)
}

func runGitBoundedEnv(ctx context.Context, git, cwd string, maximum int, extraEnvironment []string, arguments ...string) ([]byte, bool, error) {
	base := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.pager=cat",
		"-c", "color.ui=false",
		"-c", "diff.external=",
	}
	command := exec.CommandContext(ctx, git, append(base, arguments...)...)
	command.Dir = cwd
	command.Env = append(safeGitEnvironment(os.Environ()), extraEnvironment...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("prepare git inspection")
	}
	stderr := &boundedWorkspaceBuffer{maximum: maximumGitErrorBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, false, fmt.Errorf("start git inspection")
	}
	content, readErr := io.ReadAll(io.LimitReader(stdout, int64(maximum)+1))
	truncated := len(content) > maximum
	if truncated {
		content = content[:maximum]
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return nil, truncated, fmt.Errorf("git inspection timed out")
	}
	if readErr != nil {
		return nil, truncated, fmt.Errorf("read git inspection output")
	}
	if waitErr != nil && !truncated {
		message := strings.ToLower(stderr.content.String())
		if strings.Contains(message, "not a git repository") || strings.Contains(message, "not in a git directory") {
			return nil, false, errGitNotRepository
		}
		return nil, false, errGitCommandFailed
	}
	return content, truncated, nil
}

func safeGitEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+5)
	for _, entry := range environment {
		key := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		result = append(result, entry)
	}
	return append(result,
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "GIT_PAGER=cat",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C",
	)
}

func parseGitStatusV2(content []byte, maximum int) ([]workspaceChangeFile, bool, error) {
	records := bytes.Split(content, []byte{0})
	files := make([]workspaceChangeFile, 0)
	truncated := false
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) == 0 {
			continue
		}
		var file workspaceChangeFile
		switch record[0] {
		case '1':
			fields := bytes.SplitN(record, []byte{' '}, 9)
			if len(fields) != 9 {
				return nil, false, fmt.Errorf("git returned an invalid ordinary status record")
			}
			file = workspaceFileFromStatus(string(fields[1]), string(fields[8]), "")
		case '2':
			fields := bytes.SplitN(record, []byte{' '}, 10)
			if len(fields) != 10 || index+1 >= len(records) {
				return nil, false, fmt.Errorf("git returned an invalid rename status record")
			}
			index++
			file = workspaceFileFromStatus(string(fields[1]), string(fields[9]), string(records[index]))
		case 'u':
			fields := bytes.SplitN(record, []byte{' '}, 11)
			if len(fields) != 11 {
				return nil, false, fmt.Errorf("git returned an invalid conflict status record")
			}
			file = workspaceFileFromStatus(string(fields[1]), string(fields[10]), "")
			file.Conflict = true
			file.Kind = "conflicted"
		case '?':
			if len(record) < 3 || record[1] != ' ' {
				return nil, false, fmt.Errorf("git returned an invalid untracked status record")
			}
			file = workspaceFileFromStatus("??", string(record[2:]), "")
		default:
			continue
		}
		if !validWorkspaceChangePath(file.Path) || (file.OriginalPath != "" && !validWorkspaceChangePath(file.OriginalPath)) {
			return nil, false, fmt.Errorf("git returned an unsafe workspace path")
		}
		if len(files) >= maximum {
			truncated = true
			continue
		}
		files = append(files, file)
	}
	return files, truncated, nil
}

func validWorkspaceChangePath(value string) bool {
	if value == "" || filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(value)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func workspaceFileFromStatus(status, path, original string) workspaceChangeFile {
	status = strings.ToValidUTF8(status, "?")
	path = sanitizeWorkspacePath(path)
	original = sanitizeWorkspacePath(original)
	file := workspaceChangeFile{Path: path, OriginalPath: original, Status: status}
	if status == "??" {
		file.Kind = "untracked"
		file.Untracked = true
		return file
	}
	if len(status) >= 2 {
		file.Staged = status[0] != '.' && status[0] != ' '
		file.Unstaged = status[1] != '.' && status[1] != ' '
	}
	file.Conflict = isGitConflictStatus(status)
	if file.Conflict {
		file.Kind = "conflicted"
		return file
	}
	for _, value := range status {
		switch value {
		case 'R':
			file.Kind = "renamed"
			return file
		case 'C':
			file.Kind = "copied"
			return file
		case 'D':
			file.Kind = "deleted"
			return file
		case 'A':
			file.Kind = "added"
			return file
		case 'T':
			file.Kind = "type-changed"
			return file
		}
	}
	file.Kind = "modified"
	return file
}

func sanitizeWorkspacePath(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	return strings.Map(func(char rune) rune {
		if unicode.IsControl(char) || unicode.In(char, unicode.Cf, unicode.Zl, unicode.Zp) {
			return '\uFFFD'
		}
		return char
	}, value)
}

func sanitizeWorkspacePatch(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	return strings.Map(func(char rune) rune {
		if char == '\n' || char == '\t' {
			return char
		}
		if unicode.IsControl(char) || unicode.In(char, unicode.Cf, unicode.Zl, unicode.Zp) {
			return '\uFFFD'
		}
		return char
	}, value)
}

func isGitConflictStatus(status string) bool {
	switch status {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	default:
		return false
	}
}

type boundedWorkspaceBuffer struct {
	content bytes.Buffer
	maximum int
}

func (buffer *boundedWorkspaceBuffer) Write(content []byte) (int, error) {
	original := len(content)
	remaining := buffer.maximum - buffer.content.Len()
	if remaining > 0 {
		if len(content) > remaining {
			content = content[:remaining]
		}
		_, _ = buffer.content.Write(content)
	}
	return original, nil
}
