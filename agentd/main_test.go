package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSupportCLIUsesUserFacingStatusAndGrammar(t *testing.T) {
	for input, expected := range map[string]string{
		"healthy":         "Healthy",
		"attention":       "Attention",
		"action-required": "Action required",
		"future":          "Unknown",
	} {
		if actual := supportStatusLabel(input); actual != expected {
			t.Fatalf("supportStatusLabel(%q)=%q, want %q", input, actual, expected)
		}
	}
	if actual := countedLabel(1, "warning"); actual != "warning" {
		t.Fatalf("singular label=%q", actual)
	}
	if actual := countedLabel(2, "warning"); actual != "warnings" {
		t.Fatalf("plural label=%q", actual)
	}
}

func TestVersionAndHelpDoNotLoadUserConfiguration(t *testing.T) {
	root := t.TempDir()
	providers := filepath.Join(root, "providers.json")
	if err := os.WriteFile(providers, []byte(`{"schemaVersion":1,"providers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("HOBOT_CODE_MANAGED_PROVIDER_CONFIG", providers)
	if _, err := loadConfig(); err == nil {
		t.Fatal("public provider configuration unexpectedly passed private-file validation")
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if err := run(args); err != nil {
			t.Fatalf("side-effect-free command %v loaded user configuration: %v", args, err)
		}
	}
}
