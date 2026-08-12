package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
}

type diskSnapshot struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
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
	RDKUtilities  map[string]bool       `json:"rdkUtilities"`
	UptimeSeconds uint64                `json:"uptimeSeconds"`
}

func collectSystemSnapshot(cfg config) systemSnapshot {
	board := firstTextFile("/sys/firmware/devicetree/base/model", "/proc/device-tree/model")
	if board == "" {
		board = "Unknown Linux board"
	}
	host, _ := os.Hostname()
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
		Memory:        readMemorySnapshot(),
		Disk:          readDiskSnapshot(cfg.StateRoot),
		ThermalZones:  readThermalZones(),
		BPUDevices:    listBPUDevices(),
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
		if strings.HasPrefix(name, "bpu") || strings.HasPrefix(name, "hobot") || strings.HasPrefix(name, "dnn") {
			devices = append(devices, filepath.Join("/dev", entry.Name()))
		}
	}
	return devices
}

func detectRDKUtilities() map[string]bool {
	result := map[string]bool{}
	for _, name := range []string{"hrut_somstatus", "hrt_model_exec", "rdkos_info"} {
		_, err := exec.LookPath(name)
		result[name] = err == nil
	}
	return result
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
