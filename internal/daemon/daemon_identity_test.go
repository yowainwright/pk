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
	store.events = []lifecycle.Event{sessionStopEvent()}
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
