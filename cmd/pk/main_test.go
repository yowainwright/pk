package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jeffrywainwright/pk/internal/audit"
	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/docker"
	"github.com/jeffrywainwright/pk/internal/killer"
	"github.com/jeffrywainwright/pk/internal/process"
	"github.com/jeffrywainwright/pk/internal/scan"
)

func TestRunPrintsVersion(t *testing.T) {
	var out bytes.Buffer

	err := run([]string{"version"}, &out)
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if out.String() != "pk dev\n" {
		t.Fatalf("unexpected version output %q", out.String())
	}
}

func TestRunReturnsUnknownCommand(t *testing.T) {
	var out bytes.Buffer

	err := run([]string{"missing"}, &out)

	if err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestRunScanWritesReports(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.reports = []scan.Report{commandReport()}
	var out bytes.Buffer

	err := run([]string{"scan"}, &out)
	if err != nil {
		t.Fatalf("run scan: %v", err)
	}
	if !strings.Contains(out.String(), "42\tkill\thigh\tnode") {
		t.Fatalf("unexpected scan output %q", out.String())
	}
}

func TestRunScanReturnsScannerError(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.err = errors.New("scan failed")
	var out bytes.Buffer

	err := run([]string{"scan"}, &out)

	if err == nil {
		t.Fatal("expected scan error")
	}
}

func TestRunScanReturnsParseError(t *testing.T) {
	commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"scan", "-cpu", "bad"}, &out)

	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRunCleanupDefaultsToDryRun(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.reports = []scan.Report{commandReport()}
	var out bytes.Buffer

	err := run([]string{"cleanup"}, &out)
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if deps.killer.called {
		t.Fatal("expected dry run not to kill")
	}
	applied := false
	assertCleanupEvent(t, deps.audit.events[0], applied)
}

func TestRunCleanupReturnsAuditStoreError(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.reports = []scan.Report{commandReport()}
	deps.auditStoreErr = errors.New("audit unavailable")
	var out bytes.Buffer

	err := run([]string{"cleanup"}, &out)

	if err == nil {
		t.Fatal("expected audit store error")
	}
}

func TestRunCleanupReturnsRecorderError(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.reports = []scan.Report{commandReport()}
	deps.audit.err = errors.New("disk full")
	var out bytes.Buffer

	err := run([]string{"cleanup"}, &out)

	if err == nil {
		t.Fatal("expected recorder error")
	}
}

func TestRunCleanupApplyKillsTarget(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.reports = []scan.Report{commandReport()}
	var out bytes.Buffer

	err := run([]string{"cleanup", "--apply"}, &out)
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if deps.killer.pid != 42 {
		t.Fatalf("expected killed pid 42, got %d", deps.killer.pid)
	}
	applied := true
	assertCleanupEvent(t, deps.audit.events[0], applied)
}

func TestRunCleanupIncludesDockerTargets(t *testing.T) {
	deps := commandDeps(t)
	deps.docker.available = true
	deps.docker.containers = []docker.Container{testContainer()}
	var out bytes.Buffer

	err := run([]string{"cleanup", "--apply"}, &out)

	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if deps.docker.stoppedID != "abc123" {
		t.Fatalf("expected stopped container, got %q", deps.docker.stoppedID)
	}
	if !strings.Contains(out.String(), "CONTAINER\tAPPLIED") {
		t.Fatalf("expected container output, got %q", out.String())
	}
}

func TestCleanupConfigParsesWatchOptions(t *testing.T) {
	cfg, options, err := cleanupConfig([]string{"--apply", "--watch", "-interval", "5s"})
	if err != nil {
		t.Fatalf("cleanup config: %v", err)
	}
	if !options.apply {
		t.Fatal("expected apply option")
	}
	if !options.watch {
		t.Fatal("expected watch option")
	}
	if cfg.Interval != 5*time.Second {
		t.Fatalf("expected five second interval, got %s", cfg.Interval)
	}
}

