package scan

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/process"
)

func TestReportsPlansKillForRestartableDevProcessOverThreshold(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "node")
	proc.CommandLine = "node ./node_modules/.bin/vite"
	proc.Cwd = "/Users/jeff/code/app"
	proc.CPUPercent = 95

	report := onlyReport(t, cfg, proc)

	assertReport(t, report, ActionKill, ConfidenceHigh)
	assertReasons(t, report, "restartable-command", "dev-cwd", "high-cpu")
}

func TestReportsPlansKillForRestartableDevProcess(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "node")
	proc.CommandLine = "node ./node_modules/.bin/vite"
	proc.Cwd = "/Users/jeff/code/app"

	report := onlyReport(t, cfg, proc)

	assertReport(t, report, ActionKill, ConfidenceHigh)
	assertReasons(t, report, "restartable-command", "dev-cwd")
}

func TestReportsKeepsUnknownMetadataReportOnly(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "node")
	proc.CPUPercent = 95

	report := onlyReport(t, cfg, proc)

	assertReport(t, report, ActionReport, ConfidenceLow)
	assertReasons(t, report, "command-unavailable", "cwd-unavailable")
}

func TestReportsKeepsProtectedProcessesReportOnly(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "Terminal")
	proc.CommandLine = "Terminal"
	proc.Cwd = "/Users/jeff/code/app"
	proc.CPUPercent = 95

	report := onlyReport(t, cfg, proc)

	assertReport(t, report, ActionReport, ConfidenceLow)
	assertReasons(t, report, "protected-process", "high-cpu")
}

func TestReportsSkipsProcessesWithOnlyMissingMetadata(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "logd")

	reports := Reports(cfg, processes(proc))

	if len(reports) != 0 {
		t.Fatalf("expected no reports, got %d", len(reports))
	}
}

func TestReportsAvoidsBroadCommandSubstrings(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "containermanagerd")
	proc.CommandLine = "/usr/libexec/containermanagerd"
	proc.Cwd = "/"

	reports := Reports(cfg, processes(proc))

	if len(reports) != 0 {
		t.Fatalf("expected no reports, got %d", len(reports))
	}
}

func TestReportsSkipsRestartableCommandsOutsideDevCwd(t *testing.T) {
	cfg := testConfig(t, "-cpu", "80")
	proc := newProcess(42, "node")
	proc.CommandLine = "/usr/local/bin/node server.js"
	proc.Cwd = "/Applications"

	reports := Reports(cfg, processes(proc))

	if len(reports) != 0 {
		t.Fatalf("expected no reports, got %d", len(reports))
	}
}

func TestScannerReturnsReportsFromLister(t *testing.T) {
	cfg := testConfig(t)
	proc := restartableDevProcess(42)
	lister := &fakeScanLister{procs: processes(proc)}
	scanner := New(cfg, lister)

	reports, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}
}

func TestScannerReturnsListerErrors(t *testing.T) {
	cfg := testConfig(t)
	lister := &fakeScanLister{err: errors.New("denied")}
	scanner := New(cfg, lister)

	_, err := scanner.Scan(context.Background())

	if err == nil {
		t.Fatal("expected lister error")
	}
}

func TestReportsSortsByPID(t *testing.T) {
	cfg := testConfig(t)
	first := restartableDevProcess(2)
	second := restartableDevProcess(1)

	reports := Reports(cfg, []process.Process{first, second})

	if reports[0].Process.PID != 1 {
		t.Fatalf("expected pid 1 first, got %d", reports[0].Process.PID)
	}
}

func TestReportsSkipsProtectedProcessBelowThreshold(t *testing.T) {
	cfg := testConfig(t)
	proc := newProcess(42, "Terminal")
	proc.CommandLine = "Terminal"
	proc.Cwd = "/Users/jeff/code/app"

	reports := Reports(cfg, processes(proc))

	if len(reports) != 0 {
		t.Fatalf("expected no reports, got %d", len(reports))
	}
}

func TestReportsUsesMemoryThresholdReason(t *testing.T) {
	cfg := testConfig(t, "-mem", "100")
	proc := restartableDevProcess(42)
	proc.MemoryMB = 128

	report := onlyReport(t, cfg, proc)

	assertReasons(t, report, "high-memory")
}

func TestReportsMatchesRestartableCommandTokens(t *testing.T) {
	cfg := testConfig(t)
	proc := newProcess(42, "server")
	proc.CommandLine = "/usr/local/bin/python3 -m http.server"
	proc.Cwd = "/workspace"

	report := onlyReport(t, cfg, proc)

	assertReport(t, report, ActionKill, ConfidenceHigh)
}

func TestWriteReportsHandlesNoMatches(t *testing.T) {
	var out bytes.Buffer

	err := WriteReports(&out, nil)
	if err != nil {
		t.Fatalf("write reports: %v", err)
	}
	if out.String() != "No matching processes found.\n" {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestWriteReportsUsesTabularOutput(t *testing.T) {
	var out bytes.Buffer
	reports := reportsForOutput()
	err := WriteReports(&out, reports)
	if err != nil {
		t.Fatalf("write reports: %v", err)
	}

	header := "PID\tACTION\tCONFIDENCE\tNAME\tREASONS\n"
	row := "7\tkill\thigh\tnode\trestartable-command,dev-cwd\n"
	want := header + row
	if out.String() != want {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

type fakeScanLister struct {
	procs []process.Process
	err   error
}

func (l *fakeScanLister) List(ctx context.Context) ([]process.Process, error) {
	return l.procs, l.err
}

func restartableDevProcess(pid int32) process.Process {
	proc := newProcess(pid, "node")
	proc.CommandLine = "node ./node_modules/.bin/vite"
	proc.Cwd = "/Users/jeff/code/app"
	return proc
}

func reportsForOutput() []Report {
	report := newReport(newProcess(7, "node"))
	report.Action = ActionKill
	report.Confidence = ConfidenceHigh
	report.Reasons = append(report.Reasons, "restartable-command", "dev-cwd")

	reports := make([]Report, 0, 1)
	reports = append(reports, report)
	return reports
}

func testConfig(t *testing.T, args ...string) *config.Config {
	t.Helper()
	cfg, err := config.ParseArgs("test", args)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg
}

func onlyReport(t *testing.T, cfg *config.Config, proc process.Process) Report {
	t.Helper()
	reports := Reports(cfg, processes(proc))
	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}
	return reports[0]
}

func processes(proc process.Process) []process.Process {
	procs := make([]process.Process, 0, 1)
	procs = append(procs, proc)
	return procs
}

func newProcess(pid int32, name string) process.Process {
	var proc process.Process
	proc.PID = pid
	proc.Name = name
	return proc
}

func newReport(proc process.Process) Report {
	var report Report
	report.Process = proc
	return report
}

func assertReport(t *testing.T, report Report, action Action, confidence Confidence) {
	t.Helper()
	if report.Action != action {
		t.Fatalf("expected action %s, got %s", action, report.Action)
	}
	if report.Confidence != confidence {
		t.Fatalf("expected confidence %s, got %s", confidence, report.Confidence)
	}
}

func assertReasons(t *testing.T, report Report, expected ...string) {
	t.Helper()
	for _, reason := range expected {
		if !hasReason(report.Reasons, reason) {
			reasons := strings.Join(report.Reasons, ",")
			t.Fatalf("expected reason %q in %s", reason, reasons)
		}
	}
}
