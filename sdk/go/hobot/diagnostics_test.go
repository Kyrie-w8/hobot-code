package hobot

import (
	"testing"
	"time"
)

func validDiagnosticReport() DiagnosticReport {
	return DiagnosticReport{
		SchemaVersion: 1, CapturedAt: time.Now().UTC(), Status: "attention",
		Summary: SupportCheckSummary{Pass: 1, Warn: 1},
		Checks: []SupportCheck{
			{Name: "configuration-current", Status: "pass", Summary: "agentd is current"},
			{Name: "model-configuration", Status: "warn", Summary: "no provider is configured"},
		},
		Findings: []SupportFinding{{
			Code: "model-configuration", Severity: "warning", Scope: "models", Title: "No provider",
			Summary: "No provider is configured.", Action: "Configure a provider.", Count: 1,
		}},
		Repairs: []DiagnosticRepairAction{{
			ID: "restart-daemon", Executor: "client", Status: "blocked", RequiresConfirmation: true,
			Summary: "Restart agentd", Reason: "A task is active.",
		}},
	}
}

func TestDiagnosticReportValidationIsStrict(t *testing.T) {
	report := validDiagnosticReport()
	if err := validateDiagnosticReport(report); err != nil {
		t.Fatal(err)
	}
	invalid := report
	invalid.Summary.Warn = 0
	if err := validateDiagnosticReport(invalid); err == nil {
		t.Fatal("accepted a summary that does not match checks")
	}
	invalid = report
	invalid.Repairs = append([]DiagnosticRepairAction(nil), report.Repairs...)
	invalid.Repairs[0].RequiresConfirmation = false
	if err := validateDiagnosticReport(invalid); err == nil {
		t.Fatal("accepted a repair without explicit confirmation")
	}
	invalid = report
	invalid.Checks = append(invalid.Checks, invalid.Checks[0])
	invalid.Summary.Pass++
	if err := validateDiagnosticReport(invalid); err == nil {
		t.Fatal("accepted duplicate diagnostic checks")
	}
}

func TestDiagnosticRepairValidationBindsActionAndReport(t *testing.T) {
	result := DiagnosticRepairResult{SchemaVersion: 1, Action: "private-runtime-permissions", Changed: 1, Report: validDiagnosticReport()}
	if err := validateDiagnosticRepairResult(result); err != nil {
		t.Fatal(err)
	}
	result.Action = "restart-daemon"
	if err := validateDiagnosticRepairResult(result); err == nil {
		t.Fatal("accepted a client-side action as a board repair result")
	}
}
