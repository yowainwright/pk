package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/lifecycle"
	"github.com/yowainwright/pk/internal/process"
)

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

func TestTickKillsInactiveSessionAfterStaleLimit(t *testing.T) {
	event := sessionStartEvent()
	inactive := event
	inactive.EventID = "inactive-event"
	inactive.Kind = lifecycle.KindSessionInactive
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
	inactive.Kind = lifecycle.KindSessionInactive
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
	heartbeat.Kind = lifecycle.KindSessionHeartbeat
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
	events       []lifecycle.Event
	state        lifecycle.State
	procs        []process.Process
	listErr      error
	acknowledged bool
	auditEvents  []audit.Event
}

func newFakeStore(events ...lifecycle.Event) *fakeStore {
	state := lifecycle.State{}
	state.Ensure()
	return &fakeStore{events: events, state: state}
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

func (s *fakeStore) TakeEvents() ([]lifecycle.Event, error) {
	return s.events, nil
}

func (s *fakeStore) AcknowledgeEvents() error {
	s.acknowledged = true
	return nil
}

func (s *fakeStore) LoadState() (lifecycle.State, error) {
	return s.state, nil
}

func (s *fakeStore) SaveState(state lifecycle.State) error {
	s.state = state
	return nil
}

func (s *fakeStore) List(context.Context) ([]process.Process, error) {
	return s.procs, s.listErr
}

func (s *fakeStore) Record(event audit.Event) error {
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
	return New(cfg, store, killer, store, Audit(store), Options{Now: nowFunc})
}

func nowFunc() time.Time {
	return testNow
}

func sessionStartEvent() lifecycle.Event {
	return lifecycle.Event{
		Version:           lifecycle.Version,
		EventID:           "start-event",
		Kind:              lifecycle.KindSessionStart,
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

func humanSessionStartEvent() lifecycle.Event {
	event := sessionStartEvent()
	event.AgentSessionID = ""
	return event
}

func siblingSessionStartEvent() lifecycle.Event {
	event := sessionStartEvent()
	event.EventID = "sibling-start-event"
	event.TerminalSessionID = "session-2"
	event.TabID = "tab-2"
	event.ShellPID = 11
	event.ShellCreateTime = 101
	return event
}

func sessionStopEvent() lifecycle.Event {
	event := sessionStartEvent()
	event.EventID = "stop-event"
	event.Kind = lifecycle.KindSessionStop
	event.ObservedAt = testNow.Add(-time.Minute)
	return event
}

func tabStopEvent() lifecycle.Event {
	return lifecycle.Event{
		Version:    lifecycle.Version,
		EventID:    "tab-stop-event",
		Kind:       lifecycle.KindContextStop,
		ObservedAt: testNow.Add(-time.Minute),
		Source:     "terminal",
		TabID:      "tab-1",
	}
}

func windowStopEvent() lifecycle.Event {
	return lifecycle.Event{
		Version:    lifecycle.Version,
		EventID:    "window-stop-event",
		Kind:       lifecycle.KindContextStop,
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
	return lifecycle.ProcessKeyString(childProcess().PID, childProcess().CreateTime)
}

func siblingChildProcessKey() string {
	proc := siblingChildProcess()
	return lifecycle.ProcessKeyString(proc.PID, proc.CreateTime)
}

func managedChild() lifecycle.ManagedProcess {
	return lifecycle.ManagedProcess{
		ProcessKey:        lifecycle.ProcessKey{PID: 20, CreateTime: 200},
		TerminalSessionID: "session-1",
		Name:              "node",
		Cwd:               "/repo",
	}
}

func managedHumanChild() lifecycle.ManagedProcess {
	managed := managedChild()
	managed.TerminalSessionID = "session-1"
	return managed
}

func managedSiblingChild() lifecycle.ManagedProcess {
	return lifecycle.ManagedProcess{
		ProcessKey:        lifecycle.ProcessKey{PID: 21, CreateTime: 201},
		TerminalSessionID: "session-2",
		Name:              "node",
		Cwd:               "/repo",
	}
}
