package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemSnapshotHelpers(t *testing.T) {
	root := t.TempDir()
	text := filepath.Join(root, "value")
	if err := os.WriteFile(text, []byte("RDK S600\x00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := firstTextFile(filepath.Join(root, "missing"), text); got != "RDK S600" {
		t.Fatalf("firstTextFile() = %q", got)
	}
	for input, want := range map[string]string{
		"D-Robotics RDK S600": "s600",
		"RDK S100":            "s100",
		"Horizon RDK X5":      "x5",
		"generic arm64":       "unknown",
	} {
		if got := detectBoardID(input); got != want {
			t.Fatalf("detectBoardID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCollectSystemSnapshotIsBounded(t *testing.T) {
	cfg := testConfig(t)
	snapshot := collectSystemSnapshot(cfg)
	if snapshot.CapturedAt.IsZero() || snapshot.CPUCores < 1 || snapshot.Architecture == "" {
		t.Fatalf("incomplete system snapshot: %+v", snapshot)
	}
	if snapshot.Memory.AvailableBytes > snapshot.Memory.TotalBytes && snapshot.Memory.TotalBytes != 0 {
		t.Fatalf("invalid memory snapshot: %+v", snapshot.Memory)
	}
	if snapshot.Disk.AvailableBytes > snapshot.Disk.TotalBytes && snapshot.Disk.TotalBytes != 0 {
		t.Fatalf("invalid disk snapshot: %+v", snapshot.Disk)
	}
	if len(snapshot.LoadAverage) > 3 {
		t.Fatalf("load average is not bounded: %+v", snapshot.LoadAverage)
	}
	for _, zone := range snapshot.ThermalZones {
		if zone.Celsius < -100 || zone.Celsius > 200 {
			t.Fatalf("invalid thermal reading: %+v", zone)
		}
	}
}

func TestCommandAvailableRejectsMissingUtility(t *testing.T) {
	if commandAvailable("hobot-code-command-that-does-not-exist") {
		t.Fatal("missing command was reported as available")
	}
}

func TestParseIONHeapsKeepsCapacityAllocationAndOrphansSeparate(t *testing.T) {
	content := []byte(`
    cma_reserved  heap total size       1073741824
  total orphaned          4128768
          total          12451840
     ion_uncache  heap total size       2147483648
  total orphaned                0
          total         180158464
`)
	heaps := parseIONHeaps(content)
	if len(heaps) != 2 {
		t.Fatalf("unexpected heaps: %+v", heaps)
	}
	if heaps[0].Name != "cma_reserved" || heaps[0].CapacityBytes != 1073741824 || heaps[0].AllocatedBytes != 12451840 || heaps[0].OrphanedBytes != 4128768 {
		t.Fatalf("unexpected CMA heap: %+v", heaps[0])
	}
	if heaps[1].Name != "ion_uncache" || heaps[1].AllocatedBytes != 180158464 {
		t.Fatalf("unexpected uncached heap: %+v", heaps[1])
	}
}

func TestParseBPUAndDMABufMemorySummaries(t *testing.T) {
	bpu := []byte("        carveout:           200000 : 1\n          total            300000\n")
	if got := parseBPUIONClientTotal(bpu); got != 3*1024*1024 {
		t.Fatalf("BPU allocation = %d", got)
	}
	objects, size := parseDMABufTotal([]byte("Total 2 devices attached\nTotal 1 objects, 4128768 bytes\n"))
	if objects != 1 || size != 4128768 {
		t.Fatalf("dma-buf summary = %d objects, %d bytes", objects, size)
	}
}

func TestReadBoundedTelemetryFileRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry")
	if err := os.WriteFile(path, make([]byte, 65), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedFile(path, 64); err == nil {
		t.Fatal("oversized telemetry file was accepted")
	}
}
