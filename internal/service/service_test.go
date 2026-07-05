package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLaunchdWritesPlistAndStartsService(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "darwin", runner)

	if err := manager.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	data := readServiceFile(t, manager)
	assertContains(t, data, launchdLabel)
	assertContains(t, data, "com.yowainwright.pk")
	assertContains(t, data, "/bin/pk")
	assertContains(t, data, "--watch")
	assertCommands(t, runner, "launchctl bootout")
	assertCommands(t, runner, "launchctl bootstrap")
	assertCommands(t, runner, "launchctl kickstart")
}

func TestInstallLaunchdReturnsBootstrapErrors(t *testing.T) {
	runner := &fakeRunner{err: errors.New("bootstrap failed")}
	manager := testManager(t, "darwin", runner)

	if err := manager.Install(); err == nil {
		t.Fatal("expected bootstrap error")
	}
}

func TestInstallSystemdWritesUnitAndStartsService(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "linux", runner)

	if err := manager.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	data := readServiceFile(t, manager)
	assertContains(t, data, `ExecStart="/bin/pk" "cleanup" "--apply" "--watch"`)
	assertCommands(t, runner, "systemctl --user daemon-reload")
	assertCommands(t, runner, "systemctl --user enable --now pk.service")
}

func TestInstallSystemdReturnsReloadErrors(t *testing.T) {
	runner := &fakeRunner{err: errors.New("reload failed")}
	manager := testManager(t, "linux", runner)

	if err := manager.Install(); err == nil {
		t.Fatal("expected reload error")
	}
}

func TestUninstallLaunchdRemovesPlist(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "darwin", runner)
	if err := manager.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := manager.Uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	assertMissingServiceFile(t, manager)
	assertCommands(t, runner, "launchctl bootout")
}

func TestUninstallSystemdRemovesUnit(t *testing.T) {
	runner := &fakeRunner{}
	manager := testManager(t, "linux", runner)
	if err := manager.Install(); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := manager.Uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	assertMissingServiceFile(t, manager)
}

func TestStatusReportsNotInstalled(t *testing.T) {
	manager := testManager(t, "linux", &fakeRunner{})

	status, err := manager.Status()
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

	status, err := manager.Status()
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

	status, err := manager.Status()
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

	status, err := manager.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "installed but not running" {
		t.Fatalf("expected stopped status, got %q", status)
	}
}

func TestUnsupportedPlatformReturnsError(t *testing.T) {
	manager := testManager(t, "windows", &fakeRunner{})

	if err := manager.Install(); err == nil {
		t.Fatal("expected unsupported install error")
	}
	if _, err := manager.Status(); err == nil {
		t.Fatal("expected unsupported status error")
	}
	if err := manager.Uninstall(); err == nil {
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

	if err := runner.Run("true"); err != nil {
		t.Fatalf("run true: %v", err)
	}
	if _, err := runner.Output("true"); err != nil {
		t.Fatalf("output true: %v", err)
	}
}

type fakeRunner struct {
	commands []string
	output   []byte
	err      error
}

func (r *fakeRunner) Run(name string, args ...string) error {
	r.commands = append(r.commands, commandString(name, args))
	return r.err
}

func (r *fakeRunner) Output(name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, commandString(name, args))
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
