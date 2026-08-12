package hobot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
    *'"method":"system.snapshot"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"capturedAt":"2026-08-12T00:00:00Z","board":"D-Robotics RDK S100","boardId":"s100","hostname":"rdk","rdkOsVersion":"4.0.2","kernel":"6.1.83","architecture":"arm64","cpuCores":8,"loadAverage":[0.5,0.4,0.3],"memory":{"totalBytes":8589934592,"availableBytes":4294967296},"disk":{"path":"/root","totalBytes":68719476736,"availableBytes":34359738368},"thermalZones":[{"name":"pvt_bpu","celsius":52.5}],"bpuDevices":["/dev/bpu","/dev/bpu_core0"],"bpuCores":[{"index":0,"name":"BPU 0","utilizationPercent":42,"currentFrequencyHz":1000000000,"maximumFrequencyHz":1500000000}],"bpuTelemetry":{"status":"available","source":"sysfs-ratio-devfreq"},"aiMemory":{"available":true,"bpuAllocationAvailable":true,"ionAvailable":true,"cmaAvailable":false,"dmaBufAvailable":true,"bpuAllocatedBytes":33554432,"ionAllocatedBytes":134217728,"dmaBufBytes":4194304,"dmaBufObjects":1},"rdkUtilities":{"hrt_model_exec":true},"uptimeSeconds":3600}}\n' "$id"
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
    *'"method":"task.page"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"tasks":[{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"idle","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:01Z","lastSequence":7}]}}\n' "$id"
      ;;
    *'"method":"task.restart"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"id":"00112233445566778899aabb","name":"build","cwd":"/root","status":"starting","createdAt":"2026-08-11T00:00:00Z","updatedAt":"2026-08-11T00:00:02Z","lastSequence":7,"restartCount":1}}\n' "$id"
      ;;
    *'"method":"task.subscribe"'*)
      printf '{"protocol":1,"id":"%%s","ok":true,"result":{"replayed":0,"following":true}}\n' "$id"
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	capabilities, err := client.GetCapabilities(ctx)
	if err != nil || capabilities.EventSchema != 2 {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	snapshot, err := client.SystemSnapshot(ctx)
	if err != nil || snapshot.BoardID != "s100" || len(snapshot.BPUDevices) != 2 || len(snapshot.BPUCores) != 1 || snapshot.BPUCores[0].UtilizationPercent != 42 || snapshot.BPUTelemetry.Status != "available" || snapshot.AIMemory.BPUAllocatedBytes != 33554432 || snapshot.Memory.AvailableBytes == 0 || len(snapshot.ThermalZones) != 1 || snapshot.ThermalZones[0].Celsius != 52.5 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
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
	page, err := client.Tasks(ctx, false, "", 50)
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Name != "build" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	restarted, err := client.RestartTask(ctx, page.Tasks[0].ID, "start fresh")
	if err != nil || restarted.RestartCount != 1 || restarted.Status != "starting" {
		t.Fatalf("restarted=%+v err=%v", restarted, err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var received Event
	err = client.Subscribe(ctx, "00112233445566778899aabb", 7, func(event Event) error {
		received = event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.Sequence != 8 || received.Normalized == nil || received.Normalized.Type != "task.idle" {
		t.Fatalf("unexpected event: %+v", received)
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
