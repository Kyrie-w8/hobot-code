package hobot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func validSupportBundleV2(t *testing.T) SupportBundle {
	t.Helper()
	id := "112233445566"
	document := map[string]any{
		"status":   "attention",
		"manifest": map[string]any{"schema": 2, "product": "Hobot Code", "bundleId": id},
	}
	content, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return SupportBundle{
		SchemaVersion: 2, ID: id, CreatedAt: time.Now().UTC(), Path: "/private/hobot-code-support-test.json",
		SizeBytes: len(content), SHA256: hex.EncodeToString(digest[:]), Content: content,
		Excluded: []string{"prompts"}, Status: "attention",
		Checks: SupportCheckSummary{Pass: 4, Info: 2, Warn: 1},
		Findings: []SupportFinding{{
			Code: "memory", Severity: "warning", Scope: "resources", Title: "System memory is low",
			Summary: "12.0% available", Action: "Stop unused workloads before continuing.", Count: 1,
		}},
	}
}

func TestSupportBundleV2ValidationBindsMetadataContentAndFindings(t *testing.T) {
	bundle := validSupportBundleV2(t)
	if err := validateSupportBundle(bundle); err != nil {
		t.Fatal(err)
	}

	invalid := bundle
	invalid.Status = "healthy"
	if err := validateSupportBundle(invalid); err == nil {
		t.Fatal("accepted status that contradicts the check summary")
	}

	invalid = bundle
	invalid.Findings = append([]SupportFinding(nil), bundle.Findings...)
	invalid.Findings[0].Action = "unsafe\nsecond line"
	if err := validateSupportBundle(invalid); err == nil {
		t.Fatal("accepted control characters in a finding")
	}

	invalid = bundle
	invalid.Content = append([]byte(nil), bundle.Content...)
	invalid.Content[0] ^= 1
	if err := validateSupportBundle(invalid); err == nil {
		t.Fatal("accepted content that does not match its digest")
	}
}

func TestSupportBundleLegacyMetadataRemainsCompatibleButCannotClaimV2(t *testing.T) {
	content := []byte("{\"safe\":true}\n")
	digest := sha256.Sum256(content)
	bundle := SupportBundle{
		ID: "112233445566", CreatedAt: time.Now().UTC(), Path: "/private/hobot-code-support-legacy.json",
		SizeBytes: len(content), SHA256: hex.EncodeToString(digest[:]), Content: content,
		Excluded: []string{"prompts"}, Checks: SupportCheckSummary{Pass: 1},
	}
	if err := validateSupportBundle(bundle); err != nil {
		t.Fatal(err)
	}
	bundle.Status = "healthy"
	if err := validateSupportBundle(bundle); err == nil {
		t.Fatal("legacy response claimed v2 status without a v2 schema")
	}
}
