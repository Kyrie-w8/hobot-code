package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type thermalZoneSnapshot struct {
	Name    string  `json:"name"`
	Celsius float64 `json:"celsius"`
}

type memorySnapshot struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	CMATotalBytes  uint64 `json:"-"`
	CMAFreeBytes   uint64 `json:"-"`
}

type diskSnapshot struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type bpuCoreSnapshot struct {
	Index              int     `json:"index"`
	Name               string  `json:"name"`
	UtilizationPercent float64 `json:"utilizationPercent"`
	CurrentFrequencyHz uint64  `json:"currentFrequencyHz,omitempty"`
	MinimumFrequencyHz uint64  `json:"minimumFrequencyHz,omitempty"`
	MaximumFrequencyHz uint64  `json:"maximumFrequencyHz,omitempty"`
}

type bpuTelemetrySnapshot struct {
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
}

type aiMemoryHeapSnapshot struct {
	Name           string `json:"name"`
	CapacityBytes  uint64 `json:"capacityBytes,omitempty"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
	OrphanedBytes  uint64 `json:"orphanedBytes,omitempty"`
}

type aiMemorySnapshot struct {
	Available              bool                   `json:"available"`
	BPUAllocationAvailable bool                   `json:"bpuAllocationAvailable"`
	IONAvailable           bool                   `json:"ionAvailable"`
	CMAAvailable           bool                   `json:"cmaAvailable"`
	DMABufAvailable        bool                   `json:"dmaBufAvailable"`
	BPUAllocatedBytes      uint64                 `json:"bpuAllocatedBytes,omitempty"`
	IONAllocatedBytes      uint64                 `json:"ionAllocatedBytes,omitempty"`
	IONOrphanedBytes       uint64                 `json:"ionOrphanedBytes,omitempty"`
	CMATotalBytes          uint64                 `json:"cmaTotalBytes,omitempty"`
	CMAFreeBytes           uint64                 `json:"cmaFreeBytes,omitempty"`
	DMABufBytes            uint64                 `json:"dmaBufBytes,omitempty"`
	DMABufObjects          uint64                 `json:"dmaBufObjects,omitempty"`
	Heaps                  []aiMemoryHeapSnapshot `json:"heaps,omitempty"`
}

type acceleratorMemoryPoolSnapshot struct {
	Name       string `json:"name"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

type acceleratorProcessSnapshot struct {
	PID        int    `json:"pid"`
	Name       string `json:"name"`
	RSSBytes   uint64 `json:"rssBytes"`
	HbmemBytes uint64 `json:"hbmemBytes"`
}

type acceleratorSnapshot struct {
	Available     bool                            `json:"available"`
	Source        string                          `json:"source,omitempty"`
	CapturedAt    time.Time                       `json:"capturedAt,omitempty"`
	DDRReadMiBPS  float64                         `json:"ddrReadMiBps,omitempty"`
	DDRWriteMiBPS float64                         `json:"ddrWriteMiBps,omitempty"`
	HbmemPools    []acceleratorMemoryPoolSnapshot `json:"hbmemPools,omitempty"`
	Processes     []acceleratorProcessSnapshot    `json:"processes,omitempty"`
}

type systemSnapshot struct {
	CapturedAt    time.Time             `json:"capturedAt"`
	Board         string                `json:"board"`
	BoardID       string                `json:"boardId"`
	Hostname      string                `json:"hostname"`
	RDKOSVersion  string                `json:"rdkOsVersion"`
	Kernel        string                `json:"kernel"`
	Architecture  string                `json:"architecture"`
	CPUCores      int                   `json:"cpuCores"`
	LoadAverage   []float64             `json:"loadAverage"`
	Memory        memorySnapshot        `json:"memory"`
	Disk          diskSnapshot          `json:"disk"`
	ThermalZones  []thermalZoneSnapshot `json:"thermalZones"`
	BPUDevices    []string              `json:"bpuDevices"`
	BPUCores      []bpuCoreSnapshot     `json:"bpuCores"`
	BPUTelemetry  bpuTelemetrySnapshot  `json:"bpuTelemetry"`
	AIMemory      aiMemorySnapshot      `json:"aiMemory"`
	Accelerator   acceleratorSnapshot   `json:"accelerator"`
	RDKUtilities  map[string]bool       `json:"rdkUtilities"`
	UptimeSeconds uint64                `json:"uptimeSeconds"`
}

func collectSystemSnapshot(cfg config) systemSnapshot {
	board := firstTextFile("/sys/firmware/devicetree/base/model", "/proc/device-tree/model")
	if board == "" {
		board = "Unknown Linux board"
	}
	host, _ := os.Hostname()
	memory := readMemorySnapshot()
	bpuDevices := listBPUDevices()
	bpuCores, bpuTelemetry := readBPUCores(bpuDevices)
	return systemSnapshot{
		CapturedAt:    time.Now().UTC(),
		Board:         board,
		BoardID:       detectBoardID(board),
		Hostname:      host,
		RDKOSVersion:  detectRDKOSVersion(),
		Kernel:        kernelRelease(),
		Architecture:  runtime.GOARCH,
		CPUCores:      runtime.NumCPU(),
		LoadAverage:   readLoadAverage(),
		Memory:        memory,
		Disk:          readDiskSnapshot(cfg.StateRoot),
		ThermalZones:  readThermalZones(),
		BPUDevices:    bpuDevices,
		BPUCores:      bpuCores,
		BPUTelemetry:  bpuTelemetry,
		AIMemory:      readAIMemorySnapshot(memory),
		Accelerator:   readAcceleratorSnapshot(),
		RDKUtilities:  detectRDKUtilities(),
		UptimeSeconds: readUptimeSeconds(),
	}
}

func firstTextFile(paths ...string) string {
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err == nil {
			if value := strings.Trim(string(content), "\x00 \t\r\n"); value != "" {
				return value
			}
		}
	}
	return ""
}

