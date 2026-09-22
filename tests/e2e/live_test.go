//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/lifecycle"
)

type liveFixture struct {
	t          *testing.T
	home       string
	data       string
	entrypoint string
}

type liveSession struct {
	id    string
	shell int
	pid   int
}

const fixtureProtected = "process-reaper,live-tests,docker-init,tini,sh,systemctl"

func TestLiveSavedIgnoresProtectManualCleanup(t *testing.T) {
	f := newLiveFixture(t)
	target := f.session("manual-project")
	assertContains(t, f.command("scan", "--protected", fixtureProtected), "vite")
	f.command("ignore", "vite")
	scan := f.command("scan", "--protected", fixtureProtected)
	if strings.Contains(scan, "vite") {
		t.Fatalf("scan selected ignored process: %s", scan)
	}
	f.command("cleanup", "--apply", "--scope", "processes", "--protected", fixtureProtected)
	f.assertAlive(target)
	f.command("unignore", "vite")
	f.command("cleanup", "--apply", "--scope", "processes", "--protected", fixtureProtected)
	f.waitGone(target)
}

func TestLiveMonitorReloadsExactCaseIgnores(t *testing.T) {
	f := newLiveFixture(t)
	f.command("ignore", "vite")
	protected := f.session("lowercase-project")
	unprotected := f.namedSession("uppercase-project", "Vite")
	f.monitor()
	f.waitGone(unprotected)
	f.assertAlive(protected)
	f.command("unignore", "vite")
	f.waitGone(protected)
}

func TestLiveIgnoreReloadSurvivesDaemonRestartAcrossProjects(t *testing.T) {
	f := newLiveFixture(t)
	f.command("enable")
	first := f.session("project-one")
	second := f.session("project-two")
	f.waitTracked(first, second)
	f.command("ignore", "vite")
	f.end(first, second)
	f.waitProtected(first, second)
	previousPID := f.state().Daemon.PID
	f.command("enable")
	f.waitDaemonChange(previousPID)
	f.waitProtected(first, second)
	f.assertAlive(first, second)
	f.command("unignore", "vite")
	f.waitGone(first, second)
	assertContains(t, f.command("history"), `"applied":true`)
}

func TestLiveDisablePreservesDataAndReenableOnlyTracksFreshSessions(t *testing.T) {
	f := newLiveFixture(t)
	f.command("enable")
	old := f.session("old-project")
	f.waitTracked(old)
	f.command("ignore", "postgres")
	history := f.seedHistory()
	daemonPID := f.currentDaemonPID()
	f.command("disable")
	f.command("disable")
	f.assertDisabled(history, daemonPID)
	f.end(old)
	f.command("enable")
	f.waitForgotten(old)
	fresh := f.session("fresh-project")
	f.waitTracked(fresh)
	f.end(fresh)
	f.waitGone(fresh)
	f.assertAlive(old)
}

func TestLiveCorruptPreferencesStopCleanupAndRecoveryKeepsQueuedEvents(t *testing.T) {
	f := newLiveFixture(t)
	f.command("enable")
	target := f.session("corruption-project")
	f.waitTracked(target)
	f.writePreferences("{")
	f.waitPolicyError()
	f.end(target)
	f.expectFailure("enable")
	f.expectFailure("ignore", "vite")
	f.assertAlive(target)
	if got := f.read("config.json"); got != "{" {
		t.Fatalf("corrupt data overwritten: %q", got)
	}
	f.writePreferences(`{"ignored_process_names":[]}`)
	f.command("enable")
	f.waitGone(target)
}

func TestLiveUpgradeUsesStableEntrypointAndRetainsPreferences(t *testing.T) {
	f := newLiveFixture(t)
	f.command("ignore", "vite")
	f.command("enable")
	oldPID := f.currentDaemonPID()
	f.upgrade()
	assertContains(t, f.command("version"), "v9.8.8-e2e")
	f.command("enable")
	f.waitDaemonChange(oldPID)
	f.assertUpgraded()
}

func (f *liveFixture) assertUpgraded() {
	t := f.t
	t.Helper()
	path := fmt.Sprintf("/proc/%d/exe", f.state().Daemon.PID)
	executable, err := os.Readlink(path)
	liveNoError(t, err)
	assertContains(t, executable, "pk-next")
	if got := f.command("ignore", "--list"); got != "vite\n" {
		t.Fatalf("lost ignores: %q", got)
	}
	contents, err := os.ReadFile(filepath.Join(f.home, ".zshrc"))
	liveNoError(t, err)
	if strings.Count(string(contents), "# pk") != 1 {
		t.Fatal("duplicated shell registration")
	}
}

