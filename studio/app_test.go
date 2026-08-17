package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bryant-w/hobot-code/sdk/go/hobot"
)

type handshakeFixture struct {
	Info     hobot.DaemonInfo      `json:"info"`
	Snapshot *hobot.SystemSnapshot `json:"snapshot"`
	Expected struct {
		Status          string   `json:"status"`
		ValidatedTarget bool     `json:"validatedTarget"`
		IssueCodes      []string `json:"issueCodes"`
	} `json:"expected"`
}

func TestRecordedHandshakeCompatibility(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "handshakes", "*.json"))
	if err != nil || len(paths) != 3 {
		t.Fatalf("handshake fixtures: paths=%v err=%v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture handshakeFixture
			decoder := json.NewDecoder(strings.NewReader(string(content)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&fixture); err != nil {
				t.Fatal(err)
			}
			var snapshotErr error
			if fixture.Snapshot == nil {
				snapshotErr = os.ErrNotExist
			}
			result, err := assessConnectionCompatibility(fixture.Info, fixture.Snapshot, snapshotErr)
			if err != nil || result.Status != fixture.Expected.Status || result.ValidatedTarget != fixture.Expected.ValidatedTarget {
				t.Fatalf("compatibility=%+v err=%v expected=%+v", result, err, fixture.Expected)
			}
			codes := make([]string, 0, len(result.Issues))
			for _, issue := range result.Issues {
				codes = append(codes, issue.Code)
			}
			for _, code := range fixture.Expected.IssueCodes {
				if !containsValue(codes, code) {
					t.Fatalf("required issue %q missing from %v", code, codes)
				}
			}
			sort.Strings(codes)
			if fixture.Expected.Status == "supported" && len(codes) != 0 {
				t.Fatalf("supported fixture has warnings: %v", codes)
			}
		})
	}
}

