package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/daemon"
	"github.com/yowainwright/pk/internal/docker"
	"github.com/yowainwright/pk/internal/dx"
	"github.com/yowainwright/pk/internal/killer"
	"github.com/yowainwright/pk/internal/lifecycle"
	"github.com/yowainwright/pk/internal/process"
	"github.com/yowainwright/pk/internal/scan"
)

func run(args []string, out io.Writer) error {
	ctx := context.Background()
	app, commandArgs, err := newApplication(ctx, args, strings.NewReader(""), out, io.Discard)
	if err != nil {
		return err
	}
	return app.run(commandArgs)
}

func cleanupConfig(args []string) (*config.Config, cleanupOptions, error) {
	return cleanupConfigWithOutput(args, io.Discard)
}

func exitOnError(err error) {
	ui := dx.New(dx.Config{Err: os.Stderr, Timestamps: true})
	application{ctx: context.Background(), ui: ui, out: os.Stdout}.exitOnError(err)
}

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

func TestRunAcceptsGlobalColorOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "before command", args: []string{"--color=never", "version"}},
		{name: "after command", args: []string{"version", "--color", "never"}},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			var out bytes.Buffer

			if err := run(current.args, &out); err != nil {
				t.Fatalf("run version: %v", err)
			}
			if out.String() != "pk dev\n" {
				t.Fatalf("unexpected version output %q", out.String())
			}
		})
	}
}

func TestRunRejectsInvalidColorMode(t *testing.T) {
	err := run([]string{"--color=sometimes", "version"}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected invalid color mode error")
	}
}

func TestRunPrintsInjectedVersion(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() {
		version = oldVersion
	})
	version = "v1.2.3"
	var out bytes.Buffer

	err := run([]string{"--version"}, &out)
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if out.String() != "pk v1.2.3\n" {
		t.Fatalf("unexpected version output %q", out.String())
	}
}

func TestRunWithoutArgsWritesHelp(t *testing.T) {
	deps := commandDeps(t)
	var out bytes.Buffer

	err := run(nil, &out)
	if err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(out.String(), "pk tracks local terminal sessions") {
		t.Fatalf("unexpected help output %q", out.String())
	}
	if deps.cfg != nil {
		t.Fatal("expected help not to start the monitor")
	}
}

func TestRunWritesCommandHelp(t *testing.T) {
	commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"cleanup", "--help"}, &out)
	if err != nil {
		t.Fatalf("run cleanup help: %v", err)
	}
	if !strings.Contains(out.String(), "Usage: pk cleanup") {
		t.Fatalf("unexpected help output %q", out.String())
	}
}

func TestRunWritesCommandHelpAfterOptions(t *testing.T) {
	commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"cleanup", "--apply", "--help"}, &out)
	if err != nil {
		t.Fatalf("run cleanup help: %v", err)
	}
	if !strings.Contains(out.String(), "Usage: pk cleanup") {
		t.Fatalf("unexpected help output %q", out.String())
	}
}

