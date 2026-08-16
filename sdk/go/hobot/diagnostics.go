package hobot

import (
	"fmt"
	"strings"
)

const (
	maximumDiagnosticChecks  = 256
	maximumDiagnosticRepairs = 8
)

func normalizeLegacyDiagnosticReport(report *DiagnosticReport) {
	for index := range report.Checks {
		if strings.HasPrefix(report.Checks[index].Name, "utility-") {
			report.Checks[index].Name = strings.ReplaceAll(report.Checks[index].Name, "_", "-")
		}
	}
}

func validateDiagnosticReport(report DiagnosticReport) error {
	if report.SchemaVersion != 1 || report.CapturedAt.IsZero() || len(report.Checks) == 0 || len(report.Checks) > maximumDiagnosticChecks {
		return fmt.Errorf("invalid schema, capture time, or check count")
	}
	actual := SupportCheckSummary{}
	seenChecks := map[string]bool{}
	for _, check := range report.Checks {
		if !supportIdentifierPattern.MatchString(check.Name) || seenChecks[check.Name] || !extensionOneOf(check.Status, "pass", "info", "warn", "fail") || !safeExtensionText(check.Summary, 512) {
			return fmt.Errorf("invalid or duplicate diagnostic check")
		}
		seenChecks[check.Name] = true
		switch check.Status {
		case "pass":
			actual.Pass++
		case "info":
			actual.Info++
		case "warn":
			actual.Warn++
		case "fail":
			actual.Fail++
		}
	}
	if actual != report.Summary || !supportStatusMatchesChecks(report.Status, report.Summary) {
		return fmt.Errorf("diagnostic status does not match checks")
	}
	if err := validateSupportFindings(report.Findings); err != nil {
		return err
	}
	if len(report.Repairs) > maximumDiagnosticRepairs {
		return fmt.Errorf("too many diagnostic repairs")
	}
	seenRepairs := map[string]bool{}
	for _, repair := range report.Repairs {
		if !supportIdentifierPattern.MatchString(repair.ID) || seenRepairs[repair.ID] || !extensionOneOf(repair.Executor, "agentd", "client") ||
			!extensionOneOf(repair.Status, "available", "blocked") || !repair.RequiresConfirmation || !safeExtensionText(repair.Summary, 256) || !safeExtensionText(repair.Reason, 512) {
			return fmt.Errorf("invalid or duplicate diagnostic repair")
		}
		if (repair.ID == "private-runtime-permissions" && repair.Executor != "agentd") || (repair.ID == "restart-daemon" && repair.Executor != "client") ||
			(repair.ID != "private-runtime-permissions" && repair.ID != "restart-daemon") {
			return fmt.Errorf("unsupported diagnostic repair")
		}
		seenRepairs[repair.ID] = true
	}
	return nil
}

func validateDiagnosticRepairResult(result DiagnosticRepairResult) error {
	if result.SchemaVersion != 1 || result.Action != "private-runtime-permissions" || result.Changed < 1 || result.Changed > 64 {
		return fmt.Errorf("invalid diagnostic repair metadata")
	}
	return validateDiagnosticReport(result.Report)
}
