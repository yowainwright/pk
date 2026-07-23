//go:build e2e

package e2e_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const e2eVersion = "v9.8.7-e2e"

var (
	repositoryRoot string
	suiteDir       string
	pkBinary       string
)

type commandResult struct {
	stdout string
	stderr string
	err    error
}

type dockerFixture struct {
	auditPath string
	stopLog   string
}

func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

func runSuite(m *testing.M) int {
	if err := setupSuite(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	code := m.Run()
	err := os.RemoveAll(suiteDir)
	cleanupFailed := err != nil
	testsPassed := code == 0
	shouldReportCleanup := cleanupFailed && testsPassed
	if shouldReportCleanup {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}

func setupSuite() error {
	repositoryRoot = findRepositoryRoot()
	var err error
	suiteDir, err = os.MkdirTemp("", "pk-e2e-")
	if err != nil {
		return fmt.Errorf("creating suite directory: %w", err)
	}
	pkBinary = filepath.Join(suiteDir, "pk")
	return buildBinary("./cmd/pk", pkBinary, "-ldflags=-s -w -X main.version="+e2eVersion)
}

func findRepositoryRoot() string {
	_, currentFile, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(currentFile)
	return filepath.Clean(filepath.Join(testDir, "..", ".."))
}

func buildBinary(pkg string, output string, extraArgs ...string) error {
	args := []string{"build", "-o", output}
	args = append(args, extraArgs...)
	args = append(args, pkg)
	command := exec.Command("go", args...)
	command.Dir = repositoryRoot
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("building %s: %w\n%s", pkg, err, combined)
	}
	return nil
}

func runCLI(t *testing.T, args ...string) commandResult {
	t.Helper()
	command := exec.Command(pkBinary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func TestRootHelpIsSafe(t *testing.T) {
	result := runCLI(t)

	if result.err != nil {
		t.Fatalf("root help failed: %v\n%s", result.err, result.stderr)
	}
	if !strings.Contains(result.stdout, "Destructive commands require --apply") {
		t.Fatalf("unexpected root help:\n%s", result.stdout)
	}
}

func TestVersionUsesReleaseMetadata(t *testing.T) {
	result := runCLI(t, "--version")

	if result.err != nil {
		t.Fatalf("version failed: %v\n%s", result.err, result.stderr)
	}
	expected := "pk " + e2eVersion + "\n"
	if result.stdout != expected {
		t.Fatalf("expected %q, got %q", expected, result.stdout)
	}
}

func TestCommandHelpRoutes(t *testing.T) {
	cases := []struct {
		args     []string
		expected string
	}{
		{args: []string{"scan", "--help"}, expected: "Usage: pk scan"},
		{args: []string{"help", "cleanup"}, expected: "Usage: pk cleanup"},
		{args: []string{"monitor", "-h"}, expected: "Usage: pk monitor"},
		{args: []string{"skills", "install", "--help"}, expected: "pk skills install"},
	}
	for _, current := range cases {
		assertHelpRoute(t, current.args, current.expected)
	}
}

func assertHelpRoute(t *testing.T, args []string, expected string) {
	t.Helper()
	result := runCLI(t, args...)
	if result.err != nil {
		t.Fatalf("help %v failed: %v\n%s", args, result.err, result.stderr)
	}
	if !strings.Contains(result.stdout, expected) {
		t.Fatalf("help %v missing %q:\n%s", args, expected, result.stdout)
	}
}

func TestInstallRequiresExplicitApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result := runCLI(t, "install")

	assertExitCode(t, result, 1)
	if !strings.Contains(result.stderr, "install requires --apply") {
		t.Fatalf("unexpected install error:\n%s", result.stderr)
	}
	assertServiceFilesAbsent(t, home)
}

func assertExitCode(t *testing.T, result commandResult, expected int) {
	t.Helper()
	var exitError *exec.ExitError
	if !errors.As(result.err, &exitError) {
		t.Fatalf("expected exit code %d, got %v", expected, result.err)
	}
	if exitError.ExitCode() != expected {
		t.Fatalf("expected exit code %d, got %d", expected, exitError.ExitCode())
	}
}

func assertServiceFilesAbsent(t *testing.T, home string) {
	t.Helper()
	paths := []string{
		filepath.Join(home, "Library", "LaunchAgents", "com.yowainwright.pk.plist"),
		filepath.Join(home, ".config", "systemd", "user", "pk.service"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected service file %s", path)
		}
	}
}

func TestSkillInstallWritesBundledSkill(t *testing.T) {
	skillsRoot := t.TempDir()
	result := runCLI(t, "skills", "install", "--dir", skillsRoot)

	if result.err != nil {
		t.Fatalf("skill install failed: %v\n%s", result.err, result.stderr)
	}
	installed := filepath.Join(skillsRoot, "pk", "SKILL.md")
	if result.stdout != installed+"\n" {
		t.Fatalf("expected installed path %q, got %q", installed, result.stdout)
	}
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("reading installed skill: %v", err)
	}
	if !bytes.Contains(data, []byte("name: pk")) {
		t.Fatalf("unexpected installed skill:\n%s", data)
	}
}

func TestBackgroundServiceLifecycle(t *testing.T) {
	tool, servicePath := serviceFixture(t)
	home := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, tool), fakeServiceTool)
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)

	assertCommandOutput(t, []string{"install", "--apply"}, "installed\n")
	path := servicePath(home)
	assertFileContains(t, path, "cleanup", "--apply", "--watch")
	assertCommandOutput(t, []string{"status"}, "active\n")
	assertCommandOutput(t, []string{"uninstall"}, "uninstalled\n")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("service file still exists: %s", path)
	}
}

