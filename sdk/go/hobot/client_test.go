package hobot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeSSH(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts.log")
	script := filepath.Join(dir, "ssh")
	content := fmt.Sprintf(`#!/bin/sh
printf 'start\n' >> %q
while IFS= read -r line; do
  id=$(printf '%%s\n' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"method":"capabilities"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"protocolMin":1,"protocolMax":1,"eventSchema":2,"capabilities":["bridge.stdio"],"maximumRequestBytes":2097152,"maximumResponseBytes":8388608}}\n' "$id"
      ;;
	*'"method":"extensions.list"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"schemaVersion":1,"apiVersion":"hobot.extensions/v1","productVersion":"0.26.0","hostVersion":"0.26.0","entries":[{"id":"hobot.rdk-core","name":"RDK development core","version":"0.26.0","kind":"extension","description":"RDK integration","origin":"built-in","scope":"system","runtime":"pi-extension","entrypoint":"rdk/index.ts","trust":"product","defaultEnabled":true,"required":true,"provides":["provider.drobotics"],"requires":["pi.extension-api"],"permissions":["workspace"],"targets":["x5","s100","s600"]}],"policy":{"inventoryOnly":true,"executionAuthority":"pi-runtime","permissionAuthority":"board","thirdPartyRuntime":"current-user","hotReload":false}}}\n' "$id"
	  ;;
    *'"method":"system.snapshot"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"capturedAt":"2026-08-12T00:00:00Z","board":"D-Robotics RDK S100","boardId":"s100","hostname":"rdk","rdkOsVersion":"4.0.2","kernel":"6.1.83","architecture":"arm64","cpuCores":8,"loadAverage":[0.5,0.4,0.3],"memory":{"totalBytes":8589934592,"availableBytes":4294967296},"disk":{"path":"/root","totalBytes":68719476736,"availableBytes":34359738368},"thermalZones":[{"name":"pvt_bpu","celsius":52.5}],"bpuDevices":["/dev/bpu","/dev/bpu_core0"],"bpuCores":[{"index":0,"name":"BPU 0","utilizationPercent":42,"currentFrequencyHz":1000000000,"maximumFrequencyHz":1500000000}],"bpuTelemetry":{"status":"available","source":"sysfs-ratio-devfreq"},"aiMemory":{"available":true,"bpuAllocationAvailable":true,"ionAvailable":true,"cmaAvailable":false,"dmaBufAvailable":true,"bpuAllocatedBytes":33554432,"ionAllocatedBytes":134217728,"dmaBufBytes":4194304,"dmaBufObjects":1},"rdkUtilities":{"hrt_model_exec":true},"uptimeSeconds":3600}}\n' "$id"
      ;;
	*'"method":"support.bundle"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"112233445566","createdAt":"2026-08-12T00:00:00Z","path":"/private/hobot-code-support-demo.json","sizeBytes":14,"sha256":"7eeccb134911ebae5c9ab93e29604540babeda8e0f5a634d92fc0a1d3dc45c52","content":"eyJzYWZlIjp0cnVlfQo=","excluded":["prompts"],"checks":{"pass":4,"warn":1,"fail":0}}}\n' "$id"
	  ;;
	*'"method":"diagnostics.inspect"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"schemaVersion":1,"capturedAt":"2026-08-12T00:00:00Z","status":"attention","summary":{"pass":1,"info":0,"warn":1,"fail":0},"checks":[{"name":"configuration-current","status":"pass","summary":"agentd is current"},{"name":"model-configuration","status":"warn","summary":"no provider"}],"findings":[{"code":"model-configuration","severity":"warning","scope":"models","title":"No provider","summary":"No provider is configured.","action":"Configure a provider.","count":1}],"repairs":[]}}\n' "$id"
	  ;;
	*'"method":"diagnostics.repair"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"schemaVersion":1,"action":"private-runtime-permissions","changed":1,"report":{"schemaVersion":1,"capturedAt":"2026-08-12T00:00:00Z","status":"healthy","summary":{"pass":1,"info":0,"warn":0,"fail":0},"checks":[{"name":"state-directory","status":"pass","summary":"private permissions"}],"findings":[],"repairs":[]}}}\n' "$id"
	  ;;
    *'"method":"deployment.inspect"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"capturedAt":"2026-08-12T00:00:00Z","cwd":"/root/models","board":"D-Robotics RDK S100","boardId":"s100","rdkOsVersion":"4.0.5","artifacts":[{"path":"/root/models/detector_nashe.hbm","relativePath":"detector_nashe.hbm","name":"detector_nashe.hbm","kind":"rdk-hbm","sizeBytes":1024,"modifiedAt":"2026-08-12T00:00:00Z","compatibility":"candidate","reason":"march matches"}],"truncated":false}}\n' "$id"
      ;;
    *'"method":"deployment.start"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"11223344556677889900aabb","name":"Deploy detector","cwd":"/root/models","status":"starting","createdAt":"2026-08-12T00:00:00Z","updatedAt":"2026-08-12T00:00:00Z","lastSequence":0,"deployment":{"schema":1,"cwd":"/root/models","board":"D-Robotics RDK S100","boardId":"s100","rdkOsVersion":"4.0.5","goal":"benchmark","artifact":{"path":"/root/models/detector_nashe.hbm","name":"detector_nashe.hbm","kind":"rdk-hbm","compatibility":"candidate"},"reportPath":"/root/models/.hobot/deployments/report.json","createdAt":"2026-08-12T00:00:00Z"}}}\n' "$id"
      ;;
    *'"method":"deployment.status"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"taskId":"11223344556677889900aabb","phase":"running","deployment":{"schema":1,"cwd":"/root/models","boardId":"s100","goal":"benchmark","artifact":{"path":"/root/models/detector_nashe.hbm","name":"detector_nashe.hbm","kind":"rdk-hbm","compatibility":"candidate"},"reportPath":"/root/models/.hobot/deployments/report.json","createdAt":"2026-08-12T00:00:00Z"}}}\n' "$id"
      ;;
    *'"method":"workspace.changes"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"capturedAt":"2026-08-12T00:00:00Z","available":true,"repository":true,"repositoryRoot":"/root/models","scope":".","head":"abc123","files":[{"path":"main.go","status":".M","kind":"modified","unstaged":true}],"patch":"diff --git a/main.go b/main.go\\n+updated\\n"}}\n' "$id"
      ;;
	*'"method":"workspace.isolation"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"capturedAt":"2026-08-12T00:00:00Z","available":true,"repository":true,"eligible":true,"recommendedMode":"worktree","repositoryRoot":"/root/models","scope":".","head":"abc123","clean":true,"reason":"A clean Git repository can be isolated from other tasks."}}\n' "$id"
	  ;;
	*'"method":"workspace.worktrees"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"worktrees":[{"taskId":"00112233445566778899aabb","projectCwd":"/root/models","path":"/root/.local/state/hobot-code/agentd/worktrees/00112233445566778899aabb/workspace","baseRevision":"abc123","createdAt":"2026-08-12T00:00:00Z","inUse":false}]}}\n' "$id"
	  ;;
	*'"method":"workspace.writes"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"leases":[{"taskId":"00112233445566778899aabb","pid":1234,"cwd":"/root/models","acquiredAt":"2026-08-12T00:00:00Z"}]}}\n' "$id"
	  ;;
		*'"method":"workspace.cleanup"'*)
		  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"taskId":"00112233445566778899aabb","cleaned":true}}\n' "$id"
		  ;;
		*'"method":"workspace.delivery"'*)
		  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"taskId":"00112233445566778899aabb","ready":true,"reason":"ready","patchBytes":128,"digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}\n' "$id"
		  ;;
		*'"method":"workspace.apply"'*)
		  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"taskId":"00112233445566778899aabb","applied":true,"staged":true,"patchBytes":128,"digest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","appliedAt":"2026-08-12T00:00:00Z"}}\n' "$id"
		  ;;
    *'"method":"task.page"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"tasks":[{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"idle","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:01Z","lastSequence":7}]}}\n' "$id"
      ;;
    *'"method":"task.get"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"idle","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:01Z","lastSequence":7}}\n' "$id"
      ;;
    *'"method":"task.command"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{}}\n' "$id"
      ;;
    *'"method":"task.restart"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"starting","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:02Z","lastSequence":7,"restartCount":1}}\n' "$id"
      ;;
    *'"method":"task.network"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"stopped","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:02Z","lastSequence":7,"sandboxMode":"workspace","networkMode":"offline","sandbox":{"requested":"workspace","effective":"workspace","backend":"bubblewrap","filesystemRestricted":true,"devicesRestricted":true,"capabilitiesDropped":true,"networkRestricted":true}}}\n' "$id"
      ;;
	*'"method":"task.subscribe"'*)
	  printf '{"protocol":1,"id":"%%s","ok":true,"result":{"replayed":0,"following":true,"retainedFrom":5,"retainedThrough":7,"latestSequence":8,"historyTruncated":true,"cursorExpired":true}}\n' "$id"
      printf '%%s\n' '{"protocol":1,"kind":"event","taskId":"00112233445566778899aabb","sequence":8,"time":"2026-08-11T00:00:02Z","event":{"type":"agent_settled"},"normalized":{"schema":2,"type":"task.idle"}}'
      exit 0
      ;;
    *)
      printf '{"protocol":1,"id":"%%s","ok":false,"error":{"code":"method_not_found","message":"unsupported"}}\n' "$id"
      ;;
  esac
done
`, starts)
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, starts
}

