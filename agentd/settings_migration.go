package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maximumSettingsMigrationBytes = 1024 * 1024

func runSettingsMigrationCLI(args []string, output io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: agentd migrate-settings CURRENT DEFAULTS")
	}
	current, err := readSettingsMigrationFile(args[0])
	if err != nil {
		return fmt.Errorf("read current settings: %w", err)
	}
	defaults, err := readSettingsMigrationFile(args[1])
	if err != nil {
		return fmt.Errorf("read default settings: %w", err)
	}
	migrated, err := migrateSettings(current, defaults)
	if err != nil {
		return err
	}
	_, err = output.Write(migrated)
	return err
}

func readSettingsMigrationFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("path must be a regular file")
	}
	if info.Size() <= 0 || info.Size() > maximumSettingsMigrationBytes {
		return nil, fmt.Errorf("file size is outside the supported range")
	}
	return os.ReadFile(path)
}

func migrateSettings(current, defaults []byte) ([]byte, error) {
	var currentDocument map[string]json.RawMessage
	var defaultDocument map[string]json.RawMessage
	if len(current) > maximumSettingsMigrationBytes || json.Unmarshal(current, &currentDocument) != nil || currentDocument == nil {
		return nil, fmt.Errorf("current settings are invalid")
	}
	if len(defaults) > maximumSettingsMigrationBytes || json.Unmarshal(defaults, &defaultDocument) != nil || defaultDocument == nil {
		return nil, fmt.Errorf("default settings are invalid")
	}

	if raw, ok := currentDocument["enabledModels"]; ok {
		var enabled []string
		var builtIns []string
		if json.Unmarshal(raw, &enabled) != nil || json.Unmarshal(defaultDocument["enabledModels"], &builtIns) != nil {
			return nil, fmt.Errorf("enabledModels must be a string array")
		}
		seen := make(map[string]bool, len(enabled)+len(builtIns))
		merged := make([]string, 0, len(enabled)+len(builtIns))
		for _, model := range enabled {
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			merged = append(merged, model)
		}
		for _, model := range builtIns {
			if len(model) < len("drobotics/") || model[:len("drobotics/")] != "drobotics/" || seen[model] {
				continue
			}
			seen[model] = true
			merged = append(merged, model)
		}
		currentDocument["enabledModels"], _ = json.Marshal(merged)
	}

	if raw, ok := currentDocument["retry"]; ok {
		var retry map[string]json.RawMessage
		if json.Unmarshal(raw, &retry) != nil || retry == nil {
			return nil, fmt.Errorf("retry must be an object")
		}
		var attempts int
		if json.Unmarshal(retry["maxRetries"], &attempts) == nil && attempts == 3 {
			retry["maxRetries"] = json.RawMessage("5")
			currentDocument["retry"], _ = json.Marshal(retry)
		}
	}

	migrated, err := json.MarshalIndent(currentDocument, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode migrated settings: %w", err)
	}
	return append(migrated, '\n'), nil
}