func serviceFixture(t *testing.T) (string, func(string) string) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		path := func(home string) string {
			return filepath.Join(home, "Library", "LaunchAgents", "com.yowainwright.pk.plist")
		}
		return "launchctl", path
	}
	if runtime.GOOS == "linux" {
		path := func(home string) string {
			return filepath.Join(home, ".config", "systemd", "user", "pk.service")
		}
		return "systemctl", path
	}
	t.Skip("background services are supported only on macOS and Linux")
	return "", nil
}

func assertCommandOutput(t *testing.T, args []string, expected string) {
	t.Helper()
	result := runCLI(t, args...)
	if result.err != nil {
		t.Fatalf("pk %v failed: %v\n%s", args, result.err, result.stderr)
	}
	if result.stdout != expected {
		t.Fatalf("pk %v: expected %q, got %q", args, expected, result.stdout)
	}
}

func assertFileContains(t *testing.T, path string, expected ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	for _, value := range expected {
		if !bytes.Contains(data, []byte(value)) {
			t.Fatalf("%s missing %q:\n%s", path, value, data)
		}
	}
}

func writeExecutable(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("writing executable %s: %v", path, err)
	}
}

const fakeServiceTool = `#!/bin/sh
case "$*" in
  *print*|*is-active*) printf '%s\n' active ;;
esac
`

func TestDockerCleanupApplyIsAudited(t *testing.T) {
	fixture := setupDockerFixture(t)

	args := []string{"cleanup", "--apply", "--scope", "containers"}
	result := runCLI(t, args...)
	if result.err != nil {
		t.Fatalf("docker cleanup failed: %v\n%s", result.err, result.stderr)
	}
	assertContains(t, result.stdout, "abc123\ttrue")
	assertFileContains(t, fixture.stopLog, "abc123")
	history := runCLI(t, "history")
	if history.err != nil {
		t.Fatalf("history failed: %v\n%s", history.err, history.stderr)
	}
	assertContains(t, history.stdout, `"target_type":"container"`)
	assertContains(t, history.stdout, `"applied":true`)
}

func TestDockerCleanupPreviewDoesNotStop(t *testing.T) {
	fixture := setupDockerFixture(t)

	result := runCLI(t, "cleanup", "--scope", "containers")
	if result.err != nil {
		t.Fatalf("docker preview failed: %v\n%s", result.err, result.stderr)
	}
	assertContains(t, result.stdout, "abc123\tfalse")
	if _, err := os.Stat(fixture.stopLog); !os.IsNotExist(err) {
		t.Fatalf("preview unexpectedly stopped a container: %s", fixture.stopLog)
	}
	assertFileContains(t, fixture.auditPath, `"applied":false`)
}

func setupDockerFixture(t *testing.T) dockerFixture {
	t.Helper()
	binDir := t.TempDir()
	fixture := dockerFixture{
		auditPath: filepath.Join(t.TempDir(), "events.jsonl"),
		stopLog:   filepath.Join(t.TempDir(), "stopped"),
	}
	writeExecutable(t, filepath.Join(binDir, "docker"), fakeDockerTool)
	t.Setenv("PATH", binDir)
	t.Setenv("PK_AUDIT_PATH", fixture.auditPath)
	t.Setenv("PK_E2E_DOCKER_LOG", fixture.stopLog)
	return fixture
}

func assertContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q in:\n%s", expected, value)
	}
}

const fakeDockerTool = `#!/bin/sh
if [ "$1" = container ] && [ "$2" = ls ]; then
  printf '%s\n' '{"ID":"abc123","Image":"e2e:latest","Names":"pk-e2e","Command":"sleep","Labels":"com.docker.compose.project=e2e"}'
  exit 0
fi
if [ "$1" = container ] && [ "$2" = stop ]; then
  printf '%s\n' "$3" > "$PK_E2E_DOCKER_LOG"
  exit 0
fi
exit 2
`
