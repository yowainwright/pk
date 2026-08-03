package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLaunchdWritesPlistAndStartsService(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "darwin", runner)

	if err := manager.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	data := readServiceFile(t, manager)
	assertContains(t, data, launchdLabel)
	assertContains(t, data, "com.yowainwright.pk")
	assertContains(t, data, "/bin/pk")
	assertContains(t, data, "--watch")
	assertServiceMode(t, manager)
	assertCommands(t, runner, "launchctl bootout")
	assertCommands(t, runner, "launchctl bootstrap")
	assertCommands(t, runner, "launchctl kickstart")
}

func TestInstallLaunchdReturnsBootstrapErrors(t *testing.T) {
	runner := &fakeRunner{failAt: 2, failErr: errors.New("bootstrap failed")}
	manager := testManager(t, "darwin", runner)

	if err := manager.Install(context.Background()); err == nil {
		t.Fatal("expected bootstrap error")
	}
	assertMissingServiceFile(t, manager)
}

func TestInstallSystemdWritesUnitAndStartsService(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "linux", runner)

	if err := manager.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	data := readServiceFile(t, manager)
	assertContains(t, data, `ExecStart="/bin/pk" "cleanup" "--apply" "--watch"`)
	assertServiceMode(t, manager)
	assertCommands(t, runner, "systemctl --user daemon-reload")
	assertCommands(t, runner, "systemctl --user enable --now pk.service")
}

func assertServiceMode(t *testing.T, manager *Manager) {
	t.Helper()
	info, err := os.Stat(manager.servicePath())
	if err != nil {
		t.Fatalf("stat service file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("service file mode: got %o, want 600", info.Mode().Perm())
	}
}

func TestInstallSystemdReturnsReloadErrors(t *testing.T) {
	runner := &fakeRunner{failAt: 1, failErr: errors.New("reload failed")}
	manager := testManager(t, "linux", runner)

	if err := manager.Install(context.Background()); err == nil {
		t.Fatal("expected reload error")
	}
	assertMissingServiceFile(t, manager)
}

func TestCanceledSystemdInstallRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{cancelAt: 2, cancel: cancel}
	manager := testManager(t, "linux", runner)
	err := manager.Install(ctx)
	assertCanceled(t, err)
	assertMissingServiceFile(t, manager)
	assertCommandCount(t, runner, "systemctl --user daemon-reload", 2)
}

func TestCanceledLaunchdInstallRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{cancelAt: 2, cancel: cancel}
	manager := testManager(t, "darwin", runner)
	err := manager.Install(ctx)
	assertCanceled(t, err)
	assertMissingServiceFile(t, manager)
	assertCommandCount(t, runner, "launchctl bootout", 2)
}

func TestUninstallLaunchdRemovesPlist(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "darwin", runner)
	if err := manager.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	assertMissingServiceFile(t, manager)
	assertCommands(t, runner, "launchctl bootout")
}

func TestUninstallSystemdRemovesUnit(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "linux", runner)
	if err := manager.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	assertMissingServiceFile(t, manager)
}

func TestCanceledSystemdUninstallFinishesReload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{}
	manager := testManager(t, "linux", runner)
	writeTestServiceFile(t, manager)
	runner.cancelAt = 2
	runner.cancel = cancel
	err := manager.Uninstall(ctx)
	assertCanceled(t, err)
	assertMissingServiceFile(t, manager)
	assertCommandCount(t, runner, "systemctl --user daemon-reload", 2)
}

func TestCanceledLaunchdUninstallRestoresService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{cancelAt: 1, cancel: cancel}
	manager := testManager(t, "darwin", runner)
	writeTestServiceFile(t, manager)
	err := manager.Uninstall(ctx)
	assertCanceled(t, err)
	assertServiceMode(t, manager)
	assertCommands(t, runner, "launchctl bootstrap")
	assertCommands(t, runner, "launchctl kickstart")
}

func TestStatusReportsNotInstalled(t *testing.T) {
	manager := testManager(t, "linux", &fakeRunner{})

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "not installed" {
		t.Fatalf("expected not installed, got %q", status)
	}
}

func TestStatusReportsActiveService(t *testing.T) {
	runner := &fakeRunner{output: []byte("active\n")}
	manager := testManager(t, "linux", runner)
	writeTestServiceFile(t, manager)

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "active" {
		t.Fatalf("expected active, got %q", status)
	}
}

