package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestZshPromptRecordsCommandCompletion(t *testing.T) {
	for _, exitCode := range []string{"0", "7"} {
		t.Run("exit_"+exitCode, func(t *testing.T) {
			installer, eventsPath := zshFixture(t)
			runZshPrompt(t, installer, eventsPath, exitCode)
			events := readFile(t, eventsPath)
			assertZshEventCount(t, events, "command.start", 1)
			assertZshEventCount(t, events, "command.finish", 1)
			assertZshEventCount(t, events, "session.inactive", 2)
			if !strings.Contains(events, "--exit-code "+exitCode+"\n") {
				t.Fatalf("missing command exit code %s:\n%s", exitCode, events)
			}
		})
	}
}

func zshFixture(t *testing.T) (Installer, string) {
	t.Helper()
	installer := testInstaller(t)
	installer.Executable = filepath.Join(installer.Home, "pk")
	if err := os.WriteFile(installer.Executable, []byte(zshFixtureBinary), 0o700); err != nil {
		t.Fatalf("writing fixture executable: %v", err)
	}
	if err := installer.Install(); err != nil {
		t.Fatalf("installing shell plugin: %v", err)
	}
	return installer, filepath.Join(installer.Home, "events")
}

func runZshPrompt(t *testing.T, installer Installer, eventsPath string, exitCode string) {
	t.Helper()
	runZshScript(t, installer, eventsPath, zshPromptScript, exitCode)
}

func runZshScript(
	t *testing.T,
	installer Installer,
	eventsPath string,
	script string,
	extra string,
) {
	t.Helper()
	zsh := requireZsh(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pluginPath := installer.PluginPath()
	args := []string{"-dfi", "-c", script, "pk-test", pluginPath, extra}
	command := exec.CommandContext(ctx, zsh, args...)
	command.Env = zshEnvironment(installer.Home, eventsPath)
	assertZshScript(t, command)
}

func zshEnvironment(home string, eventsPath string) []string {
	environment := zshTestEnvironment()
	eventLog := "PK_TEST_EVENT_LOG=" + eventsPath
	homeEntry := "HOME=" + home
	return append(environment, eventLog, homeEntry)
}

func requireZsh(t *testing.T) string {
	t.Helper()
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is required for shell integration tests")
	}
	return zsh
}

func assertZshScript(t *testing.T, command *exec.Cmd) {
	t.Helper()
	output, err := command.CombinedOutput()
	failed := err != nil || len(output) != 0
	if failed {
		t.Fatalf("running zsh prompt: %v\n%s", err, output)
	}
}

func TestLoadedZshHookStopsEmittingWhenDisabled(t *testing.T) {
	installer, eventsPath := zshFixture(t)
	script := "source \"$1\"\ncommand rm -- \"$1\"\n_pk_preexec\n_pk_precmd\n_pk_zshexit\n"
	runZshScript(t, installer, eventsPath, script, "")
	events := readFile(t, eventsPath)
	assertZshEventCount(t, events, "session.start", 1)
	assertZshEventCount(t, events, "command.start", 0)
	assertZshEventCount(t, events, "session.stop", 0)
}

func TestFailedReinstallPreservesWorkingPlugin(t *testing.T) {
	installer := testInstaller(t)
	if err := installer.Install(); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, installer.PluginPath())
	installer.Executable = "/different/pk"
	installer.ZDOTDIR = filepath.Join(installer.Home, "blocked")
	if err := os.WriteFile(installer.ZDOTDIR, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installer.Install(); err == nil {
		t.Fatal("expected shell failure")
	}
	if readFile(t, installer.PluginPath()) != before {
		t.Fatal("lost original plugin")
	}
}

func zshTestEnvironment() []string {
	environment := make([]string, 0)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		inheritedSession := name == "PK_TERMINAL_SESSION_ID" || name == "PK_DISABLE_SESSION"
		if !inheritedSession {
			environment = append(environment, entry)
		}
	}
	return environment
}

