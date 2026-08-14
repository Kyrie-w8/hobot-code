package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const (
	maximumInventoryConfigBytes = 512 * 1024
	maximumInventoryEntries     = 64
)

type extensionInventoryContext struct {
	Cwd            string
	ProjectTrusted bool
}

func discoverConfiguredExtensions(catalog extensionCatalog, contexts ...extensionInventoryContext) extensionCatalog {
	catalog.CapturedAt = time.Now().UTC().Format(time.RFC3339)
	entries := append([]extensionEntry(nil), catalog.Entries...)
	diagnostics := make([]extensionDiagnostic, 0, 4)
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		seen[entry.ID] = struct{}{}
	}

	paths := configuredExtensionPaths()
	sources := []struct {
		name string
		path string
		load func(map[string]any) ([]extensionEntry, bool)
	}{
		{name: "providers", path: paths.models, load: configuredProviders},
		{name: "managed-providers", path: paths.managedProviders, load: configuredManagedProviders},
		{name: "hooks", path: paths.hooks, load: configuredHooks},
		{name: "lsp", path: paths.lsp, load: configuredLSP},
	}
	for _, source := range sources {
		document, status := readPrivateInventoryConfig(source.path)
		if status != "ok" {
			diagnostics = append(diagnostics, extensionDiagnostic{Source: source.name, Status: status, Message: inventoryDiagnosticMessage(source.name, status)})
			continue
		}
		discovered, valid := source.load(document)
		if !valid {
			diagnostics = append(diagnostics, extensionDiagnostic{Source: source.name, Status: "invalid", Message: inventoryDiagnosticMessage(source.name, "invalid")})
			continue
		}
		sourceStatus := "ok"
		for _, entry := range discovered {
			if len(entries) >= maximumCatalogEntries {
				sourceStatus = "truncated"
				break
			}
			entry.ID = uniqueInventoryID(entry.ID, seen)
			seen[entry.ID] = struct{}{}
			entries = append(entries, entry)
		}
		diagnostics = append(diagnostics, extensionDiagnostic{Source: source.name, Status: sourceStatus, Message: inventoryDiagnosticMessage(source.name, sourceStatus)})
	}
	var inventoryContext *extensionInventoryContext
	if len(contexts) > 0 {
		inventoryContext = &contexts[0]
	}
	for _, source := range discoverPiResourceSources(paths, inventoryContext) {
		sourceStatus := source.status
		for _, entry := range source.entries {
			if len(entries) >= maximumCatalogEntries {
				sourceStatus = "truncated"
				break
			}
			entry.ID = uniqueInventoryID(entry.ID, seen)
			seen[entry.ID] = struct{}{}
			entries = append(entries, entry)
		}
		diagnostics = append(diagnostics, extensionDiagnostic{Source: source.name, Status: sourceStatus, Message: source.message})
	}

	sort.Slice(entries, func(left, right int) bool { return entries[left].ID < entries[right].ID })
	catalog.Entries = entries
	catalog.Diagnostics = diagnostics
	return catalog
}

type configuredExtensionPathSet struct {
	agentDir         string
	models           string
	managedProviders string
	hooks            string
	lsp              string
}

func configuredExtensionPaths() configuredExtensionPathSet {
	agentDir := strings.TrimSpace(os.Getenv("HOBOT_CODING_AGENT_DIR"))
	if agentDir == "" {
		configRoot := strings.TrimSpace(os.Getenv("HOBOT_CODE_CONFIG_DIR"))
		if configRoot == "" {
			configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
			if configHome == "" {
				configHome = filepath.Join(strings.TrimSpace(os.Getenv("HOME")), ".config")
			}
			configRoot = filepath.Join(configHome, "hobot-code")
		}
		agentDir = filepath.Join(configRoot, "agent")
	}
	hooks := strings.TrimSpace(os.Getenv("HOBOT_CODE_HOOK_CONFIG"))
	if hooks == "" {
		hooks = filepath.Join(agentDir, "hooks.json")
	}
	lsp := strings.TrimSpace(os.Getenv("HOBOT_CODE_LSP_CONFIG"))
	if lsp == "" {
		lsp = filepath.Join(agentDir, "lsp.json")
	}
	managedProviders := strings.TrimSpace(os.Getenv("HOBOT_CODE_MANAGED_PROVIDER_CONFIG"))
	if managedProviders == "" {
		managedProviders = filepath.Join(agentDir, "providers.json")
	}
	return configuredExtensionPathSet{agentDir: agentDir, models: filepath.Join(agentDir, "models.json"), managedProviders: managedProviders, hooks: hooks, lsp: lsp}
}

