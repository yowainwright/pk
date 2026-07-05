package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/process"
)

func TestCheckRecordsOffenseBeforeGracePeriod(t *testing.T) {
	monitor := testMonitor(applyConfig())
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}

	monitor.check(context.Background())

	if len(monitor.offenses) != 1 {
		t.Fatalf("expected one offense, got %d", len(monitor.offenses))
	}
}

func TestCheckKillsAfterGracePeriod(t *testing.T) {
	cfg := applyConfig()
	cfg.GracePeriod = 0
	killer := &fakeKiller{}
	monitor := testMonitorWithKiller(cfg, killer)
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}

	monitor.check(context.Background())
	monitor.check(context.Background())

	if killer.pid != 42 {
		t.Fatalf("expected killed pid 42, got %d", killer.pid)
	}
}

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	monitor := testMonitor(applyConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := monitor.Run(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
}

func TestCheckDryRunDoesNotKill(t *testing.T) {
	cfg := dryRunConfig()
	cfg.GracePeriod = 0
	killer := &fakeKiller{}
	monitor := testMonitorWithKiller(cfg, killer)
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}

	monitor.check(context.Background())
	monitor.check(context.Background())

	if killer.called {
		t.Fatal("expected dry run not to kill")
	}
}

func TestCheckNotifiesAfterKill(t *testing.T) {
	cfg := applyConfig()
	cfg.GracePeriod = 0
	killer := &fakeKiller{}
	notified := false
	monitor := New(cfg, &fakeLister{}, killer, func(string, int32) {
		notified = true
	})
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}

	monitor.check(context.Background())
	monitor.check(context.Background())

	if !notified {
		t.Fatal("expected kill notification")
	}
}

func TestCheckDoesNotNotifyWhenKillFails(t *testing.T) {
	cfg := applyConfig()
	cfg.GracePeriod = 0
	killer := &fakeKiller{err: errors.New("denied")}
	notified := false
	monitor := New(cfg, &fakeLister{}, killer, func(string, int32) {
		notified = true
	})
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}

	monitor.check(context.Background())
	monitor.check(context.Background())

	if notified {
		t.Fatal("expected no notification")
	}
}

func TestCheckSkipsProtectedProcesses(t *testing.T) {
	monitor := testMonitor(applyConfig())
	protected := overCPUProcess()
	protected.Name = "Code"
	monitor.lister = &fakeLister{procs: processes(protected)}

	monitor.check(context.Background())

	if len(monitor.offenses) != 0 {
		t.Fatalf("expected no offenses, got %d", len(monitor.offenses))
	}
}

func TestCheckDeletesGoneOffense(t *testing.T) {
	monitor := testMonitor(applyConfig())
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}
	monitor.check(context.Background())

	monitor.lister = &fakeLister{}
	monitor.check(context.Background())

	if len(monitor.offenses) != 0 {
		t.Fatalf("expected no offenses, got %d", len(monitor.offenses))
	}
}

func TestCheckDeletesRecoveredOffense(t *testing.T) {
	monitor := testMonitor(applyConfig())
	monitor.lister = &fakeLister{procs: processes(overCPUProcess())}
	monitor.check(context.Background())

	monitor.lister = &fakeLister{procs: processes(normalProcess())}
	monitor.check(context.Background())

	if len(monitor.offenses) != 0 {
		t.Fatalf("expected no offenses, got %d", len(monitor.offenses))
	}
}

func TestCheckRecordsMemoryOffense(t *testing.T) {
	monitor := testMonitor(applyConfig())
	monitor.lister = &fakeLister{procs: processes(overMemoryProcess())}

	monitor.check(context.Background())

	if len(monitor.offenses) != 1 {
		t.Fatalf("expected one offense, got %d", len(monitor.offenses))
	}
}

func TestCheckIgnoresListerErrors(t *testing.T) {
	monitor := testMonitor(applyConfig())
	monitor.lister = &fakeLister{err: errors.New("denied")}

	monitor.check(context.Background())

	if len(monitor.offenses) != 0 {
		t.Fatalf("expected no offenses, got %d", len(monitor.offenses))
	}
}

func TestKillReasonUsesMemoryThreshold(t *testing.T) {
	monitor := testMonitor(applyConfig())

	reason := monitor.killReason(overMemoryProcess())

	if reason != "memory" {
		t.Fatalf("expected memory reason, got %q", reason)
	}
}

func TestKillReasonFallsBackToThresholdExceeded(t *testing.T) {
	monitor := testMonitor(applyConfig())

	reason := monitor.killReason(normalProcess())

	if reason != "threshold exceeded" {
		t.Fatalf("expected fallback reason, got %q", reason)
	}
}

type fakeLister struct {
	procs []process.Process
	err   error
}

func (l *fakeLister) List(ctx context.Context) ([]process.Process, error) {
	return l.procs, l.err
}

type fakeKiller struct {
	called bool
	pid    int32
	err    error
}

func (k *fakeKiller) Kill(ctx context.Context, pid int32) error {
	k.called = true
	k.pid = pid
	return k.err
}

func testMonitor(cfg *config.Config) *Monitor {
	return testMonitorWithKiller(cfg, &fakeKiller{})
}

func testMonitorWithKiller(cfg *config.Config, killer *fakeKiller) *Monitor {
	return New(cfg, &fakeLister{}, killer, nil)
}

func applyConfig() *config.Config {
	cfg := baseConfig()
	cfg.DryRun = false
	return cfg
}

func dryRunConfig() *config.Config {
	cfg := baseConfig()
	cfg.DryRun = true
	return cfg
}

func baseConfig() *config.Config {
	cfg := &config.Config{}
	cfg.CPUThreshold = 80
	cfg.MemoryThreshold = 1024
	cfg.Interval = time.Millisecond
	cfg.GracePeriod = time.Hour
	cfg.Protected = []string{"Code"}
	return cfg
}

func overCPUProcess() process.Process {
	proc := normalProcess()
	proc.CPUPercent = 95
	return proc
}

func overMemoryProcess() process.Process {
	proc := normalProcess()
	proc.MemoryMB = 2048
	return proc
}

func normalProcess() process.Process {
	var proc process.Process
	proc.PID = 42
	proc.Name = "node"
	proc.CPUPercent = 1
	return proc
}

func processes(proc process.Process) []process.Process {
	procs := make([]process.Process, 0, 1)
	procs = append(procs, proc)
	return procs
}
