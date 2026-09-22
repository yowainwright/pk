package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/config"

	"github.com/yowainwright/pk/internal/process"
)

func TestTickReloadsSavedIgnores(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	prefs := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	requireNoError(t, runner.cfg.UseStore(prefs))
	requireNoError(t, prefs.Add([]string{childProcess().Name}))
	requireNoError(t, runner.Tick(t.Context()))
	if killer.called {
		t.Fatal("killed newly ignored process")
	}
	requireNoError(t, prefs.Remove([]string{childProcess().Name}))
	requireNoError(t, runner.Tick(t.Context()))
	if !killer.called {
		t.Fatal("unignore did not restore cleanup")
	}
}

func TestTickRejectsMalformedPreferencesBeforeKilling(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	prefs := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	requireNoError(t, runner.cfg.UseStore(prefs))
	requireNoError(t, os.WriteFile(prefs.Path(), []byte("{"), 0o600))
	if err := runner.Tick(t.Context()); err == nil {
		t.Fatal("accepted malformed preferences")
	}
	acted := killer.called || store.acknowledged
	if acted {
		t.Fatal("acted on invalid policy")
	}
	if store.state.LastError == "" {
		t.Fatal("preference error not observable")
	}
}

func TestReenableDiscardsOldSessionsWithoutKilling(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	store.state = applyEvents(store.state, store.events)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.cfg.SessionSince = shellProcess().CreateTime + 1
	requireNoError(t, runner.Tick(t.Context()))
	if killer.called {
		t.Fatal("re-enable killed an old tracked process")
	}
	retained := len(store.state.Processes) != 0 || len(store.state.Sessions) != 0
	if retained {
		t.Fatal("retained sessions from before re-enable")
	}
}

func TestDaemonRestartKeepsSessionsSinceEnable(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	store.state = applyEvents(store.state, store.events)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.cfg.SessionSince = shellProcess().CreateTime - 1
	requireNoError(t, runner.Tick(t.Context()))
	if !killer.called {
		t.Fatal("daemon restart forgot eligible session")
	}
}