func readPrivateInventoryConfig(path string) (map[string]any, string) {
	if path == "" {
		return nil, "missing"
	}
	if !filepath.IsAbs(path) {
		return nil, "unsafe"
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, "missing"
	}
	if err != nil {
		return nil, "unreadable"
	}
	if !privateInventoryFileInfo(info) {
		return nil, "unsafe"
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "unreadable"
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, "unreadable"
	}
	if !os.SameFile(info, openedInfo) || !privateInventoryFileInfo(openedInfo) {
		return nil, "unsafe"
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumInventoryConfigBytes+1))
	if err != nil {
		return nil, "unreadable"
	}
	if len(raw) == 0 || len(raw) > maximumInventoryConfigBytes {
		return nil, "unsafe"
	}
	var document map[string]any
	if rejectDuplicateJSONKeys(string(raw)) != nil || json.Unmarshal(raw, &document) != nil || document == nil {
		return nil, "invalid"
	}
	return document, "ok"
}

func configuredManagedProviders(document map[string]any) ([]extensionEntry, bool) {
	providers, err := validateManagedProviderDocument(document)
	if err != nil {
		return nil, false
	}
	entries := make([]extensionEntry, 0, len(providers))
	for _, provider := range providers {
		id := provider["id"].(string)
		models := provider["models"].([]any)
		entries = append(entries, extensionEntry{
			ID: "managed.provider." + inventorySlug(id), Name: strings.TrimSpace(id), Version: "configured", Kind: "provider",
			Description: inventoryCountDescription("Hobot-managed credential provider", len(models), "model"), Origin: "user", Scope: "user", Runtime: "hobot-provider",
			Entrypoint: "providers.json#" + inventorySlug(id), Trust: "user", DefaultEnabled: true, Provides: []string{"provider." + inventorySlug(id)}, Requires: []string{"hobot.rdk-core"}, Permissions: []string{"model-network"}, Targets: []string{}, Status: "configured",
		})
	}
	return entries, true
}

