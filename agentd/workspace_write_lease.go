package main

import (
	"crypto/rand"
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
	workspaceWriteLeaseSchema       = 1
	maximumWorkspaceWriteLeases     = 128
	maximumWorkspaceLeaseOwnerBytes = 16 * 1024
	workspaceRegistryLockAttempts   = 100
	workspaceRegistryLockDelay      = 25 * time.Millisecond
	workspaceRegistryStaleAge       = 10 * time.Second
)

type workspaceLeaseOwner struct {
	SchemaVersion int       `json:"schemaVersion"`
	LeaseID       string    `json:"leaseId"`
	TaskID        string    `json:"taskId"`
	PID           int       `json:"pid"`
	Cwd           string    `json:"cwd"`
	AcquiredAt    time.Time `json:"acquiredAt"`
}

type workspaceRegistryLockOwner struct {
	SchemaVersion int    `json:"schemaVersion"`
	PID           int    `json:"pid"`
	LeaseID       string `json:"leaseId"`
}

type workspaceWriteLease struct {
	registryDir string
	claimDir    string
	leaseID     string
	pid         int
}

func acquireWorkspaceWriteLease(stateRoot, cwd, taskID string) (*workspaceWriteLease, error) {
	physicalCwd, err := workspaceLeaseRoot(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace write lease: %w", err)
	}
	registryDir := filepath.Join(stateRoot, "workspace-write-leases")
	if err := ensurePrivateDir(registryDir); err != nil {
		return nil, fmt.Errorf("prepare workspace write lease registry: %w", err)
	}
	pid := os.Getpid()
	unlock, err := acquireWorkspaceRegistryLock(registryDir, pid)
	if err != nil {
		return nil, err
	}
	locked := true
	defer func() {
		if locked {
			_ = unlock()
		}
	}()

	entries, err := os.ReadDir(registryDir)
	if err != nil {
		return nil, err
	}
	if len(entries) > maximumWorkspaceWriteLeases+8 {
		return nil, fmt.Errorf("workspace lease registry has too many entries; run Hobot Code diagnostics")
	}
	live := 0
	for _, entry := range entries {
		if !entry.IsDir() || !workspaceWriteLeaseDirectoryPattern.MatchString(entry.Name()) {
			continue
		}
		leaseDir := filepath.Join(registryDir, entry.Name())
		owner, ownerErr := readWorkspaceLeaseOwner(leaseDir)
		if ownerErr != nil {
			age := directoryAge(leaseDir)
			if age <= workspaceRegistryStaleAge {
				return nil, fmt.Errorf("this workspace is being changed by another Agent, but its owner metadata is unavailable")
			}
			if err := removeWorkspaceLeaseDirectory(registryDir, leaseDir); err != nil {
				return nil, err
			}
			continue
		}
		if !processAlive(owner.PID) {
			if err := removeWorkspaceLeaseDirectory(registryDir, leaseDir); err != nil {
				return nil, err
			}
			continue
		}
		live++
		if pathsOverlap(physicalCwd, owner.Cwd) {
			return nil, fmt.Errorf("workspace writes are busy: task %s (PID %d) is changing %s; wait for that Agent turn to finish or stop its task", owner.TaskID, owner.PID, owner.Cwd)
		}
	}
	if live >= maximumWorkspaceWriteLeases {
		return nil, fmt.Errorf("workspace write capacity is full; stop an inactive task and retry")
	}

	leaseID, err := newWorkspaceLeaseID()
	if err != nil {
		return nil, err
	}
	claimDir := filepath.Join(registryDir, "lease-"+leaseID)
	if err := os.Mkdir(claimDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(claimDir, 0o700); err != nil {
		_ = os.Remove(claimDir)
		return nil, err
	}
	owner := workspaceLeaseOwner{
		SchemaVersion: workspaceWriteLeaseSchema, LeaseID: leaseID, TaskID: truncateWorkspaceLeaseTaskID(taskID),
		PID: pid, Cwd: physicalCwd, AcquiredAt: time.Now().UTC(),
	}
	if err := writeExclusivePrivateJSON(filepath.Join(claimDir, "owner.json"), owner); err != nil {
		_ = os.Remove(claimDir)
		return nil, err
	}
	if err := unlock(); err != nil {
		_ = removeWorkspaceLeaseDirectory(registryDir, claimDir)
		return nil, err
	}
	locked = false
	return &workspaceWriteLease{registryDir: registryDir, claimDir: claimDir, leaseID: leaseID, pid: pid}, nil
}

func (lease *workspaceWriteLease) release() error {
	if lease == nil || lease.claimDir == "" {
		return nil
	}
	unlock, err := acquireWorkspaceRegistryLock(lease.registryDir, lease.pid)
	if err != nil {
		return err
	}
	defer unlock()
	owner, err := readWorkspaceLeaseOwner(lease.claimDir)
	if errors.Is(err, os.ErrNotExist) {
		lease.claimDir = ""
		return nil
	}
	if err != nil {
		return err
	}
	if owner.LeaseID != lease.leaseID {
		return fmt.Errorf("workspace write lease ownership changed unexpectedly")
	}
	if err := removeWorkspaceLeaseDirectory(lease.registryDir, lease.claimDir); err != nil {
		return err
	}
	lease.claimDir = ""
	return nil
}

func workspaceLeaseRoot(cwd string) (string, error) {
	physical, err := normalizeWorkingDirectory(cwd)
	if err != nil {
		return "", err
	}
	current := physical
	for {
		marker := filepath.Join(current, ".git")
		info, markerErr := os.Lstat(marker)
		if markerErr == nil && info.Mode()&os.ModeSymlink == 0 && (info.IsDir() || info.Mode().IsRegular()) {
			return current, nil
		}
		if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
			return "", markerErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return physical, nil
		}
		current = parent
	}
}

