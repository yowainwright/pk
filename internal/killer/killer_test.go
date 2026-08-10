package killer

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	gopsutilProcess "github.com/shirou/gopsutil/v4/process"
	appProcess "github.com/yowainwright/pk/internal/process"
)

const testCreateTime int64 = 123

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
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()
	restoreCreateTime := replaceCreateTimeReader(t, createTimeFor(proc))
	defer restoreCreateTime()
	killer := testKiller()

	err := killer.Kill(context.Background(), testTarget(42))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	assertSignals(t, proc.signals, syscall.SIGTERM)
}

func TestKillEscalatesToKillWhenProcessStaysAlive(t *testing.T) {
	proc := &fakeProcess{alive: true, stayAlive: true}
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()
	restoreCreateTime := replaceCreateTimeReader(t, matchingCreateTime)
	defer restoreCreateTime()
	killer := testKiller()

	err := killer.Kill(context.Background(), testTarget(42))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	assertSignals(t, proc.signals, syscall.SIGTERM, syscall.SIGKILL)
}

func TestKillReturnsFindProcessErrors(t *testing.T) {
	restoreFind := replaceFindProcess(t, nil, errors.New("missing"))
	defer restoreFind()
	restoreCreateTime := replaceCreateTimeReader(t, matchingCreateTime)
	defer restoreCreateTime()

	err := New().Kill(context.Background(), testTarget(42))

	if err == nil {
		t.Fatal("expected find process error")
	}
}

func TestWaitForExitReturnsTrueForMissingProcess(t *testing.T) {
	restore := replaceCreateTimeReader(t, missingCreateTime)
	defer restore()
	killer := testKiller()

	terminated, err := killer.waitForExit(context.Background(), testTarget(42))
	if err != nil {
		t.Fatalf("wait for exit: %v", err)
	}
	if !terminated {
		t.Fatal("expected missing process to be terminated")
	}
}

func TestWaitForExitReturnsFalseWhenContextCancelled(t *testing.T) {
	killer := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	terminated, err := killer.waitForExit(ctx, testTarget(1))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if terminated {
		t.Fatal("expected cancelled wait to return false")
	}
}

func TestKillRejectsReusedPIDBeforeTerm(t *testing.T) {
	proc := &fakeProcess{alive: true, stayAlive: true}
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()
	restoreCreateTime := replaceCreateTimeReader(t, reusedCreateTime)
	defer restoreCreateTime()

	err := testKiller().Kill(context.Background(), testTarget(42))

	assertErrorContains(t, err, "identity changed")
	assertSignals(t, proc.signals)
}

func TestKillRejectsReusedPIDBeforeKill(t *testing.T) {
	proc := &fakeProcess{alive: true, stayAlive: true}
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()
	createTimes := []int64{testCreateTime, testCreateTime + 1}
	restoreCreateTime := replaceCreateTimeReader(t, sequenceCreateTime(createTimes))
	defer restoreCreateTime()
	killer := &SignalKiller{termTimeout: time.Millisecond, pollInterval: time.Hour}

	err := killer.Kill(context.Background(), testTarget(42))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	assertSignals(t, proc.signals, syscall.SIGTERM)
}

func TestKillDoesNotEscalateAfterPIDReuse(t *testing.T) {
	proc := &fakeProcess{alive: true, stayAlive: true}
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()
	createTimes := []int64{testCreateTime, testCreateTime + 1}
	restoreCreateTime := replaceCreateTimeReader(t, sequenceCreateTime(createTimes))
	defer restoreCreateTime()

	err := testKiller().Kill(context.Background(), testTarget(42))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	assertSignals(t, proc.signals, syscall.SIGTERM)
}

func TestKillRejectsMissingCreateTime(t *testing.T) {
	proc := &fakeProcess{alive: true}
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()

	err := testKiller().Kill(context.Background(), appProcess.Process{PID: 42})

	assertErrorContains(t, err, "no creation time")
	assertSignals(t, proc.signals)
}

func TestKillFailsClosedWhenCreateTimeCannotBeRead(t *testing.T) {
	proc := &fakeProcess{alive: true}
	restoreFind := replaceFindProcess(t, proc, nil)
	defer restoreFind()
	readerErr := errors.New("permission denied")
	reader := func(ctx context.Context, pid int32) (int64, error) {
		return 0, readerErr
	}
	restoreCreateTime := replaceCreateTimeReader(t, reader)
	defer restoreCreateTime()

	err := testKiller().Kill(context.Background(), testTarget(42))

	assertErrorContains(t, err, "reading process 42 creation time")
	assertSignals(t, proc.signals)
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

func replaceCreateTimeReader(
	t *testing.T,
	reader func(context.Context, int32) (int64, error),
) func() {
	t.Helper()
	oldReader := readProcessCreateTime
	readProcessCreateTime = reader
	return func() {
		readProcessCreateTime = oldReader
	}
}

func createTimeFor(proc *fakeProcess) func(context.Context, int32) (int64, error) {
	return func(ctx context.Context, pid int32) (int64, error) {
		if !proc.alive {
			return 0, gopsutilProcess.ErrorProcessNotRunning
		}
		return testCreateTime, nil
	}
}

func matchingCreateTime(ctx context.Context, pid int32) (int64, error) {
	return testCreateTime, nil
}

func missingCreateTime(ctx context.Context, pid int32) (int64, error) {
	return 0, gopsutilProcess.ErrorProcessNotRunning
}

func reusedCreateTime(ctx context.Context, pid int32) (int64, error) {
	return testCreateTime + 1, nil
}

func sequenceCreateTime(createTimes []int64) func(context.Context, int32) (int64, error) {
	index := 0
	return func(ctx context.Context, pid int32) (int64, error) {
		createTime := createTimes[index]
		if index < len(createTimes)-1 {
			index++
		}
		return createTime, nil
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

func assertErrorContains(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", expected)
	}
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got %v", expected, err)
	}
}

func testTarget(pid int32) appProcess.Process {
	return appProcess.Process{PID: pid, CreateTime: testCreateTime}
}