func newLiveFixture(t *testing.T) *liveFixture {
	t.Helper()
	isolated := runtime.GOOS == "linux" && os.Getenv("PK_E2E_ISOLATED") == "1"
	if !isolated {
		t.Skip("requires the disposable process-test container")
	}
	home := t.TempDir()
	data := filepath.Join(home, "custom config", "pk")
	entrypoint := filepath.Join(home, "bin", "pk")
	f := &liveFixture{t: t, home: home, data: data, entrypoint: entrypoint}
	f.configure()
	t.Cleanup(f.close)
	return f
}

func (f *liveFixture) configure() {
	f.t.Helper()
	bin := filepath.Dir(f.entrypoint)
	liveNoError(f.t, os.MkdirAll(bin, 0o700))
	liveNoError(f.t, os.Symlink(pkBinary, f.entrypoint))
	writeExecutable(
		f.t,
		filepath.Join(bin, "systemctl"),
		"#!/bin/sh\nexec /fixture/process-reaper service \"$@\"\n",
	)
	liveNoError(
		f.t,
		os.WriteFile(filepath.Join(f.home, ".zshrc"), []byte("export KEEP_ME=1\n"), 0o600),
	)
	f.t.Setenv("HOME", f.home)
	f.t.Setenv("XDG_CONFIG_HOME", filepath.Dir(f.data))
	f.t.Setenv("PATH", bin)
	f.t.Setenv("ZDOTDIR", "")
	f.t.Setenv("PK_AUDIT_PATH", "")
}

func (f *liveFixture) command(args ...string) string {
	f.t.Helper()
	output, err := exec.Command(f.entrypoint, args...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("pk %v: %v\n%s\n%s", args, err, output, f.serviceLog())
	}
	return string(output)
}

func (f *liveFixture) expectFailure(args ...string) {
	f.t.Helper()
	output, err := exec.Command(f.entrypoint, args...).CombinedOutput()
	if err == nil {
		f.t.Fatalf("pk %v unexpectedly succeeded: %s", args, output)
	}
}

func (f *liveFixture) close() {
	output, err := exec.Command(f.entrypoint, "disable").CombinedOutput()
	if err != nil {
		f.t.Errorf("fixture disable: %v\n%s", err, output)
	}
}

func (f *liveFixture) session(id string) liveSession {
	f.t.Helper()
	return f.namedSession(id, "vite")
}

func (f *liveFixture) namedSession(id string, program string) liveSession {
	f.t.Helper()
	work := filepath.Join(f.home, "code", id)
	liveNoError(f.t, os.MkdirAll(work, 0o700))
	pidPath := filepath.Join(work, "pid")
	executable := filepath.Join("/fixture", program)
	reaper := exec.Command("/fixture/process-reaper", "reap", executable, work, pidPath)
	liveNoError(f.t, reaper.Start())
	f.wait("fixture child", func() bool { return readLivePID(pidPath) > 0 })
	session := liveSession{id: id, shell: reaper.Process.Pid, pid: readLivePID(pidPath)}
	f.t.Cleanup(func() { _ = syscall.Kill(session.pid, syscall.SIGTERM); _ = reaper.Wait() })
	f.event(session, "session.start")
	return session
}

func (f *liveFixture) monitor() {
	f.t.Helper()
	path := filepath.Join(f.home, "monitor.log")
	log, err := os.Create(path)
	liveNoError(f.t, err)
	command := exec.Command(f.entrypoint, "monitor", "--apply", "--mem", "0", "--cpu", "100000",
		"--grace", "0", "--interval", "50ms", "--protected", fixtureProtected)
	command.Stdout, command.Stderr = log, log
	liveNoError(f.t, command.Start())
	f.t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		_ = command.Wait()
		_ = log.Close()
	})
}

func readLivePID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func (f *liveFixture) event(session liveSession, kind string) {
	f.command("__session", "--kind", kind, "--source", "e2e", "--terminal-session-id", session.id,
		"--shell-pid", strconv.Itoa(session.shell))
}

func (f *liveFixture) end(sessions ...liveSession) {
	for _, session := range sessions {
		f.event(session, "session.stop")
	}
}

