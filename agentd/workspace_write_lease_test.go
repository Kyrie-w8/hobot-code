package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceWriteLeaseBlocksOverlappingGitWorkspaces(t *testing.T) {
	stateRoot := t.TempDir()
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(repository, "src")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := acquireWorkspaceWriteLease(stateRoot, subdir, "task-one")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.release() })
	physicalRepository, err := normalizeWorkingDirectory(repository)
	if err != nil {
		t.Fatal(err)
	}
	if firstOwner, err := readWorkspaceLeaseOwner(first.claimDir); err != nil || firstOwner.Cwd != physicalRepository || firstOwner.TaskID != "task-one" {
		t.Fatalf("unexpected first owner: %+v err=%v", firstOwner, err)
	}
	if _, err := acquireWorkspaceWriteLease(stateRoot, repository, "task-two"); err == nil || !strings.Contains(err.Error(), "task-one") {
		t.Fatalf("overlapping lease was accepted: %v", err)
	}
	other := t.TempDir()
	second, err := acquireWorkspaceWriteLease(stateRoot, other, "task-two")
	if err != nil {
		t.Fatalf("independent lease was rejected: %v", err)
	}
	if err := second.release(); err != nil {
		t.Fatal(err)
	}
	if err := first.release(); err != nil {
		t.Fatal(err)
	}
	if leases := readWorkspaceWriteLeases(config{StateRoot: stateRoot}); len(leases) != 0 {
		t.Fatalf("released leases remain visible: %+v", leases)
	}
}

func TestWorkspaceWriteLeaseReclaimsCrashedCompatibleOwner(t *testing.T) {
	stateRoot := t.TempDir()
	registry := filepath.Join(stateRoot, "workspace-write-leases")
	if err := os.MkdirAll(registry, 0o700); err != nil {
		t.Fatal(err)
	}
	leaseDir := filepath.Join(registry, "lease-00112233-4455-4677-8899-aabbccddeeff")
	if err := os.Mkdir(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := map[string]any{
		"schemaVersion": 1, "leaseId": "00112233-4455-4677-8899-aabbccddeeff", "taskId": "javascript-worker",
		"pid": 99999999, "cwd": t.TempDir(), "acquiredAt": time.Now().UTC(),
	}
	content, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leaseDir, "owner.json"), append(content, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := acquireWorkspaceWriteLease(stateRoot, owner["cwd"].(string), "go-delivery")
	if err != nil {
		t.Fatalf("crashed compatible owner was not reclaimed: %v", err)
	}
	if _, err := os.Stat(leaseDir); !os.IsNotExist(err) {
		t.Fatalf("stale lease directory remains: %v", err)
	}
	if err := current.release(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceWriteLeaseFailsClosedOnFreshCorruptOwner(t *testing.T) {
	stateRoot := t.TempDir()
	registry := filepath.Join(stateRoot, "workspace-write-leases")
	if err := os.MkdirAll(registry, 0o700); err != nil {
		t.Fatal(err)
	}
	leaseDir := filepath.Join(registry, "lease-10223344-5566-4788-99aa-bbccddeeff00")
	if err := os.Mkdir(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leaseDir, "owner.json"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireWorkspaceWriteLease(stateRoot, t.TempDir(), "delivery"); err == nil || !strings.Contains(err.Error(), "metadata is unavailable") {
		t.Fatalf("fresh corrupt owner did not fail closed: %v", err)
	}
}
