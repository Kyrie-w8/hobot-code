package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	gatewayTokenEnvironment      = "ANTHROPIC_AUTH_TOKEN"
	providerTokenPrefix          = "HOBOT_CODE_PROVIDER_KEY_"
	gatewayTokenFDEnvironment    = "HOBOT_CODE_GATEWAY_TOKEN_FD"
	gatewayTokenFileEnvironment  = "HOBOT_CODE_GATEWAY_TOKEN_FILE"
	gatewayTokenFDPlaceholder    = "{hobot-code-gateway-fd}"
	maximumGatewayTokenBytes     = 8192
	maximumProviderCredentials   = 64
	maximumCredentialBundleBytes = 512 * 1024
)

var taskCoordinationEnvironment = map[string]bool{
	"HOBOT_CODE_AGENT_ROLE":              true,
	"HOBOT_CODE_BACKGROUND_TASK":         true,
	"HOBOT_CODE_BACKGROUND_TASK_ID":      true,
	"HOBOT_CODE_PARENT_TASK_ID":          true,
	"HOBOT_CODE_SIDE_AGENT":              true,
	"HOBOT_CODE_SIDE_COLLABORATION_FILE": true,
	"HOBOT_CODE_SIDE_PARENT_SESSION":     true,
	"HOBOT_CODE_SOURCE_TASK_ID":          true,
	"HOBOT_CODE_TASK_ID":                 true,
	"HOBOT_CODE_TASK_CONTROL_SOCKET":     true,
}

type gatewayCredentialBundle struct {
	SchemaVersion int               `json:"schemaVersion"`
	DRobotics     string            `json:"drobotics,omitempty"`
	ProviderKeys  map[string]string `json:"providerKeys,omitempty"`
}

func environmentWithoutGatewayCredential(source []string) []string {
	result := make([]string, 0, len(source))
	for _, value := range source {
		name, _, _ := strings.Cut(value, "=")
		if name == gatewayTokenEnvironment || name == gatewayTokenFDEnvironment || name == gatewayTokenFileEnvironment || name == modelEgressSocketEnv || name == modelEgressProvidersEnv || strings.HasPrefix(name, providerTokenPrefix) {
			continue
		}
		result = append(result, value)
	}
	return result
}

func safeChildEnvironment(source []string) []string {
	withoutCredentials := environmentWithoutGatewayCredential(source)
	result := make([]string, 0, len(withoutCredentials))
	for _, value := range withoutCredentials {
		name, _, _ := strings.Cut(value, "=")
		if !taskCoordinationEnvironment[name] {
			result = append(result, value)
		}
	}
	return result
}

func gatewayCredentialDirectory(cfg config) string {
	return filepath.Join(cfg.AgentdRoot, "credential")
}

func gatewayCredentialFile(cfg config) string {
	return filepath.Join(gatewayCredentialDirectory(cfg), "token")
}

func loadGatewayCredential() (string, error) {
	bundle, err := loadGatewayCredentials()
	return bundle.DRobotics, err
}

func loadGatewayCredentials() (gatewayCredentialBundle, error) {
	fdValue := strings.TrimSpace(os.Getenv(gatewayTokenFDEnvironment))
	ambient, err := ambientGatewayCredentials(os.Environ())
	if err != nil {
		return gatewayCredentialBundle{}, err
	}
	_ = os.Unsetenv(gatewayTokenFDEnvironment)
	clearAmbientGatewayCredentials()
	if fdValue != "" {
		fd, err := strconv.Atoi(fdValue)
		if err != nil || fd < 3 || fd > 1<<20 {
			return gatewayCredentialBundle{}, fmt.Errorf("%s must identify an inherited file descriptor", gatewayTokenFDEnvironment)
		}
		file := os.NewFile(uintptr(fd), "hobot-code-gateway-credential")
		if file == nil {
			return gatewayCredentialBundle{}, fmt.Errorf("gateway credential descriptor is unavailable")
		}
		defer file.Close()
		value, err := io.ReadAll(io.LimitReader(file, maximumCredentialBundleBytes+1))
		if err != nil {
			return gatewayCredentialBundle{}, fmt.Errorf("read gateway credential: %w", err)
		}
		if len(value) > maximumCredentialBundleBytes {
			return gatewayCredentialBundle{}, fmt.Errorf("gateway credential bundle exceeds %d bytes", maximumCredentialBundleBytes)
		}
		return decodeGatewayCredentialBundle(value)
	}
	return ambient, nil
}