func (f *liveFixture) state() lifecycle.State {
	var state lifecycle.State
	data, err := os.ReadFile(filepath.Join(f.data, "lifecycle-state.json"))
	if err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

func (f *liveFixture) tracked(pid int) bool {
	for _, process := range f.state().Processes {
		if int(process.ProcessKey.PID) == pid {
			return true
		}
	}
	return false
}

func (f *liveFixture) waitTracked(sessions ...liveSession) {
	for _, session := range sessions {
		f.wait("tracked "+session.id, func() bool { return f.tracked(session.pid) })
	}
}

func (f *liveFixture) waitForgotten(session liveSession) {
	f.wait("fresh enabled period", func() bool {
		pid := readLivePID(filepath.Join(f.home, "service.pid"))
		current := f.state().Daemon.PID == pid
		forgotten := !f.tracked(session.pid) && current
		return forgotten
	})
}

func (f *liveFixture) waitProtected(sessions ...liveSession) {
	for _, session := range sessions {
		f.wait("protected "+session.id, func() bool { return f.protectionRecorded(session.pid) })
	}
}

func (f *liveFixture) protectionRecorded(pid int) bool {
	data, _ := os.ReadFile(filepath.Join(f.data, "events.jsonl"))
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			PID     int      `json:"pid"`
			Reasons []string `json:"reasons"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		reasons := strings.Join(event.Reasons, ",")
		protected := event.PID == pid && strings.Contains(reasons, "protected-process")
		if protected {
			return true
		}
	}
	return false
}

func (f *liveFixture) assertAlive(sessions ...liveSession) {
	f.t.Helper()
	for _, session := range sessions {
		if err := syscall.Kill(session.pid, 0); err != nil {
			f.t.Fatalf("ignored process %d died: %v", session.pid, err)
		}
	}
}

func (f *liveFixture) waitGone(sessions ...liveSession) {
	for _, session := range sessions {
		f.wait("terminated "+session.id, func() bool { return syscall.Kill(session.pid, 0) != nil })
	}
}

func (f *liveFixture) waitDaemonChange(previous int) {
	f.wait("restarted daemon", func() bool {
		pid := f.state().Daemon.PID
		current := pid == readLivePID(filepath.Join(f.home, "service.pid"))
		active := pid > 0 && liveProcess(pid)
		restarted := pid != previous && current
		changed := active && restarted
		return changed
	})
}

func (f *liveFixture) currentDaemonPID() int {
	f.wait("current daemon state", func() bool {
		pid := readLivePID(filepath.Join(f.home, "service.pid"))
		current := pid > 0 && f.state().Daemon.PID == pid && liveProcess(pid)
		return current
	})
	return f.state().Daemon.PID
}

func liveProcess(pid int) bool {
	path := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return !strings.Contains(string(data), "State:\tZ")
}

func (f *liveFixture) waitPolicyError() {
	f.wait(
		"visible preferences error",
		func() bool { return strings.Contains(f.state().LastError, "preferences") },
	)
}

func (f *liveFixture) writePreferences(contents string) {
	liveNoError(f.t, os.WriteFile(filepath.Join(f.data, "config.json"), []byte(contents), 0o600))
}

func (f *liveFixture) read(name string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.data, name))
	liveNoError(f.t, err)
	return string(data)
}

func (f *liveFixture) seedHistory() string {
	current := time.Now().UTC()
	now := current.Format(time.RFC3339Nano)
	data := fmt.Sprintf(
		"{\"time\":%q,\"command\":\"e2e\",\"action\":\"preview\",\"applied\":false}\n",
		now,
	)
	liveNoError(f.t, os.WriteFile(filepath.Join(f.data, "events.jsonl"), []byte(data), 0o600))
	return data
}

func (f *liveFixture) assertDisabled(history string, pid int) {
	f.t.Helper()
	f.wait("stopped daemon", func() bool { return syscall.Kill(pid, 0) != nil })
	if f.read("events.jsonl") != history {
		f.t.Fatal("disable changed retained history")
	}
	if f.command("ignore", "--list") != "postgres\n" {
		f.t.Fatal("disable lost saved ignores")
	}
	f.assertIntegrationRemoved()
}

func (f *liveFixture) assertIntegrationRemoved() {
	f.t.Helper()
	plugin := filepath.Join(f.home, ".config", "pk", "shell", "pk.zsh")
	if _, err := os.Stat(plugin); !os.IsNotExist(err) {
		f.t.Fatalf("shell plugin remains: %v", err)
	}
	path := filepath.Join(f.home, ".config", "systemd", "user", "pk.service")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		f.t.Fatalf("service registration remains: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(f.home, ".zshrc"))
	liveNoError(f.t, err)
	if string(data) != "export KEEP_ME=1\n" {
		f.t.Fatalf("unrelated zshrc changed: %q", data)
	}
}

func (f *liveFixture) upgrade() {
	next := f.entrypoint + ".next"
	liveNoError(f.t, os.Symlink("/fixture/pk-next", next))
	liveNoError(f.t, os.Rename(next, f.entrypoint))
}

func (f *liveFixture) serviceLog() string {
	data, _ := os.ReadFile(filepath.Join(f.home, "service-test.log"))
	return string(data)
}

func (f *liveFixture) wait(description string, condition func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out: %s\n%s\n%+v", description, f.serviceLog(), f.state())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func liveNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
