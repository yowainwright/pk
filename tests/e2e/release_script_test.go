//go:build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseDryRunUsesExplicitVersionWithoutDispatching(t *testing.T) {
	fakeDir := t.TempDir()
	logPath := filepath.Join(fakeDir, "gh.log")
	setupReleaseTools(t, fakeDir)
	result := runReleaseScript(t, fakeDir, logPath, "", "--dry-run", "v0.1.0-rc.1")
	if result.err != nil {
		t.Fatalf("release dry run failed: %v\n%s", result.err, result.stderr)
	}
	assertContains(t, result.stdout, "Selected v0.1.0-rc.1")
	assertContains(t, result.stdout, "no GitHub state changed")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading gh log: %v", err)
	}
	if strings.Contains(string(logData), "workflow run") {
		t.Fatalf("dry run dispatched a workflow:\n%s", logData)
	}
}

func TestReleaseDryRunCanSelectVersion(t *testing.T) {
	fakeDir := t.TempDir()
	logPath := filepath.Join(fakeDir, "gh.log")
	setupReleaseTools(t, fakeDir)
	result := runReleaseScript(t, fakeDir, logPath, "1\n", "--dry-run")
	if result.err != nil {
		t.Fatalf("release dry run failed: %v\n%s", result.err, result.stderr)
	}
	assertContains(t, result.stdout, "Current: v0.0.0")
	assertContains(t, result.stdout, "Selected v0.1.0-rc.1")
}

func setupReleaseTools(t *testing.T, fakeDir string) {
	t.Helper()
	writeExecutable(t, filepath.Join(fakeDir, "git"), fakeGit)
	writeExecutable(t, filepath.Join(fakeDir, "gh"), fakeGH)
	writeExecutable(t, filepath.Join(fakeDir, "mise"), fakeMise)
}

func runReleaseScript(
	t *testing.T,
	fakeDir string,
	logPath string,
	input string,
	args ...string,
) commandResult {
	t.Helper()
	script := filepath.Join(repositoryRoot, "scripts", "release.sh")
	commandArgs := append([]string{script}, args...)
	command := exec.Command("/bin/bash", commandArgs...)
	command.Dir = repositoryRoot
	command.Env = []string{"PATH=" + fakeDir, "GH_LOG=" + logPath}
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	return commandResult{stdout: string(output), stderr: string(output), err: err}
}

const fakeGit = `#!/bin/sh
case "$*" in
  "rev-parse --is-inside-work-tree") printf 'true\n' ;;
  "status --porcelain") ;;
  "branch --show-current") printf 'main\n' ;;
  "fetch --quiet origin main --tags") ;;
  "rev-parse HEAD"|"rev-parse origin/main") printf 'abc123\n' ;;
  "tag --list v0.* --sort=-version:refname") ;;
  "rev-parse -q --verify refs/tags/v0.1.0-rc.1") exit 1 ;;
  "ls-remote --exit-code --tags origin refs/tags/v0.1.0-rc.1") exit 2 ;;
  *) exit 41 ;;
esac
`

const fakeGH = `#!/bin/sh
printf '%s\n' "$*" >> "$GH_LOG"
case "$*" in
  "auth status") ;;
  "repo view --json nameWithOwner --jq .nameWithOwner") printf 'yowainwright/pk\n' ;;
  "release view v0.1.0-rc.1") exit 1 ;;
  *) exit 42 ;;
esac
`

const fakeMise = `#!/bin/sh
[ "$*" = "run release-preview" ] || exit 43
`
