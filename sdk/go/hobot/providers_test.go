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

func providerSSH(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "command.log")
	script := filepath.Join(dir, "ssh")
	content := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do printf '<%%s>\n' "$arg" >> %q; done
case "$*" in
  *'hobot provider list --json')
    printf '%%s\n' '[{"id":"acme","api":"openai-responses","models":[{"id":"coder","contextWindow":65536,"maxTokens":4096,"reasoning":true,"image":false}],"credential":"ready","credentialUsers":1}]'
    ;;
  *'hobot provider add --request-stdin')
    IFS= read -r metadata
    IFS= read -r credential
    printf 'metadata=%%s\ncredential-length=%%s\n' "$metadata" "${#credential}" >> %q
    printf 'added\n'
    ;;
  *'hobot provider remove --request-stdin')
    IFS= read -r metadata
    printf 'remove=%%s\n' "$metadata" >> %q
    printf 'removed\n'
    ;;
  *'hobot provider rotate --request-stdin')
    IFS= read -r metadata
    IFS= read -r credential
    printf 'rotate=%%s\nrotate-credential-length=%%s\n' "$metadata" "${#credential}" >> %q
    printf 'rotated\n'
    ;;
  *'hobot daemon restart') printf 'restarted\n' ;;
  *) exit 2 ;;
esac
`, log, log, log, log)
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, log
}

func TestManagedProviderCommandsUseFixedSSHCommandsAndStdinSecrets(t *testing.T) {
	ssh, log := providerSSH(t)
	client, err := NewClient(Config{Host: "rdk.local", User: "root", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	providers, err := client.ManagedProviders(ctx)
	if err != nil || len(providers) != 1 || providers[0].Credential != "ready" || providers[0].Models[0].ID != "coder" {
		t.Fatalf("managed providers=%+v err=%v", providers, err)
	}
	secret := "never-in-argv"
	request := AddManagedProviderRequest{ID: "private", BaseURL: "https://private.example/v1", API: "openai-responses", Model: "coder"}
	if err := client.AddManagedProvider(ctx, request, secret); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveManagedProvider(ctx, "private", false); err != nil {
		t.Fatal(err)
	}
	if err := client.RotateManagedProviderCredential(ctx, "private", "replacement-key", true); err != nil {
		t.Fatal(err)
	}
	if err := client.RestartDaemon(ctx); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, secret) || strings.Contains(text, "replacement-key") || !strings.Contains(text, "<hobot provider add --request-stdin>") || !strings.Contains(text, "credential-length=13") || !strings.Contains(text, `remove={"id":"private","keepCredential":false}`) || !strings.Contains(text, `rotate={"allowShared":true,"id":"private"}`) || !strings.Contains(text, "rotate-credential-length=15") {
		t.Fatalf("provider SSH boundary failed: %s", text)
	}
}

func TestManagedProviderCommandErrorsNeverContainInputCredential(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'safe failure\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	secret := "private-failure-secret"
	err = client.AddManagedProvider(context.Background(), AddManagedProviderRequest{ID: "acme", BaseURL: "https://example.com/v1", API: "openai-completions", Model: "coder"}, secret)
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "safe failure") {
		t.Fatalf("unexpected provider failure: %v", err)
	}
}

func TestProviderCommandErrorsAreBoundedSafeSingleLine(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nprintf 'first\\n\\033[31msecond\\033[0m\\tthird\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ManagedProviders(context.Background())
	if err == nil || strings.ContainsAny(err.Error(), "\n\r\t\x1b") || !strings.Contains(err.Error(), "first second third") || len(err.Error()) > 1200 {
		t.Fatalf("unsafe provider error presentation: %q", err)
	}
}

func TestManagedProvidersRejectsUntrustedBoardResponse(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "trailing JSON", payload: `[] {}`},
		{name: "unknown credential state", payload: `[{"id":"acme","api":"openai-responses","models":[{"id":"coder","contextWindow":65536,"maxTokens":4096,"reasoning":true,"image":false}],"credential":"secret-ready","credentialUsers":1}]`},
		{name: "missing credential users", payload: `[{"id":"acme","api":"openai-responses","models":[{"id":"coder","contextWindow":65536,"maxTokens":4096,"reasoning":true,"image":false}],"credential":"ready"}]`},
		{name: "duplicate model", payload: `[{"id":"acme","api":"openai-responses","models":[{"id":"coder","contextWindow":65536,"maxTokens":4096,"reasoning":true,"image":false},{"id":"coder","contextWindow":65536,"maxTokens":4096,"reasoning":false,"image":false}],"credential":"ready","credentialUsers":1}]`},
		{name: "control character in name", payload: "[{\"id\":\"acme\",\"name\":\"unsafe\\nname\",\"api\":\"openai-responses\",\"models\":[{\"id\":\"coder\",\"contextWindow\":65536,\"maxTokens\":4096,\"reasoning\":true,\"image\":false}],\"credential\":\"ready\",\"credentialUsers\":1}]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			ssh := filepath.Join(dir, "ssh")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\n", test.payload)
			if err := os.WriteFile(ssh, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			client, err := NewClient(Config{Host: "rdk.local", SSHBinary: ssh})
			if err != nil {
				t.Fatal(err)
			}
			if providers, err := client.ManagedProviders(context.Background()); err == nil {
				t.Fatalf("accepted untrusted provider response: %+v", providers)
			}
		})
	}
}
