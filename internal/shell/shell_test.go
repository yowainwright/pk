package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWritesPluginAndZshrcLine(t *testing.T) {
	installer := testInstaller(t)

	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin: %v", err)
	}

	assertFileContains(t, installer.PluginPath(), "/bin/pk")
	assertFileContains(t, installer.PluginPath(), "__session")
	assertFileContains(t, installer.PluginPath(), "--tab-id")
	assertFileContains(t, installer.PluginPath(), "--window-id")
	assertFileContains(t, installer.ZshrcPath(), installer.SourceLine())
	assertMode(t, installer.PluginPath(), pluginMode)
}

func TestInstallIsIdempotent(t *testing.T) {
	installer := testInstaller(t)
	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin: %v", err)
	}
	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin again: %v", err)
	}

	data := readFile(t, installer.ZshrcPath())

	if strings.Count(data, installer.SourceLine()) != 1 {
		t.Fatalf("expected one source line, got:\n%s", data)
	}
}

func TestInstallPreservesExistingZshrcMode(t *testing.T) {
	installer := testInstaller(t)
	err := os.WriteFile(
		installer.ZshrcPath(),
		[]byte("alias ll='ls -la'\n"),
		0o644,
	)
	if err != nil {
		t.Fatalf("writing zshrc: %v", err)
	}

	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin: %v", err)
	}

	assertMode(t, installer.ZshrcPath(), 0o644)
}

func TestUninstallRemovesSourceLineAndPlugin(t *testing.T) {
	installer := testInstaller(t)
	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin: %v", err)
	}

	if err := installer.Uninstall(); err != nil {
		t.Fatalf("uninstall shell plugin: %v", err)
	}

	assertFileMissing(t, installer.PluginPath())
	data := readFile(t, installer.ZshrcPath())
	if strings.Contains(data, installer.SourceLine()) {
		t.Fatalf("source line still present:\n%s", data)
	}
}

func TestZDOTDIRSelectsZshrcLocation(t *testing.T) {
	zdotdir := t.TempDir()
	installer := testInstaller(t)
	installer.ZDOTDIR = zdotdir

	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin: %v", err)
	}

	expected := filepath.Join(zdotdir, ".zshrc")
	assertFileContains(t, expected, installer.SourceLine())
}

func testInstaller(t *testing.T) Installer {
	t.Helper()
	return Installer{
		Home:       t.TempDir(),
		Executable: "/bin/pk",
	}
}

func assertFileContains(t *testing.T, path string, expected string) {
	t.Helper()
	data := readFile(t, path)
	if !strings.Contains(data, expected) {
		t.Fatalf("%s missing %q:\n%s", path, expected, data)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected missing file %s, got %v", path, err)
	}
}

func assertMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != expected {
		t.Fatalf("expected mode %o, got %o", expected, info.Mode().Perm())
	}
}
