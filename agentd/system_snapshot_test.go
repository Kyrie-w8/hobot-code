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