func TestStatusUsesLaunchdOnDarwin(t *testing.T) {
	runner := &fakeRunner{output: []byte("service = enabled\n")}
	manager := testManager(t, "darwin", runner)
	writeTestServiceFile(t, manager)

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "service = enabled" {
		t.Fatalf("expected launchd output, got %q", status)
	}
	assertCommands(t, runner, "launchctl print")
}

func TestStatusReportsInstalledButStopped(t *testing.T) {
	runner := &fakeRunner{err: errors.New("inactive")}
	manager := testManager(t, "linux", runner)
	writeTestServiceFile(t, manager)

	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "installed but not running" {
		t.Fatalf("expected stopped status, got %q", status)
	}
}

func TestStatusReturnsCancellation(t *testing.T) {
	runner := &fakeRunner{err: context.Canceled}
	manager := testManager(t, "linux", runner)
	writeTestServiceFile(t, manager)
	_, err := manager.Status(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled status, got %v", err)
	}
}

func TestUnsupportedPlatformReturnsError(t *testing.T) {
	manager := testManager(t, "windows", &fakeRunner{})

	if err := manager.Install(context.Background()); err == nil {
		t.Fatal("expected unsupported install error")
	}
	if _, err := manager.Status(context.Background()); err == nil {
		t.Fatal("expected unsupported status error")
	}
	if err := manager.Uninstall(context.Background()); err == nil {
		t.Fatal("expected unsupported uninstall error")
	}
}

func TestQuoteSystemdArgEscapesSpecialCharacters(t *testing.T) {
	quoted := quoteSystemdArg(`/tmp/pk "dev"`)

	if quoted != `"/tmp/pk \"dev\""` {
		t.Fatalf("unexpected quoted arg %q", quoted)
	}
}

func TestCommandRunnerRunsCommands(t *testing.T) {
	runner := commandRunner{}

	if err := runner.Run(context.Background(), "true"); err != nil {
		t.Fatalf("run true: %v", err)
	}
	if _, err := runner.Output(context.Background(), "true"); err != nil {
		t.Fatalf("output true: %v", err)
	}
}

func TestCommandRunnerStopsCanceledCommands(t *testing.T) {
	runner := commandRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.Run(ctx, "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled run, got %v", err)
	}
	if _, err := runner.Output(ctx, "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled output, got %v", err)
	}
}

type fakeRunner struct {
	commands []string
	output   []byte
	err      error
	cancelAt int
	cancel   context.CancelFunc
	failAt   int
	failErr  error
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	r.commands = append(r.commands, commandString(name, args))
	if len(r.commands) == r.cancelAt {
		r.cancel()
		return context.Canceled
	}
	if len(r.commands) == r.failAt {
		return r.failErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.err
}

func (r *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, commandString(name, args))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.output, r.err
}

func testManager(t *testing.T, goos string, runner Runner) *Manager {
	t.Helper()
	return NewManager(goos, t.TempDir(), "/bin/pk", "501", runner)
}

func readServiceFile(t *testing.T, manager *Manager) string {
	t.Helper()
	data, err := os.ReadFile(manager.servicePath())
	if err != nil {
		t.Fatalf("read service file: %v", err)
	}
	return string(data)
}

func writeTestServiceFile(t *testing.T, manager *Manager) {
	t.Helper()
	if err := ensureDir(filepath.Dir(manager.servicePath())); err != nil {
		t.Fatalf("create service dir: %v", err)
	}
	if err := writeFile(manager.servicePath(), []byte("service")); err != nil {
		t.Fatalf("write service file: %v", err)
	}
}

func assertMissingServiceFile(t *testing.T, manager *Manager) {
	t.Helper()
	_, err := os.Stat(manager.servicePath())
	if !os.IsNotExist(err) {
		t.Fatalf("expected service file removed, got %v", err)
	}
}

func commandString(name string, args []string) string {
	parts := append([]string{name}, args...)
	return strings.Join(parts, " ")
}

func assertContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q in %q", expected, value)
	}
}

func assertCommands(t *testing.T, runner *fakeRunner, expected string) {
	t.Helper()
	for _, command := range runner.commands {
		if strings.HasPrefix(command, expected) {
			return
		}
	}
	t.Fatalf("expected command %q in %#v", expected, runner.commands)
}

func assertCommandCount(t *testing.T, runner *fakeRunner, expected string, want int) {
	t.Helper()
	count := 0
	for _, command := range runner.commands {
		if strings.HasPrefix(command, expected) {
			count++
		}
	}
	if count != want {
		t.Fatalf("command %q count: got %d, want %d", expected, count, want)
	}
}

func assertCanceled(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