func validateManagedProviderDocument(document map[string]any) ([]map[string]any, error) {
	schema, schemaOK := integerValue(document["schemaVersion"])
	rawProviders, providersOK := document["providers"].([]any)
	if !schemaOK || schema != 1 || !providersOK || len(document) != 2 || len(rawProviders) > maximumManagedProviders {
		return nil, fmt.Errorf("invalid managed provider schema")
	}
	providers := make([]map[string]any, 0, len(rawProviders))
	seen := make(map[string]bool, len(rawProviders))
	for _, raw := range rawProviders {
		provider, ok := raw.(map[string]any)
		if !ok || !hasOnlyFields(provider, managedProviderFields) {
			return nil, fmt.Errorf("invalid managed provider entry")
		}
		id, idOK := provider["id"].(string)
		baseURL, baseURLOK := provider["baseUrl"].(string)
		api, apiOK := provider["api"].(string)
		credential, credentialOK := provider["credentialEnv"].(string)
		models, modelsOK := provider["models"].([]any)
		if !idOK || !managedProviderIDPattern.MatchString(id) || id == "drobotics" || seen[id] || !optionalSafeLabel(provider["name"], 120) || !baseURLOK || !safeManagedProviderURL(baseURL) || !apiOK || !managedProviderAPIs[api] || !credentialOK || !validProviderCredentialEnvironment(credential) || !optionalBoolean(provider["authHeader"]) || !modelsOK || len(models) < 1 || len(models) > maximumManagedModels {
			return nil, fmt.Errorf("invalid managed provider")
		}
		seen[id] = true
		modelIDs := make(map[string]bool, len(models))
		for _, rawModel := range models {
			model, ok := rawModel.(map[string]any)
			if !ok || !validManagedModel(model, modelIDs) {
				return nil, fmt.Errorf("invalid managed model")
			}
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func privateInventoryFileInfo(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumInventoryConfigBytes || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || int(stat.Uid) == os.Getuid()
}

func configuredProviders(document map[string]any) ([]extensionEntry, bool) {
	providers, ok := document["providers"].(map[string]any)
	if !ok || len(providers) > maximumInventoryEntries {
		return nil, false
	}
	entries := make([]extensionEntry, 0, len(providers))
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := providers[name]
		provider, ok := raw.(map[string]any)
		if !ok || !safeInventoryLabel(name, 80) {
			return nil, false
		}
		models, ok := provider["models"].([]any)
		if provider["models"] != nil && (!ok || len(models) > maximumInventoryEntries) {
			return nil, false
		}
		for _, rawModel := range models {
			if _, ok := rawModel.(map[string]any); !ok {
				return nil, false
			}
		}
		entries = append(entries, extensionEntry{
			ID: "user.provider." + inventorySlug(name), Name: strings.TrimSpace(name), Version: "configured", Kind: "provider",
			Description: inventoryCountDescription("User-configured model provider", len(models), "model"), Origin: "user", Scope: "user", Runtime: "pi-provider",
			Entrypoint: "models.json#providers", Trust: "user", DefaultEnabled: true, Provides: []string{"provider." + inventorySlug(name)}, Requires: []string{}, Permissions: []string{"model-network"}, Targets: []string{}, Status: "configured",
		})
	}
	return entries, true
}

func configuredHooks(document map[string]any) ([]extensionEntry, bool) {
	schema, schemaOK := integerValue(document["schemaVersion"])
	enabled, enabledOK := document["enabled"].(bool)
	hooks, hooksOK := document["hooks"].([]any)
	if !schemaOK || schema != 1 || !enabledOK || !hooksOK || len(hooks) > maximumInventoryEntries {
		return nil, false
	}
	entries := make([]extensionEntry, 0, len(hooks))
	for index, raw := range hooks {
		hook, ok := raw.(map[string]any)
		name, nameOK := hook["name"].(string)
		event, eventOK := hook["event"].(string)
		tool, toolOK := hook["tool"].(string)
		command, commandOK := hook["command"].([]any)
		if !ok || !nameOK || !eventOK || !toolOK || !commandOK || !safeInventoryStringArray(command, 1, 32, 1000) || !safeInventoryLabel(name, 80) || !safeInventoryLabel(event, 32) || !safeInventoryLabel(tool, 128) {
			return nil, false
		}
		status := "configured"
		if !enabled {
			status = "disabled"
		}
		entries = append(entries, extensionEntry{
			ID: "user.hook." + inventorySlug(name), Name: strings.TrimSpace(name), Version: "configured", Kind: "integration",
			Description: event + " policy hook for " + tool + ".", Origin: "user", Scope: "user", Runtime: "hobot-hook",
			Entrypoint: "hooks.json#" + inventorySlug(name), Trust: "user", DefaultEnabled: enabled, Provides: []string{"hook." + inventorySlug(strings.ToLower(event))}, Requires: []string{}, Permissions: []string{"subprocess"}, Targets: []string{}, Status: status, StatusDetail: inventoryIndexDetail(index),
		})
	}
	return entries, true
}

func configuredLSP(document map[string]any) ([]extensionEntry, bool) {
	schema, schemaOK := integerValue(document["schemaVersion"])
	enabled, enabledOK := document["enabled"].(bool)
	servers, serversOK := document["servers"].([]any)
	if !schemaOK || schema != 1 || !enabledOK || !serversOK || len(servers) > maximumInventoryEntries {
		return nil, false
	}
	entries := make([]extensionEntry, 0, len(servers))
	for _, raw := range servers {
		server, ok := raw.(map[string]any)
		id, idOK := server["id"].(string)
		language, languageOK := server["languageId"].(string)
		extensions, extensionsOK := server["extensions"].([]any)
		command, commandOK := server["command"].([]any)
		if !ok || !idOK || !languageOK || !extensionsOK || !commandOK || !safeInventoryStringArray(extensions, 1, 32, 32) || !safeInventoryStringArray(command, 1, 32, 1000) || !safeInventoryLabel(id, 80) || !safeInventoryLabel(language, 80) {
			return nil, false
		}
		executable, executableOK := command[0].(string)
		if !executableOK || !safeInventoryLabel(executable, 1000) {
			return nil, false
		}
		status := "disabled"
		if enabled {
			if inventoryExecutableAvailable(executable) {
				status = "available"
			} else {
				status = "missing"
			}
		}
		entries = append(entries, extensionEntry{
			ID: "user.lsp." + inventorySlug(id), Name: strings.TrimSpace(id), Version: "configured", Kind: "integration",
			Description: inventoryCountDescription("Language intelligence for "+language, len(extensions), "file type"), Origin: "user", Scope: "user", Runtime: "lsp-process",
			Entrypoint: "lsp.json#" + inventorySlug(id), Trust: "user", DefaultEnabled: enabled, Provides: []string{"lsp." + inventorySlug(language)}, Requires: []string{}, Permissions: []string{"subprocess", "workspace"}, Targets: []string{}, Status: status,
		})
	}
	return entries, true
}

func inventoryExecutableAvailable(command string) bool {
	if filepath.IsAbs(command) {
		info, err := os.Lstat(command)
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func safeInventoryLabel(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func safeInventoryStringArray(values []any, minimum, maximum, maximumLength int) bool {
	if len(values) < minimum || len(values) > maximum {
		return false
	}
	for _, raw := range values {
		value, ok := raw.(string)
		if !ok || !safeInventoryLabel(value, maximumLength) {
			return false
		}
	}
	return true
}

func inventorySlug(value string) string {
	var result strings.Builder
	previousDash := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			result.WriteRune(character)
			previousDash = false
		} else if !previousDash && result.Len() > 0 {
			result.WriteByte('-')
			previousDash = true
		}
		if result.Len() >= 64 {
			break
		}
	}
	slug := strings.Trim(result.String(), "-")
	if slug != "" {
		return slug
	}
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("item-%x", digest[:6])
}

func uniqueInventoryID(candidate string, seen map[string]struct{}) string {
	if _, exists := seen[candidate]; !exists {
		return candidate
	}
	for suffix := 2; suffix <= maximumInventoryEntries+1; suffix++ {
		value := candidate + "-" + strconv.Itoa(suffix)
		if _, exists := seen[value]; !exists {
			return value
		}
	}
	digest := sha256.Sum256([]byte(candidate))
	return fmt.Sprintf("%s-%x", candidate, digest[:4])
}

func integerValue(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case float64:
		return int(number), number == float64(int(number))
	default:
		return 0, false
	}
}

func inventoryCountDescription(prefix string, count int, singular string) string {
	if count == 0 {
		return prefix + "."
	}
	suffix := singular
	if count != 1 {
		suffix += "s"
	}
	return prefix + " with " + strconv.Itoa(count) + " " + suffix + "."
}

func inventoryIndexDetail(index int) string {
	return "Configuration entry " + strconv.Itoa(index+1)
}

func inventoryDiagnosticMessage(source, status string) string {
	labels := map[string]string{"providers": "Pi provider", "managed-providers": "Managed provider", "hooks": "Hook", "lsp": "LSP"}
	label := labels[source]
	switch status {
	case "ok":
		return label + " configuration inspected"
	case "missing":
		return label + " configuration is missing"
	case "unsafe":
		return label + " configuration failed private-file checks"
	case "unreadable":
		return label + " configuration could not be read"
	case "truncated":
		return label + " inventory reached its entry limit"
	default:
		return label + " configuration is invalid"
	}
}
