package daemon

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/lifecycle"
	"github.com/yowainwright/pk/internal/process"
)

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
	inactive.Kind = lifecycle.KindSessionInactive
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

func unreadableShellStore(events ...lifecycle.Event) *fakeStore {
	events = append([]lifecycle.Event{sessionStartEvent()}, events...)
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