func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}

func acquireWorkspaceRegistryLock(registryDir string, pid int) (func() error, error) {
	lockDir := filepath.Join(registryDir, ".registry-lock")
	for attempt := 0; attempt < workspaceRegistryLockAttempts; attempt++ {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			if chmodErr := os.Chmod(lockDir, 0o700); chmodErr != nil {
				_ = os.Remove(lockDir)
				return nil, chmodErr
			}
			leaseID, idErr := newWorkspaceLeaseID()
			if idErr != nil {
				_ = os.Remove(lockDir)
				return nil, idErr
			}
			owner := workspaceRegistryLockOwner{SchemaVersion: workspaceWriteLeaseSchema, PID: pid, LeaseID: leaseID}
			if writeErr := writeExclusivePrivateJSON(filepath.Join(lockDir, "owner.json"), owner); writeErr != nil {
				_ = removeRegistryLockDirectory(registryDir, lockDir)
				return nil, writeErr
			}
			return func() error {
				current, readErr := readWorkspaceRegistryLockOwner(lockDir)
				if errors.Is(readErr, os.ErrNotExist) {
					return nil
				}
				if readErr != nil {
					return readErr
				}
				if current.LeaseID != leaseID {
					return fmt.Errorf("workspace lease registry lock ownership changed unexpectedly")
				}
				return removeRegistryLockDirectory(registryDir, lockDir)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		owner, ownerErr := readWorkspaceRegistryLockOwner(lockDir)
		if (ownerErr == nil && !processAlive(owner.PID)) || (ownerErr != nil && directoryAge(lockDir) > workspaceRegistryStaleAge) {
			if removeErr := removeRegistryLockDirectory(registryDir, lockDir); removeErr != nil {
				return nil, removeErr
			}
			continue
		}
		time.Sleep(workspaceRegistryLockDelay)
	}
	return nil, fmt.Errorf("timed out waiting for the workspace lease registry")
}

func readWorkspaceLeaseOwner(leaseDir string) (workspaceLeaseOwner, error) {
	if err := validatePrivateChildDirectory(filepath.Dir(leaseDir), leaseDir); err != nil {
		return workspaceLeaseOwner{}, err
	}
	content, err := readPrivateRegularFile(filepath.Join(leaseDir, "owner.json"), maximumWorkspaceLeaseOwnerBytes)
	if err != nil {
		return workspaceLeaseOwner{}, err
	}
	var owner workspaceLeaseOwner
	if json.Unmarshal(content, &owner) != nil || owner.SchemaVersion != workspaceWriteLeaseSchema ||
		owner.LeaseID == "" || len(owner.LeaseID) > 64 || owner.TaskID == "" || len(owner.TaskID) > 128 ||
		owner.PID <= 0 || !filepath.IsAbs(owner.Cwd) || len(owner.Cwd) > 4096 || owner.AcquiredAt.IsZero() {
		return workspaceLeaseOwner{}, fmt.Errorf("invalid workspace write lease owner")
	}
	if filepath.Base(leaseDir) != "lease-"+owner.LeaseID {
		return workspaceLeaseOwner{}, fmt.Errorf("workspace write lease identity does not match its directory")
	}
	return owner, nil
}

func readWorkspaceRegistryLockOwner(lockDir string) (workspaceRegistryLockOwner, error) {
	if err := validatePrivateChildDirectory(filepath.Dir(lockDir), lockDir); err != nil {
		return workspaceRegistryLockOwner{}, err
	}
	content, err := readPrivateRegularFile(filepath.Join(lockDir, "owner.json"), maximumWorkspaceLeaseOwnerBytes)
	if err != nil {
		return workspaceRegistryLockOwner{}, err
	}
	var owner workspaceRegistryLockOwner
	if json.Unmarshal(content, &owner) != nil || owner.SchemaVersion != workspaceWriteLeaseSchema || owner.PID <= 0 || owner.LeaseID == "" || len(owner.LeaseID) > 64 {
		return workspaceRegistryLockOwner{}, fmt.Errorf("invalid workspace lease registry lock owner")
	}
	return owner, nil
}

func validatePrivateChildDirectory(parent, child string) error {
	if filepath.Dir(child) != parent {
		return fmt.Errorf("workspace lease path escapes its registry")
	}
	info, err := os.Lstat(child)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("workspace lease state is not a private directory")
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return fmt.Errorf("workspace lease state has an unexpected owner")
	}
	return nil
}

func removeWorkspaceLeaseDirectory(registryDir, leaseDir string) error {
	if filepath.Dir(leaseDir) != registryDir || !workspaceWriteLeaseDirectoryPattern.MatchString(filepath.Base(leaseDir)) {
		return fmt.Errorf("refusing to remove an invalid workspace lease path")
	}
	if err := os.Remove(filepath.Join(leaseDir, "owner.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(leaseDir)
}

func removeRegistryLockDirectory(registryDir, lockDir string) error {
	if lockDir != filepath.Join(registryDir, ".registry-lock") {
		return fmt.Errorf("refusing to remove an invalid workspace registry lock path")
	}
	if err := os.Remove(filepath.Join(lockDir, "owner.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(lockDir)
}

func writeExclusivePrivateJSON(path string, value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func newWorkspaceLeaseID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	hexValue := hex.EncodeToString(value)
	return strings.Join([]string{hexValue[:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:]}, "-"), nil
}

func truncateWorkspaceLeaseTaskID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fmt.Sprintf("process-%d", os.Getpid())
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func directoryAge(path string) time.Duration {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	return time.Since(info.ModTime())
}
