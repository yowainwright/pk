package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFailedResumeDoesNotInstallShellHook(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			runner := &fakeRunner{}
			manager := testManager(t, goos, runner)
			requireNoError(t, manager.Install(t.Context()))
			requireNoError(t, manager.shellInstaller().Uninstall())
			runner.err = errors.New("native resume failed")
			if err := manager.Install(t.Context()); !errors.Is(err, runner.err) {
				t.Fatalf("expected native resume failure, got %v", err)
			}
			assertShellPluginRemoved(t, manager)
		})
	}
}

func TestFailedResumePreservesExistingShellHook(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			runner := &fakeRunner{}
			manager := testManager(t, goos, runner)
			requireNoError(t, manager.Install(t.Context()))
			plugin := manager.shellInstaller().PluginPath()
			previous := "# previous plugin\n"
			requireNoError(t, os.WriteFile(plugin, []byte(previous), 0o600))
			runner.err = errors.New("native resume failed")
			if err := manager.Install(t.Context()); !errors.Is(err, runner.err) {
				t.Fatalf("expected native resume failure, got %v", err)
			}
			assertFileContents(t, plugin, previous)
		})
	}
}

func TestResumeWaitsForRunningServiceBeforeInstallingShell(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			runner := &fakeRunner{}
			manager := testManager(t, goos, runner)
			requireNoError(t, manager.Install(t.Context()))
			requireNoError(t, manager.shellInstaller().Uninstall())
			runner.output = []byte("inactive\n")
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			defer cancel()
			if err := manager.Install(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected startup timeout, got %v", err)
			}
			assertShellPluginRemoved(t, manager)
		})
	}
}

func TestResumeRecoversServiceAfterShellInstallFailure(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			runner := &fakeRunner{}
			manager := testManager(t, goos, runner)
			requireNoError(t, manager.Install(t.Context()))
			previous := readServiceFile(t, manager)
			runner.commands = nil
			manager.zdotdir = filepath.Join(manager.home, "blocked")
			requireNoError(t, os.WriteFile(manager.zdotdir, []byte("file"), 0o600))
			if err := manager.Install(t.Context()); err == nil {
				t.Fatal("expected shell install failure")
			}
			assertFileContents(t, manager.servicePath(), previous)
			assertResumeRecovery(t, runner, goos)
		})
	}
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	if string(data) != want {
		t.Fatalf("changed %s: got %q, want %q", path, data, want)
	}
}

func assertResumeRecovery(t *testing.T, runner *fakeRunner, goos string) {
	t.Helper()
	if goos == "darwin" {
		assertCommands(t, runner, "launchctl bootout")
		assertCommandCount(t, runner, "launchctl kickstart", 2)
		return
	}
	assertCommands(t, runner, "systemctl --user disable --now pk.service")
	assertCommandCount(t, runner, "systemctl --user enable --now pk.service", 2)
}