func detectBoardID(board string) string {
	lower := strings.ToLower(board)
	for _, id := range []string{"s600", "s100", "x5"} {
		if strings.Contains(lower, id) {
			return id
		}
	}
	return "unknown"
}

func detectRDKOSVersion() string {
	if version := firstTextFile("/etc/version"); version != "" {
		return version
	}
	content := firstTextFile("/etc/os-release")
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "VERSION_ID=") {
			return strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}
	return "unknown"
}

func kernelRelease() string {
	content, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err == nil {
		return strings.TrimSpace(string(content))
	}
	return runtime.GOOS
}

func readLoadAverage() []float64 {
	fields := strings.Fields(firstTextFile("/proc/loadavg"))
	values := make([]float64, 0, 3)
	for _, field := range fields {
		if len(values) == 3 {
			break
		}
		value, err := strconv.ParseFloat(field, 64)
		if err != nil {
			break
		}
		values = append(values, value)
	}
	return values
}

func readMemorySnapshot() memorySnapshot {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return memorySnapshot{}
	}
	defer file.Close()
	result := memorySnapshot{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			result.TotalBytes = value * 1024
		case "MemAvailable":
			result.AvailableBytes = value * 1024
		case "CmaTotal":
			result.CMATotalBytes = value * 1024
		case "CmaFree":
			result.CMAFreeBytes = value * 1024
		}
	}
	return result
}

func readDiskSnapshot(path string) diskSnapshot {
	result := diskSnapshot{Path: path}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return result
	}
	blockSize := uint64(stat.Bsize)
	result.TotalBytes = stat.Blocks * blockSize
	result.AvailableBytes = stat.Bavail * blockSize
	return result
}

func readThermalZones() []thermalZoneSnapshot {
	entries, err := filepath.Glob("/sys/class/thermal/thermal_zone*")
	if err != nil {
		return []thermalZoneSnapshot{}
	}
	zones := make([]thermalZoneSnapshot, 0, len(entries))
	for _, root := range entries {
		raw := firstTextFile(filepath.Join(root, "temp"))
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		if value >= 1000 {
			value /= 1000
		}
		if value < -100 || value > 200 {
			continue
		}
		name := firstTextFile(filepath.Join(root, "type"))
		if name == "" {
			name = filepath.Base(root)
		}
		zones = append(zones, thermalZoneSnapshot{Name: name, Celsius: value})
	}
	return zones
}

func listBPUDevices() []string {
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return []string{}
	}
	devices := []string{}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if name == "bpu" || strings.HasPrefix(name, "bpu_core") || strings.HasPrefix(name, "dnn") {
			devices = append(devices, filepath.Join("/dev", entry.Name()))
		}
	}
	return devices
}

const maximumTelemetryFileBytes = 2 * 1024 * 1024
const maximumMonitorOutputBytes = 256 * 1024

var acceleratorMonitorCache struct {
	sync.Mutex
	captured time.Time
	value    acceleratorSnapshot
}

