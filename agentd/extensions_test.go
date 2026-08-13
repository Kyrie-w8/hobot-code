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
