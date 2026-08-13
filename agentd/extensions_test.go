package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceExtensionCatalog(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "extensions", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPackagedExtensionCatalogIsValidAndDeterministic(t *testing.T) {
	catalog, err := loadExtensionCatalog(sourceExtensionCatalog(t), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 || catalog.APIVersion != extensionCatalogAPI || catalog.HostVersion != "dev" {
		t.Fatalf("unexpected catalog header: %+v", catalog)
	}
	if !catalog.Policy.InventoryOnly || catalog.Policy.PermissionAuthority != "board" || catalog.Policy.HotReload {
		t.Fatalf("unsafe or ambiguous extension policy: %+v", catalog.Policy)
	}
	if len(catalog.Entries) < 4 || catalog.Entries[0].ID != "hobot.rdk-core" {
		t.Fatalf("built-in entries missing or unsorted: %+v", catalog.Entries)
	}
	for _, entry := range catalog.Entries {
		if entry.Origin != "built-in" || entry.Trust != "product" || !entry.DefaultEnabled {
			t.Fatalf("built-in extension has an unexpected declaration: %+v", entry)
		}
	}
}

func TestExtensionCatalogRejectsUnknownFieldsDuplicatesAndVersionDrift(t *testing.T) {
	raw, err := os.ReadFile(sourceExtensionCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{name: "unknown", mutate: func(value map[string]any) { value["surprise"] = true }, want: "unknown field"},
		{name: "duplicate", mutate: func(value map[string]any) {
			entries := value["entries"].([]any)
			value["entries"] = append(entries, entries[0])
		}, want: "duplicate id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyRaw, _ := json.Marshal(document)
			var value map[string]any
			_ = json.Unmarshal(copyRaw, &value)
			test.mutate(value)
			encoded, _ := json.Marshal(value)
			path := filepath.Join(t.TempDir(), "catalog.json")
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadExtensionCatalog(path, "dev"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid catalog was accepted: %v", err)
			}
		})
	}
	if _, err := loadExtensionCatalog(sourceExtensionCatalog(t), "99.0.0"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("catalog version drift was accepted: %v", err)
	}
}

func TestExtensionCatalogRejectsEntrypointsOutsideProductRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "extensions"), 0o700); err != nil {
		t.Fatal(err)
	}
	catalog := `{"schemaVersion":1,"apiVersion":"hobot.extensions/v1","productVersion":"0.26.0","entries":[{"id":"hobot.escape","name":"Escape","version":"1","kind":"extension","description":"invalid","origin":"built-in","scope":"system","runtime":"pi-extension","entrypoint":"../../outside.ts","trust":"product","defaultEnabled":true,"required":false,"provides":[],"requires":[],"permissions":[],"targets":[]}]}`
	path := filepath.Join(root, "extensions", "catalog.json")
	if err := os.WriteFile(path, []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExtensionCatalog(path, "dev"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping entrypoint was accepted: %v", err)
	}
}

