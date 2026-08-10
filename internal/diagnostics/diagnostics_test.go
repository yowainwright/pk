package diagnostics

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReportExcludesSensitiveDiagnosticDetails(t *testing.T) {
	input := Input{
		Version:       "v1.2.3",
		ServiceStatus: "path = /Users/example/private\nstate = running",
		AuditErr:      errors.New("opening /Users/example/.config/pk/events.jsonl"),
	}
	var output bytes.Buffer

	if err := Write(&output, New(input)); err != nil {
		t.Fatalf("write report: %v", err)
	}

	report := output.String()
	assertDiagnosticContains(t, report, "version: v1.2.3")
	assertDiagnosticContains(t, report, "background service: running")
	assertDiagnosticContains(t, report, "audit log: unreadable")
	assertDiagnosticMissing(t, report, "/Users/example")
}

func TestReportSummarizesAvailableServices(t *testing.T) {
	input := Input{
		Version:         "dev",
		ServiceStatus:   "active",
		DockerAvailable: true,
		AuditEvents:     3,
		AuditOverride:   true,
	}
	var output bytes.Buffer

	if err := Write(&output, New(input)); err != nil {
		t.Fatalf("write report: %v", err)
	}

	report := output.String()
	assertDiagnosticContains(t, report, "background service: active")
	assertDiagnosticContains(t, report, "docker CLI: available")
	assertDiagnosticContains(t, report, "audit log: readable (3 events)")
	assertDiagnosticContains(t, report, "audit path override: set")
}

func TestReportIdentifiesStoppedLaunchdService(t *testing.T) {
	input := Input{ServiceStatus: "state = not running"}
	report := New(input)

	if report.Service != "installed but not running" {
		t.Fatalf("unexpected service state %q", report.Service)
	}
}

func TestReportDoesNotAssumeUnknownServiceIsRunning(t *testing.T) {
	input := Input{ServiceStatus: "service = enabled"}
	report := New(input)

	if report.Service != "unknown" {
		t.Fatalf("unexpected service state %q", report.Service)
	}
}

func assertDiagnosticContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q in diagnostic report:\n%s", expected, value)
	}
}

func assertDiagnosticMissing(t *testing.T, value string, unexpected string) {
	t.Helper()
	if strings.Contains(value, unexpected) {
		t.Fatalf("unexpected %q in diagnostic report:\n%s", unexpected, value)
	}
}
