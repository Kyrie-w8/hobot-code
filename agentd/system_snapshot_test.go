package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestHardwareLeaseSnapshotIsPrivateBoundedAndLive(t *testing.T) {
	cfg := testConfig(t)
	root := filepath.Join(cfg.StateRoot, "hardware-leases")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLease := func(resource string, value map[string]any, mode os.FileMode) {
		t.Helper()
		dir := filepath.Join(root, resource)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		content, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, "owner.json"), append(content, '\n'), mode); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	writeLease("bpu", map[string]any{
		"schemaVersion": 1, "resource": "bpu", "taskId": "task-live", "pid": os.Getpid(), "cwd": cfg.StateRoot, "acquiredAt": now,
	}, 0o600)
	writeLease("camera-video0", map[string]any{
		"schemaVersion": 1, "resource": "camera-video0", "taskId": "task-dead", "pid": 99999999, "acquiredAt": now,
	}, 0o600)
	writeLease("media-pipeline", map[string]any{
		"schemaVersion": 1, "resource": "media-pipeline", "taskId": "too-open", "pid": os.Getpid(), "acquiredAt": now,
	}, 0o644)
	writeLease("not-a-resource", map[string]any{
		"schemaVersion": 1, "resource": "not-a-resource", "taskId": "invalid", "pid": os.Getpid(), "acquiredAt": now,
	}, 0o600)
	leases := readHardwareLeases(cfg)
	if len(leases) != 1 || leases[0].Resource != "bpu" || leases[0].TaskID != "task-live" || leases[0].PID != os.Getpid() {
		t.Fatalf("unexpected hardware leases: %+v", leases)
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

func TestParseIONHeapClientsReadsOnlyHeapSummary(t *testing.T) {
	content := []byte(`
    cma_reserved  heap total size       1073741824
       heap name           client              pid             size
-------------------------------------------------------------------------
    cma_reserved         modprobe              504         50069504
    cma_reserved           python3            54851          6422528
allocations (info is from last known client):
          client              pid             tgid             type             size
          python3            54851            54851              vio          4194304
          total          56492032
        carveout  heap total size        536870912
       heap name           client              pid             size
-------------------------------------------------------------------------
        carveout         modprobe              496         20512768
        carveout           python3            54851         88014848
allocations (info is from last known client):
          client              pid             tgid             type             size
          python3            54851            54851              hbm         66846720
          total         108527616
`)
	clients := parseIONHeapClients(content)
	if len(clients) != 4 {
		t.Fatalf("unexpected clients: %+v", clients)
	}
	if clients[1].Heap != "cma_reserved" || clients[1].Name != "python3" || clients[1].PID != 54851 || clients[1].Bytes != 6422528 {
		t.Fatalf("unexpected CMA client: %+v", clients[1])
	}
	if clients[3].Heap != "carveout" || clients[3].Bytes != 88014848 {
		t.Fatalf("unexpected carveout client: %+v", clients[3])
	}
}

func TestAcceleratorFromIONAttributesOnlyLiveProcesses(t *testing.T) {
	const mib = uint64(1024 * 1024)
	procRoot := t.TempDir()
	statusRoot := filepath.Join(procRoot, "54851")
	if err := os.MkdirAll(statusRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusRoot, "status"), []byte("Name:\tpython3\nVmRSS:\t245760 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	memory := aiMemorySnapshot{
		Heaps: []aiMemoryHeapSnapshot{
			{Name: "cma_reserved", CapacityBytes: 1024 * mib, AllocatedBytes: 56 * mib},
			{Name: "ion_cma", CapacityBytes: 512 * mib},
			{Name: "carveout", CapacityBytes: 512 * mib, AllocatedBytes: 108 * mib},
			{Name: "system", CapacityBytes: 1024 * mib, AllocatedBytes: 64 * mib},
		},
		clients: []ionHeapClientSnapshot{
			{Heap: "cma_reserved", Name: "python3", PID: 54851, Bytes: 6 * mib},
			{Heap: "cma_reserved", Name: "modprobe", PID: 504, Bytes: 50 * mib},
			{Heap: "carveout", Name: "python3", PID: 54851, Bytes: 88 * mib},
			{Heap: "carveout", Name: "modprobe", PID: 496, Bytes: 20 * mib},
		},
	}
	got := acceleratorFromIONAt(memory, procRoot)
	if !got.Available || got.Source != "ion-debugfs" || len(got.HbmemPools) != 3 || len(got.Processes) != 1 {
		t.Fatalf("unexpected exact accelerator snapshot: %+v", got)
	}
	if got.HbmemPools[0].ProcessBytes != 6*mib || got.HbmemPools[0].SystemBytes != 50*mib {
		t.Fatalf("unexpected CMA attribution: %+v", got.HbmemPools[0])
	}
	if got.HbmemPools[2].ProcessBytes != 88*mib || got.HbmemPools[2].SystemBytes != 20*mib {
		t.Fatalf("unexpected carveout attribution: %+v", got.HbmemPools[2])
	}
	if got.Processes[0].PID != 54851 || got.Processes[0].Name != "python3" || got.Processes[0].HbmemBytes != 94*mib || got.Processes[0].RSSBytes != 240*mib {
		t.Fatalf("unexpected process attribution: %+v", got.Processes[0])
	}
}

func TestAcceleratorFromIONRejectsReusedPID(t *testing.T) {
	procRoot := t.TempDir()
	statusRoot := filepath.Join(procRoot, "123")
	if err := os.MkdirAll(statusRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusRoot, "status"), []byte("Name:\tunrelated\nVmRSS:\t1024 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := acceleratorFromIONAt(aiMemorySnapshot{
		Heaps:   []aiMemoryHeapSnapshot{{Name: "carveout", CapacityBytes: 512 << 20, AllocatedBytes: 64 << 20}},
		clients: []ionHeapClientSnapshot{{Heap: "carveout", Name: "python3", PID: 123, Bytes: 64 << 20}},
	}, procRoot)
	if len(got.Processes) != 0 || got.HbmemPools[0].ProcessBytes != 0 || got.HbmemPools[0].SystemBytes != 64<<20 {
		t.Fatalf("reused PID was attributed: %+v", got)
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

func TestAcceleratorFromIONKeepsAllocationWhenDriverCapacityIsAnAddressWindow(t *testing.T) {
	got := acceleratorFromIONAt(aiMemorySnapshot{Heaps: []aiMemoryHeapSnapshot{
		{Name: "carveout", AllocatedBytes: 2 * 1024 * 1024 * 1024},
		{Name: "ion_cma", CapacityBytes: 1024 * 1024 * 1024},
	}}, t.TempDir())
	if !got.Available || len(got.HbmemPools) != 2 {
		t.Fatalf("allocation-only pool was dropped: %+v", got)
	}
	if got.HbmemPools[0].Name != "carveout" || got.HbmemPools[0].TotalBytes != 0 || got.HbmemPools[0].UsedBytes != 2*1024*1024*1024 {
		t.Fatalf("unexpected allocation-only pool: %+v", got.HbmemPools[0])
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
	if got.Source != "hrt_ucp_monitor-estimate" {
		t.Fatalf("fallback source = %q", got.Source)
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