func TestBoardConnectionSerializesReconnectState(t *testing.T) {
	encoded, err := json.Marshal(BoardConnection{Connected: true, Reconnected: true, NotInstalled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"reconnected":true`) || !strings.Contains(string(encoded), `"notInstalled":true`) {
		t.Fatalf("state missing from Studio response: %s", encoded)
	}
}

func TestProbeBoardRejectsInvalidCandidateWithoutPersistingIt(t *testing.T) {
	app := NewApp()
	result := app.ProbeBoard(Board{Name: "Broken", Host: "-invalid", User: "root", Port: 22})
	if result.Connected || result.Error == "" {
		t.Fatalf("invalid candidate was not rejected: %+v", result)
	}
	if boards := app.ListBoards(); len(boards) != 0 {
		t.Fatalf("probe persisted a failed board: %+v", boards)
	}
}

func TestInstallBoardServiceRejectsInvalidCandidate(t *testing.T) {
	app := NewApp()
	result, err := app.InstallBoardService(Board{Name: "Broken", Host: "-invalid", User: "root", Port: 22})
	if err == nil || result.Success {
		t.Fatalf("invalid candidate was accepted for install: %+v", result)
	}
}

func TestWorkspaceChangesRejectsInvalidTaskIDBeforeConnecting(t *testing.T) {
	app := NewApp()
	if _, err := app.GetWorkspaceChanges("", "not-a-task"); err == nil || !strings.Contains(err.Error(), "task id is invalid") {
		t.Fatalf("invalid task id was accepted: %v", err)
	}
	if _, err := app.InspectWorkspaceDelivery("", "not-a-task"); err == nil || !strings.Contains(err.Error(), "task id is invalid") {
		t.Fatalf("invalid delivery task id was accepted: %v", err)
	}
	if _, err := app.ApplyWorkspace("", "not-a-task", strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "task id is invalid") {
		t.Fatalf("invalid apply task id was accepted: %v", err)
	}
	if _, err := app.ApplyWorkspace("", "00112233445566778899aabb", "bad"); err == nil || !strings.Contains(err.Error(), "digest is invalid") {
		t.Fatalf("invalid apply digest was accepted: %v", err)
	}
}

func TestModelQualificationRejectsUnknownBoardBeforeConnecting(t *testing.T) {
	app := NewApp()
	if _, err := app.ProbeModelRuntime("missing", "drobotics/kimi-k3"); err == nil || !strings.Contains(err.Error(), "board does not exist") {
		t.Fatalf("runtime probe error = %v", err)
	}
	if _, err := app.ProbeModelRDK("missing", "drobotics/kimi-k3", "read-only-rdk-diagnostic-v1"); err == nil || !strings.Contains(err.Error(), "board does not exist") {
		t.Fatalf("RDK probe error = %v", err)
	}
	if _, err := app.GetModelRDKMatrix("missing", "drobotics/kimi-k3"); err == nil || !strings.Contains(err.Error(), "board does not exist") {
		t.Fatalf("RDK matrix read error = %v", err)
	}
	if _, err := app.GetModelQualification("missing", "drobotics/kimi-k3"); err == nil || !strings.Contains(err.Error(), "board does not exist") {
		t.Fatalf("qualification read error = %v", err)
	}
}

func TestBoardUpdateRejectsUnknownOrDisconnectedBoard(t *testing.T) {
	app := NewApp()
	if _, err := app.CheckBoardUpdate("missing"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unexpected update check error: %v", err)
	}
	if _, err := app.InstallBoardUpdate("missing"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unexpected update install error: %v", err)
	}
}

func TestConnectionCompatibilityMatrix(t *testing.T) {
	allCapabilities := []string{
		"extensions.catalog.v1", "schedules.v1", "tasks.lifecycle", "tasks.page", "events.page", "models.capabilities.v1", "models.health.v1", "models.conformance.v1", "models.runtime-probe.v1", "models.rdk-probe.v1", "models.rdk-matrix.v1", "models.qualification.v1", "providers.manage.v1", "system.snapshot", "diagnostics.inspect.v1", "diagnostics.repair.v1",
		"support.bundle.v1", "deployments.v1", "tasks.fork", "tasks.collaboration.v1", "tasks.queue.v1", "tasks.failure.v1", "tasks.turn-evidence.v1", "events.items.v1", "events.retention.v1", "workspaces.browse", "workspaces.changes.v1", "workspaces.isolation.v1", "workspaces.write-leases.v1", "workspaces.delivery.v1", "tasks.sandbox.v1", "tasks.network.v1", "build.identity.v1", "pi.compatibility.v1",
	}
	dirty := false
	info := hobot.DaemonInfo{
		Version: "0.28.0", Protocol: hobot.ProtocolVersion,
		Capabilities: hobot.Capabilities{ProtocolMin: 1, ProtocolMax: 1, EventSchema: 4, Capabilities: allCapabilities, Sandbox: hobot.SandboxCapability{Available: true, Backend: "bubblewrap", Profiles: []string{"review", "workspace", "system", "off"}, NetworkModes: []string{"shared", "offline"}}},
		Build:        hobot.BuildIdentity{Status: "verified", Commit: strings.Repeat("a", 40), Dirty: &dirty, Target: "linux-arm64", PiVersion: "0.84.1", PiCompatibilitySHA256: strings.Repeat("d", 64)},
	}
	snapshot := &hobot.SystemSnapshot{BoardID: "s100", RDKOSVersion: "4.0.5"}
	compatible, err := assessConnectionCompatibility(info, snapshot, nil)
	if err != nil || compatible.Status != "supported" || !compatible.ValidatedTarget {
		t.Fatalf("validated S100 was not supported: result=%+v err=%v", compatible, err)
	}
	sandboxUnavailable := info
	sandboxUnavailable.Capabilities.Sandbox = hobot.SandboxCapability{Profiles: []string{"off"}, Reason: "bubblewrap is not installed"}
	sandboxLimited, err := assessConnectionCompatibility(sandboxUnavailable, snapshot, nil)
	if err != nil || sandboxLimited.Status != "limited" || sandboxLimited.Issues[0].Code != "sandbox-unavailable" {
		t.Fatalf("unavailable sandbox was not surfaced: result=%+v err=%v", sandboxLimited, err)
	}
	beta, err := assessConnectionCompatibility(info, &hobot.SystemSnapshot{BoardID: "s100", RDKOSVersion: "4.0.5-Beta"}, nil)
	if err != nil || beta.Status != "supported" || !beta.ValidatedTarget || len(beta.Issues) != 0 {
		t.Fatalf("validated S100 beta image was not supported: result=%+v err=%v", beta, err)
	}
	unknownPrerelease, err := assessConnectionCompatibility(info, &hobot.SystemSnapshot{BoardID: "s100", RDKOSVersion: "4.0.5-RC1"}, nil)
	if err != nil || unknownPrerelease.Status != "limited" || unknownPrerelease.ValidatedTarget || len(unknownPrerelease.Issues) != 1 || unknownPrerelease.Issues[0].Code != "rdk-os-unvalidated-version" {
		t.Fatalf("unknown S100 prerelease was accepted: result=%+v err=%v", unknownPrerelease, err)
	}
	dirty = true
	dirtyBuild := info
	dirtyBuild.Build.Dirty = &dirty
	dirtyResult, err := assessConnectionCompatibility(dirtyBuild, snapshot, nil)
	if err != nil || dirtyResult.Status != "limited" || dirtyResult.Issues[0].Code != "unreleased-board-build" {
		t.Fatalf("dirty board build was not surfaced: result=%+v err=%v", dirtyResult, err)
	}
	invalidBuild := info
	invalidBuild.Build.Status = "invalid"
	invalidResult, err := assessConnectionCompatibility(invalidBuild, snapshot, nil)
	if err != nil || invalidResult.Status != "limited" || invalidResult.Issues[0].Code != "invalid-build-identity" {
		t.Fatalf("invalid board build identity was not surfaced: result=%+v err=%v", invalidResult, err)
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

	configurationCurrent := false
	drifted := info
	drifted.ConfigurationCurrent = &configurationCurrent
	incompatible, err = assessConnectionCompatibility(drifted, snapshot, nil)
	if err == nil || incompatible.Status != "upgrade-required" || len(incompatible.Issues) != 1 || incompatible.Issues[0].Code != "configuration-restart-required" {
		t.Fatalf("configuration drift was accepted: result=%+v err=%v", incompatible, err)
	}
}

func TestVersionCompatibilityHelpers(t *testing.T) {
	app := NewApp()
	if currentStudioVersion() != "0.28.5" {
		t.Fatalf("Studio version is not sourced from wails.json: %q", currentStudioVersion())
	}
	if app.GetAppVersion() != currentStudioVersion() {
		t.Fatalf("exposed Studio version = %q, want %q", app.GetAppVersion(), currentStudioVersion())
	}
	if !differentReleaseLine("0.28.0", "0.27.7") || differentReleaseLine("0.28.0", "0.28.0") {
		t.Fatal("release line comparison is incorrect")
	}
	if major, ok := versionMajor("5.1.0"); !ok || major != 5 {
		t.Fatalf("RDK OS major parsing failed: major=%d ok=%v", major, ok)
	}
	if !containsFoldedValue([]string{"4.0.5", "4.0.5-Beta"}, " 4.0.5-beta ") {
		t.Fatal("validated RDK OS comparison should ignore case and surrounding whitespace")
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
	for _, status := range []string{"queued", "starting", "idle", "running", "waiting", "stopping"} {
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

func TestStudioModelsExposeBuiltInsAndExplicitManagedProviders(t *testing.T) {
	models := studioModels([]hobot.ModelOption{
		{Provider: "anthropic", ID: "claude-sonnet", Name: "Claude Sonnet"},
		{Provider: "acme", ID: "coder", Name: "Acme Coder", Managed: true},
		{Provider: "drobotics", ID: "claude-sonnet", Name: "Claude via gateway"},
		{Provider: "drobotics", ID: "kimi-k3", Name: "kimi-k3", Default: true, Capabilities: hobot.ModelCapabilities{Reasoning: true, ImageInput: true}, CapabilitySource: "runtime-model-table"},
		{Provider: "drobotics", ID: "qwen3.8-max", Name: "qwen3.8-max"},
		{Provider: "drobotics", ID: "glm-5.2", Name: "glm-5.2"},
		{Provider: "drobotics", ID: "deepseek/deepseek-v4-flash", Name: "deepseek/deepseek-v4-flash", Capabilities: hobot.ModelCapabilities{Reasoning: true}},
		{Provider: "drobotics", ID: "deepseek-v4-pro", Name: "deepseek-v4-pro", Capabilities: hobot.ModelCapabilities{Reasoning: true}},
	})
	if len(models) != 6 || models[0].ID != "kimi-k3" || models[1].ID != "qwen3.8-max" || models[2].ID != "glm-5.2" || models[3].ID != "deepseek/deepseek-v4-flash" || models[4].ID != "deepseek-v4-pro" || models[5].Provider != "acme" || models[5].ID != "coder" {
		t.Fatalf("unexpected Studio models: %+v", models)
	}
	if !models[0].Default || !models[0].Capabilities.ImageInput || models[0].CapabilitySource != "runtime-model-table" {
		t.Fatalf("Studio discarded model capabilities: %+v", models[0])
	}
}