func TestRunRejectsUnknownHelpTopic(t *testing.T) {
	commandDeps(t)

	err := run([]string{"help", "missing"}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected unknown help topic error")
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

func TestApplicationCancellationStopsScan(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.requireCanceled = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer

	app, args, err := newApplication(ctx, []string{"scan"}, strings.NewReader(""), &out, io.Discard)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	err = app.run(args)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled scan, got %v", err)
	}
}

func TestApplicationCancellationStopsInstall(t *testing.T) {
	deps := commandDeps(t)
	deps.background.installStarted = make(chan struct{})
	deps.background.waitForCancellation = true
	ctx, cancel := context.WithCancel(context.Background())
	app, args := testApplication(t, ctx, "install", "--apply")
	finished := make(chan error, 1)
	go func() { finished <- app.run(args) }()
	assertCanceled(t, deps.background.installStarted)
	cancel()
	if err := waitForCommand(t, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled install, got %v", err)
	}
}

func testApplication(t *testing.T, ctx context.Context, args ...string) (application, []string) {
	t.Helper()
	app, commandArgs, err := newApplication(
		ctx, args, strings.NewReader(""), io.Discard, io.Discard,
	)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	return app, commandArgs
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

func TestRunCleanupContainersScopeSkipsProcessScan(t *testing.T) {
	deps := commandDeps(t)
	deps.scanner.err = errors.New("scan should not run")
	var out bytes.Buffer

	err := run([]string{"cleanup", "--scope", "containers"}, &out)
	if err != nil {
		t.Fatalf("run container cleanup: %v", err)
	}
	if deps.scanner.called {
		t.Fatal("expected process scan to be skipped")
	}
}

func TestRunCleanupProcessesScopeSkipsDocker(t *testing.T) {
	deps := commandDeps(t)
	deps.docker.available = true
	deps.docker.err = errors.New("docker should not run")
	var out bytes.Buffer

	err := run([]string{"cleanup", "--scope", "processes"}, &out)
	if err != nil {
		t.Fatalf("run process cleanup: %v", err)
	}
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

func TestRunCleanupIgnoresUnavailableDockerDaemon(t *testing.T) {
	deps := commandDeps(t)
	deps.docker.available = true
	message := "Cannot connect to the Docker daemon. Is the docker daemon running?"
	deps.docker.err = errors.New(message)
	var out bytes.Buffer

	err := run([]string{"cleanup"}, &out)
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if out.String() != "No cleanup targets found.\n" {
		t.Fatalf("unexpected cleanup output %q", out.String())
	}
}

func TestRunCleanupReturnsDockerListErrors(t *testing.T) {
	deps := commandDeps(t)
	deps.docker.available = true
	deps.docker.err = errors.New("permission denied")
	var out bytes.Buffer

	err := run([]string{"cleanup"}, &out)

	if err == nil {
		t.Fatal("expected docker list error")
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
	if options.scope != cleanupScopeAll {
		t.Fatalf("expected all scope, got %q", options.scope)
	}
}

func TestCleanupConfigRejectsUnknownScope(t *testing.T) {
	_, _, err := cleanupConfig([]string{"--scope", "unknown"})

	if err == nil {
		t.Fatal("expected invalid scope error")
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

	err := run([]string{"install", "--apply"}, &out)
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

func TestRunInstallRequiresApply(t *testing.T) {
	deps := commandDeps(t)

	err := run([]string{"install"}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected install to require apply")
	}
	if deps.background.installed {
		t.Fatal("expected background service not to be installed")
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

func TestRunObsWritesLifecycleState(t *testing.T) {
	deps := commandDeps(t)
	setupObsState(deps)
	var out bytes.Buffer

	err := run([]string{"obs"}, &out)
	if err != nil {
		t.Fatalf("run obs: %v", err)
	}
	assertMainOutputContains(t, out.String(), "daemon: active")
	assertMainOutputContains(t, out.String(), "sessions: 1 active, 1 ended")
	assertMainOutputContains(t, out.String(), "managed processes: 1")
	assertMainOutputContains(t, out.String(), "lifecycle events: 1")
	assertMainOutputContains(t, out.String(), "tabs: 1 active, 0 ended")
	assertMainOutputContains(t, out.String(), "windows: 0 active, 1 ended")
	assertMainOutputContains(t, out.String(), "last decision: session-ended: ended")
}

func setupObsState(deps *commandTestDeps) {
	deps.background.status = "active"
	deps.lifecycle.state = lifecycle.State{
		Sessions:     obsSessions(),
		Tabs:         obsTabs(),
		Windows:      obsWindows(),
		Processes:    obsProcesses(),
		Daemon:       obsDaemonState(),
		LastDecision: "session-ended: ended",
	}
	deps.lifecycle.events = []lifecycle.Event{{EventID: "event"}}
}

func obsSessions() map[string]lifecycle.TerminalSession {
	return map[string]lifecycle.TerminalSession{
		"active": {ID: "active", Exists: true, Active: true},
		"ended":  {ID: "ended", EndedAt: time.Now()},
	}
}

func obsProcesses() map[string]lifecycle.ManagedProcess {
	return map[string]lifecycle.ManagedProcess{
		"42:123": {Name: "node"},
	}
}

func obsTabs() map[string]lifecycle.Presence {
	return map[string]lifecycle.Presence{
		"tab": {ID: "tab", Exists: true, Active: true},
	}
}

func obsWindows() map[string]lifecycle.Presence {
	return map[string]lifecycle.Presence{
		"window": {ID: "window", EndedAt: time.Now()},
	}
}

func obsDaemonState() lifecycle.DaemonState {
	return lifecycle.DaemonState{
		LastTickAt: time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC),
	}
}

func TestRunSessionIDWritesID(t *testing.T) {
	commandDeps(t)
	var out bytes.Buffer

	err := run([]string{"__session-id"}, &out)
	if err != nil {
		t.Fatalf("run session id: %v", err)
	}
	if len(strings.TrimSpace(out.String())) != 32 {
		t.Fatalf("expected hex session id, got %q", out.String())
	}
}

func TestRunSessionAppendsLifecycleEvent(t *testing.T) {
	deps := commandDeps(t)
	var out bytes.Buffer

	err := run(sessionArgs(), &out)
	if err != nil {
		t.Fatalf("run session event: %v", err)
	}
	event := deps.lifecycle.appended[0]
	assertSessionEventIdentity(t, event)
	if event.ShellCreateTime != 1234 {
		t.Fatalf("expected hydrated shell create time, got %d", event.ShellCreateTime)
	}
	assertSessionEventExitCode(t, event)
}

func TestRunSessionRejectsOutOfRangePID(t *testing.T) {
	commandDeps(t)
	var out bytes.Buffer
	args := sessionArgs()
	args = append(args, "--process-pid", "2147483648")

	err := run(args, &out)
	if err == nil {
		t.Fatal("expected pid range error")
	}
	if !strings.Contains(err.Error(), "process pid out of range") {
		t.Fatalf("expected pid range error, got %v", err)
	}
}

func assertSessionEventExitCode(t *testing.T, event lifecycle.Event) {
	t.Helper()
	missingExitCode := event.ExitCode == nil
	wrongExitCode := !missingExitCode && *event.ExitCode != 7
	invalidExitCode := missingExitCode || wrongExitCode
	if invalidExitCode {
		t.Fatalf("expected exit code, got %#v", event.ExitCode)
	}
}

func assertSessionEventIdentity(t *testing.T, event lifecycle.Event) {
	t.Helper()
	wrongTabID := event.TabID != "tab"
	wrongWindowID := event.WindowID != "window"
	wrongIdentity := wrongTabID || wrongWindowID
	if wrongIdentity {
		t.Fatalf("expected tab/window ids, got %#v", event)
	}
}

func sessionArgs() []string {
	return []string{
		"__session",
		"--kind", lifecycle.KindCommandFinish,
		"--source", "zsh",
		"--terminal-session-id", "session",
		"--tab-id", "tab",
		"--window-id", "window",
		"--agent-session-id", "agent",
		"--user-session-id", "user",
		"--shell-pid", "42",
		"--exit-code", "7",
	}
}

func TestRunDaemonUsesRunner(t *testing.T) {
	deps := commandDeps(t)

	err := run([]string{"__daemon", "-interval", "1ms"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run daemon: %v", err)
	}
	if !deps.runner.called {
		t.Fatal("expected daemon runner")
	}
	if deps.cfg.Interval != time.Millisecond {
		t.Fatalf("expected one millisecond interval, got %s", deps.cfg.Interval)
	}
}

func TestRunDoctorWritesShareableDiagnostics(t *testing.T) {
	deps := commandDeps(t)
	deps.background.status = "active"
	deps.docker.available = true
	deps.audit.events = []audit.Event{{}, {}}
	var out bytes.Buffer

	err := run([]string{"doctor"}, &out)
	if err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	assertMainOutputContains(t, out.String(), "version: dev")
	assertMainOutputContains(t, out.String(), "background service: active")
	assertMainOutputContains(t, out.String(), "docker CLI: available")
	assertMainOutputContains(t, out.String(), "audit log: readable (2 events)")
}

func TestRunDoctorKeepsCheckErrorsPrivate(t *testing.T) {
	deps := commandDeps(t)
	deps.background.err = errors.New("private service path")
	deps.audit.err = errors.New("private audit path")
	var out bytes.Buffer

	if err := run([]string{"doctor"}, &out); err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	assertMainOutputContains(t, out.String(), "background service: error")
	assertMainOutputContains(t, out.String(), "audit log: unreadable")
	if strings.Contains(out.String(), "private") {
		t.Fatalf("doctor leaked check error: %q", out.String())
	}
}

func assertMainOutputContains(t *testing.T, output string, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Fatalf("expected %q in output: %q", expected, output)
	}
}

func TestRunInstallReturnsManagerErrors(t *testing.T) {
	deps := commandDeps(t)
	deps.backgroundErr = errors.New("manager failed")
	var out bytes.Buffer

	err := run([]string{"install", "--apply"}, &out)

	if !errors.Is(err, deps.backgroundErr) {
		t.Fatalf("expected manager error, got %v", err)
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
	if deps.monitorOptions.apply {
		t.Fatal("expected monitor to default to preview")
	}
}

func TestRunMonitorApplyUsesActiveMode(t *testing.T) {
	deps := commandDeps(t)

	err := run([]string{"monitor", "--apply"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run monitor: %v", err)
	}
	if !deps.monitorOptions.apply {
		t.Fatal("expected active monitor mode")
	}
}

func TestNewMonitorReturnsMonitor(t *testing.T) {
	commandDeps(t)

	monitor := newMonitor(&config.Config{}, monitorOptions{}, nil)

	if monitor == nil {
		t.Fatal("expected monitor")
	}
}

func TestNotifyKilledSendsNotification(t *testing.T) {
	deps := commandDeps(t)

	if err := notifyKilled("node", 42); err != nil {
		t.Fatalf("notify killed: %v", err)
	}

	if deps.notificationTitle != "pk" {
		t.Fatalf("expected notification title, got %q", deps.notificationTitle)
	}
	if !strings.Contains(deps.notificationMessage, "PID 42") {
		t.Fatalf("unexpected notification message %q", deps.notificationMessage)
	}
}

func TestNotifyKilledReturnsNotificationErrors(t *testing.T) {
	commandDeps(t)
	expected := errors.New("notification failed")
	sendNotification = func(title string, message string) error {
		return expected
	}

	if err := notifyKilled("node", 42); !errors.Is(err, expected) {
		t.Fatalf("expected notification error, got %v", err)
	}
}

func TestExitOnErrorIgnoresExpectedErrors(t *testing.T) {
	oldExitProcess := exitProcess
	defer func() {
		exitProcess = oldExitProcess
	}()
	exited := false
	exitProcess = func(int) {
		exited = true
	}

	exitOnError(nil)
	exitOnError(context.Canceled)

	if exited {
		t.Fatal("expected no exit for expected errors")
	}
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

func TestHandleSignalsCancelsAndRestoresDefaults(t *testing.T) {
	saved := saveCommandDeps()
	t.Cleanup(saved.restore)
	harness := newSignalHarness()
	harness.install()
	canceled := make(chan struct{})
	restore := handleSignals(func() { close(canceled) })
	t.Cleanup(restore)
	harness.signalChannel <- syscall.SIGTERM
	assertCanceled(t, canceled)
	assertCanceled(t, harness.stopped)
	if len(harness.reset) != 2 {
		t.Fatalf("expected restored signals, got %#v", harness.reset)
	}
}

type signalHarness struct {
	signalChannel chan<- os.Signal
	stopped       chan struct{}
	reset         []os.Signal
}

func newSignalHarness() *signalHarness {
	return &signalHarness{stopped: make(chan struct{})}
}

func (h *signalHarness) install() {
	notifySignal = func(channel chan<- os.Signal, signals ...os.Signal) {
		h.signalChannel = channel
	}
	stopSignal = func(chan<- os.Signal) { close(h.stopped) }
	resetSignals = func(signals ...os.Signal) { h.reset = signals }
}

func TestRunRejectsImplicitMonitorFlags(t *testing.T) {
	deps := commandDeps(t)

	err := run([]string{"-cpu", "90"}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected top-level flag error")
	}
	if deps.cfg != nil {
		t.Fatal("expected top-level flags not to start the monitor")
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
	reports         []scan.Report
	err             error
	called          bool
	requireCanceled bool
}

func (s *fakeScanner) Scan(ctx context.Context) ([]scan.Report, error) {
	s.called = true
	if s.requireCanceled {
		return nil, ctx.Err()
	}
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

func (k *fakeCommandKiller) Kill(ctx context.Context, target process.Process) error {
	k.called = true
	k.pid = target.PID
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
	err    error
	called bool
}

func (r *fakeRunner) Run(ctx context.Context) error {
	r.called = true
	return r.err
}

type fakeBackgroundManager struct {
	installed           bool
	uninstalled         bool
	status              string
	err                 error
	installStarted      chan struct{}
	waitForCancellation bool
}

func (m *fakeBackgroundManager) Install(ctx context.Context) error {
	m.installed = true
	if m.installStarted != nil {
		close(m.installStarted)
	}
	if m.waitForCancellation {
		<-ctx.Done()
		return ctx.Err()
	}
	return m.err
}

func (m *fakeBackgroundManager) Uninstall(context.Context) error {
	m.uninstalled = true
	return m.err
}

func (m *fakeBackgroundManager) Status(context.Context) (string, error) {
	return m.status, m.err
}

type fakeLifecycleStore struct {
	events       []lifecycle.Event
	appended     []lifecycle.Event
	state        lifecycle.State
	acknowledged bool
	err          error
}

func newFakeLifecycleStore() *fakeLifecycleStore {
	state := lifecycle.State{}
	state.Ensure()
	return &fakeLifecycleStore{state: state}
}

func (s *fakeLifecycleStore) Append(event lifecycle.Event) error {
	if s.err != nil {
		return s.err
	}
	s.appended = append(s.appended, event)
	return nil
}

func (s *fakeLifecycleStore) Events() ([]lifecycle.Event, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

func (s *fakeLifecycleStore) TakeEvents() ([]lifecycle.Event, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

func (s *fakeLifecycleStore) AcknowledgeEvents() error {
	if s.err != nil {
		return s.err
	}
	s.acknowledged = true
	return nil
}

func (s *fakeLifecycleStore) LoadState() (lifecycle.State, error) {
	if s.err != nil {
		return lifecycle.State{}, s.err
	}
	return s.state, nil
}

func (s *fakeLifecycleStore) SaveState(state lifecycle.State) error {
	if s.err != nil {
		return s.err
	}
	s.state = state
	return nil
}

type commandTestDeps struct {
	scanner             *fakeScanner
	audit               *fakeAuditStore
	auditStoreErr       error
	lifecycle           *fakeLifecycleStore
	lifecycleStoreErr   error
	killer              *fakeCommandKiller
	docker              *fakeDockerClient
	runner              *fakeRunner
	background          *fakeBackgroundManager
	backgroundErr       error
	cfg                 *config.Config
	monitorOptions      monitorOptions
	readCreateTimePID   int32
	notificationTitle   string
	notificationMessage string
}

func commandDeps(t *testing.T) *commandTestDeps {
	t.Helper()
	deps := &commandTestDeps{}
	deps.scanner = &fakeScanner{}
	deps.audit = &fakeAuditStore{}
	deps.lifecycle = newFakeLifecycleStore()
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
	newLifecycleStore = func() (lifecycleStore, error) {
		return deps.lifecycle, deps.lifecycleStoreErr
	}
	newProcessKiller = func() killer.Killer { return deps.killer }
	newDockerClient = func() docker.Client { return deps.docker }
	newMonitorRunner = func(
		cfg *config.Config,
		options monitorOptions,
		logger *slog.Logger,
	) monitorRunner {
		deps.cfg = cfg
		deps.monitorOptions = options
		return deps.runner
	}
	newDaemonRunner = func(
		cfg *config.Config,
		store daemon.Store,
		log daemon.Audit,
	) daemonRunner {
		deps.cfg = cfg
		return deps.runner
	}
	newBackgroundManager = func() (backgroundManager, error) {
		return deps.background, deps.backgroundErr
	}
	readCreateTime = func(ctx context.Context, pid int32) (int64, error) {
		deps.readCreateTimePID = pid
		return 1234, nil
	}
	sendNotification = func(title string, message string) error {
		deps.notificationTitle = title
		deps.notificationMessage = message
		return nil
	}
	handleShutdownSignal = func(cancel context.CancelFunc) func() { return func() {} }
}

type savedCommandDeps struct {
	newLister        func() process.Lister
	newScanner       func(*config.Config, process.Lister) processScanner
	newAudit         func() (auditStore, error)
	newLifecycle     func() (lifecycleStore, error)
	newKiller        func() killer.Killer
	newDocker        func() docker.Client
	newRunner        func(*config.Config, monitorOptions, *slog.Logger) monitorRunner
	newDaemon        func(*config.Config, daemon.Store, daemon.Audit) daemonRunner
	newBackground    func() (backgroundManager, error)
	readCreateTime   func(context.Context, int32) (int64, error)
	send             func(string, string) error
	handleSignalFunc func(context.CancelFunc) func()
	notifySignalFunc func(chan<- os.Signal, ...os.Signal)
	stopSignalFunc   func(chan<- os.Signal)
	resetSignalsFunc func(...os.Signal)
	exitFunc         func(int)
}

func saveCommandDeps() savedCommandDeps {
	return savedCommandDeps{
		newLister:        newProcessLister,
		newScanner:       newProcessScanner,
		newAudit:         newAuditStore,
		newLifecycle:     newLifecycleStore,
		newKiller:        newProcessKiller,
		newDocker:        newDockerClient,
		newRunner:        newMonitorRunner,
		newDaemon:        newDaemonRunner,
		newBackground:    newBackgroundManager,
		readCreateTime:   readCreateTime,
		send:             sendNotification,
		handleSignalFunc: handleShutdownSignal,
		notifySignalFunc: notifySignal,
		stopSignalFunc:   stopSignal,
		resetSignalsFunc: resetSignals,
		exitFunc:         exitProcess,
	}
}

func (d savedCommandDeps) restore() {
	newProcessLister = d.newLister
	newProcessScanner = d.newScanner
	newAuditStore = d.newAudit
	newLifecycleStore = d.newLifecycle
	newProcessKiller = d.newKiller
	newDockerClient = d.newDocker
	newMonitorRunner = d.newRunner
	newDaemonRunner = d.newDaemon
	newBackgroundManager = d.newBackground
	readCreateTime = d.readCreateTime
	sendNotification = d.send
	handleShutdownSignal = d.handleSignalFunc
	notifySignal = d.notifySignalFunc
	stopSignal = d.stopSignalFunc
	resetSignals = d.resetSignalsFunc
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

func waitForCommand(t *testing.T, finished <-chan error) error {
	t.Helper()
	select {
	case err := <-finished:
		return err
	case <-time.After(time.Second):
		t.Fatal("expected command to finish")
		return nil
	}
}
