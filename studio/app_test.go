package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bryant-w/hobot-code/sdk/go/hobot"
)

func TestBoardConnectionSerializesReconnectState(t *testing.T) {
	encoded, err := json.Marshal(BoardConnection{Connected: true, Reconnected: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"reconnected":true`) {
		t.Fatalf("reconnect state missing from Studio response: %s", encoded)
	}
}

func TestConnectionCompatibilityMatrix(t *testing.T) {
	allCapabilities := []string{
		"tasks.lifecycle", "tasks.page", "events.page", "models.capabilities.v1", "system.snapshot",
		"support.bundle.v1", "deployments.v1", "tasks.fork", "workspaces.browse",
	}
	info := hobot.DaemonInfo{
		Version: "0.24.0", Protocol: hobot.ProtocolVersion,
		Capabilities: hobot.Capabilities{ProtocolMin: 1, ProtocolMax: 1, EventSchema: 3, Capabilities: allCapabilities},
	}
	snapshot := &hobot.SystemSnapshot{BoardID: "s100", RDKOSVersion: "4.0.5"}
	compatible, err := assessConnectionCompatibility(info, snapshot, nil)
	if err != nil || compatible.Status != "supported" || !compatible.ValidatedTarget {
		t.Fatalf("validated S100 was not supported: result=%+v err=%v", compatible, err)
	}

	limitedInfo := info
	limitedInfo.Capabilities.Capabilities = []string{"tasks.lifecycle", "tasks.page", "events.page", "system.snapshot"}
	limited, err := assessConnectionCompatibility(limitedInfo, &hobot.SystemSnapshot{BoardID: "s600", RDKOSVersion: "5.2.0"}, nil)
	if err != nil || limited.Status != "limited" || len(limited.Issues) == 0 || limited.ValidatedTarget {
		t.Fatalf("missing optional capabilities or unvalidated OS did not degrade: result=%+v err=%v", limited, err)
	}

	protocolInfo := info
	protocolInfo.Capabilities.ProtocolMin = 2
	incompatible, err := assessConnectionCompatibility(protocolInfo, snapshot, nil)
	if err == nil || incompatible.Status != "upgrade-required" {
		t.Fatalf("protocol mismatch was accepted: result=%+v err=%v", incompatible, err)
	}

	missingRequired := info
	missingRequired.Capabilities.Capabilities = []string{"tasks.lifecycle", "events.page"}
	incompatible, err = assessConnectionCompatibility(missingRequired, snapshot, nil)
	if err == nil || incompatible.Status != "upgrade-required" {
		t.Fatalf("missing required capability was accepted: result=%+v err=%v", incompatible, err)
	}
}

func TestVersionCompatibilityHelpers(t *testing.T) {
	if currentStudioVersion() != "0.24.0" {
		t.Fatalf("Studio version is not sourced from wails.json: %q", currentStudioVersion())
	}
	if !differentReleaseLine("0.24.0", "0.23.9") || differentReleaseLine("0.24.0", "0.24.1") {
		t.Fatal("release line comparison is incorrect")
	}
	if major, ok := versionMajor("5.1.0"); !ok || major != 5 {
		t.Fatalf("RDK OS major parsing failed: major=%d ok=%v", major, ok)
	}
}

func TestBoardStoreRoundTrip(t *testing.T) {
	store := &boardStore{path: filepath.Join(t.TempDir(), "boards.json")}
	want := []Board{{
		ID: "00112233445566778899aabb", Name: "RDK S100", Host: "10.112.10.98", User: "root", Port: 22,
	}}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("board store mode = %o, want 600", got)
	}
	got, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("loaded boards = %+v, want %+v", got, want)
	}
}

func TestWritePrivateLocalFileRejectsSymlinkAndUsesPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "support.json")
	if err := writePrivateLocalFile(target, []byte("safe diagnostics\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("support file permissions: info=%v err=%v", info, err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateLocalFile(link, []byte("overwrite")); err == nil {
		t.Fatal("support bundle writer accepted a symbolic link")
	}
}

func TestBoardStoreRejectsUnsafeInput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    os.FileMode
		want    string
	}{
		{name: "permissions", content: `[]`, mode: 0o644, want: "permissions"},
		{name: "invalid ID", content: `[{"id":"bad","name":"RDK","host":"rdk","user":"root","port":22}]`, mode: 0o600, want: "invalid board"},
		{name: "duplicate ID", content: `[{"id":"00112233445566778899aabb","name":"A"},{"id":"00112233445566778899aabb","name":"B"}]`, mode: 0o600, want: "duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "boards.json")
			if err := os.WriteFile(path, []byte(test.content), test.mode); err != nil {
				t.Fatal(err)
			}
			_, err := (&boardStore{path: path}).load()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("load error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestBoardStoreRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "boards.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (&boardStore{path: link}).load(); err == nil {
		t.Fatal("symlink board store was accepted")
	}
}

func TestSortedBoards(t *testing.T) {
	boards := sortedBoards(map[string]Board{
		"b": {ID: "b", Name: "RDK X5"},
		"a": {ID: "a", Name: "RDK S100"},
	})
	if len(boards) != 2 || boards[0].Name != "RDK S100" || boards[1].Name != "RDK X5" {
		t.Fatalf("unexpected sort order: %+v", boards)
	}
}

func TestSafeExternalURL(t *testing.T) {
	for _, input := range []string{"https://developer.d-robotics.cc/docs", "http://10.112.10.98:8000/health"} {
		if got, err := safeExternalURL(input); err != nil || got != input {
			t.Fatalf("safeExternalURL(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://user:secret@example.com", "/relative"} {
		if _, err := safeExternalURL(input); err == nil {
			t.Fatalf("unsafe URL was accepted: %q", input)
		}
	}
}

func TestStudioTaskIsLive(t *testing.T) {
	for _, status := range []string{"starting", "idle", "running", "waiting", "stopping"} {
		if !studioTaskIsLive(status) {
			t.Fatalf("status %q should be live", status)
		}
	}
	for _, status := range []string{"stopped", "failed", "interrupted"} {
		if studioTaskIsLive(status) {
			t.Fatalf("status %q should be terminal", status)
		}
	}
}

func TestStudioModelsOnlyExposeDRobotics(t *testing.T) {
	models := studioModels([]hobot.ModelOption{
		{Provider: "anthropic", ID: "claude-sonnet", Name: "Claude Sonnet"},
		{Provider: "drobotics", ID: "claude-sonnet", Name: "Claude via gateway"},
		{Provider: "drobotics", ID: "kimi-k3", Name: "kimi-k3", Default: true, Capabilities: hobot.ModelCapabilities{Reasoning: true, ImageInput: true}, CapabilitySource: "runtime-model-table"},
		{Provider: "drobotics", ID: "qwen3.8-max", Name: "qwen3.8-max"},
		{Provider: "drobotics", ID: "glm-5.2", Name: "glm-5.2"},
		{Provider: "drobotics", ID: "deepseek-v4-flash", Name: "deepseek-v4-flash", Capabilities: hobot.ModelCapabilities{Reasoning: true}},
		{Provider: "drobotics", ID: "deepseek-v4-pro", Name: "deepseek-v4-pro", Capabilities: hobot.ModelCapabilities{Reasoning: true}},
	})
	if len(models) != 5 || models[0].ID != "kimi-k3" || models[1].ID != "qwen3.8-max" || models[2].ID != "glm-5.2" || models[3].ID != "deepseek-v4-flash" || models[4].ID != "deepseek-v4-pro" {
		t.Fatalf("unexpected Studio models: %+v", models)
	}
	if !models[0].Default || !models[0].Capabilities.ImageInput || models[0].CapabilitySource != "runtime-model-table" {
		t.Fatalf("Studio discarded model capabilities: %+v", models[0])
	}
}
