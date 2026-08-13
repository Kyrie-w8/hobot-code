package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectWorkspaceChangesScopesAndBoundsContent(t *testing.T) {
	git := requireGit(t)
	root := t.TempDir()
	runGitTest(t, git, root, "init", "-q")
	runGitTest(t, git, root, "config", "user.name", "Hobot Test")
	runGitTest(t, git, root, "config", "user.email", "hobot@example.invalid")
	project := filepath.Join(root, "project")
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(project, "main.go")
	if err := os.WriteFile(tracked, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "outside.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, root, "add", ".")
	runGitTest(t, git, root, "commit", "-qm", "initial")
	if err := os.WriteFile(tracked, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "untracked-secret-content"
	if err := os.WriteFile(filepath.Join(project, "new.txt"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "outside.txt"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	changes, err := inspectWorkspaceChanges(project)
	if err != nil {
		t.Fatal(err)
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !changes.Available || !changes.Repository || changes.RepositoryRoot != physicalRoot || changes.Scope != "project" || changes.Head == "" {
		t.Fatalf("unexpected repository metadata: %+v", changes)
	}
	if len(changes.Files) != 2 || changes.Files[0].Path != "project/main.go" || changes.Files[1].Path != "project/new.txt" || !changes.Files[1].Untracked {
		t.Fatalf("unexpected scoped files: %+v", changes.Files)
	}
	if !strings.Contains(changes.Patch, "+func main() {}") || strings.Contains(changes.Patch, secret) || strings.Contains(changes.Patch, "outside.txt") {
		t.Fatalf("unsafe or incomplete patch: %q", changes.Patch)
	}
}

func TestInspectWorkspaceChangesDisablesExternalDiff(t *testing.T) {
	git := requireGit(t)
	root := t.TempDir()
	runGitTest(t, git, root, "init", "-q")
	runGitTest(t, git, root, "config", "user.name", "Hobot Test")
	runGitTest(t, git, root, "config", "user.email", "hobot@example.invalid")
	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, root, "add", "tracked.txt")
	runGitTest(t, git, root, "commit", "-qm", "initial")
	marker := filepath.Join(root, "external-diff-ran")
	script := filepath.Join(root, "external-diff")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \"$HOBOT_DIFF_MARKER\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, root, "config", "diff.external", script)
	t.Setenv("HOBOT_DIFF_MARKER", marker)
	if err := os.WriteFile(tracked, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := inspectWorkspaceChanges(root)
	if err != nil || !strings.Contains(changes.Patch, "+after") {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("external diff was executed: %v", err)
	}
}

func TestGitStatusParserSanitizesPathsAndLimitsFiles(t *testing.T) {
	records := make([]string, 0, 202)
	records = append(records, "? unsafe\nname.txt")
	for index := 0; index < 201; index++ {
		records = append(records, fmt.Sprintf("1 .M N... 100644 100644 100644 a b file-%03d.txt", index))
	}
	content := []byte(strings.Join(records, "\x00") + "\x00")
	files, truncated, err := parseGitStatusV2(content, maximumWorkspaceChangeFiles)
	if err != nil || !truncated || len(files) != maximumWorkspaceChangeFiles {
		t.Fatalf("files=%d truncated=%v err=%v", len(files), truncated, err)
	}
	if strings.ContainsAny(files[0].Path, "\r\n") || files[0].Kind != "untracked" {
		t.Fatalf("unsafe path was retained: %+v", files[0])
	}

	rename, truncated, err := parseGitStatusV2([]byte("2 R. N... 100644 100644 100644 a b R100 new name.txt\x00old name.txt\x00"), 10)
	if err != nil || truncated || len(rename) != 1 || rename[0].Kind != "renamed" || rename[0].Path != "new name.txt" || rename[0].OriginalPath != "old name.txt" {
		t.Fatalf("rename parse failed: files=%+v truncated=%v err=%v", rename, truncated, err)
	}
	if _, _, err := parseGitStatusV2([]byte("? ../../etc/passwd\x00"), 10); err == nil {
		t.Fatal("path traversal status was accepted")
	}
}

func TestWorkspaceChangesAreTaskBoundAndGitEnvironmentIsolated(t *testing.T) {
	root := t.TempDir()
	manager := &taskManager{tasks: map[string]*task{}}
	current := &task{manager: manager, metadata: taskMetadata{ID: strings.Repeat("a", 24), Cwd: root}}
	manager.tasks[current.metadata.ID] = current
	changes, err := manager.workspaceChanges(workspaceChangesParams{TaskID: current.metadata.ID})
	if err != nil || !changes.Available || changes.Repository {
		t.Fatalf("non-repository task inspection failed: changes=%+v err=%v", changes, err)
	}
	if _, err := manager.workspaceChanges(workspaceChangesParams{TaskID: strings.Repeat("b", 24)}); err == nil {
		t.Fatal("unknown task was accepted")
	}
	environment := safeGitEnvironment([]string{"PATH=/bin", "HOME=/tmp/home", "GIT_DIR=/private", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=diff.external"})
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "GIT_DIR=") || strings.Contains(joined, "GIT_CONFIG_COUNT=") || strings.Contains(joined, "GIT_CONFIG_KEY_") ||
		!strings.Contains(joined, "GIT_OPTIONAL_LOCKS=0") || !strings.Contains(joined, "GIT_CONFIG_GLOBAL=/dev/null") || !strings.Contains(joined, "HOME=/tmp/home") {
		t.Fatalf("unsafe git environment: %q", joined)
	}
	if got := sanitizeWorkspacePatch("safe\n\tcode\u202Esevil\x1b[31m"); strings.Contains(got, "\u202E") || strings.Contains(got, "\x1b") || !strings.Contains(got, "safe\n\tcode") {
		t.Fatalf("unsafe patch text was retained: %q", got)
	}
}

func requireGit(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	return git
}

func runGitTest(t *testing.T, git, cwd string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(context.Background(), git, arguments...)
	command.Dir = cwd
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", arguments, err, output)
	}
}
