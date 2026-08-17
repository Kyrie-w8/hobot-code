package hobot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func updateSSH(t *testing.T, checkOutput string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "commands.log")
	script := filepath.Join(dir, "ssh")
	content := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do printf '<%%s>\n' "$arg" >> %q; done
case "$*" in
  *'hobot update --check') printf '%%s\n' %q ;;
  *'hobot update --version 0.27.0') printf 'updated\n' ;;
  *) exit 2 ;;
esac
`, log, checkOutput)
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, log
}

func TestBoardUpdateUsesOnlyFixedCommandsAndStrictStatus(t *testing.T) {
	ssh, log := updateSSH(t, "Hobot Code 0.27.0 is available; installed version is 0.26.0.")
	client, err := NewClient(Config{Host: "rdk.local", User: "root", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	check, err := client.CheckBoardUpdate(ctx)
	if err != nil || check.Status != "available" || check.InstalledVersion != "0.26.0" || check.AvailableVersion != "0.27.0" {
		t.Fatalf("check=%+v err=%v", check, err)
	}
	if err := client.InstallBoardUpdate(ctx, "0.27.0"); err != nil {
		t.Fatal(err)
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(commands)
	if !strings.Contains(text, "<hobot update --check>") || !strings.Contains(text, "<hobot update --version 0.27.0>") || strings.Contains(text, "--force") || strings.Contains(text, "--allow-downgrade") {
		t.Fatalf("unsafe update command boundary: %s", text)
	}
	if err := client.InstallBoardUpdate(ctx, "0.27.0; reboot"); err == nil {
		t.Fatal("unsafe update version was accepted")
	}
}

func TestBoardUpdateStatusRejectsUntrustedOutput(t *testing.T) {
	for _, output := range []string{
		"Hobot Code latest is current.",
		"Hobot Code 0.27.0 is available; installed version is 0.26.0. injected",
		"Hobot Code 0.27.0 is available; installed version is 0.26.0.\nsecret",
	} {
		ssh, _ := updateSSH(t, output)
		client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := client.CheckBoardUpdate(context.Background()); err == nil {
			t.Fatalf("accepted untrusted update output %q: %+v", output, result)
		}
	}
}

func TestBoardUpdateStatusDistinguishesCurrentAndNewerInstall(t *testing.T) {
	for _, test := range []struct {
		output, status, installed, available string
	}{
		{"Hobot Code 0.26.0 is current.", "current", "0.26.0", "0.26.0"},
		{"Hobot Code release metadata reports 0.25.0, older than installed version 0.26.0; no update will be applied.", "source-older", "0.26.0", "0.25.0"},
	} {
		ssh, _ := updateSSH(t, test.output)
		client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.CheckBoardUpdate(context.Background())
		if err != nil || result.Status != test.status || result.InstalledVersion != test.installed || result.AvailableVersion != test.available {
			t.Fatalf("output=%q result=%+v err=%v", test.output, result, err)
		}
	}
}

func TestInstallBoardServiceExecutesInstallScript(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands.log")
	script := filepath.Join(dir, "ssh")
	content := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do printf '<%%s>\n' "$arg" >> %q; done
printf 'installed\n'
`, log)
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{Host: "rdk.local", User: "root", SSHBinary: script})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.InstallBoardService(ctx, ""); err != nil {
		t.Fatalf("InstallBoardService failed: %v", err)
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(commands)
	if !strings.Contains(text, "hobot-install.sh | sh") {
		t.Fatalf("install command missing install script: %s", text)
	}
	if err := client.InstallBoardService(ctx, "0.28.0; rm -rf /"); err == nil {
		t.Fatal("invalid install version was accepted")
	}
}
