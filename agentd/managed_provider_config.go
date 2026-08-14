package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maximumManagedProviders = 64
	maximumManagedModels    = 128
)

var managedProviderIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var managedModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

var managedProviderFields = map[string]bool{
	"id": true, "name": true, "baseUrl": true, "api": true, "credentialEnv": true, "authHeader": true, "models": true,
}

var managedModelFields = map[string]bool{
	"id": true, "name": true, "reasoning": true, "input": true, "contextWindow": true, "maxTokens": true, "thinkingLevelMap": true, "compat": true,
}

var managedProviderAPIs = map[string]bool{
	"anthropic-messages": true, "openai-completions": true, "openai-responses": true, "google-generative-ai": true,
}

var managedThinkingLevels = map[string]bool{
	"off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true,
}

var managedCompatibilityFields = map[string]bool{
	"supportsDeveloperRole": true, "supportsReasoningEffort": true, "supportsStore": true, "supportsUsageInStreaming": true, "supportsStrictMode": true,
}

func managedProviderConfigPath(cfg config) string {
	if strings.TrimSpace(cfg.ManagedProviderConfig) != "" {
		return cfg.ManagedProviderConfig
	}
	if strings.TrimSpace(cfg.AgentDir) == "" {
		return ""
	}
	return filepath.Join(cfg.AgentDir, "providers.json")
}

func loadManagedProviderDefinitions(path string) ([]map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	document, status := readPrivateInventoryConfig(path)
	if status == "missing" {
		return nil, nil
	}
	if status != "ok" {
		return nil, fmt.Errorf("managed provider configuration failed %s validation", status)
	}
	return validateManagedProviderDocument(document)
}

func configuredManagedProviderIDs(path string) (map[string]bool, error) {
	providers, err := loadManagedProviderDefinitions(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(providers))
	for _, provider := range providers {
		result[provider["id"].(string)] = true
	}
	return result, nil
}

func selectedManagedProvider(path, id string) (map[string]any, error) {
	providers, err := loadManagedProviderDefinitions(path)
	if err != nil {
		return nil, err
	}
	for _, provider := range providers {
		if provider["id"] == id {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("managed provider is not configured")
}

func selectedModelCredentialPayload(cfg config, model modelOption) (string, error) {
	bundle, err := decodeGatewayCredentialBundle([]byte(gatewayCredentialPayload(cfg)))
	if err != nil {
		return "", err
	}
	if model.Provider == "drobotics" {
		return encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, DRobotics: bundle.DRobotics})
	}
	provider, err := selectedManagedProvider(managedProviderConfigPath(cfg), model.Provider)
	if err != nil {
		return "", err
	}
	credential := provider["credentialEnv"].(string)
	token := bundle.ProviderKeys[credential]
	if token == "" {
		return "", fmt.Errorf("managed provider credential is unavailable")
	}
	return encodeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, ProviderKeys: map[string]string{credential: token}})
}

func hasOnlyFields(value map[string]any, allowed map[string]bool) bool {
	for name := range value {
		if !allowed[name] {
			return false
		}
	}
	return true
}

func optionalSafeLabel(value any, maximum int) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && safeInventoryLabel(text, maximum)
}

func optionalBoolean(value any) bool {
	if value == nil {
		return true
	}
	_, ok := value.(bool)
	return ok
}

func safeManagedProviderURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func validManagedModel(model map[string]any, seen map[string]bool) bool {
	if !hasOnlyFields(model, managedModelFields) {
		return false
	}
	id, idOK := model["id"].(string)
	if !idOK || !managedModelIDPattern.MatchString(id) || seen[id] || !optionalSafeLabel(model["name"], 120) || !optionalBoolean(model["reasoning"]) {
		return false
	}
	seen[id] = true
	contextWindow, contextOK := optionalBoundedInteger(model["contextWindow"], 1024, 4_000_000, 128_000)
	maxTokens, maxOK := optionalBoundedInteger(model["maxTokens"], 128, 131_072, 16_384)
	if !contextOK || !maxOK || maxTokens > contextWindow || !validManagedInput(model["input"]) || !validManagedThinkingMap(model["thinkingLevelMap"]) || !validManagedCompatibility(model["compat"]) {
		return false
	}
	return true
}

func optionalBoundedInteger(value any, minimum, maximum, fallback int) (int, bool) {
	if value == nil {
		return fallback, true
	}
	parsed, ok := integerValue(value)
	return parsed, ok && parsed >= minimum && parsed <= maximum
}

func validManagedInput(value any) bool {
	if value == nil {
		return true
	}
	items, ok := value.([]any)
	if !ok || len(items) < 1 || len(items) > 2 || items[0] != "text" {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range items {
		item, ok := raw.(string)
		if !ok || (item != "text" && item != "image") || seen[item] {
			return false
		}
		seen[item] = true
	}
	return true
}

func validManagedThinkingMap(value any) bool {
	if value == nil {
		return true
	}
	mapping, ok := value.(map[string]any)
	if !ok || len(mapping) > len(managedThinkingLevels) {
		return false
	}
	for level, raw := range mapping {
		if !managedThinkingLevels[level] {
			return false
		}
		if raw == nil {
			continue
		}
		mapped, ok := raw.(string)
		if !ok || mapped == "" || len(mapped) > 32 || !safeInventoryLabel(mapped, 32) {
			return false
		}
	}
	return true
}

func validManagedCompatibility(value any) bool {
	if value == nil {
		return true
	}
	compatibility, ok := value.(map[string]any)
	if !ok || !hasOnlyFields(compatibility, managedCompatibilityFields) {
		return false
	}
	for _, raw := range compatibility {
		if _, ok := raw.(bool); !ok {
			return false
		}
	}
	return true
}

func sortedManagedProviderValues(providers map[string]any) []any {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]any, 0, len(names))
	for _, name := range names {
		values = append(values, providers[name])
	}
	return values
}
