package killer

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNewUsesTermTimeout(t *testing.T) {
	killer := New()

	if killer.termTimeout != 2*time.Second {
		t.Fatalf("expected two second timeout, got %s", killer.termTimeout)
	}
	if killer.pollInterval != defaultPollInterval {
		t.Fatalf("expected default poll interval, got %s", killer.pollInterval)
	}
}

func TestKillSendsTermWithoutKillAfterProcessExits(t *testing.T) {
	proc := &fakeProcess{alive: true}
	restore := replaceFindProcess(t, proc, nil)
	defer restore()
	killer := testKiller()

	err := killer.Kill(context.Background(), 42)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	assertSignals(t, proc.signals, syscall.SIGTERM)
}

func TestKillEscalatesToKillWhenProcessStaysAlive(t *testing.T) {
	proc := &fakeProcess{alive: true, stayAlive: true}
	restore := replaceFindProcess(t, proc, nil)
	defer restore()
	killer := testKiller()

	err := killer.Kill(context.Background(), 42)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	assertSignals(t, proc.signals, syscall.SIGTERM, syscall.SIGKILL)
}

func TestKillReturnsFindProcessErrors(t *testing.T) {
	restore := replaceFindProcess(t, nil, errors.New("missing"))
	defer restore()

	err := New().Kill(context.Background(), 42)

	if err == nil {
		t.Fatal("expected find process error")
	}
}

func TestWaitForExitReturnsTrueForMissingProcess(t *testing.T) {
	killer := New()
	timeout := time.Second

	terminated := killer.waitForExit(context.Background(), missingPID(), timeout)

	if !terminated {
		t.Fatal("expected missing process to be terminated")
	}
}

func TestWaitForExitReturnsFalseWhenContextCancelled(t *testing.T) {
	killer := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	terminated := killer.waitForExit(ctx, 1, time.Second)

	if terminated {
		t.Fatal("expected cancelled wait to return false")
	}
}

func TestProcessExistsReturnsFalseForMissingProcess(t *testing.T) {
	if processExists(missingPID()) {
		t.Fatal("expected missing process not to exist")
	}
}

func TestSignalProcessIgnoresDoneProcesses(t *testing.T) {
	proc := &fakeProcess{err: os.ErrProcessDone}

	err := signalProcess(proc, 42, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("expected process done to be ignored, got %v", err)
	}
}

func TestSignalProcessWrapsErrors(t *testing.T) {
	proc := &fakeProcess{err: errors.New("denied")}

	err := signalProcess(proc, 42, syscall.SIGTERM)

	if err == nil {
		t.Fatal("expected signal error")
	}
}

type fakeProcess struct {
	alive     bool
	stayAlive bool
	signals   []os.Signal
	err       error
}

func (p *fakeProcess) Signal(signal os.Signal) error {
	if p.err != nil {
		return p.err
	}
	if signal == syscall.Signal(0) {
		return p.existsError()
	}
	p.signals = append(p.signals, signal)
	shouldExit := signal == syscall.SIGTERM && !p.stayAlive
	if shouldExit {
		p.alive = false
	}
	return nil
}

func (p *fakeProcess) existsError() error {
	if p.alive {
		return nil
	}
	return os.ErrProcessDone
}

func replaceFindProcess(t *testing.T, proc processHandle, err error) func() {
	t.Helper()
	oldFindProcess := findProcess
	findProcess = func(pid int32) (processHandle, error) {
		return proc, err
	}
	return func() {
		findProcess = oldFindProcess
	}
}

func testKiller() *SignalKiller {
	return &SignalKiller{
		termTimeout:  20 * time.Millisecond,
		pollInterval: time.Millisecond,
	}
}

func assertSignals(t *testing.T, actual []os.Signal, expected ...os.Signal) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("expected signals %#v, got %#v", expected, actual)
	}
	for i, signal := range expected {
		if actual[i] != signal {
			t.Fatalf("expected signal %v at %d, got %v", signal, i, actual[i])
		}
	}
}

func missingPID() int32 {
	return -1
}