func TestCleanupLoopStopsWhenCanceled(t *testing.T) {
	cfg := &config.Config{}
	options := cleanupOptions{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cleanupLoop(ctx, nil, cfg, options, &bytes.Buffer{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
}

func TestCleanupLoopRunsOnTicks(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.err = errors.New("scan failed")
	cfg := &config.Config{}
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()

	err := cleanupLoop(context.Background(), ticks, cfg, cleanupOptions{}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected scan error")
	}
}

func TestRunHistoryReturnsAuditError(t *testing.T) {
	deps := commandDeps(t)
	deps.audit.err = errors.New("read failed")
	var out bytes.Buffer

	err := run([]string{"history"}, &out)

	if err == nil {
		t.Fatal("expected audit error")
	}
}

func TestRunHistoryWritesAuditEvents(t *testing.T) {
	deps := commandDeps(t)
	deps.audit.events = []audit.Event{{Command: "cleanup", Name: "node"}}
	var out bytes.Buffer

	err := run([]string{"history"}, &out)
	if err != nil {
		t.Fatalf("run history: %v", err)
	}
	if !strings.Contains(out.String(), `"name":"node"`) {
		t.Fatalf("unexpected history output %q", out.String())
	}
}

func TestRunInstallInstallsBackgroundService(t *testing.T) {
	deps := commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"install"}, &out)
	if err != nil {
		t.Fatalf("run install: %v", err)
	}
	if !deps.background.installed {
		t.Fatal("expected background install")
	}
	if out.String() != "installed\n" {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestRunUninstallRemovesBackgroundService(t *testing.T) {
	deps := commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"uninstall"}, &out)
	if err != nil {
		t.Fatalf("run uninstall: %v", err)
	}
	if !deps.background.uninstalled {
		t.Fatal("expected background uninstall")
	}
}

func TestRunUninstallReturnsManagerErrors(t *testing.T) {
	deps := commandDeps(t)
	deps.backgroundErr = errors.New("manager failed")
	var out bytes.Buffer

	err := run([]string{"uninstall"}, &out)

	if err == nil {
		t.Fatal("expected manager error")
	}
}

func TestRunStatusPrintsBackgroundStatus(t *testing.T) {
	deps := commandDeps(t)
	deps.background.status = "active"
	var out bytes.Buffer

	err := run([]string{"status"}, &out)
	if err != nil {
		t.Fatalf("run status: %v", err)
	}
	if out.String() != "active\n" {
		t.Fatalf("unexpected status output %q", out.String())
	}
}

func TestRunStatusReturnsStatusErrors(t *testing.T) {
	deps := commandDeps(t)
	deps.background.err = errors.New("status failed")
	var out bytes.Buffer

	err := run([]string{"status"}, &out)

	if err == nil {
		t.Fatal("expected status error")
	}
}

func TestRunInstallReturnsManagerErrors(t *testing.T) {
	deps := commandDeps(t)
	deps.backgroundErr = errors.New("manager failed")
	var out bytes.Buffer

	err := run([]string{"install"}, &out)

	if err == nil {
		t.Fatal("expected manager error")
	}
}

func TestRunMonitorReturnsParseError(t *testing.T) {
	commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"monitor", "-interval", "bad"}, &out)

	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRunMonitorUsesRunner(t *testing.T) {
	deps := commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"monitor", "-interval", "1ms"}, &out)
	if err != nil {
		t.Fatalf("run monitor: %v", err)
	}
	if deps.cfg.Interval != time.Millisecond {
		t.Fatalf("expected one millisecond interval, got %s", deps.cfg.Interval)
	}
}

func TestNewMonitorReturnsMonitor(t *testing.T) {
	commandDeps(t)

	monitor := newMonitor(&config.Config{})

	if monitor == nil {
		t.Fatal("expected monitor")
	}
}

func TestNotifyKilledSendsNotification(t *testing.T) {
	deps := commandDeps(t)

	notifyKilled("node", 42)

	if deps.notificationTitle != "pk" {
		t.Fatalf("expected notification title, got %q", deps.notificationTitle)
	}
	if !strings.Contains(deps.notificationMessage, "PID 42") {
		t.Fatalf("unexpected notification message %q", deps.notificationMessage)
	}
}

func TestNotifyKilledIgnoresNotificationErrors(t *testing.T) {
	commandDeps(t)
	sendNotification = func(title string, message string) error {
		return errors.New("notification failed")
	}

	notifyKilled("node", 42)
}

func TestExitOnErrorIgnoresExpectedErrors(t *testing.T) {
	exitOnError(nil)
	exitOnError(context.Canceled)
}

