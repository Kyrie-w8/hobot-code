package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	extensionCatalogSchema = 1
	extensionCatalogAPI    = "hobot.extensions/v1"
	maximumCatalogBytes    = 256 * 1024
	maximumCatalogEntries  = 128
)

type extensionCatalog struct {
	SchemaVersion  int              `json:"schemaVersion"`
	APIVersion     string           `json:"apiVersion"`
	ProductVersion string           `json:"productVersion"`
	HostVersion    string           `json:"hostVersion"`
	Entries        []extensionEntry `json:"entries"`
	Policy         extensionPolicy  `json:"policy"`
}

type extensionEntry struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Kind           string   `json:"kind"`
	Description    string   `json:"description"`
	Origin         string   `json:"origin"`
	Scope          string   `json:"scope"`
	Runtime        string   `json:"runtime"`
	Entrypoint     string   `json:"entrypoint"`
	Trust          string   `json:"trust"`
	DefaultEnabled bool     `json:"defaultEnabled"`
	Required       bool     `json:"required"`
	Provides       []string `json:"provides"`
	Requires       []string `json:"requires"`
	Permissions    []string `json:"permissions"`
	Targets        []string `json:"targets"`
}

type extensionPolicy struct {
	InventoryOnly       bool   `json:"inventoryOnly"`
	ExecutionAuthority  string `json:"executionAuthority"`
	PermissionAuthority string `json:"permissionAuthority"`
	ThirdPartyRuntime   string `json:"thirdPartyRuntime"`
	HotReload           bool   `json:"hotReload"`
}

func loadExtensionCatalog(path, hostVersion string) (extensionCatalog, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return extensionCatalog{}, fmt.Errorf("read extension catalog: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return extensionCatalog{}, fmt.Errorf("extension catalog must be a regular file and not a symbolic link")
	}
	if info.Size() <= 0 || info.Size() > maximumCatalogBytes {
		return extensionCatalog{}, fmt.Errorf("extension catalog exceeds the %d-byte limit", maximumCatalogBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return extensionCatalog{}, fmt.Errorf("read extension catalog: %w", err)
	}
	var catalog extensionCatalog
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return extensionCatalog{}, fmt.Errorf("parse extension catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return extensionCatalog{}, fmt.Errorf("extension catalog must contain one JSON object")
	}
	if err := validateExtensionCatalog(catalog, hostVersion); err != nil {
		return extensionCatalog{}, err
	}
	resolvedCatalog, err := filepath.EvalSymlinks(path)
	if err != nil {
		return extensionCatalog{}, fmt.Errorf("resolve extension catalog: %w", err)
	}
	productRoot := filepath.Dir(filepath.Dir(resolvedCatalog))
	for _, entry := range catalog.Entries {
		entrypoint := filepath.Clean(filepath.Join(filepath.Dir(resolvedCatalog), entry.Entrypoint))
		if !pathIsWithinProductRoot(productRoot, entrypoint) {
			return extensionCatalog{}, fmt.Errorf("extension %s entrypoint escapes the product root", entry.ID)
		}
		entryInfo, err := os.Lstat(entrypoint)
		if err != nil {
			return extensionCatalog{}, fmt.Errorf("extension %s entrypoint is unavailable: %w", entry.ID, err)
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			return extensionCatalog{}, fmt.Errorf("extension %s entrypoint must be a regular file and not a symbolic link", entry.ID)
		}
		resolvedEntrypoint, err := filepath.EvalSymlinks(entrypoint)
		if err != nil || !pathIsWithinProductRoot(productRoot, resolvedEntrypoint) {
			return extensionCatalog{}, fmt.Errorf("extension %s entrypoint resolves outside the product root", entry.ID)
		}
	}
	catalog.HostVersion = hostVersion
	catalog.Policy = extensionPolicy{
		InventoryOnly:       true,
		ExecutionAuthority:  "pi-runtime",
		PermissionAuthority: "board",
		ThirdPartyRuntime:   "current-user",
		HotReload:           false,
	}
	sort.Slice(catalog.Entries, func(left, right int) bool { return catalog.Entries[left].ID < catalog.Entries[right].ID })
	return catalog, nil
}

func pathIsWithinProductRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateExtensionCatalog(catalog extensionCatalog, hostVersion string) error {
	if catalog.SchemaVersion != extensionCatalogSchema || catalog.APIVersion != extensionCatalogAPI {
		return fmt.Errorf("unsupported extension catalog schema %d or API %q", catalog.SchemaVersion, catalog.APIVersion)
	}
	if catalog.ProductVersion == "" || (hostVersion != "dev" && hostVersion != catalog.ProductVersion) {
		return fmt.Errorf("extension catalog product version %q does not match host %q", catalog.ProductVersion, hostVersion)
	}
	if len(catalog.Entries) == 0 || len(catalog.Entries) > maximumCatalogEntries {
		return fmt.Errorf("extension catalog must contain between 1 and %d entries", maximumCatalogEntries)
	}
	seen := make(map[string]struct{}, len(catalog.Entries))
	for index := range catalog.Entries {
		entry := &catalog.Entries[index]
		if !validExtensionIdentifier(entry.ID) || entry.Name == "" || entry.Version == "" || entry.Description == "" {
			return fmt.Errorf("extension catalog entry %d has incomplete or invalid identity", index+1)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return fmt.Errorf("extension catalog contains duplicate id %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if entry.Kind != "extension" && entry.Kind != "skill" && entry.Kind != "provider" && entry.Kind != "integration" {
			return fmt.Errorf("extension %s has unsupported kind %q", entry.ID, entry.Kind)
		}
		if entry.Origin != "built-in" || entry.Scope != "system" || entry.Trust != "product" {
			return fmt.Errorf("extension %s has an unsupported built-in trust declaration", entry.ID)
		}
		if entry.Runtime != "pi-extension" && entry.Runtime != "pi-skill" {
			return fmt.Errorf("extension %s has unsupported runtime %q", entry.ID, entry.Runtime)
		}
		if entry.Entrypoint == "" || filepath.IsAbs(entry.Entrypoint) || strings.Contains(entry.Entrypoint, "\\") {
			return fmt.Errorf("extension %s has an invalid entrypoint", entry.ID)
		}
		for _, values := range [][]string{entry.Provides, entry.Requires, entry.Permissions, entry.Targets} {
			if !validExtensionValues(values) {
				return fmt.Errorf("extension %s has an invalid or duplicate declaration", entry.ID)
			}
		}
	}
	return nil
}

func validExtensionIdentifier(value string) bool {
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

func validExtensionValues(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validExtensionIdentifier(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