var (
	ionHeapPattern     = regexp.MustCompile(`^\s*([^\s]+)\s+heap total size\s+(\d+)\s*$`)
	ionTotalPattern    = regexp.MustCompile(`^\s*total\s+(\d+)\s*$`)
	ionOrphanedPattern = regexp.MustCompile(`^\s*total orphaned\s+(\d+)\s*$`)
	dmaBufTotalPattern = regexp.MustCompile(`^Total\s+(\d+)\s+objects?,\s+(\d+)\s+bytes$`)
	monitorIOPattern   = regexp.MustCompile(`^\|\s*(Read|Write)\s+([0-9.]+)\s*\|?$`)
	monitorPoolPattern = regexp.MustCompile(`^\|\s*([^|\s]+)\s+([0-9.]+[KMG]?)\s+([0-9.]+[KMG]?)\s+([0-9.]+[KMG]?)\s*\|?$`)
	monitorProcPattern = regexp.MustCompile(`^\|\s*(\d+)\s+(\S+)\s+([0-9.]+[KMG]?)\s+([0-9.]+[KMG]?)\s*\|?$`)
)

func readAcceleratorSnapshot() acceleratorSnapshot {
	acceleratorMonitorCache.Lock()
	defer acceleratorMonitorCache.Unlock()
	if !acceleratorMonitorCache.captured.IsZero() && time.Since(acceleratorMonitorCache.captured) < 5*time.Second {
		return acceleratorMonitorCache.value
	}
	value, err := sampleAcceleratorMonitor()
	acceleratorMonitorCache.captured = time.Now()
	if err == nil {
		acceleratorMonitorCache.value = value
	} else {
		acceleratorMonitorCache.value = acceleratorSnapshot{}
	}
	return acceleratorMonitorCache.value
}

func sampleAcceleratorMonitor() (acceleratorSnapshot, error) {
	const path = "/usr/hobot/bin/hrt_ucp_monitor"
	if _, err := os.Stat(path); err != nil {
		return acceleratorSnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, path, "-b", "-n", "1", "-d", "100", "-e", "bpu")
	output := &boundedMonitorOutput{maximum: maximumMonitorOutputBytes}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return acceleratorSnapshot{}, ctx.Err()
		}
		return acceleratorSnapshot{}, err
	}
	if output.exceeded {
		return acceleratorSnapshot{}, fmt.Errorf("accelerator monitor output is too large")
	}
	value := parseAcceleratorMonitor(output.buffer.Bytes())
	if !value.Available {
		return acceleratorSnapshot{}, fmt.Errorf("accelerator monitor output is incomplete")
	}
	return value, nil
}

type boundedMonitorOutput struct {
	buffer   bytes.Buffer
	maximum  int
	exceeded bool
}

func (output *boundedMonitorOutput) Write(content []byte) (int, error) {
	original := len(content)
	remaining := output.maximum - output.buffer.Len()
	if remaining <= 0 {
		output.exceeded = true
		return original, nil
	}
	if len(content) > remaining {
		content = content[:remaining]
		output.exceeded = true
	}
	_, _ = output.buffer.Write(content)
	return original, nil
}

func parseAcceleratorMonitor(content []byte) acceleratorSnapshot {
	result := acceleratorSnapshot{Source: "hrt_ucp_monitor", CapturedAt: time.Now().UTC()}
	section := ""
	for _, raw := range bytes.Split(content, []byte{'\n'}) {
		line := strings.TrimSpace(string(raw))
		switch {
		case strings.Contains(line, "DDR Bandwidth"):
			section = "ddr"
		case strings.Contains(line, "ION Info"):
			section = "ion"
		case strings.Contains(line, "Process Mem Info"):
			section = "process"
		case section == "ddr":
			if match := monitorIOPattern.FindStringSubmatch(line); match != nil {
				value, _ := strconv.ParseFloat(match[2], 64)
				if match[1] == "Read" {
					result.DDRReadMiBPS = value
				} else {
					result.DDRWriteMiBPS = value
				}
			}
		case section == "ion":
			if match := monitorPoolPattern.FindStringSubmatch(line); match != nil && match[1] != "total" {
				total, totalOK := parseMonitorBytes(match[2])
				used, usedOK := parseMonitorBytes(match[3])
				free, freeOK := parseMonitorBytes(match[4])
				if totalOK && usedOK && freeOK && total > 0 && len(result.HbmemPools) < 16 {
					result.HbmemPools = append(result.HbmemPools, acceleratorMemoryPoolSnapshot{Name: match[1], TotalBytes: total, UsedBytes: used, FreeBytes: free})
				}
			}
		case section == "process":
			if match := monitorProcPattern.FindStringSubmatch(line); match != nil && len(result.Processes) < 32 {
				pid, _ := strconv.Atoi(match[1])
				rss, rssOK := parseMonitorBytes(match[3])
				hbmem, hbmemOK := parseMonitorBytes(match[4])
				if pid > 0 && rssOK && hbmemOK {
					result.Processes = append(result.Processes, acceleratorProcessSnapshot{PID: pid, Name: match[2], RSSBytes: rss, HbmemBytes: hbmem})
				}
			}
		}
	}
	result.Available = len(result.HbmemPools) > 0
	return result
}

