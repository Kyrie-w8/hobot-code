package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMigrateSettingsAddsBuiltInsWithoutRewritingUserChoices(t *testing.T) {
	current := []byte(`{
  "theme":"dark",
  "retry":{"enabled":true,"maxRetries":3,"provider":{"maxRetries":0}},
  "enabledModels":["drobotics/kimi-k3","custom/coder","drobotics/deepseek-v4-flash"]
}`)
	defaults := []byte(`{
  "retry":{"enabled":true,"maxRetries":5},
  "enabledModels":["drobotics/kimi-k3","drobotics/kimi-k2.6","drobotics/deepseek/deepseek-v4-flash","drobotics/deepseek-v4-flash"]
}`)
	migrated, err := migrateSettings(current, defaults)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Theme         string   `json:"theme"`
		EnabledModels []string `json:"enabledModels"`
		Retry         struct {
			MaxRetries int `json:"maxRetries"`
			Provider   struct {
				MaxRetries int `json:"maxRetries"`
			} `json:"provider"`
		} `json:"retry"`
	}
	if err := json.Unmarshal(migrated, &document); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"drobotics/kimi-k3",
		"custom/coder",
		"drobotics/deepseek-v4-flash",
		"drobotics/kimi-k2.6",
		"drobotics/deepseek/deepseek-v4-flash",
	}
	if strings.Join(document.EnabledModels, "|") != strings.Join(want, "|") {
		t.Fatalf("enabledModels=%v, expected %v", document.EnabledModels, want)
	}
	if document.Theme != "dark" || document.Retry.MaxRetries != 5 || document.Retry.Provider.MaxRetries != 0 {
		t.Fatalf("user settings were not preserved: %+v", document)
	}
}

func TestMigrateSettingsPreservesAnUnfilteredCatalog(t *testing.T) {
	migrated, err := migrateSettings([]byte(`{"theme":"dark"}`), []byte(`{"enabledModels":["drobotics/kimi-k3"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migrated), "enabledModels") {
		t.Fatalf("migration introduced a filter into an unfiltered configuration: %s", migrated)
	}
}

func TestMigrateSettingsRejectsInvalidStructures(t *testing.T) {
	for _, current := range [][]byte{
		[]byte(`[]`),
		[]byte(`{"enabledModels":"all"}`),
		[]byte(`{"retry":[]}`),
	} {
		if _, err := migrateSettings(current, []byte(`{"enabledModels":["drobotics/kimi-k3"]}`)); err == nil {
			t.Fatalf("invalid settings were accepted: %s", current)
		}
	}
}
