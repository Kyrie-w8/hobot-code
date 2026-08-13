package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	attachCursorSchema       = 1
	maximumAttachCursorBytes = 4096
)

type attachCursor struct {
	Schema    int       `json:"schema"`
	TaskID    string    `json:"taskId"`
	Sequence  uint64    `json:"sequence"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func readAttachCursor(root, taskID string) (uint64, error) {
	path, err := attachCursorPath(root, taskID)
	if err != nil {
		return 0, err
	}
	if err := ensureAttachCursorRoot(root); err != nil {
		return 0, err
	}
	content, err := readPrivateRegularFile(path, maximumAttachCursorBytes)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read attach cursor: %w", err)
	}
	var cursor attachCursor
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Schema != attachCursorSchema || cursor.TaskID != taskID || cursor.UpdatedAt.IsZero() {
		return 0, fmt.Errorf("attach cursor is invalid; use --replay-all to recover without skipping output")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("attach cursor contains invalid trailing data; use --replay-all")
	}
	return cursor.Sequence, nil
}

func writeAttachCursor(root, taskID string, sequence uint64) error {
	if sequence == 0 {
		return nil
	}
	path, err := attachCursorPath(root, taskID)
	if err != nil {
		return err
	}
	if err := ensureAttachCursorRoot(root); err != nil {
		return err
	}
	unlock, err := lockAttachCursorRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	if existing, readErr := readAttachCursorFile(path, taskID); readErr == nil && existing >= sequence {
		return nil
	}
	content, err := json.MarshalIndent(attachCursor{Schema: attachCursorSchema, TaskID: taskID, Sequence: sequence, UpdatedAt: time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(content, '\n'))
}

func lockAttachCursorRoot(root string) (func() error, error) {
	path := filepath.Join(root, ".write.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open attach cursor lock: %w", err)
	}
	closeOnError := func(lockErr error) (func() error, error) {
		_ = file.Close()
		return nil, lockErr
	}
	if err := file.Chmod(0o600); err != nil {
		return closeOnError(err)
	}
	info, err := file.Stat()
	if err != nil {
		return closeOnError(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return closeOnError(fmt.Errorf("attach cursor lock must be a private regular file"))
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return closeOnError(fmt.Errorf("attach cursor lock has an unexpected owner"))
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return closeOnError(fmt.Errorf("lock attach cursor: %w", err))
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}

func readAttachCursorFile(path, taskID string) (uint64, error) {
	content, err := readPrivateRegularFile(path, maximumAttachCursorBytes)
	if err != nil {
		return 0, err
	}
	var cursor attachCursor
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Schema != attachCursorSchema || cursor.TaskID != taskID || cursor.UpdatedAt.IsZero() {
		return 0, fmt.Errorf("attach cursor is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("attach cursor contains invalid trailing data")
	}
	return cursor.Sequence, nil
}

func ensureAttachCursorRoot(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("attach cursor directory must be absolute")
	}
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		if err := ensurePrivateDir(root); err != nil {
			return fmt.Errorf("create attach cursor directory: %w", err)
		}
	} else if err != nil {
		return err
	}
	return validateAttachCursorRoot(root)
}

func attachCursorPath(root, taskID string) (string, error) {
	if !taskIDPattern.MatchString(taskID) {
		return "", fmt.Errorf("task id is invalid")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("attach cursor directory must be absolute")
	}
	return filepath.Join(root, taskID+".json"), nil
}

func validateAttachCursorRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("attach cursor directory must be a private real directory")
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return fmt.Errorf("attach cursor directory has an unexpected owner")
	}
	return nil
}
