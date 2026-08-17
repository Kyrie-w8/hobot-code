package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskControlSocketScopesScheduleManagementToCurrentMainTask(t *testing.T) {
	manager, schedules, cfg, _ := newScheduleTestManager(t)
	target := addScheduleTarget(t, cfg, manager)
	target.mu.Lock()
	target.command = &exec.Cmd{}
	target.mu.Unlock()
	path, err := target.startTaskControlSocket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.stopTaskControlSocket)
	client := taskControlClient{path: path}
	result, err := client.call("schedule.create", createScheduleParams{Name: "health", Prompt: "check", Every: "1m"})
	if err != nil || !strings.Contains(string(result), target.snapshot().ID) {
		t.Fatalf("scoped schedule create failed: result=%s err=%v", result, err)
	}
	if _, err := client.call("schedule.create", createScheduleParams{Name: "escape", TaskID: "ffeeddccbbaa998877665544", Prompt: "check", Every: "1m"}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("cross-task schedule create was accepted: %v", err)
	}
	if len(schedules.list(true)) != 1 {
		t.Fatalf("unexpected scoped schedules: %+v", schedules.list(true))
	}
}

func TestTaskControlSocketRejectsSideTaskEvenWithAValidPeer(t *testing.T) {
	manager, _, cfg, _ := newScheduleTestManager(t)
	target := addScheduleTarget(t, cfg, manager)
	target.mu.Lock()
	target.metadata.BranchKind = "side"
	target.command = &exec.Cmd{}
	target.mu.Unlock()
	path, err := target.startTaskControlSocket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(target.stopTaskControlSocket)
	if _, err := (taskControlClient{path: path}).call("schedule.list", listScheduleParams{}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("side task schedule control was accepted: %v", err)
	}
}

func TestTaskControlSocketAllowsEditedMainButRejectsEditedSide(t *testing.T) {
	manager, schedules, cfg, _ := newScheduleTestManager(t)
	root := addScheduleTarget(t, cfg, manager)
	edited := addScheduleBranchTarget(t, cfg, manager, "111122223333444455556666", "edit", root.snapshot().ID)
	edited.mu.Lock()
	edited.command = &exec.Cmd{}
	edited.mu.Unlock()
	path, err := edited.startTaskControlSocket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(edited.stopTaskControlSocket)
	if _, err := (taskControlClient{path: path}).call("schedule.create", createScheduleParams{Prompt: "check", Every: "1m"}); err != nil {
		t.Fatalf("edited main task schedule control failed: %v", err)
	}
	if len(schedules.list(true)) != 1 {
		t.Fatalf("edited main task did not create its schedule: %+v", schedules.list(true))
	}

	side := addScheduleBranchTarget(t, cfg, manager, "222233334444555566667777", "side", root.snapshot().ID)
	sideEdit := addScheduleBranchTarget(t, cfg, manager, "333344445555666677778888", "edit", side.snapshot().ID)
	sideEdit.mu.Lock()
	sideEdit.command = &exec.Cmd{}
	sideEdit.mu.Unlock()
	sidePath, err := sideEdit.startTaskControlSocket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sideEdit.stopTaskControlSocket)
	if _, err := (taskControlClient{path: sidePath}).call("schedule.list", listScheduleParams{}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("edited side task schedule control was accepted: %v", err)
	}
}

func TestRemoveEmptyTaskControlDirectoryFailsClosed(t *testing.T) {
	root := t.TempDir()
	id := "0123456789abcdef01234567"
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unexpected"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyTaskControlDirectory(root, id); err == nil {
		t.Fatal("non-empty task control directory was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "unexpected")); err != nil {
		t.Fatalf("unexpected task control content was lost: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "unexpected")); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyTaskControlDirectory(root, id); err != nil {
		t.Fatalf("empty task control directory was not removed: %v", err)
	}
}
