package board

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Snapshot struct {
	Profile          string   `json:"profile"`
	Hostname         string   `json:"hostname"`
	Model            string   `json:"model"`
	OS               string   `json:"os"`
	Architecture     string   `json:"architecture"`
	Kernel           string   `json:"kernel"`
	CPUs             int      `json:"cpus"`
	MemoryTotalBytes uint64   `json:"memory_total_bytes"`
	MemoryFreeBytes  uint64   `json:"memory_available_bytes"`
	DiskFreeBytes    uint64   `json:"disk_free_bytes"`
	BPUCores         int      `json:"bpu_cores"`
	MaxTemperatureC  *float64 `json:"max_temperature_c,omitempty"`
	Devices          []string `json:"devices"`
	Commands         []string `json:"commands"`
}

func Detect(profileHint string) Snapshot {
	host, _ := os.Hostname()
	model := trimFile("/proc/device-tree/model")
	if model == "" {
		model = runtime.GOARCH
	}
	s := Snapshot{
		Profile: profileHint, Hostname: host, Model: model,
		OS: osRelease(), Architecture: runtime.GOARCH, Kernel: unameRelease(), CPUs: runtime.NumCPU(),
	}
	s.MemoryTotalBytes, s.MemoryFreeBytes = memory()
	var stat syscall.Statfs_t
	if syscall.Statfs("/", &stat) == nil {
		s.DiskFreeBytes = stat.Bavail * uint64(stat.Bsize)
	}
	s.BPUCores = countGlob("/sys/class/bpu/bpu_core*")
	if s.BPUCores == 0 {
		s.BPUCores = countGlob("/sys/class/devfreq/*.bpu")
	}
	if s.BPUCores == 0 && commandExists("hrt_model_exec") {
		s.BPUCores = 1
	}
	if s.Profile == "" || s.Profile == "auto" {
		s.Profile = inferProfile(model, runtime.NumCPU())
	}
	s.MaxTemperatureC = maxTemperature()
	for _, pattern := range []string{"/dev/video*", "/dev/i2c-*", "/dev/spidev*", "/dev/ttyUSB*", "/dev/can*"} {
		matches, _ := filepath.Glob(pattern)
		s.Devices = append(s.Devices, matches...)
	}
	for _, name := range []string{"hrt_model_exec", "ros2", "tros", "docker", "podman", "systemctl"} {
		if commandExists(name) {
			s.Commands = append(s.Commands, name)
		}
	}
	return s
}

func (s Snapshot) JSON() string {
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func inferProfile(model string, cpus int) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "s600") || cpus >= 16:
		return "s600"
	case strings.Contains(lower, "s100"):
		return "s100"
	case strings.Contains(lower, "x5"):
		return "x5"
	default:
		return "generic"
	}
}

func memory() (uint64, uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var total, available uint64
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = v * 1024
		case "MemAvailable":
			available = v * 1024
		}
	}
	return total, available
}

func maxTemperature() *float64 {
	paths, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	var max float64
	found := false
	for _, path := range paths {
		v, err := strconv.ParseFloat(trimFile(path), 64)
		if err == nil {
			if v > 1000 {
				v /= 1000
			}
			if !found || v > max {
				max, found = v, true
			}
		}
	}
	if !found {
		return nil
	}
	return &max
}

func trimFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.Trim(strings.ReplaceAll(string(b), "\x00", ""), " \t\r\n")
}

func osRelease() string {
	b, _ := os.ReadFile("/etc/os-release")
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(value, `"`)
		}
	}
	return strings.TrimSpace(values["NAME"] + " " + values["VERSION_ID"])
}

func unameRelease() string {
	b, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

func countGlob(pattern string) int {
	values, _ := filepath.Glob(pattern)
	return len(values)
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