func parseMonitorBytes(raw string) (uint64, bool) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	multiplier := float64(1)
	if len(value) > 0 {
		switch value[len(value)-1] {
		case 'K':
			multiplier, value = 1024, value[:len(value)-1]
		case 'M':
			multiplier, value = 1024*1024, value[:len(value)-1]
		case 'G':
			multiplier, value = 1024*1024*1024, value[:len(value)-1]
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number < 0 || number*multiplier > float64(^uint64(0)) {
		return 0, false
	}
	return uint64(number * multiplier), true
}

func readBPUCores(devices []string) ([]bpuCoreSnapshot, bpuTelemetrySnapshot) {
	return readBPUCoresAt(devices, "/sys/devices/platform/soc", "/sys/devices/system/bpu", "/sys/class/devfreq")
}

func readBPUCoresAt(devices []string, platformRoot, systemRoot, devfreqRoot string) ([]bpuCoreSnapshot, bpuTelemetrySnapshot) {
	paths, _ := filepath.Glob(filepath.Join(platformRoot, "*.bpu", "ratio"))
	sort.Strings(paths)
	if len(paths) == 0 {
		if _, err := os.Stat(filepath.Join(systemRoot, "ratio")); err == nil {
			paths = []string{filepath.Join(systemRoot, "ratio")}
		}
	}
	if len(paths) == 0 {
		if len(devices) == 0 {
			return []bpuCoreSnapshot{}, bpuTelemetrySnapshot{Status: "device-not-detected"}
		}
		return []bpuCoreSnapshot{}, bpuTelemetrySnapshot{Status: "metrics-not-exposed"}
	}
	cores := make([]bpuCoreSnapshot, 0, len(paths))
	readFailed := false
	for _, ratioPath := range paths {
		if len(cores) == 16 {
			break
		}
		ratio, err := strconv.ParseFloat(firstTextFile(ratioPath), 64)
		if err != nil || ratio < 0 || ratio > 100 {
			readFailed = true
			continue
		}
		root := filepath.Dir(ratioPath)
		index := len(cores)
		frequencyRoot := findBPUFrequencyRoot(root, index, systemRoot, devfreqRoot)
		cores = append(cores, bpuCoreSnapshot{
			Index: index, Name: fmt.Sprintf("BPU %d", index), UtilizationPercent: ratio,
			CurrentFrequencyHz: readUintFile(filepath.Join(frequencyRoot, "cur_freq")),
			MinimumFrequencyHz: readUintFile(filepath.Join(frequencyRoot, "min_freq")),
			MaximumFrequencyHz: readUintFile(filepath.Join(frequencyRoot, "max_freq")),
		})
	}
	if len(cores) == 0 {
		status := "metrics-not-exposed"
		if readFailed {
			status = "read-failed"
		}
		return cores, bpuTelemetrySnapshot{Status: status, Source: "sysfs"}
	}
	return cores, bpuTelemetrySnapshot{Status: "available", Source: "sysfs-ratio-devfreq"}
}

func findBPUFrequencyRoot(deviceRoot string, index int, systemRoot, devfreqRoot string) string {
	for _, pattern := range []string{
		filepath.Join(deviceRoot, "devfreq", "*"),
		filepath.Join(systemRoot, fmt.Sprintf("bpu%d", index), "devfreq", "*"),
	} {
		roots, _ := filepath.Glob(pattern)
		sort.Strings(roots)
		if len(roots) > 0 {
			return roots[0]
		}
	}
	roots, _ := filepath.Glob(filepath.Join(devfreqRoot, "*.bpu"))
	sort.Strings(roots)
	if index >= 0 && index < len(roots) {
		return roots[index]
	}
	return ""
}

func readUintFile(path string) uint64 {
	if path == "" {
		return 0
	}
	value, _ := strconv.ParseUint(firstTextFile(path), 10, 64)
	return value
}

func readAIMemorySnapshot(memory memorySnapshot) aiMemorySnapshot {
	result := aiMemorySnapshot{CMATotalBytes: memory.CMATotalBytes, CMAFreeBytes: memory.CMAFreeBytes}
	if content, err := readBoundedFile("/sys/kernel/debug/ion/heaps/all_heap_info", maximumTelemetryFileBytes); err == nil {
		result.Heaps = sanitizeIONHeapCapacities(parseIONHeaps(content), memory.TotalBytes)
		result.Available, result.IONAvailable = true, true
		for _, heap := range result.Heaps {
			result.IONAllocatedBytes += heap.AllocatedBytes
			result.IONOrphanedBytes += heap.OrphanedBytes
		}
	}
	if content, err := readBoundedFile("/sys/kernel/debug/ion/clients/bpu-0", maximumTelemetryFileBytes); err == nil {
		result.BPUAllocatedBytes = parseBPUIONClientTotal(content)
		result.Available, result.BPUAllocationAvailable = true, true
	}
	if content, err := readBoundedFile("/sys/kernel/debug/dma_buf/bufinfo", maximumTelemetryFileBytes); err == nil {
		result.DMABufObjects, result.DMABufBytes = parseDMABufTotal(content)
		result.Available, result.DMABufAvailable = true, true
	}
	if result.CMATotalBytes > 0 {
		result.Available, result.CMAAvailable = true, true
	}
	return result
}

func sanitizeIONHeapCapacities(heaps []aiMemoryHeapSnapshot, physicalMemory uint64) []aiMemoryHeapSnapshot {
	for index := range heaps {
		capacity := heaps[index].CapacityBytes
		if physicalMemory > 0 && capacity > physicalMemory {
			heaps[index].CapacityBytes = 0
		}
	}
	return heaps
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, fmt.Errorf("telemetry file exceeds %d bytes", maximum)
	}
	return content, nil
}

