package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newDiagnosticTestServer(t *testing.T, cfg config) *daemonServer {
	t.Helper()
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &daemonServer{cfg: cfg, manager: manager, started: time.Now().UTC(), build: currentBuildIdentity()}
}

func diagnosticCheckByName(t *testing.T, report diagnosticReport, name string) supportCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("diagnostic check %q is missing", name)
	return supportCheck{}
}

func TestDiagnosticsInspectIsReadOnlyAndReportsConfigurationState(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := newDiagnosticTestServer(t, cfg)
	before, err := os.ReadDir(cfg.SupportRoot)
	if err != nil {
		t.Fatal(err)
	}
	report, err := server.inspectDiagnostics(cfg.ConfigFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(cfg.SupportRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("read-only diagnostics created support artifacts: before=%d after=%d", len(before), len(after))
	}
	if report.SchemaVersion != diagnosticsSchemaVersion || report.CapturedAt.IsZero() {
		t.Fatalf("invalid diagnostic identity: %+v", report)
	}
	if check := diagnosticCheckByName(t, report, "configuration-current"); check.Status != "pass" {
		t.Fatalf("current configuration check = %+v", check)
	}
	stale, err := server.inspectDiagnostics("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if check := diagnosticCheckByName(t, stale, "configuration-current"); check.Status != "fail" {
		t.Fatalf("stale configuration check = %+v", check)
	}
	action, ok := diagnosticRepairByID(stale.Repairs, diagnosticRepairRestartDaemon)
	if !ok || action.Status != "available" || action.Executor != "client" {
		t.Fatalf("stale configuration repair = %+v found=%t", action, ok)
	}
}

func TestDiagnosticsDetectsConfiguredAndMissingModelsWithoutCredentials(t *testing.T) {
	cfg := testConfig(t)
	server := newDiagnosticTestServer(t, cfg)
	report, err := server.inspectDiagnostics("")
	if err != nil {
		t.Fatal(err)
	}
	if check := diagnosticCheckByName(t, report, "model-configuration"); check.Status != "warn" {
		t.Fatalf("missing model configuration check = %+v", check)
	}
	if err := os.WriteFile(filepath.Join(cfg.AgentDir, "models.json"), []byte(`{"providers":{"local":{"baseUrl":"http://127.0.0.1:8000"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = server.inspectDiagnostics("")
	if err != nil {
		t.Fatal(err)
	}
	if check := diagnosticCheckByName(t, report, "model-configuration"); check.Status != "pass" {
		t.Fatalf("custom model configuration check = %+v", check)
	}
}

func TestDiagnosticsRepairsOnlyKnownPrivatePermissions(t *testing.T) {
	cfg := testConfig(t)
	server := newDiagnosticTestServer(t, cfg)
	if err := os.Chmod(cfg.StateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := server.inspectDiagnostics("")
	if err != nil {
		t.Fatal(err)
	}
	action, ok := diagnosticRepairByID(report.Repairs, diagnosticRepairPrivatePermissions)
	if !ok || action.Status != "available" || action.Executor != "agentd" {
		t.Fatalf("private permission repair = %+v found=%t", action, ok)
	}
	if _, err := server.repairDiagnostics(diagnosticRepairParams{Action: action.ID}, ""); err == nil {
		t.Fatal("repair succeeded without explicit confirmation")
	}
	result, err := server.repairDiagnostics(diagnosticRepairParams{Action: action.ID, Confirm: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed < 1 {
		t.Fatalf("repair changed %d paths", result.Changed)
	}
	info, err := os.Stat(cfg.StateRoot)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("state root permissions = %v err=%v", info, err)
	}
}

func TestDiagnosticsNeverFollowsUnsafeRuntimeSymlink(t *testing.T) {
	cfg := testConfig(t)
	server := newDiagnosticTestServer(t, cfg)
	target := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cfg.LogPath); err != nil {
		t.Fatal(err)
	}
	report, err := server.inspectDiagnostics("")
	if err != nil {
		t.Fatal(err)
	}
	action, ok := diagnosticRepairByID(report.Repairs, diagnosticRepairPrivatePermissions)
	if !ok || action.Status != "blocked" {
		t.Fatalf("unsafe symlink repair = %+v found=%t", action, ok)
	}
	if _, err := server.repairDiagnostics(diagnosticRepairParams{Action: diagnosticRepairPrivatePermissions, Confirm: true}, ""); err == nil {
		t.Fatal("repair accepted an unsafe symlink-only plan")
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("symlink target permissions changed: info=%v err=%v", info, err)
	}
}

func TestDiagnosticsBlocksRestartWhileTasksAreQueued(t *testing.T) {
	cfg := testConfig(t)
	cfg.ConfigFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := newDiagnosticTestServer(t, cfg)
	server.manager.tasks["00112233445566778899aabb"] = &task{manager: server.manager, metadata: taskMetadata{
		ID: "00112233445566778899aabb", Status: statusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}, subscribers: map[uint64]chan taskEvent{}}
	report, err := server.inspectDiagnostics("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	action, ok := diagnosticRepairByID(report.Repairs, diagnosticRepairRestartDaemon)
	if !ok || action.Status != "blocked" {
		t.Fatalf("restart action while queued = %+v found=%t", action, ok)
	}
}

func TestDiagnosticRepairsRejectBroadSystemAndHomePaths(t *testing.T) {
	if safeDiagnosticRepairPath("/") || safeDiagnosticRepairPath("/etc") || safeDiagnosticRepairPath("/root") {
		t.Fatal("broad system path was accepted for automatic repair")
	}
	if home, err := os.UserHomeDir(); err == nil && safeDiagnosticRepairPath(home) {
		t.Fatal("the user home directory was accepted for automatic repair")
	}
	if !safeDiagnosticRepairPath(filepath.Join(t.TempDir(), "hobot-code")) {
		t.Fatal("a scoped private runtime path was rejected")
	}
}
