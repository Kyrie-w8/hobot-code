package hobot

import (
	"context"
	"strings"
	"testing"
)

func validSDKExtensionCatalog() ExtensionCatalog {
	return ExtensionCatalog{
		SchemaVersion: 1, APIVersion: "hobot.extensions/v1", ProductVersion: "0.26.0", HostVersion: "0.26.0", CapturedAt: "2026-08-14T00:00:00Z",
		Entries: []ExtensionEntry{{
			ID: "pi.project.package.demo", Name: "demo", Version: "configured", Kind: "package", ResourceType: "package", Description: "Declared package.",
			Origin: "package", Scope: "project", Runtime: "pi-package", Entrypoint: "project/settings.json#packages-1", Trust: "third-party", DefaultEnabled: true,
			Provides: []string{"pi.package"}, Requires: []string{}, Permissions: []string{"current-user"}, Targets: []string{}, Status: "declared",
		}},
		Diagnostics: []ExtensionDiagnostic{{Source: "project-resources", Status: "untrusted", Message: "Project resources were not inspected"}},
		Policy:      ExtensionPolicy{InventoryOnly: true, ExecutionAuthority: "pi-runtime", PermissionAuthority: "board", ThirdPartyRuntime: "current-user"},
	}
}

func TestExtensionCatalogResultValidationAcceptsAdditivePiResources(t *testing.T) {
	if err := validateExtensionCatalogResult(validSDKExtensionCatalog()); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionCatalogResultValidationRejectsUntrustedResponses(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ExtensionCatalog)
		want   string
	}{
		{name: "duplicate", mutate: func(catalog *ExtensionCatalog) { catalog.Entries = append(catalog.Entries, catalog.Entries[0]) }, want: "duplicate"},
		{name: "absolute-entrypoint", mutate: func(catalog *ExtensionCatalog) { catalog.Entries[0].Entrypoint = "/private/secret" }, want: "entrypoint"},
		{name: "unknown-status", mutate: func(catalog *ExtensionCatalog) { catalog.Entries[0].Status = "executing" }, want: "status"},
		{name: "unsafe-policy", mutate: func(catalog *ExtensionCatalog) { catalog.Policy.HotReload = true }, want: "policy"},
		{name: "control-character", mutate: func(catalog *ExtensionCatalog) { catalog.Entries[0].Name = "bad\nname" }, want: "identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog := validSDKExtensionCatalog()
			test.mutate(&catalog)
			if err := validateExtensionCatalogResult(catalog); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid response accepted: %v", err)
			}
		})
	}
}

func TestExtensionsRejectsInvalidTaskContextBeforeTransport(t *testing.T) {
	client := &Client{}
	if _, err := client.Extensions(context.Background(), "../../private"); err == nil || !strings.Contains(err.Error(), "task ID") {
		t.Fatalf("invalid task context was accepted: %v", err)
	}
}