func TestFreshSessionUsesProcessClockWhenWallClockDiffers(t *testing.T) {
	event := sessionStartEvent()
	event.ObservedAt = time.UnixMilli(90)
	store := newFakeStore(event)
	store.procs = []process.Process{shellProcess(), childProcess()}
	runner := testRunner(store, nil)
	runner.cfg.SessionSince = 99
	requireNoError(t, runner.Tick(t.Context()))
	if len(store.state.Processes) != 1 {
		t.Fatal("wall-clock skew discarded a fresh shell identity")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTickTracksSessionDescendants(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	runner := testRunner(store, nil)
	store.procs = []process.Process{shellProcess(), childProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(store.state.Processes) != 1 {
		t.Fatalf("expected one managed process, got %d", len(store.state.Processes))
	}
}

func TestTickKillsStoredChildWhenSessionEnds(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if killer.pid != childProcess().PID {
		t.Fatalf("expected killed pid %d, got %d", childProcess().PID, killer.pid)
	}
	if len(store.auditEvents) != 1 {
		t.Fatalf("expected one audit event, got %d", len(store.auditEvents))
	}
}

func TestTickSkipsReusedPIDs(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{reusedChildProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if killer.called {
		t.Fatal("expected reused pid not to be killed")
	}
	if len(store.state.Processes) != 0 {
		t.Fatalf("expected stale managed process to be removed")
	}
}

func TestTickKeepsChildWhenLiveShellIsMissingFromSnapshot(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	assertSessionSurvives(t, store, killer)
}

func TestTickDefersCleanupWhenShellIdentityCannotBeRead(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	store.shellErr = os.ErrPermission
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	err := runner.Tick(t.Context())
	if err != nil {
		t.Fatalf("expected a deferred session without stopping the daemon, got %v", err)
	}
	assertSessionSurvives(t, store, killer)
	if !strings.Contains(store.state.LastError, os.ErrPermission.Error()) {
		t.Fatal("expected the error to be saved without ending the session")
	}
}

func assertSessionSurvives(t *testing.T, store *fakeStore, killer *fakeKiller) {
	t.Helper()
	if killer.called {
		t.Fatal(
			"a shell missing from the snapshot must not trigger cleanup without confirming its exit",
		)
	}
	session := store.state.Sessions["session-1"]
	if !session.Exists {
		t.Fatal("expected the session to remain live")
	}
}

func TestTickCleansUpWhenShellExitIsConfirmed(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	store.shellErr = os.ErrNotExist
	assertMissingShellCleanup(t, store)
}

func TestTickCleansUpWhenShellPIDWasReused(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	store.shellCreateTime++
	assertMissingShellCleanup(t, store)
}

func TestTickSurfacesCleanupAuditErrors(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(fmt.Sprint("protected=", protected), func(t *testing.T) {
			store := newFakeStore(sessionStartEvent(), sessionStopEvent())
			store.state.Processes[childProcessKey()] = managedChild()
			store.procs = []process.Process{childProcess()}
			store.auditErr = errors.New("audit disk full")
			killer := &fakeKiller{}
			runner := testRunner(store, killer)
			if protected {
				runner.cfg.Protected = append(runner.cfg.Protected, childProcess().Name)
			}
			assertAuditFailureVisible(t, runner, store)
			if killer.called == protected {
				t.Fatal("audit failure must preserve process protection")
			}
		})
	}
}

func assertAuditFailureVisible(t *testing.T, runner *Runner, store *fakeStore) {
	t.Helper()
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !strings.Contains(store.state.LastError, store.auditErr.Error()) {
		t.Fatalf("expected audit error in observability, got %q", store.state.LastError)
	}
}

func assertMissingShellCleanup(t *testing.T, store *fakeStore) {
	t.Helper()
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !killer.killedPID(childProcess().PID) {
		t.Fatal("expected cleanup after the original shell has exited")
	}
}

func TestTickKillsInactiveSessionAfterStaleLimit(t *testing.T) {
	event := sessionStartEvent()
	inactive := event
	inactive.EventID = "inactive-event"
	inactive.Kind = KindSessionInactive
	inactive.ObservedAt = testNow.Add(-time.Minute)
	store := newFakeStore(event, inactive)
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.cfg.StaleLimit = time.Second
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{shellProcess(), childProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !killer.called {
		t.Fatal("expected stale session process to be killed")
	}
}

func TestTickDoesNotStaleKillHumanSession(t *testing.T) {
	event := humanSessionStartEvent()
	inactive := event
	inactive.EventID = "human-inactive-event"
	inactive.Kind = KindSessionInactive
	inactive.ObservedAt = testNow.Add(-time.Minute)
	store := newFakeStore(event, inactive)
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.cfg.StaleLimit = time.Second
	store.state.Processes[childProcessKey()] = managedHumanChild()
	store.procs = []process.Process{shellProcess(), childProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if killer.called {
		t.Fatal("expected human session not to be stale killed")
	}
}

func TestTickKillsWhenBoundTabEnds(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), tabStopEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{shellProcess(), childProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !killer.called {
		t.Fatal("expected tab-ended process to be killed")
	}
	assertAuditReason(t, store.auditEvents[0], "tab-ended")
}

func TestTickClosingTabDoesNotKillSiblingTabInSameWindow(t *testing.T) {
	store := siblingTabStore()
	killer := &fakeKiller{}
	runner := testRunner(store, killer)

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	assertSiblingTabKill(t, killer)
}

func TestTickSessionStopDoesNotReactivateEndedWindow(t *testing.T) {
	store := newFakeStore(sessionStartEvent(), windowStopEvent(), sessionStopEvent())
	runner := testRunner(store, nil)

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	window := store.state.Windows["window-1"]
	if window.Exists {
		t.Fatal("expected explicit window stop to remain ended")
	}
}

func TestTickKeepsExistingSessionBindingWhenEventOmitsIDs(t *testing.T) {
	heartbeat := sessionStartEvent()
	heartbeat.EventID = "heartbeat-event"
	heartbeat.Kind = KindSessionHeartbeat
	heartbeat.TabID = ""
	store := newFakeStore(sessionStartEvent(), heartbeat)
	runner := testRunner(store, nil)
	store.procs = []process.Process{shellProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	session := store.state.Sessions["session-1"]
	if session.TabID != "tab-1" {
		t.Fatalf("expected sticky tab id, got %q", session.TabID)
	}
}

func TestTickReturnsListErrorsAfterSavingState(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	runner := testRunner(store, nil)
	store.listErr = errors.New("denied")

	err := runner.Tick(context.Background())

	if !errors.Is(err, store.listErr) {
		t.Fatalf("expected list error, got %v", err)
	}
	if store.state.LastError != "denied" {
		t.Fatalf("expected saved error, got %q", store.state.LastError)
	}
}

func TestTickAcknowledgesTakenEvents(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	runner := testRunner(store, nil)
	store.procs = []process.Process{shellProcess()}

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !store.acknowledged {
		t.Fatal("expected lifecycle events to be acknowledged")
	}
}

func TestTickKeepsAppliedEventsBounded(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	runner := testRunner(store, nil)
	store.procs = []process.Process{shellProcess()}
	fillAppliedEvents(store.state.AppliedEvents, maxAppliedEventIDs+10)

	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(store.state.AppliedEvents) > maxAppliedEventIDs {
		t.Fatalf("expected bounded applied events, got %d", len(store.state.AppliedEvents))
	}
}

type fakeStore struct {
	events          []Event
	state           State
	procs           []process.Process
	listErr         error
	shellCreateTime int64
	shellErr        error
	acknowledged    bool
	auditEvents     []audit.Event
	auditErr        error
}

func newFakeStore(events ...Event) *fakeStore {
	state := State{}
	state.Ensure()
	return &fakeStore{events: events, state: state, shellCreateTime: shellProcess().CreateTime}
}

func siblingTabStore() *fakeStore {
	store := newFakeStore(
		sessionStartEvent(),
		siblingSessionStartEvent(),
		sessionStopEvent(),
	)
	store.state.Processes[childProcessKey()] = managedChild()
	store.state.Processes[siblingChildProcessKey()] = managedSiblingChild()
	store.procs = siblingTabProcesses()
	return store
}

func siblingTabProcesses() []process.Process {
	return []process.Process{
		childProcess(),
		siblingShellProcess(),
		siblingChildProcess(),
	}
}

func assertSiblingTabKill(t *testing.T, killer *fakeKiller) {
	t.Helper()
	if !killer.killedPID(20) {
		t.Fatalf("expected closed tab child kill, got %#v", killer.killed)
	}
	if killer.killedPID(21) {
		t.Fatalf("expected sibling tab child to survive, got %#v", killer.killed)
	}
}

func (s *fakeStore) TakeEvents() ([]Event, error) {
	return s.events, nil
}

func (s *fakeStore) AcknowledgeEvents() error {
	s.acknowledged = true
	return nil
}

func (s *fakeStore) LoadState() (State, error) {
	return s.state, nil
}

func (s *fakeStore) SaveState(state State) error {
	s.state = state
	return nil
}

func (s *fakeStore) List(context.Context) ([]process.Process, error) {
	return s.procs, s.listErr
}

func (s *fakeStore) readCreateTime(context.Context, int32) (int64, error) {
	return s.shellCreateTime, s.shellErr
}

func (s *fakeStore) Record(event audit.Event) error {
	if s.auditErr != nil {
		return s.auditErr
	}
	s.auditEvents = append(s.auditEvents, event)
	return nil
}

func fillAppliedEvents(applied map[string]bool, count int) {
	for index := 0; index < count; index++ {
		applied[appliedEventID(index)] = true
	}
}

func appliedEventID(index int) string {
	return "event-" + fmt.Sprint(index)
}

type fakeKiller struct {
	called bool
	pid    int32
	killed []int32
}

func (k *fakeKiller) Kill(ctx context.Context, target process.Process) error {
	k.called = true
	k.pid = target.PID
	k.killed = append(k.killed, target.PID)
	return nil
}

func (k *fakeKiller) killedPID(pid int32) bool {
	for _, killed := range k.killed {
		if killed == pid {
			return true
		}
	}
	return false
}

var testNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func testRunner(store *fakeStore, killer *fakeKiller) *Runner {
	cfg := &config.Config{Interval: time.Hour, StaleLimit: time.Hour}
	cfg.Protected = []string{"zsh", "pk"}
	if killer == nil {
		killer = &fakeKiller{}
	}
	options := RunnerOptions{Now: nowFunc, ReadCreateTime: store.readCreateTime}
	return NewRunner(cfg, store, killer, store, Audit(store), options)
}

func nowFunc() time.Time {
	return testNow
}

func sessionStartEvent() Event {
	return Event{
		Version:           Version,
		EventID:           "start-event",
		Kind:              KindSessionStart,
		ObservedAt:        testNow.Add(-time.Hour),
		Source:            "zsh",
		TerminalSessionID: "session-1",
		AgentSessionID:    "agent-1",
		UserSessionID:     "user-1",
		WindowID:          "window-1",
		TabID:             "tab-1",
		ShellPID:          10,
		ShellCreateTime:   100,
	}
}

func humanSessionStartEvent() Event {
	event := sessionStartEvent()
	event.AgentSessionID = ""
	return event
}

func siblingSessionStartEvent() Event {
	event := sessionStartEvent()
	event.EventID = "sibling-start-event"
	event.TerminalSessionID = "session-2"
	event.TabID = "tab-2"
	event.ShellPID = 11
	event.ShellCreateTime = 101
	return event
}

func sessionStopEvent() Event {
	event := sessionStartEvent()
	event.EventID = "stop-event"
	event.Kind = KindSessionStop
	event.ObservedAt = testNow.Add(-time.Minute)
	return event
}

func tabStopEvent() Event {
	return Event{
		Version:    Version,
		EventID:    "tab-stop-event",
		Kind:       KindContextStop,
		ObservedAt: testNow.Add(-time.Minute),
		Source:     "terminal",
		TabID:      "tab-1",
	}
}

func windowStopEvent() Event {
	return Event{
		Version:    Version,
		EventID:    "window-stop-event",
		Kind:       KindContextStop,
		ObservedAt: testNow.Add(-time.Minute),
		Source:     "terminal",
		WindowID:   "window-1",
	}
}

func assertAuditReason(t *testing.T, event audit.Event, reason string) {
	t.Helper()
	for _, current := range event.Reasons {
		if current == reason {
			return
		}
	}
	t.Fatalf("expected reason %q, got %#v", reason, event.Reasons)
}

func shellProcess() process.Process {
	return process.Process{PID: 10, CreateTime: 100, Name: "zsh"}
}

func siblingShellProcess() process.Process {
	return process.Process{PID: 11, CreateTime: 101, Name: "zsh"}
}

func childProcess() process.Process {
	return process.Process{
		PID:         20,
		CreateTime:  200,
		ParentPID:   10,
		Name:        "node",
		CommandLine: "node dev.js",
		Cwd:         "/repo",
	}
}

func siblingChildProcess() process.Process {
	return process.Process{
		PID:         21,
		CreateTime:  201,
		ParentPID:   11,
		Name:        "node",
		CommandLine: "node sibling.js",
		Cwd:         "/repo",
	}
}

func reusedChildProcess() process.Process {
	proc := childProcess()
	proc.CreateTime = 201
	return proc
}

func childProcessKey() string {
	return ProcessKeyString(childProcess().PID, childProcess().CreateTime)
}

func siblingChildProcessKey() string {
	proc := siblingChildProcess()
	return ProcessKeyString(proc.PID, proc.CreateTime)
}

func managedChild() ManagedProcess {
	return ManagedProcess{
		ProcessKey:        ProcessKey{PID: 20, CreateTime: 200},
		TerminalSessionID: "session-1",
		Name:              "node",
		Cwd:               "/repo",
	}
}

func managedHumanChild() ManagedProcess {
	managed := managedChild()
	managed.TerminalSessionID = "session-1"
	return managed
}

func managedSiblingChild() ManagedProcess {
	return ManagedProcess{
		ProcessKey:        ProcessKey{PID: 21, CreateTime: 201},
		TerminalSessionID: "session-2",
		Name:              "node",
		Cwd:               "/repo",
	}
}

func TestTickCleansUpChildAfterOmittedSnapshot(t *testing.T) {
	store := newFakeStore(sessionStartEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	createTime := childProcess().CreateTime
	runner.readCreateTime = childIdentityReader(store, createTime, nil)
	store.procs = []process.Process{shellProcess(), childProcess()}
	assertTickSucceeds(t, runner)
	store.procs = []process.Process{shellProcess()}
	assertTickSucceeds(t, runner)
	store.events = []Event{sessionStopEvent()}
	store.procs = []process.Process{orphanedChildProcess()}
	assertTickSucceeds(t, runner)
	if !killer.killedPID(childProcess().PID) {
		t.Fatal("expected tracked child cleanup after an omitted snapshot and session exit")
	}
}

func TestTickDefersUnreadableChildWhileCleaningSibling(t *testing.T) {
	t.Run("permission error", func(t *testing.T) {
		createTime := childProcess().CreateTime
		assertDeferredChildCleanup(t, createTime, os.ErrPermission)
	})
	t.Run("missing creation time", func(t *testing.T) {
		assertDeferredChildCleanup(t, 0, nil)
	})
}

func assertDeferredChildCleanup(t *testing.T, createTime int64, err error) {
	t.Helper()
	store := siblingTabStore()
	store.events = append(store.events, windowStopEvent())
	store.procs = siblingTabProcesses()[1:]
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.readCreateTime = childIdentityReader(store, createTime, err)
	assertTickSucceeds(t, runner)
	assertChildDeferred(t, store, killer)
	store.procs = []process.Process{orphanedChildProcess()}
	assertTickSucceeds(t, runner)
	if !killer.killedPID(childProcess().PID) {
		t.Fatal("expected deferred child cleanup after its metadata becomes readable")
	}
}

func assertChildDeferred(t *testing.T, store *fakeStore, killer *fakeKiller) {
	t.Helper()
	_, tracked := store.state.Processes[childProcessKey()]
	if !tracked {
		t.Fatal("expected unreadable child to remain tracked")
	}
	if killer.killedPID(childProcess().PID) {
		t.Fatal("unreadable child must not be killed")
	}
	if !killer.killedPID(siblingChildProcess().PID) {
		t.Fatal("expected healthy sibling cleanup to continue")
	}
	if !strings.Contains(store.state.Daemon.LastError, childProcessKey()) {
		t.Fatal("expected the unreadable child's identity in daemon diagnostics")
	}
}

func TestTickRemovesConfirmedExitedChildren(t *testing.T) {
	t.Run("gone", func(t *testing.T) {
		assertMissingChildRemoved(t, 0, os.ErrNotExist)
	})
	t.Run("reused PID", func(t *testing.T) {
		createTime := reusedChildProcess().CreateTime
		assertMissingChildRemoved(t, createTime, nil)
	})
}

func assertMissingChildRemoved(t *testing.T, createTime int64, err error) {
	t.Helper()
	store := newFakeStore(sessionStartEvent(), sessionStopEvent())
	store.state.Processes[childProcessKey()] = managedChild()
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.readCreateTime = childIdentityReader(store, createTime, err)
	assertTickSucceeds(t, runner)
	if len(store.state.Processes) != 0 {
		t.Fatal("expected confirmed exited child to be removed from tracking")
	}
	if killer.called {
		t.Fatal("confirmed exited child must not be signaled")
	}
}

func childIdentityReader(
	store *fakeStore,
	createTime int64,
	err error,
) func(context.Context, int32) (int64, error) {
	return func(ctx context.Context, pid int32) (int64, error) {
		if pid == childProcess().PID {
			return createTime, err
		}
		return store.readCreateTime(ctx, pid)
	}
}

func orphanedChildProcess() process.Process {
	child := childProcess()
	child.ParentPID = 1
	return child
}

func assertTickSucceeds(t *testing.T, runner *Runner) {
	t.Helper()
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func TestTickDefersUnreadableSessionWhileCleaningSibling(t *testing.T) {
	store := unreadableShellStore(siblingSessionStartEvent(), windowStopEvent())
	store.procs = siblingTabProcesses()
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if killer.killedPID(childProcess().PID) {
		t.Fatal("unreadable session must survive even when its window has ended")
	}
	if !killer.killedPID(siblingChildProcess().PID) {
		t.Fatal("expected the healthy sibling's child to be discovered and cleaned up")
	}
	assertDeferredState(t, store)
}

func TestTickRetriesDeferredCleanup(t *testing.T) {
	store := unreadableShellStore(tabStopEvent())
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	for range 2 {
		if err := runner.Tick(t.Context()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		assertSessionSurvives(t, store, killer)
	}
	store.shellErr = nil
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("recovered tick: %v", err)
	}
	if !killer.killedPID(childProcess().PID) {
		t.Fatal("expected deferred cleanup to resume once the shell identity is known")
	}
}

func TestTickDefersStaleCleanupWithInvalidShellIdentity(t *testing.T) {
	inactive := sessionStartEvent()
	inactive.EventID = "inactive-event"
	inactive.Kind = KindSessionInactive
	store := unreadableShellStore(inactive)
	store.shellErr = nil
	store.shellCreateTime = 0
	killer := &fakeKiller{}
	runner := testRunner(store, killer)
	runner.cfg.StaleLimit = time.Second
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	assertSessionSurvives(t, store, killer)
	assertDeferredState(t, store)
}

func TestRunKeepsPollingAfterShellIdentityError(t *testing.T) {
	store := unreadableShellStore()
	runner := testRunner(store, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runner.readCreateTime = cancelOnSecondIdentityRead(cancel)
	runner.cfg.Interval = time.Nanosecond
	err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected polling to continue until cancellation, got %v", err)
	}
}

func unreadableShellStore(events ...Event) *fakeStore {
	events = append([]Event{sessionStartEvent()}, events...)
	store := newFakeStore(events...)
	store.state.Processes[childProcessKey()] = managedChild()
	store.procs = []process.Process{childProcess()}
	store.shellErr = os.ErrPermission
	return store
}

func assertDeferredState(t *testing.T, store *fakeStore) {
	t.Helper()
	if !store.acknowledged {
		t.Fatal("expected events to be acknowledged while one session is deferred")
	}
	if _, tracked := store.state.Processes[childProcessKey()]; !tracked {
		t.Fatal("expected the deferred session's child to remain tracked")
	}
	daemonError := store.state.Daemon.LastError
	if !strings.Contains(daemonError, "session-1") {
		t.Fatalf("expected the affected session in daemon diagnostics, got %q", daemonError)
	}
}

func cancelOnSecondIdentityRead(
	cancel context.CancelFunc,
) func(context.Context, int32) (int64, error) {
	reads := 0
	return func(context.Context, int32) (int64, error) {
		reads++
		if reads == 2 {
			cancel()
		}
		return 0, os.ErrPermission
	}
}
