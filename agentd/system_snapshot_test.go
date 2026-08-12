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

func TestSanitizeIONHeapCapacitiesRejectsImpossibleDriverValues(t *testing.T) {
	heaps := []aiMemoryHeapSnapshot{
		{Name: "ion_uncache", CapacityBytes: 2 * 1024 * 1024 * 1024, AllocatedBytes: 64 * 1024 * 1024},
		{Name: "carveout", CapacityBytes: 48 * 1024 * 1024 * 1024},
	}
	got := sanitizeIONHeapCapacities(heaps, 12*1024*1024*1024)
	if got[0].CapacityBytes == 0 || got[1].CapacityBytes != 0 {
		t.Fatalf("unexpected sanitized heaps: %+v", got)
	}
}

func TestReadBPUCoresReportsStatusAndFindsSystemDevfreq(t *testing.T) {
	root := t.TempDir()
	platformRoot := filepath.Join(root, "platform")
	systemRoot := filepath.Join(root, "system", "bpu")
	devfreqRoot := filepath.Join(root, "devfreq")
	if err := os.MkdirAll(systemRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemRoot, "ratio"), []byte("37\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	frequencyRoot := filepath.Join(devfreqRoot, "28108000.bpu")
	if err := os.MkdirAll(frequencyRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"cur_freq": "1000000000\n", "min_freq": "500000000\n", "max_freq": "1500000000\n"} {
		if err := os.WriteFile(filepath.Join(frequencyRoot, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cores, status := readBPUCoresAt([]string{"/dev/bpu"}, platformRoot, systemRoot, devfreqRoot)
	if status.Status != "available" || len(cores) != 1 || cores[0].UtilizationPercent != 37 || cores[0].CurrentFrequencyHz != 1_000_000_000 {
		t.Fatalf("unexpected BPU telemetry: cores=%+v status=%+v", cores, status)
	}
}

func TestReadBPUCoresDistinguishesMissingDeviceAndMetrics(t *testing.T) {
	root := t.TempDir()
	_, status := readBPUCoresAt(nil, filepath.Join(root, "platform"), filepath.Join(root, "system"), filepath.Join(root, "devfreq"))
	if status.Status != "device-not-detected" {
		t.Fatalf("missing device status = %+v", status)
	}
	_, status = readBPUCoresAt([]string{"/dev/bpu"}, filepath.Join(root, "platform"), filepath.Join(root, "system"), filepath.Join(root, "devfreq"))
	if status.Status != "metrics-not-exposed" {
		t.Fatalf("missing metrics status = %+v", status)
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

func TestParseAcceleratorMonitor(t *testing.T) {
	content := []byte(`
| DDR Bandwidth                                  |
|          Read              232                 |
|          Write             310                 |
| ION Info                                       |
| cma_reserved      1.0G    64.0K  1023.9M       |
| ion_cma         512.0M     0.0    512.0M       |
| carveout        512.0M     1.0M   511.0M       |
| Process Mem Info                               |
|     18342  infer_hbm          245.5M    96.0M   |
`)
	got := parseAcceleratorMonitor(content)
	if !got.Available || got.DDRReadMiBPS != 232 || got.DDRWriteMiBPS != 310 || len(got.HbmemPools) != 3 || len(got.Processes) != 1 {
		t.Fatalf("unexpected accelerator snapshot: %+v", got)
	}
	if got.HbmemPools[0].TotalBytes != 1<<30 || got.HbmemPools[0].UsedBytes != 64<<10 || got.Processes[0].PID != 18342 || got.Processes[0].HbmemBytes != 96<<20 {
		t.Fatalf("unexpected parsed values: %+v", got)
	}
}

func TestParseMonitorBytes(t *testing.T) {
	for input, want := range map[string]uint64{"0.0": 0, "64.0K": 64 << 10, "245.5M": 257425408, "1.0G": 1 << 30} {
		got, ok := parseMonitorBytes(input)
		if !ok || got != want {
			t.Fatalf("parseMonitorBytes(%q) = %d, %t; want %d", input, got, ok, want)
		}
	}
}

func TestBoundedMonitorOutput(t *testing.T) {
	output := &boundedMonitorOutput{maximum: 4}
	if written, err := output.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("Write() = %d, %v; want 6, nil", written, err)
	}
	if got := output.buffer.String(); got != "abcd" || !output.exceeded {
		t.Fatalf("bounded output = %q, exceeded=%t; want abcd, true", got, output.exceeded)
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