func assertZshEventCount(t *testing.T, events string, kind string, expected int) {
	t.Helper()
	count := strings.Count(events, "--kind "+kind+" ")
	if count != expected {
		t.Fatalf("expected %d %s events, got %d:\n%s", expected, kind, count, events)
	}
}

const zshFixtureBinary = `#!/bin/sh
set -eu
if [ "$1" = "__session-id" ]; then
  printf '%s\n' test-session
  exit 0
fi
printf '%s\n' "$*" >> "$PK_TEST_EVENT_LOG"
`

const zshPromptScript = `source "$1"
_pk_preexec
(exit "$2")
_pk_precmd
_pk_precmd
`

func TestInstallWritesPluginAndZshrcLine(t *testing.T) {
	installer := testInstaller(t)

	if err := installer.Install(); err != nil {
		t.Fatalf("install shell plugin: %v", err)
	}

	assertFileContains(t, installer.PluginPath(), "/bin/pk")
	assertFileContains(t, installer.PluginPath(), "__session")
	assertFileContains(t, installer.PluginPath(), "--tab-id")
	assertFileContains(t, installer.PluginPath(), "--window-id")
	assertFileMissingText(t, installer.PluginPath(), "&!")
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

func TestInstallRemovesPluginWhenZshrcWriteFails(t *testing.T) {
	installer := testInstaller(t)
	installer.ZDOTDIR = filepath.Join(installer.Home, "blocked")
	if err := os.WriteFile(installer.ZDOTDIR, []byte("file"), 0o600); err != nil {
		t.Fatalf("writing blocked zdotdir: %v", err)
	}

	if err := installer.Install(); err == nil {
		t.Fatal("expected install failure")
	}

	assertFileMissing(t, installer.PluginPath())
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

func assertFileMissingText(t *testing.T, path string, unexpected string) {
	t.Helper()
	data := readFile(t, path)
	if strings.Contains(data, unexpected) {
		t.Fatalf("%s contains %q:\n%s", path, unexpected, data)
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

func TestInstallAndUninstallPreserveSymlinkedZshrc(t *testing.T) {
	for _, relative := range []bool{false, true} {
		t.Run(fmt.Sprint(relative), func(t *testing.T) {
			installer := testInstaller(t)
			original := "alias ll='ls -la'\n"
			target, link := symlinkedZshrc(t, installer, original, relative)
			if err := installer.Install(); err != nil {
				t.Fatal(err)
			}
			assertFileContains(t, target, installer.SourceLine())
			if err := installer.Uninstall(); err != nil {
				t.Fatal(err)
			}
			assertPreservedZshrc(t, installer, original, target, link)
		})
	}
}

func symlinkedZshrc(
	t *testing.T,
	installer Installer,
	original string,
	relative bool,
) (string, string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "zshrc")
	if err := os.WriteFile(target, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	link := zshrcLink(t, installer.Home, target, relative)
	if err := os.Symlink(link, installer.ZshrcPath()); err != nil {
		t.Fatal(err)
	}
	return target, link
}

func zshrcLink(t *testing.T, home string, target string, relative bool) string {
	t.Helper()
	if !relative {
		return target
	}
	link, err := filepath.Rel(home, target)
	if err != nil {
		t.Fatal(err)
	}
	return link
}

func assertPreservedZshrc(
	t *testing.T,
	installer Installer,
	original string,
	target string,
	link string,
) {
	t.Helper()
	if readFile(t, target) != original {
		t.Fatal("changed existing shell configuration")
	}
	got, err := os.Readlink(installer.ZshrcPath())
	intact := err == nil && got == link
	if !intact {
		t.Fatalf("replaced symlink: %q, %v", got, err)
	}
	assertMode(t, target, 0o640)
}

func TestInstalledPluginPreservesExecutableMetacharacters(t *testing.T) {
	installer := testInstaller(t)
	installer.Executable = filepath.Join(installer.Home, "pk '$HOME `literal` \"quoted\"")
	if err := os.WriteFile(installer.Executable, []byte(zshFixtureBinary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installer.Install(); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(installer.Home, "events")
	runZshScript(t, installer, events, `source "$1"`, "")
	assertZshEventCount(t, readFile(t, events), "session.start", 1)
}
