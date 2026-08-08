package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type launcherConfig struct {
	Config   string `json:"config"`
	Provider string `json:"provider,omitempty"`
	Board    string `json:"board,omitempty"`
}

func applyLauncherDefaults(opts *options) error {
	defaults := launcherConfig{}
	for _, path := range launcherCandidates() {
		loaded, err := readLauncher(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		mergeLauncher(&defaults, loaded)
	}
	mergeLauncher(&defaults, launcherConfig{
		Config: os.Getenv("ASTER_CONFIG"), Provider: os.Getenv("ASTER_PROVIDER"), Board: os.Getenv("ASTER_BOARD"),
	})
	if opts.configPath == "" {
		opts.configPath = defaults.Config
	}
	if opts.providerPath == "" {
		opts.providerPath = defaults.Provider
	}
	if opts.boardPath == "" {
		opts.boardPath = defaults.Board
	}
	return nil
}

func launcherCandidates() []string {
	if path := os.Getenv("ASTER_LAUNCHER"); path != "" {
		return []string{path}
	}
	paths := []string{"/etc/aster/launcher.json"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "aster", "launcher.json"))
	}
	return paths
}

func readLauncher(path string) (launcherConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return launcherConfig{}, err
	}
	var launcher launcherConfig
	if err := json.Unmarshal(data, &launcher); err != nil {
		return launcherConfig{}, fmt.Errorf("parse launcher %s: %w", path, err)
	}
	base := filepath.Dir(path)
	launcher.Config = resolveLauncherPath(base, launcher.Config)
	launcher.Provider = resolveLauncherPath(base, launcher.Provider)
	launcher.Board = resolveLauncherPath(base, launcher.Board)
	return launcher, nil
}

func mergeLauncher(dst *launcherConfig, src launcherConfig) {
	if src.Config != "" {
		dst.Config = src.Config
	}
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.Board != "" {
		dst.Board = src.Board
	}
}

func resolveLauncherPath(base, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(base, path))
}

func saveLauncher(opts options) int {
	launcher := launcherConfig{Config: absolutePath(opts.configPath), Provider: absolutePath(opts.providerPath), Board: absolutePath(opts.boardPath)}
	if launcher.Config == "" {
		return fail(fmt.Errorf("configure requires --config"))
	}
	for _, entry := range []struct {
		name string
		path string
	}{{"config", launcher.Config}, {"provider", launcher.Provider}, {"board", launcher.Board}} {
		if entry.path == "" {
			continue
		}
		if info, err := os.Stat(entry.path); err != nil || info.IsDir() {
			if err == nil {
				err = fmt.Errorf("is a directory")
			}
			return fail(fmt.Errorf("invalid %s path %s: %w", entry.name, entry.path, err))
		}
	}
	path := launcherWritePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fail(err)
	}
	data, _ := json.MarshalIndent(launcher, "", "  ")
	data = append(data, '\n')
	if err := atomicWriteFile(path, data, 0644); err != nil {
		return fail(fmt.Errorf("write launcher %s: %w", path, err))
	}
	fmt.Println("Saved default launch profile to", path)
	return 0
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".launcher-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err = temporary.Write(data); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func launcherWritePath() string {
	if path := os.Getenv("ASTER_LAUNCHER"); path != "" {
		return path
	}
	if os.Geteuid() == 0 {
		return "/etc/aster/launcher.json"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "aster", "launcher.json")
}

func absolutePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}
