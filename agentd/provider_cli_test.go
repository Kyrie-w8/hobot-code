package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func providerTestConfig(t *testing.T) config {
	t.Helper()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	providerPath := filepath.Join(agent, "providers.json")
	if err := os.WriteFile(providerPath, []byte("{\n  \"schemaVersion\": 1,\n  \"providers\": []\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hobot.env"), []byte("# private credentials\nANTHROPIC_AUTH_TOKEN=drobotics-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return config{
		ConfigRoot: root, AgentDir: agent, ManagedProviderConfig: providerPath,
		SocketPath: filepath.Join(root, "missing", "agentd.sock"),
	}
}

func TestProviderAddStoresCredentialSeparatelyAndRedactsOutput(t *testing.T) {
	cfg := providerTestConfig(t)
	secret := "provider-super-secret"
	var stdout, stderr strings.Builder
	err := runProviderCLI(cfg, []string{
		"add", "acme", "--base-url", "https://models.example/v1", "--api", "openai-responses",
		"--model", "coder-v1", "--name", "Acme Models", "--model-name", "Coder",
		"--context-window", "200000", "--max-tokens", "32000", "--reasoning", "--image", "--token-stdin",
	}, strings.NewReader(secret+"\n"), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	providerBytes, err := os.ReadFile(cfg.ManagedProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	environmentBytes, err := os.ReadFile(filepath.Join(cfg.ConfigRoot, "hobot.env"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(providerBytes, []byte(secret)) || !bytes.Contains(environmentBytes, []byte(secret)) {
		t.Fatalf("credential storage boundary failed: providers=%s envHasSecret=%t", providerBytes, bytes.Contains(environmentBytes, []byte(secret)))
	}
	if strings.Contains(stdout.String()+stderr.String(), secret) || strings.Contains(stdout.String()+stderr.String(), "HOBOT_CODE_PROVIDER_KEY_") || strings.Contains(stdout.String()+stderr.String(), "models.example") {
		t.Fatalf("provider command leaked sensitive configuration: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	providers, err := loadManagedProviderDefinitions(cfg.ManagedProviderConfig)
	if err != nil || len(providers) != 1 {
		t.Fatalf("saved provider did not pass runtime validation: providers=%+v err=%v", providers, err)
	}
	model := providers[0]["models"].([]any)[0].(map[string]any)
	if providers[0]["api"] != "openai-responses" || model["id"] != "coder-v1" || model["reasoning"] != true {
		t.Fatalf("provider options were not preserved: %+v", providers[0])
	}
	for _, path := range []string{cfg.ManagedProviderConfig, filepath.Join(cfg.ConfigRoot, "hobot.env"), filepath.Join(cfg.ConfigRoot, ".provider.lock")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("provider file is not private: path=%s mode=%v err=%v", path, info.Mode().Perm(), err)
		}
	}
}

func TestProviderListIsSafeAndReportsCredentialReadiness(t *testing.T) {
	cfg := providerTestConfig(t)
	provider := `{"schemaVersion":1,"providers":[{"id":"private","name":"Private","baseUrl":"https://internal.example/v1","api":"anthropic-messages","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_PRIVATE","models":[{"id":"reasoner","contextWindow":65536,"maxTokens":4096,"reasoning":true}]}]}`
	if err := os.WriteFile(cfg.ManagedProviderConfig, []byte(provider), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, ProviderKeys: map[string]string{"HOBOT_CODE_PROVIDER_KEY_PRIVATE": "never-print-this"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.gatewayCredential = bundle
	var output strings.Builder
	if err := runProviderCLI(cfg, []string{"list", "--json"}, strings.NewReader(""), &output, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, forbidden := range []string{"never-print-this", "internal.example", "HOBOT_CODE_PROVIDER_KEY_PRIVATE"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("provider list leaked %q: %s", forbidden, text)
		}
	}
	var items []providerListItem
	if err := json.Unmarshal([]byte(text), &items); err != nil || len(items) != 1 || items[0].Credential != "ready" || items[0].CredentialUsers != 1 || !items[0].Models[0].Reasoning {
		t.Fatalf("unexpected safe provider list: items=%+v err=%v", items, err)
	}
}

func TestProviderRotateReplacesOnlyCredentialAndNeverPrintsIt(t *testing.T) {
	cfg := providerTestConfig(t)
	oldSecret := "old-provider-secret"
	newSecret := "new-provider-secret"
	if err := runProviderCLI(cfg, []string{"add", "rotate-me", "--base-url", "https://api.example/v1", "--model", "chat", "--token-stdin"}, strings.NewReader(oldSecret+"\n"), &strings.Builder{}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	providerBefore, err := os.ReadFile(cfg.ManagedProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := runProviderCLI(cfg, []string{"rotate", "rotate-me", "--token-stdin"}, strings.NewReader(newSecret+"\n"), &output, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	providerAfter, _ := os.ReadFile(cfg.ManagedProviderConfig)
	environment, _ := os.ReadFile(filepath.Join(cfg.ConfigRoot, "hobot.env"))
	if !bytes.Equal(providerBefore, providerAfter) {
		t.Fatal("credential rotation changed provider metadata")
	}
	if bytes.Contains(environment, []byte(oldSecret)) || !bytes.Contains(environment, []byte(newSecret)) {
		t.Fatalf("credential was not replaced: old=%t new=%t", bytes.Contains(environment, []byte(oldSecret)), bytes.Contains(environment, []byte(newSecret)))
	}
	if strings.Contains(output.String(), oldSecret) || strings.Contains(output.String(), newSecret) || strings.Contains(output.String(), "HOBOT_CODE_PROVIDER_KEY_") {
		t.Fatalf("credential rotation leaked sensitive data: %q", output.String())
	}
}

func TestProviderRotateRepairsMissingCredentialAndProtectsSharedKeys(t *testing.T) {
	cfg := providerTestConfig(t)
	provider := `{"schemaVersion":1,"providers":[{"id":"one","baseUrl":"https://one.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_SHARED","models":[{"id":"chat"}]},{"id":"two","baseUrl":"https://two.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_SHARED","models":[{"id":"coder"}]}]}`
	if err := os.WriteFile(cfg.ManagedProviderConfig, []byte(provider), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runProviderCLI(cfg, []string{"rotate", "one", "--token-stdin"}, strings.NewReader("must-not-persist\n"), &strings.Builder{}, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "--yes-shared") {
		t.Fatalf("shared credential rotation did not require confirmation: %v", err)
	}
	environmentPath := filepath.Join(cfg.ConfigRoot, "hobot.env")
	environment, _ := os.ReadFile(environmentPath)
	if bytes.Contains(environment, []byte("must-not-persist")) {
		t.Fatal("unconfirmed shared credential was persisted")
	}
	var output strings.Builder
	if err := runProviderCLI(cfg, []string{"rotate", "one", "--yes-shared", "--token-stdin"}, strings.NewReader("shared-replacement\n"), &output, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	environment, _ = os.ReadFile(environmentPath)
	if !bytes.Contains(environment, []byte("HOBOT_CODE_PROVIDER_KEY_SHARED=shared-replacement")) || !strings.Contains(output.String(), "shared by 2 providers") {
		t.Fatalf("explicit shared rotation failed: output=%q env=%q", output.String(), environment)
	}
}

func TestProviderRemoveRequiresConfirmationAndDeletesUnreferencedCredential(t *testing.T) {
	cfg := providerTestConfig(t)
	secret := "remove-me-secret"
	if err := runProviderCLI(cfg, []string{"add", "remove-me", "--base-url", "https://api.example/v1", "--model", "chat", "--token-stdin"}, strings.NewReader(secret+"\n"), &strings.Builder{}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfg.ManagedProviderConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := runProviderCLI(cfg, []string{"remove", "remove-me"}, strings.NewReader(""), &strings.Builder{}, &strings.Builder{}); err == nil {
		t.Fatal("provider removal without --yes succeeded")
	}
	afterRejected, _ := os.ReadFile(cfg.ManagedProviderConfig)
	if !bytes.Equal(before, afterRejected) {
		t.Fatal("unconfirmed removal changed the provider configuration")
	}
	var output strings.Builder
	if err := runProviderCLI(cfg, []string{"remove", "remove-me", "--yes"}, strings.NewReader(""), &output, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	providers, err := loadManagedProviderDefinitions(cfg.ManagedProviderConfig)
	if err != nil || len(providers) != 0 {
		t.Fatalf("provider was not removed: %+v err=%v", providers, err)
	}
	environment, err := os.ReadFile(filepath.Join(cfg.ConfigRoot, "hobot.env"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(environment, []byte(secret)) || strings.Contains(output.String(), secret) {
		t.Fatal("removed provider credential was retained or printed")
	}
}

func TestProviderRemoveRetainsSharedCredential(t *testing.T) {
	cfg := providerTestConfig(t)
	provider := `{"schemaVersion":1,"providers":[{"id":"one","baseUrl":"https://one.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_SHARED","models":[{"id":"chat"}]},{"id":"two","baseUrl":"https://two.example/v1","api":"openai-completions","credentialEnv":"HOBOT_CODE_PROVIDER_KEY_SHARED","models":[{"id":"coder"}]}]}`
	if err := os.WriteFile(cfg.ManagedProviderConfig, []byte(provider), 0o600); err != nil {
		t.Fatal(err)
	}
	environmentPath := filepath.Join(cfg.ConfigRoot, "hobot.env")
	if err := os.WriteFile(environmentPath, []byte("HOBOT_CODE_PROVIDER_KEY_SHARED=shared-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := runProviderCLI(cfg, []string{"remove", "one", "--yes"}, strings.NewReader(""), &output, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	environment, _ := os.ReadFile(environmentPath)
	if !bytes.Contains(environment, []byte("shared-secret")) || !strings.Contains(output.String(), "retained") {
		t.Fatalf("shared provider credential was removed: output=%q env=%q", output.String(), environment)
	}
}

func TestProviderAddRejectsInvalidOrDuplicateConfigurationBeforeReadingSecret(t *testing.T) {
	cfg := providerTestConfig(t)
	invalid := []string{"add", "bad", "--base-url", "http://remote.example/v1", "--model", "chat", "--token-stdin"}
	if err := runProviderCLI(cfg, invalid, strings.NewReader("should-not-be-read\n"), &strings.Builder{}, &strings.Builder{}); err == nil {
		t.Fatal("unsafe provider URL was accepted")
	}
	valid := []string{"add", "safe", "--base-url", "https://api.example/v1", "--model", "chat", "--token-stdin"}
	if err := runProviderCLI(cfg, valid, strings.NewReader("first-secret\n"), &strings.Builder{}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if err := runProviderCLI(cfg, valid, strings.NewReader("replacement-secret\n"), &strings.Builder{}, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate provider was not rejected safely: %v", err)
	}
	environment, _ := os.ReadFile(filepath.Join(cfg.ConfigRoot, "hobot.env"))
	if bytes.Contains(environment, []byte("replacement-secret")) {
		t.Fatal("rejected replacement credential was persisted")
	}
}

func TestProviderCredentialValidationRejectsShellFileAmbiguity(t *testing.T) {
	for _, value := range []string{"", "has space", "has'quote", "has\"quote", strings.Repeat("x", maximumGatewayTokenBytes+1)} {
		if _, err := readCredentialLine(strings.NewReader(value + "\n")); err == nil {
			t.Fatalf("unsafe credential was accepted: length=%d", len(value))
		}
	}
	value, err := readCredentialLine(strings.NewReader("safe-key_+/=:.@#\n"))
	if err != nil || string(value) != "safe-key_+/=:.@#" {
		t.Fatalf("safe credential was rejected: %q %v", value, err)
	}
}

func TestProviderStructuredInputIsStrictAndNeverLeaksCredential(t *testing.T) {
	cfg := providerTestConfig(t)
	secret := "structured-secret"
	metadata := `{"id":"structured","name":"Structured","baseUrl":"https://api.example/v1","api":"openai-responses","model":"coder","contextWindow":32768,"maxTokens":4096,"reasoning":true}`
	var output strings.Builder
	if err := runProviderCLI(cfg, []string{"add", "--request-stdin"}, strings.NewReader(metadata+"\n"+secret+"\n"), &output, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("structured provider input leaked its credential")
	}
	remove := `{"id":"structured"}`
	if err := runProviderCLI(cfg, []string{"remove", "--request-stdin"}, strings.NewReader(remove+"\n"), &strings.Builder{}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	providers, err := loadManagedProviderDefinitions(cfg.ManagedProviderConfig)
	if err != nil || len(providers) != 0 {
		t.Fatalf("structured removal failed: providers=%+v err=%v", providers, err)
	}

	invalidInputs := []string{
		`{"id":"bad","id":"duplicate","baseUrl":"https://api.example/v1","api":"openai-responses","model":"coder"}` + "\n" + secret + "\n",
		`{"id":"bad","baseUrl":"https://api.example/v1","api":"openai-responses","model":"coder","unknown":true}` + "\n" + secret + "\n",
		`{"id":"bad","baseUrl":"https://api.example/v1","api":"openai-responses","model":"coder"}` + "\n" + secret + "\nextra\n",
	}
	for index, input := range invalidInputs {
		var stderr strings.Builder
		err := runProviderCLI(cfg, []string{"add", "--request-stdin"}, strings.NewReader(input), &strings.Builder{}, &stderr)
		if err == nil || strings.Contains(err.Error()+stderr.String(), secret) {
			t.Fatalf("invalid structured request %d was accepted or leaked its secret: %v %q", index, err, stderr.String())
		}
	}
	if _, err := readProviderRemoveRequest(strings.NewReader(`{"id":"bad","unknown":true}` + "\n")); err == nil {
		t.Fatal("structured removal accepted an unknown field")
	}
	if _, err := readProviderRemoveRequest(strings.NewReader(`{"id":"bad"}` + "\nextra\n")); err == nil {
		t.Fatal("structured removal accepted trailing data")
	}
	rotation, token, err := readProviderRotateRequest(strings.NewReader(`{"id":"structured","allowShared":true}` + "\nreplacement-key\n"))
	if err != nil || rotation.ID != "structured" || !rotation.AllowShared || string(token) != "replacement-key" {
		t.Fatalf("valid rotation request failed: request=%+v err=%v", rotation, err)
	}
	clearBytes(token)
	if _, token, err := readProviderRotateRequest(strings.NewReader(`{"id":"bad","unknown":true}` + "\n" + secret + "\n")); err == nil || token != nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe rotation request was accepted or leaked its key: %v", err)
	}
	if _, token, err := readProviderRotateRequest(strings.NewReader(`{"id":"bad"}` + "\n" + secret + "\nextra\n")); err == nil || token != nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("trailing rotation request was accepted or leaked its key: %v", err)
	}
}
