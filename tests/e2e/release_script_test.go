//go:build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseDryRunUsesSvuWithoutDispatching(t *testing.T) {
	fakeDir := t.TempDir()
	logPath := filepath.Join(fakeDir, "gh.log")
	setupReleaseTools(t, fakeDir)
	result := runReleaseScript(t, fakeDir, logPath, "1\n")
	if result.err != nil {
		t.Fatalf("release dry run failed: %v\n%s", result.err, result.stderr)
	}
	assertContains(t, result.stdout, "Selected v1.3.0")
	assertContains(t, result.stdout, "no GitHub state changed")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading gh log: %v", err)
	}
	if strings.Contains(string(logData), "workflow run") {
		t.Fatalf("dry run dispatched a workflow:\n%s", logData)
	}
}

func TestReleaseFailsWhenSvuIsMissing(t *testing.T) {
	fakeDir := t.TempDir()
	writeExecutable(t, filepath.Join(fakeDir, "git"), fakeGit)
	writeExecutable(t, filepath.Join(fakeDir, "gh"), fakeGH)
	writeExecutable(t, filepath.Join(fakeDir, "mise"), fakeMise)
	result := runReleaseScript(t, fakeDir, filepath.Join(fakeDir, "gh.log"), "")
	if result.err == nil {
		t.Fatal("release unexpectedly succeeded without svu")
	}
	assertContains(t, result.stderr, "Required command not found: svu")
}

func setupReleaseTools(t *testing.T, fakeDir string) {
	t.Helper()
	writeExecutable(t, filepath.Join(fakeDir, "git"), fakeGit)
	writeExecutable(t, filepath.Join(fakeDir, "gh"), fakeGH)
	writeExecutable(t, filepath.Join(fakeDir, "mise"), fakeMise)
	writeExecutable(t, filepath.Join(fakeDir, "svu"), fakeSvu)
}

func runReleaseScript(t *testing.T, fakeDir string, logPath string, input string) commandResult {
	t.Helper()
	script := filepath.Join(repositoryRoot, "scripts", "release.sh")
	command := exec.Command("/bin/bash", script, "--dry-run")
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
  *) exit 41 ;;
esac
`

const fakeGH = `#!/bin/sh
printf '%s\n' "$*" >> "$GH_LOG"
case "$*" in
  "auth status") ;;
  "repo view --json nameWithOwner --jq .nameWithOwner") printf 'yowainwright/pk\n' ;;
  *) exit 42 ;;
esac
`

const fakeMise = `#!/bin/sh
[ "$*" = "run release-preview" ] || exit 43
`

const fakeSvu = `#!/bin/sh
case "$*" in
  current) printf 'v1.2.3' ;;
  "next --always") printf 'v1.3.0' ;;
  patch) printf 'v1.2.4' ;;
  minor) printf 'v1.3.0' ;;
  major) printf 'v2.0.0' ;;
  "next --always --prerelease rc.1") printf 'v1.3.0-rc.1' ;;
  *) exit 44 ;;
esac
`
