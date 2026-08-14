package hobot

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	maximumExtensionEntries     = 128
	maximumExtensionDiagnostics = 32
)

var sdkTaskIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

func validateExtensionCatalogResult(catalog ExtensionCatalog) error {
	if catalog.SchemaVersion != 1 || catalog.APIVersion != "hobot.extensions/v1" {
		return fmt.Errorf("unsupported schema or API")
	}
	if !safeExtensionText(catalog.ProductVersion, 64) || !safeExtensionText(catalog.HostVersion, 64) {
		return fmt.Errorf("invalid product identity")
	}
	if catalog.CapturedAt != "" {
		if _, err := time.Parse(time.RFC3339, catalog.CapturedAt); err != nil {
			return fmt.Errorf("invalid capture time")
		}
	}
	if len(catalog.Entries) < 1 || len(catalog.Entries) > maximumExtensionEntries {
		return fmt.Errorf("entry count is outside the supported range")
	}
	if len(catalog.Diagnostics) > maximumExtensionDiagnostics {
		return fmt.Errorf("too many diagnostics")
	}
	if !catalog.Policy.InventoryOnly || catalog.Policy.PermissionAuthority != "board" || catalog.Policy.ExecutionAuthority != "pi-runtime" || catalog.Policy.ThirdPartyRuntime != "current-user" || catalog.Policy.HotReload {
		return fmt.Errorf("unsafe or unsupported policy")
	}
	seen := make(map[string]bool, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		if err := validateExtensionEntryResult(entry); err != nil {
			return fmt.Errorf("entry %q: %w", entry.ID, err)
		}
		if seen[entry.ID] {
			return fmt.Errorf("duplicate entry %q", entry.ID)
		}
		seen[entry.ID] = true
	}
	diagnosticSources := make(map[string]bool, len(catalog.Diagnostics))
	for _, diagnostic := range catalog.Diagnostics {
		if !safeExtensionText(diagnostic.Source, 128) || !safeExtensionText(diagnostic.Message, 512) || !extensionOneOf(diagnostic.Status, "ok", "missing", "invalid", "unsafe", "unreadable", "partial", "truncated", "contextual", "untrusted") {
			return fmt.Errorf("invalid source diagnostic")
		}
		if diagnosticSources[diagnostic.Source] {
			return fmt.Errorf("duplicate source diagnostic")
		}
		diagnosticSources[diagnostic.Source] = true
	}
	return nil
}

func validateExtensionEntryResult(entry ExtensionEntry) error {
	if !safeExtensionIdentifier(entry.ID) || !safeExtensionText(entry.Name, 120) || !safeExtensionText(entry.Version, 64) || !safeExtensionText(entry.Description, 512) {
		return fmt.Errorf("invalid identity")
	}
	if !extensionOneOf(entry.Kind, "extension", "skill", "provider", "integration", "package", "prompt", "theme") {
		return fmt.Errorf("unsupported kind")
	}
	if entry.ResourceType != "" && !extensionOneOf(entry.ResourceType, "extension", "skill", "package", "prompt", "theme") {
		return fmt.Errorf("unsupported resource type")
	}
	if !extensionOneOf(entry.Origin, "built-in", "user", "project", "package") || !extensionOneOf(entry.Scope, "system", "user", "project") || !extensionOneOf(entry.Trust, "product", "user", "project", "third-party") {
		return fmt.Errorf("unsupported provenance")
	}
	if !extensionOneOf(entry.Runtime, "pi-extension", "pi-skill", "pi-provider", "hobot-provider", "hobot-hook", "lsp-process", "pi-package", "pi-prompt", "pi-theme") {
		return fmt.Errorf("unsupported runtime")
	}
	if !safeExtensionText(entry.Entrypoint, 1024) || filepath.IsAbs(entry.Entrypoint) || strings.Contains(entry.Entrypoint, "\\") {
		return fmt.Errorf("invalid entrypoint")
	}
	if entry.Status != "" && !extensionOneOf(entry.Status, "included", "configured", "available", "missing", "disabled", "declared", "discovered") {
		return fmt.Errorf("unsupported status")
	}
	if entry.StatusDetail != "" && !safeExtensionText(entry.StatusDetail, 512) {
		return fmt.Errorf("invalid status detail")
	}
	for _, values := range [][]string{entry.Provides, entry.Requires, entry.Permissions, entry.Targets} {
		if values == nil || len(values) > 64 {
			return fmt.Errorf("invalid declaration list")
		}
		seen := make(map[string]bool, len(values))
		for _, value := range values {
			if !safeExtensionIdentifier(value) || seen[value] {
				return fmt.Errorf("invalid declaration value")
			}
			seen[value] = true
		}
	}
	return nil
}

func safeExtensionIdentifier(value string) bool {
	if value == "" || len(value) > 128 || value[0] == '.' || value[len(value)-1] == '.' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func safeExtensionText(value string, maximum int) bool {
	if strings.TrimSpace(value) == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func extensionOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
