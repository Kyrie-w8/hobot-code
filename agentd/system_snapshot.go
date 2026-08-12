package main

import (
	"bufio"
	"bytes"
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
	AIMemory      aiMemorySnapshot      `json:"aiMemory"`
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
		BPUDevices:    listBPUDevices(),
		BPUCores:      readBPUCores(),
		AIMemory:      readAIMemorySnapshot(memory),
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

var (
	ionHeapPattern     = regexp.MustCompile(`^\s*([^\s]+)\s+heap total size\s+(\d+)\s*$`)
	ionTotalPattern    = regexp.MustCompile(`^\s*total\s+(\d+)\s*$`)
	ionOrphanedPattern = regexp.MustCompile(`^\s*total orphaned\s+(\d+)\s*$`)
	dmaBufTotalPattern = regexp.MustCompile(`^Total\s+(\d+)\s+objects?,\s+(\d+)\s+bytes$`)
)

func readBPUCores() []bpuCoreSnapshot {
	paths, _ := filepath.Glob("/sys/devices/platform/soc/*.bpu/ratio")
	sort.Strings(paths)
	if len(paths) == 0 {
		if _, err := os.Stat("/sys/devices/system/bpu/ratio"); err == nil {
			paths = []string{"/sys/devices/system/bpu/ratio"}
		}
	}
	cores := make([]bpuCoreSnapshot, 0, len(paths))
	for _, ratioPath := range paths {
		if len(cores) == 16 {
			break
		}
		ratio, err := strconv.ParseFloat(firstTextFile(ratioPath), 64)
		if err != nil || ratio < 0 || ratio > 100 {
			continue
		}
		root := filepath.Dir(ratioPath)
		devfreqRoots, _ := filepath.Glob(filepath.Join(root, "devfreq", "*"))
		frequencyRoot := ""
		if len(devfreqRoots) > 0 {
			frequencyRoot = devfreqRoots[0]
		}
		index := len(cores)
		cores = append(cores, bpuCoreSnapshot{
			Index: index, Name: fmt.Sprintf("BPU %d", index), UtilizationPercent: ratio,
			CurrentFrequencyHz: readUintFile(filepath.Join(frequencyRoot, "cur_freq")),
			MinimumFrequencyHz: readUintFile(filepath.Join(frequencyRoot, "min_freq")),
			MaximumFrequencyHz: readUintFile(filepath.Join(frequencyRoot, "max_freq")),
		})
	}
	return cores
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
		result.Heaps = parseIONHeaps(content)
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
