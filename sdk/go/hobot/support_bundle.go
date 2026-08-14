package hobot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maximumSupportBundleBytes = 4 * 1024 * 1024
	maximumSupportFindings    = 16
	maximumSupportChecks      = 256
)

var supportIdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var supportBundleIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)
var supportDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validateSupportBundle(bundle SupportBundle) error {
	if bundle.SchemaVersion != 0 && bundle.SchemaVersion != 1 && bundle.SchemaVersion != 2 {
		return fmt.Errorf("unsupported schema version")
	}
	if !supportBundleIDPattern.MatchString(bundle.ID) || bundle.CreatedAt.IsZero() {
		return fmt.Errorf("invalid identity or capture time")
	}
	name := filepath.Base(bundle.Path)
	if !filepath.IsAbs(bundle.Path) || name == "." || !strings.HasPrefix(name, "hobot-code-support-") || !strings.HasSuffix(name, ".json") {
		return fmt.Errorf("invalid board path")
	}
	if bundle.SizeBytes <= 0 || bundle.SizeBytes > maximumSupportBundleBytes || !supportDigestPattern.MatchString(bundle.SHA256) {
		return fmt.Errorf("invalid size or digest")
	}
	if len(bundle.Content) > 0 {
		if len(bundle.Content) != bundle.SizeBytes {
			return fmt.Errorf("content size does not match metadata")
		}
		digest := sha256.Sum256(bundle.Content)
		if hex.EncodeToString(digest[:]) != bundle.SHA256 {
			return fmt.Errorf("content digest does not match metadata")
		}
	}
	if len(bundle.Excluded) == 0 || len(bundle.Excluded) > 16 {
		return fmt.Errorf("invalid privacy exclusion list")
	}
	seenExcluded := map[string]bool{}
	for _, excluded := range bundle.Excluded {
		if !safeExtensionText(excluded, 160) || seenExcluded[excluded] {
			return fmt.Errorf("invalid or duplicate privacy exclusion")
		}
		seenExcluded[excluded] = true
	}
	if bundle.Checks.Pass < 0 || bundle.Checks.Info < 0 || bundle.Checks.Warn < 0 || bundle.Checks.Fail < 0 ||
		bundle.Checks.Pass+bundle.Checks.Info+bundle.Checks.Warn+bundle.Checks.Fail > maximumSupportChecks {
		return fmt.Errorf("invalid check summary")
	}
	if bundle.SchemaVersion < 2 {
		if bundle.Status != "" || len(bundle.Findings) != 0 || bundle.Checks.Info != 0 {
			return fmt.Errorf("legacy bundle contains v2 diagnostics")
		}
		return nil
	}
	if !supportStatusMatchesChecks(bundle.Status, bundle.Checks) {
		return fmt.Errorf("status does not match check summary")
	}
	if err := validateSupportFindings(bundle.Findings); err != nil {
		return err
	}
	if len(bundle.Content) > 0 {
		var document struct {
			Status   string `json:"status"`
			Manifest struct {
				Schema   int    `json:"schema"`
				Product  string `json:"product"`
				BundleID string `json:"bundleId"`
			} `json:"manifest"`
		}
		if json.Unmarshal(bundle.Content, &document) != nil || document.Manifest.Schema != 2 || document.Manifest.Product != "Hobot Code" ||
			document.Manifest.BundleID != bundle.ID || document.Status != bundle.Status {
			return fmt.Errorf("content identity does not match metadata")
		}
	}
	return nil
}

func validateSupportFindings(findings []SupportFinding) error {
	if len(findings) > maximumSupportFindings {
		return fmt.Errorf("too many findings")
	}
	seenFindings := map[string]bool{}
	for _, finding := range findings {
		if !supportIdentifierPattern.MatchString(finding.Code) || seenFindings[finding.Code] ||
			!extensionOneOf(finding.Severity, "info", "warning", "error") ||
			!extensionOneOf(finding.Scope, "runtime", "board", "storage", "security", "models", "resources", "tasks") ||
			!safeExtensionText(finding.Title, 160) || !safeExtensionText(finding.Summary, 512) || !safeExtensionText(finding.Action, 512) ||
			finding.Count < 0 || finding.Count > 10000 {
			return fmt.Errorf("invalid or duplicate finding")
		}
		seenFindings[finding.Code] = true
	}
	return nil
}

func supportStatusMatchesChecks(status string, checks SupportCheckSummary) bool {
	switch status {
	case "action-required":
		return checks.Fail > 0
	case "attention":
		return checks.Fail == 0 && checks.Warn > 0
	case "healthy":
		return checks.Fail == 0 && checks.Warn == 0
	default:
		return false
	}
}