func ambientGatewayCredentials(environment []string) (gatewayCredentialBundle, error) {
	bundle := gatewayCredentialBundle{SchemaVersion: 1, ProviderKeys: map[string]string{}}
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch {
		case name == gatewayTokenEnvironment:
			bundle.DRobotics = value
		case strings.HasPrefix(name, providerTokenPrefix):
			if !validProviderCredentialEnvironment(name) {
				return gatewayCredentialBundle{}, fmt.Errorf("invalid managed provider credential variable: %s", name)
			}
			if value != "" {
				bundle.ProviderKeys[name] = value
			}
		}
	}
	return normalizeGatewayCredentialBundle(bundle)
}

func validProviderCredentialEnvironment(name string) bool {
	suffix := strings.TrimPrefix(name, providerTokenPrefix)
	if suffix == "" || len(suffix) > 96 {
		return false
	}
	for _, current := range suffix {
		if (current < 'A' || current > 'Z') && (current < '0' || current > '9') && current != '_' {
			return false
		}
	}
	return true
}

func clearAmbientGatewayCredentials() {
	_ = os.Unsetenv(gatewayTokenEnvironment)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, providerTokenPrefix) {
			_ = os.Unsetenv(name)
		}
	}
}

func normalizeGatewayCredentialBundle(bundle gatewayCredentialBundle) (gatewayCredentialBundle, error) {
	bundle.SchemaVersion = 1
	bundle.DRobotics = strings.TrimSpace(bundle.DRobotics)
	if len(bundle.DRobotics) > maximumGatewayTokenBytes {
		return gatewayCredentialBundle{}, fmt.Errorf("D-Robotics gateway credential exceeds %d bytes", maximumGatewayTokenBytes)
	}
	if bundle.ProviderKeys == nil {
		bundle.ProviderKeys = map[string]string{}
	}
	if len(bundle.ProviderKeys) > maximumProviderCredentials {
		return gatewayCredentialBundle{}, fmt.Errorf("managed provider credentials exceed %d entries", maximumProviderCredentials)
	}
	for name, value := range bundle.ProviderKeys {
		value = strings.TrimSpace(value)
		if !validProviderCredentialEnvironment(name) || value == "" || len(value) > maximumGatewayTokenBytes {
			return gatewayCredentialBundle{}, fmt.Errorf("managed provider credential %s is invalid", name)
		}
		bundle.ProviderKeys[name] = value
	}
	return bundle, nil
}

func decodeGatewayCredentialBundle(value []byte) (gatewayCredentialBundle, error) {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" {
		return gatewayCredentialBundle{SchemaVersion: 1, ProviderKeys: map[string]string{}}, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return normalizeGatewayCredentialBundle(gatewayCredentialBundle{SchemaVersion: 1, DRobotics: trimmed, ProviderKeys: map[string]string{}})
	}
	if err := rejectDuplicateJSONKeys(trimmed); err != nil {
		return gatewayCredentialBundle{}, fmt.Errorf("invalid gateway credential bundle: %w", err)
	}
	var bundle gatewayCredentialBundle
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil || bundle.SchemaVersion != 1 {
		return gatewayCredentialBundle{}, fmt.Errorf("invalid gateway credential bundle")
	}
	return normalizeGatewayCredentialBundle(bundle)
}

func encodeGatewayCredentialBundle(bundle gatewayCredentialBundle) (string, error) {
	bundle, err := normalizeGatewayCredentialBundle(bundle)
	if err != nil {
		return "", err
	}
	if bundle.DRobotics == "" && len(bundle.ProviderKeys) == 0 {
		return "", nil
	}
	value, err := json.Marshal(bundle)
	if err != nil || len(value) > maximumCredentialBundleBytes {
		return "", fmt.Errorf("encode gateway credential bundle")
	}
	return string(value), nil
}

func attachGatewayCredential(command *exec.Cmd, token string) (func(), error) {
	command.Env = environmentWithoutGatewayCredential(command.Env)
	if token == "" {
		return func() {}, nil
	}
	if len(token) > maximumCredentialBundleBytes {
		return nil, fmt.Errorf("gateway credential bundle exceeds %d bytes", maximumCredentialBundleBytes)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create gateway credential pipe: %w", err)
	}
	if _, err := io.WriteString(writer, token); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("write gateway credential pipe: %w", err)
	}
	if err := writer.Close(); err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("close gateway credential pipe: %w", err)
	}
	fd := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, reader)
	command.Env = append(command.Env, fmt.Sprintf("%s=%d", gatewayTokenFDEnvironment, fd))
	return func() { _ = reader.Close() }, nil
}