func parseIONHeaps(content []byte) []aiMemoryHeapSnapshot {
	heaps := []aiMemoryHeapSnapshot{}
	var current *aiMemoryHeapSnapshot
	flush := func() {
		if current != nil && len(heaps) < 32 {
			heaps = append(heaps, *current)
		}
		current = nil
	}
	for _, raw := range bytes.Split(content, []byte{'\n'}) {
		line := string(raw)
		if match := ionHeapPattern.FindStringSubmatch(line); match != nil {
			flush()
			capacity, _ := strconv.ParseUint(match[2], 10, 64)
			current = &aiMemoryHeapSnapshot{Name: match[1], CapacityBytes: capacity}
			continue
		}
		if current == nil {
			continue
		}
		if match := ionOrphanedPattern.FindStringSubmatch(line); match != nil {
			current.OrphanedBytes, _ = strconv.ParseUint(match[1], 10, 64)
		} else if match := ionTotalPattern.FindStringSubmatch(line); match != nil {
			current.AllocatedBytes, _ = strconv.ParseUint(match[1], 10, 64)
		}
	}
	flush()
	return heaps
}

func parseBPUIONClientTotal(content []byte) uint64 {
	var total uint64
	for _, raw := range bytes.Split(content, []byte{'\n'}) {
		fields := strings.Fields(string(raw))
		if len(fields) == 2 && fields[0] == "total" {
			if value, err := strconv.ParseUint(fields[1], 16, 64); err == nil {
				total = value
			}
		}
	}
	return total
}

func parseDMABufTotal(content []byte) (uint64, uint64) {
	var objects, size uint64
	for _, raw := range bytes.Split(content, []byte{'\n'}) {
		match := dmaBufTotalPattern.FindStringSubmatch(strings.TrimSpace(string(raw)))
		if match == nil {
			continue
		}
		objects, _ = strconv.ParseUint(match[1], 10, 64)
		size, _ = strconv.ParseUint(match[2], 10, 64)
	}
	return objects, size
}

func detectRDKUtilities() map[string]bool {
	result := map[string]bool{}
	for _, name := range []string{"hrut_somstatus", "hrt_model_exec", "rdkos_info"} {
		result[name] = commandAvailable(name)
	}
	return result
}

func commandAvailable(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	for _, root := range []string{"/usr/hobot/bin", "/usr/local/bin", "/usr/sbin", "/usr/bin"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}

func readUptimeSeconds() uint64 {
	fields := strings.Fields(firstTextFile("/proc/uptime"))
	if len(fields) == 0 {
		return 0
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || value < 0 {
		return 0
	}
	return uint64(value)
}
