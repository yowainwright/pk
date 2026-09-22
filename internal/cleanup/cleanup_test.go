package cleanup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/process"
)

func TestRunDryRunRecordsWithoutKilling(t *testing.T) {
	killer := &fakeKiller{}
	recorder := &fakeRecorder{}
	reports := reports(testReport(42, ActionKill, ConfidenceHigh))
	apply := false

	results, err := Run(context.Background(), reports, killer, recorder, apply)

	assertNoError(t, err)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if killer.called {
		t.Fatal("expected dry run not to kill")
	}
	assertRecorded(t, recorder, apply)
}

func TestRunApplyKillsTargets(t *testing.T) {
	killer := &fakeKiller{}
	recorder := &fakeRecorder{}
	reports := reports(testReport(42, ActionKill, ConfidenceHigh))
	apply := true

	results, err := Run(context.Background(), reports, killer, recorder, apply)

	assertNoError(t, err)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	assertKilled(t, killer, 42)
	assertRecorded(t, recorder, apply)
}

func TestRunKillsDescendantsBeforeTarget(t *testing.T) {
	killer := &fakeKiller{}
	recorder := &fakeRecorder{}
	report := testReport(42, ActionKill, ConfidenceHigh)
	report.Descendants = append(report.Descendants, childProcess(43, 42))
	report.Descendants = append(report.Descendants, childProcess(44, 43))
	apply := true

	results, err := Run(context.Background(), reports(report), killer, recorder, apply)

	assertNoError(t, err)
	if len(results) != 3 {
		t.Fatalf("expected three results, got %d", len(results))
	}
	assertKilled(t, killer, 44, 43, 42)
	if len(recorder.events) != 3 {
		t.Fatalf("expected three events, got %d", len(recorder.events))
	}
}

func TestRunRecordsKillErrors(t *testing.T) {
	killer := &fakeKiller{err: errors.New("denied")}
	recorder := &fakeRecorder{}
	reports := reports(testReport(42, ActionKill, ConfidenceHigh))
	apply := true

	results, err := Run(context.Background(), reports, killer, recorder, apply)

	assertNoError(t, err)
	if results[0].Error != "denied" {
		t.Fatalf("expected denied error, got %q", results[0].Error)
	}
	if recorder.events[0].Error != "denied" {
		t.Fatalf("expected recorded error, got %q", recorder.events[0].Error)
	}
}

func TestTargetsIgnoresReportsBelowHighConfidence(t *testing.T) {
	report := testReport(42, ActionReport, ConfidenceMedium)
	targets := Targets(reports(report))

	if len(targets) != 0 {
		t.Fatalf("expected no targets, got %d", len(targets))
	}
}

func TestWriteResultsWritesTabularOutput(t *testing.T) {
	var out bytes.Buffer
	report := testReport(42, ActionKill, ConfidenceHigh)
	result := Result{Report: report, Process: report.Process}
	results := make([]Result, 0, 1)
	results = append(results, result)

	if err := WriteResults(&out, results); err != nil {
		t.Fatalf("write results: %v", err)
	}
	if !strings.Contains(out.String(), "PID\tAPPLIED\tNAME") {
		t.Fatalf("expected header, got %q", out.String())
	}
}

func TestWriteResultsHandlesNoTargets(t *testing.T) {
	var out bytes.Buffer
	results := make([]Result, 0)

	if err := WriteResults(&out, results); err != nil {
		t.Fatalf("write results: %v", err)
	}
	if out.String() != "No cleanup targets found.\n" {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestRunReturnsRecorderErrors(t *testing.T) {
	killer := &fakeKiller{}
	recorder := &fakeRecorder{err: errors.New("disk full")}
	reports := reports(testReport(42, ActionKill, ConfidenceHigh))
	apply := false

	_, err := Run(context.Background(), reports, killer, recorder, apply)

	if err == nil {
		t.Fatal("expected recorder error")
	}
}

type fakeRecorder struct {
	events []audit.Event
	err    error
}

func (r *fakeRecorder) Record(event audit.Event) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

func testReport(pid int32, action Action, confidence Confidence) Report {
	var report Report
	report.Process = testProcess(pid)
	report.Action = action
	report.Confidence = confidence
	report.Reasons = append(report.Reasons, "restartable-command", "dev-cwd")
	return report
}

func testProcess(pid int32) process.Process {
	var proc process.Process
	proc.PID = pid
	proc.CreateTime = 123
	proc.Name = "node"
	return proc
}

func childProcess(pid int32, parentPID int32) process.Process {
	proc := testProcess(pid)
	proc.ParentPID = parentPID
	return proc
}

func reports(report Report) []Report {
	reports := make([]Report, 0, 1)
	reports = append(reports, report)
	return reports
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertRecorded(t *testing.T, recorder *fakeRecorder, applied bool) {
	t.Helper()
	if len(recorder.events) != 1 {
		t.Fatalf("expected one event, got %d", len(recorder.events))
	}
	if recorder.events[0].Applied != applied {
		t.Fatalf("expected applied %t", applied)
	}
}
