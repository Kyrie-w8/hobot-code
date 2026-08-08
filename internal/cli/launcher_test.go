package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLauncherDefaultsPreserveExplicitFlags(t *testing.T) {
	dir := t.TempDir()
	launcherPath := filepath.Join(dir, "launcher.json")
	data := []byte(`{"config":"base.json","provider":"provider.json","board":"board.json"}`)
	if err := os.WriteFile(launcherPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASTER_LAUNCHER", launcherPath)
	t.Setenv("ASTER_CONFIG", "")
	t.Setenv("ASTER_PROVIDER", "")
	t.Setenv("ASTER_BOARD", "")
	opts := options{providerPath: "/explicit/provider.json"}
	if err := applyLauncherDefaults(&opts); err != nil {
		t.Fatal(err)
	}
	if opts.configPath != filepath.Join(dir, "base.json") || opts.boardPath != filepath.Join(dir, "board.json") {
		t.Fatalf("unexpected defaults: %+v", opts)
	}
	if opts.providerPath != "/explicit/provider.json" {
		t.Fatalf("explicit provider was replaced: %+v", opts)
	}
}

func TestLauncherEnvironmentOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte(`{"config":"file.json"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASTER_LAUNCHER", path)
	t.Setenv("ASTER_CONFIG", "/env/config.json")
	opts := options{}
	if err := applyLauncherDefaults(&opts); err != nil {
		t.Fatal(err)
	}
	if opts.configPath != "/env/config.json" {
		t.Fatalf("config=%q", opts.configPath)
	}
}

func TestSaveLauncher(t *testing.T) {
	dir := t.TempDir()
	launcherPath := filepath.Join(dir, "launcher.json")
	t.Setenv("ASTER_LAUNCHER", launcherPath)
	paths := launcherConfig{
		Config: filepath.Join(dir, "config.json"), Provider: filepath.Join(dir, "provider.json"), Board: filepath.Join(dir, "board.json"),
	}
	for _, path := range []string{paths.Config, paths.Provider, paths.Board} {
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if code := saveLauncher(options{configPath: paths.Config, providerPath: paths.Provider, boardPath: paths.Board}); code != 0 {
		t.Fatalf("exit code=%d", code)
	}
	data, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved launcherConfig
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved != paths {
		t.Fatalf("saved=%+v want=%+v", saved, paths)
	}
}

func TestTerminalLayoutHelpers(t *testing.T) {
	t.Setenv("COLUMNS", "64")
	if width := terminalWidth(); width != 64 {
		t.Fatalf("width=%d", width)
	}
	if got := fitText("abcdefgh", 5); got != "abcd…" {
		t.Fatalf("fitText=%q", got)
	}
	if got := padSides("left", "right", 12); got != "left   right" {
		t.Fatalf("padSides=%q", got)
	}
}

func TestRendererStopsCompletedStatus(t *testing.T) {
	renderer := newEventRenderer(chatOptions{})
	renderer.theme.interactive = false
	renderer.setStatus("Thinking")
	renderer.StopStatus()
	if renderer.status != "" || renderer.statusVisible {
		t.Fatalf("status was not stopped: %+v", renderer)
	}
}