func TestExitOnErrorExitsForUnexpectedErrors(t *testing.T) {
	oldExitProcess := exitProcess
	defer func() {
		exitProcess = oldExitProcess
	}()
	var code int
	exitProcess = func(status int) {
		code = status
	}

	exitOnError(errors.New("boom"))

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestHandleSignalsCancelsOnSignal(t *testing.T) {
	oldNotifySignal := notifySignal
	defer func() {
		notifySignal = oldNotifySignal
	}()
	var sigCh chan<- os.Signal
	notifySignal = func(c chan<- os.Signal, signals ...os.Signal) {
		sigCh = c
	}
	canceled := make(chan struct{})

	handleSignals(func() { close(canceled) })
	sigCh <- syscall.SIGTERM

	assertCanceled(t, canceled)
}

func TestSplitCommandDefaultsFlagsToMonitor(t *testing.T) {
	args := make([]string, 0, 1)
	args = append(args, "-dry-run")

	command, commandArgs := splitCommand(args)

	if command != "" {
		t.Fatalf("expected monitor command, got %q", command)
	}
	if commandArgs[0] != "-dry-run" {
		t.Fatalf("expected flag to remain in args, got %#v", commandArgs)
	}
}

func TestSplitCommandReturnsSubcommand(t *testing.T) {
	args := make([]string, 0, 3)
	args = append(args, "scan", "-cpu", "90")

	command, commandArgs := splitCommand(args)

	if command != "scan" {
		t.Fatalf("expected scan command, got %q", command)
	}
	if len(commandArgs) != 2 {
		t.Fatalf("expected two args, got %#v", commandArgs)
	}
}

func TestSplitCommandTrimsSeparator(t *testing.T) {
	args := make([]string, 0, 2)
	args = append(args, "--", "scan")

	command, commandArgs := splitCommand(args)

	if command != "scan" {
		t.Fatalf("expected scan command, got %q", command)
	}
	if len(commandArgs) != 0 {
		t.Fatalf("expected no command args, got %#v", commandArgs)
	}
}

func TestIsVersionCommand(t *testing.T) {
	versionFlag := []string{"--version"}
	versionCommand := []string{"version"}
	if !isVersionCommand(versionFlag) {
		t.Fatal("expected --version to be a version command")
	}
	if !isVersionCommand(versionCommand) {
		t.Fatal("expected version to be a version command")
	}
}

type fakeScanner struct {
	reports []scan.Report
	err     error
}

func (s *fakeScanner) Scan(ctx context.Context) ([]scan.Report, error) {
	return s.reports, s.err
}

type fakeAuditStore struct {
	events []audit.Event
	err    error
}

func (s *fakeAuditStore) Record(event audit.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func (s *fakeAuditStore) Events() ([]audit.Event, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

type fakeCommandKiller struct {
	called bool
	pid    int32
}

func (k *fakeCommandKiller) Kill(ctx context.Context, pid int32) error {
	k.called = true
	k.pid = pid
	return nil
}

type fakeDockerClient struct {
	available  bool
	containers []docker.Container
	stoppedID  string
	err        error
}

func (c *fakeDockerClient) Available() bool {
	return c.available
}

func (c *fakeDockerClient) List(ctx context.Context) ([]docker.Container, error) {
	return c.containers, c.err
}

func (c *fakeDockerClient) Stop(ctx context.Context, id string) error {
	c.stoppedID = id
	return c.err
}

type fakeRunner struct {
	err error
}

func (r *fakeRunner) Run(ctx context.Context) error {
	return r.err
}

type fakeBackgroundManager struct {
	installed   bool
	uninstalled bool
	status      string
	err         error
}

func (m *fakeBackgroundManager) Install() error {
	m.installed = true
	return m.err
}

func (m *fakeBackgroundManager) Uninstall() error {
	m.uninstalled = true
	return m.err
}

func (m *fakeBackgroundManager) Status() (string, error) {
	return m.status, m.err
}

type commandTestDeps struct {
	scanner             *fakeScanner
	audit               *fakeAuditStore
	auditStoreErr       error
	killer              *fakeCommandKiller
	docker              *fakeDockerClient
	runner              *fakeRunner
	background          *fakeBackgroundManager
	backgroundErr       error
	cfg                 *config.Config
	notificationTitle   string
	notificationMessage string
}

func commandDeps(t *testing.T) *commandTestDeps {
	t.Helper()
	deps := &commandTestDeps{}
	deps.scanner = &fakeScanner{}
	deps.audit = &fakeAuditStore{}
	deps.killer = &fakeCommandKiller{}
	deps.docker = &fakeDockerClient{}
	deps.runner = &fakeRunner{}
	deps.background = &fakeBackgroundManager{}
	installCommandDeps(t, deps)
	return deps
}

func installCommandDeps(t *testing.T, deps *commandTestDeps) {
	t.Helper()
	oldDeps := saveCommandDeps()
	t.Cleanup(oldDeps.restore)
	newProcessLister = func() process.Lister { return fakeCommandLister{} }
	newProcessScanner = func(cfg *config.Config, lister process.Lister) processScanner {
		deps.cfg = cfg
		return deps.scanner
	}
	newAuditStore = func() (auditStore, error) {
		return deps.audit, deps.auditStoreErr
	}
	newProcessKiller = func() killer.Killer { return deps.killer }
	newDockerClient = func() docker.Client { return deps.docker }
	newMonitorRunner = func(cfg *config.Config) monitorRunner {
		deps.cfg = cfg
		return deps.runner
	}
	newBackgroundManager = func() (backgroundManager, error) {
		return deps.background, deps.backgroundErr
	}
	sendNotification = func(title string, message string) error {
		deps.notificationTitle = title
		deps.notificationMessage = message
		return nil
	}
	handleShutdownSignal = func(cancel context.CancelFunc) {}
}

type savedCommandDeps struct {
	newLister        func() process.Lister
	newScanner       func(*config.Config, process.Lister) processScanner
	newAudit         func() (auditStore, error)
	newKiller        func() killer.Killer
	newDocker        func() docker.Client
	newRunner        func(*config.Config) monitorRunner
	newBackground    func() (backgroundManager, error)
	send             func(string, string) error
	handleSignalFunc func(context.CancelFunc)
	notifySignalFunc func(chan<- os.Signal, ...os.Signal)
	exitFunc         func(int)
}

func saveCommandDeps() savedCommandDeps {
	return savedCommandDeps{
		newLister:        newProcessLister,
		newScanner:       newProcessScanner,
		newAudit:         newAuditStore,
		newKiller:        newProcessKiller,
		newDocker:        newDockerClient,
		newRunner:        newMonitorRunner,
		newBackground:    newBackgroundManager,
		send:             sendNotification,
		handleSignalFunc: handleShutdownSignal,
		notifySignalFunc: notifySignal,
		exitFunc:         exitProcess,
	}
}

func (d savedCommandDeps) restore() {
	newProcessLister = d.newLister
	newProcessScanner = d.newScanner
	newAuditStore = d.newAudit
	newProcessKiller = d.newKiller
	newDockerClient = d.newDocker
	newMonitorRunner = d.newRunner
	newBackgroundManager = d.newBackground
	sendNotification = d.send
	handleShutdownSignal = d.handleSignalFunc
	notifySignal = d.notifySignalFunc
	exitProcess = d.exitFunc
}

type fakeCommandLister struct{}

func (l fakeCommandLister) List(ctx context.Context) ([]process.Process, error) {
	return nil, nil
}

func commandReport() scan.Report {
	var report scan.Report
	report.Process.PID = 42
	report.Process.Name = "node"
	report.Action = scan.ActionKill
	report.Confidence = scan.ConfidenceHigh
	report.Reasons = append(report.Reasons, "restartable-command", "dev-cwd")
	return report
}

func testContainer() docker.Container {
	return docker.Container{
		ID:    "abc123",
		Name:  "web",
		Image: "node:20",
		Labels: map[string]string{
			"com.docker.compose.project": "app",
		},
	}
}

func assertCleanupEvent(t *testing.T, event audit.Event, applied bool) {
	t.Helper()
	if event.PID != 42 {
		t.Fatalf("expected pid 42, got %d", event.PID)
	}
	if event.Applied != applied {
		t.Fatalf("expected applied %t, got %t", applied, event.Applied)
	}
}

func assertCanceled(t *testing.T, canceled <-chan struct{}) {
	t.Helper()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("expected cancellation")
	}
}
