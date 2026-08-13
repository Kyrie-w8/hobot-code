package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachCursorRoundTripIsPrivateAndMonotonicAtCallSite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "attach-cursors")
	taskID := "00112233445566778899aabb"
	if got, err := readAttachCursor(root, taskID); err != nil || got != 0 {
		t.Fatalf("new cursor = %d, %v", got, err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("cursor root permissions: info=%v err=%v", rootInfo, err)
	}
	if err := writeAttachCursor(root, taskID, 42); err != nil {
		t.Fatal(err)
	}
	if got, err := readAttachCursor(root, taskID); err != nil || got != 42 {
		t.Fatalf("stored cursor = %d, %v", got, err)
	}
	if err := writeAttachCursor(root, taskID, 7); err != nil {
		t.Fatal(err)
	}
	if got, err := readAttachCursor(root, taskID); err != nil || got != 42 {
		t.Fatalf("cursor moved backwards = %d, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(root, taskID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cursor permissions: info=%v err=%v", info, err)
	}
}

func TestAttachCursorFailsClosedAndReplayCanReplaceIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "attach-cursors")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	taskID := "11223344556677889900aabb"
	path := filepath.Join(root, taskID+".json")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAttachCursor(root, taskID); err == nil || !strings.Contains(err.Error(), "--replay-all") {
		t.Fatalf("corrupt cursor was accepted: %v", err)
	}
	if err := writeAttachCursor(root, taskID, 7); err != nil {
		t.Fatal(err)
	}
	if got, err := readAttachCursor(root, taskID); err != nil || got != 7 {
		t.Fatalf("replaced cursor = %d, %v", got, err)
	}
}

func TestAttachCursorRejectsUnsafeState(t *testing.T) {
	taskID := "223344556677889900aabbcc"
	worldReadable := filepath.Join(t.TempDir(), "attach-cursors")
	if err := os.Mkdir(worldReadable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAttachCursor(worldReadable, taskID, 1); err == nil {
		t.Fatal("world-readable cursor directory was accepted")
	}
	if _, err := readAttachCursor(worldReadable, "invalid"); err == nil {
		t.Fatal("invalid task ID was accepted")
	}
}

func TestAttachCursorRejectsSymlinkLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "attach-cursors")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside-lock")
	if err := os.WriteFile(target, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".write.lock")); err != nil {
		t.Fatal(err)
	}
	if err := writeAttachCursor(root, "3344556677889900aabbccdd", 1); err == nil {
		t.Fatal("symlink cursor lock was accepted")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "unchanged" {
		t.Fatalf("symlink target was changed: %q, %v", content, err)
	}
}