func TestClientReusesControlBridgeAndDecodesTasks(t *testing.T) {
	ssh, starts := fakeSSH(t)
	client, err := NewClient(Config{Host: "10.0.0.2", User: "root", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	capabilities, err := client.GetCapabilities(ctx)
	if err != nil || capabilities.EventSchema != 2 {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	sent, err := client.SubmitPrompt(ctx, "00112233445566778899aabb", "legacy idle prompt")
	if err != nil || sent.Disposition != "sent" {
		t.Fatalf("legacy idle prompt fallback=%+v err=%v", sent, err)
	}
	extensions, err := client.Extensions(ctx)
	if err != nil || len(extensions.Entries) != 1 || extensions.Entries[0].ID != "hobot.rdk-core" || !extensions.Policy.InventoryOnly || extensions.Policy.PermissionAuthority != "board" {
		t.Fatalf("extensions=%+v err=%v", extensions, err)
	}
	snapshot, err := client.SystemSnapshot(ctx)
	if err != nil || snapshot.BoardID != "s100" || len(snapshot.BPUDevices) != 2 || len(snapshot.BPUCores) != 1 || snapshot.BPUCores[0].UtilizationPercent != 42 || snapshot.BPUTelemetry.Status != "available" || snapshot.AIMemory.BPUAllocatedBytes != 33554432 || snapshot.Memory.AvailableBytes == 0 || len(snapshot.ThermalZones) != 1 || snapshot.ThermalZones[0].Celsius != 52.5 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	bundle, err := client.SupportBundle(ctx, true)
	if err != nil || string(bundle.Content) != "{\"safe\":true}\n" || bundle.Path != "/private/hobot-code-support-demo.json" || bundle.Checks.Pass != 4 {
		t.Fatalf("support bundle=%+v err=%v", bundle, err)
	}
	diagnostics, err := client.Diagnostics(ctx)
	if err != nil || diagnostics.SchemaVersion != 1 || diagnostics.Summary.Warn != 1 || len(diagnostics.Findings) != 1 {
		t.Fatalf("diagnostics=%+v err=%v", diagnostics, err)
	}
	repaired, err := client.RepairDiagnostics(ctx, "private-runtime-permissions", true)
	if err != nil || repaired.Changed != 1 || repaired.Report.Status != "healthy" {
		t.Fatalf("diagnostic repair=%+v err=%v", repaired, err)
	}
	inspection, err := client.InspectDeployment(ctx, "/root/models")
	if err != nil || inspection.BoardID != "s100" || len(inspection.Artifacts) != 1 || inspection.Artifacts[0].Compatibility != "candidate" {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	deployment, err := client.StartDeployment(ctx, StartDeploymentRequest{Cwd: inspection.Cwd, ArtifactPath: inspection.Artifacts[0].Path, Goal: "benchmark"})
	if err != nil || deployment.Deployment == nil || deployment.Deployment.BoardID != "s100" {
		t.Fatalf("deployment=%+v err=%v", deployment, err)
	}
	deploymentStatus, err := client.DeploymentStatus(ctx, deployment.ID)
	if err != nil || deploymentStatus.Phase != "running" || deploymentStatus.Deployment.Goal != "benchmark" {
		t.Fatalf("deployment status=%+v err=%v", deploymentStatus, err)
	}
	changes, err := client.WorkspaceChanges(ctx, "00112233445566778899aabb")
	if err != nil || !changes.Available || !changes.Repository || len(changes.Files) != 1 || changes.Files[0].Path != "main.go" || !strings.Contains(changes.Patch, "+updated") {
		t.Fatalf("workspace changes=%+v err=%v", changes, err)
	}
	isolation, err := client.InspectWorkspaceIsolation(ctx, "/root/models")
	if err != nil || !isolation.Eligible || isolation.RecommendedMode != "worktree" || isolation.RepositoryRoot != "/root/models" {
		t.Fatalf("workspace isolation=%+v err=%v", isolation, err)
	}
	worktrees, err := client.ManagedWorktrees(ctx)
	if err != nil || len(worktrees.Worktrees) != 1 || worktrees.Worktrees[0].TaskID != "00112233445566778899aabb" || worktrees.Worktrees[0].InUse {
		t.Fatalf("managed worktrees=%+v err=%v", worktrees, err)
	}
	writes, err := client.WorkspaceWrites(ctx)
	if err != nil || len(writes.Leases) != 1 || writes.Leases[0].TaskID != "00112233445566778899aabb" || writes.Leases[0].Cwd != "/root/models" {
		t.Fatalf("workspace writes=%+v err=%v", writes, err)
	}
	delivery, err := client.InspectWorkspaceDelivery(ctx, "00112233445566778899aabb")
	if err != nil || !delivery.Ready || delivery.PatchBytes != 128 || len(delivery.Digest) != 64 {
		t.Fatalf("workspace delivery=%+v err=%v", delivery, err)
	}
	applied, err := client.ApplyWorkspace(ctx, "00112233445566778899aabb", delivery.Digest)
	if err != nil || !applied.Applied || !applied.Staged || applied.Digest != delivery.Digest {
		t.Fatalf("workspace apply=%+v err=%v", applied, err)
	}
	cleanup, err := client.CleanupWorkspace(ctx, "00112233445566778899aabb")
	if err != nil || !cleanup.Cleaned {
		t.Fatalf("workspace cleanup=%+v err=%v", cleanup, err)
	}
	page, err := client.Tasks(ctx, false, "", 50)
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Name != "build" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	restarted, err := client.RestartTask(ctx, page.Tasks[0].ID, "start fresh")
	if err != nil || restarted.RestartCount != 1 || restarted.Status != "starting" {
		t.Fatalf("restarted=%+v err=%v", restarted, err)
	}
	networked, err := client.SetNetworkMode(ctx, page.Tasks[0].ID, "offline")
	if err != nil || networked.NetworkMode != "offline" || !networked.Sandbox.NetworkRestricted {
		t.Fatalf("network boundary=%+v err=%v", networked, err)
	}
	content, err := os.ReadFile(starts)
	if err != nil || string(content) != "start\n" {
		t.Fatalf("control bridge was not reused: %q err=%v", content, err)
	}
}

func TestSubscriptionUsesDedicatedBridge(t *testing.T) {
	ssh, _ := fakeSSH(t)
	client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// The race detector can delay process startup substantially on loaded CI hosts.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var received Event
	var ready SubscriptionState
	err = client.SubscribeWithState(ctx, "00112233445566778899aabb", 7, func(state SubscriptionState) {
		ready = state
	}, func(event Event) error {
		if !ready.Following {
			t.Fatal("event arrived before subscription acknowledgement")
		}
		received = event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ready.RetainedFrom != 5 || ready.RetainedThrough != 7 || ready.LatestSequence != 8 || !ready.HistoryTruncated || !ready.CursorExpired {
		t.Fatalf("subscription retention state was not decoded: %+v", ready)
	}
	if received.Sequence != 8 || received.Normalized == nil || received.Normalized.Type != "task.idle" {
		t.Fatalf("unexpected event: %+v", received)
	}
	legacyReady := false
	err = client.SubscribeWithReady(ctx, "00112233445566778899aabb", 7, func() {
		legacyReady = true
	}, func(event Event) error {
		if !legacyReady {
			t.Fatal("event arrived before subscription acknowledgement")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionTransportErrorClassification(t *testing.T) {
	transportErr := transientSubscriptionError(fmt.Errorf("connection reset by peer"))
	if !IsTransientSubscriptionError(transportErr) {
		t.Fatal("SSH transport failure must be retryable")
	}
	if !IsTransientSubscriptionError(fmt.Errorf("watch failed: %w", transportErr)) {
		t.Fatal("wrapped SSH transport failure must remain retryable")
	}
	if IsTransientSubscriptionError(fmt.Errorf("corrupt task event envelope")) {
		t.Fatal("protocol corruption must fail instead of retrying forever")
	}
}

func TestConnectionValidationFailsClosed(t *testing.T) {
	ssh, _ := fakeSSH(t)
	for _, config := range []Config{
		{Host: "-oProxyCommand=bad", SSHBinary: ssh},
		{Host: "host name", SSHBinary: ssh},
		{Host: "board", User: "root@other", SSHBinary: ssh},
		{Host: "board", Port: 70000, SSHBinary: ssh},
		{Host: "board", HostKeyPolicy: "off", SSHBinary: ssh},
	} {
		if _, err := NewClient(config); err == nil {
			t.Fatalf("unsafe config was accepted: %+v", config)
		}
	}
}