func TestExtensionCatalogRejectsEntrypointsThroughSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	extensionsRoot := filepath.Join(root, "extensions")
	if err := os.Mkdir(extensionsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.ts")
	if err := os.WriteFile(outside, []byte("export {};"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(extensionsRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	catalog := `{"schemaVersion":1,"apiVersion":"hobot.extensions/v1","productVersion":"0.26.0","entries":[{"id":"hobot.escape","name":"Escape","version":"1","kind":"extension","description":"invalid","origin":"built-in","scope":"system","runtime":"pi-extension","entrypoint":"linked/outside.ts","trust":"product","defaultEnabled":true,"required":false,"provides":[],"requires":[],"permissions":[],"targets":[]}]}`
	path := filepath.Join(extensionsRoot, "catalog.json")
	if err := os.WriteFile(path, []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExtensionCatalog(path, "dev"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlinked directory escape was accepted: %v", err)
	}
}

func TestConfiguredExtensionInventoryIsPrivateBoundedAndNonSecret(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "agent")
	if err := os.Mkdir(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOBOT_CODING_AGENT_DIR", configRoot)
	t.Setenv("HOBOT_CODE_HOOK_CONFIG", filepath.Join(configRoot, "hooks.json"))
	t.Setenv("HOBOT_CODE_LSP_CONFIG", filepath.Join(configRoot, "lsp.json"))
	paths := configuredExtensionPaths()
	writePrivateJSON(t, paths.models, `{"providers":{"acme":{"baseUrl":"https://secret.example/token","apiKey":"sk-do-not-return","models":[{"id":"one"}]}}}`)
	writePrivateJSON(t, paths.hooks, `{"schemaVersion":1,"enabled":true,"hooks":[{"name":"guard","event":"PreToolUse","tool":"bash","command":["/secret/guard","--token","private"]}]}`)
	writePrivateJSON(t, paths.lsp, `{"schemaVersion":1,"enabled":true,"servers":[{"id":"missing-lsp","languageId":"demo","extensions":[".demo"],"command":["hobot-code-missing-lsp","--secret"]}]}`)

	builtIn, err := loadExtensionCatalog(sourceExtensionCatalog(t), "dev")
	if err != nil {
		t.Fatal(err)
	}
	catalog := discoverConfiguredExtensions(builtIn)
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"requires":[]`) || !strings.Contains(text, `"targets":[]`) {
		t.Fatalf("dynamic extension fields must remain JSON arrays: %s", text)
	}
	for _, secret := range []string{"secret.example", "sk-do-not-return", "/secret/guard", "--token", "private", "--secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("extension inventory leaked private configuration %q: %s", secret, text)
		}
	}
	wantStatus := map[string]string{"user.provider.acme": "configured", "user.hook.guard": "configured", "user.lsp.missing-lsp": "missing"}
	for _, entry := range catalog.Entries {
		if expected, ok := wantStatus[entry.ID]; ok {
			if entry.Status != expected {
				t.Fatalf("entry %s status = %q, want %q", entry.ID, entry.Status, expected)
			}
			delete(wantStatus, entry.ID)
		}
	}
	if len(wantStatus) != 0 {
		t.Fatalf("configured entries missing: %+v", wantStatus)
	}
}

func TestConfiguredExtensionInventoryFailsClosedPerSource(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "agent")
	if err := os.Mkdir(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOBOT_CODING_AGENT_DIR", configRoot)
	t.Setenv("HOBOT_CODE_HOOK_CONFIG", filepath.Join(configRoot, "hooks.json"))
	t.Setenv("HOBOT_CODE_LSP_CONFIG", filepath.Join(configRoot, "lsp.json"))
	paths := configuredExtensionPaths()
	writePrivateJSON(t, paths.models, `{"providers":{}}`)
	if err := os.Chmod(paths.models, 0o644); err != nil {
		t.Fatal(err)
	}
	writePrivateJSON(t, paths.hooks, `{"schemaVersion":1,"enabled":true,"hooks":"invalid"}`)
	linkTarget := filepath.Join(configRoot, "lsp-target.json")
	writePrivateJSON(t, linkTarget, `{"schemaVersion":1,"enabled":false,"servers":[]}`)
	if err := os.Symlink(linkTarget, paths.lsp); err != nil {
		t.Fatal(err)
	}

	builtIn, err := loadExtensionCatalog(sourceExtensionCatalog(t), "dev")
	if err != nil {
		t.Fatal(err)
	}
	catalog := discoverConfiguredExtensions(builtIn)
	statuses := make(map[string]string)
	for _, diagnostic := range catalog.Diagnostics {
		statuses[diagnostic.Source] = diagnostic.Status
		if strings.Contains(diagnostic.Message, configRoot) {
			t.Fatalf("diagnostic leaked config path: %+v", diagnostic)
		}
	}
	if statuses["providers"] != "unsafe" || statuses["hooks"] != "invalid" || statuses["lsp"] != "unsafe" {
		t.Fatalf("unexpected source diagnostics: %+v", statuses)
	}
	if len(catalog.Entries) != len(builtIn.Entries) {
		t.Fatalf("unsafe sources changed built-in catalog: got %d entries, want %d", len(catalog.Entries), len(builtIn.Entries))
	}
}

func TestConfiguredExtensionInventoryRefreshesWithoutDaemonRestart(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "agent")
	if err := os.Mkdir(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOBOT_CODING_AGENT_DIR", configRoot)
	paths := configuredExtensionPaths()
	writePrivateJSON(t, paths.models, `{"providers":{"first":{"models":[]}}}`)
	builtIn, err := loadExtensionCatalog(sourceExtensionCatalog(t), "dev")
	if err != nil {
		t.Fatal(err)
	}
	first := discoverConfiguredExtensions(builtIn)
	writePrivateJSON(t, paths.models, `{"providers":{"second":{"models":[]}}}`)
	second := discoverConfiguredExtensions(builtIn)
	if hasExtensionEntry(first, "user.provider.second") || !hasExtensionEntry(second, "user.provider.second") || hasExtensionEntry(second, "user.provider.first") {
		t.Fatalf("configured provider inventory was not refreshed: first=%+v second=%+v", first.Entries, second.Entries)
	}
	if second.CapturedAt == "" {
		t.Fatal("extension inventory did not record its capture time")
	}
}

func TestConfiguredExtensionInventoryReportsOneStatusWhenTruncated(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "agent")
	if err := os.Mkdir(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOBOT_CODING_AGENT_DIR", configRoot)
	paths := configuredExtensionPaths()
	writePrivateJSON(t, paths.models, `{"providers":{"extra":{"models":[]}}}`)
	builtIn := extensionCatalog{Entries: make([]extensionEntry, maximumCatalogEntries)}
	catalog := discoverConfiguredExtensions(builtIn)
	providerDiagnostics := 0
	for _, diagnostic := range catalog.Diagnostics {
		if diagnostic.Source == "providers" {
			providerDiagnostics++
			if diagnostic.Status != "truncated" {
				t.Fatalf("provider status = %q, want truncated", diagnostic.Status)
			}
		}
	}
	if providerDiagnostics != 1 {
		t.Fatalf("provider diagnostics = %d, want 1: %+v", providerDiagnostics, catalog.Diagnostics)
	}
}

func hasExtensionEntry(catalog extensionCatalog, id string) bool {
	for _, entry := range catalog.Entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func writePrivateJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
