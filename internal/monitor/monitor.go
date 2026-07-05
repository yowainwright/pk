package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/killer"
	"github.com/jeffrywainwright/pk/internal/process"
	"github.com/jeffrywainwright/pk/internal/processtree"
)

type offense struct {
	firstSeen time.Time
	proc      process.Process
}

type Monitor struct {
	cfg    *config.Config
	lister process.Lister
	killer killer.Killer
	notify func(name string, pid int32)

	mu       sync.Mutex
	offenses map[int32]*offense
}

func New(
	cfg *config.Config,
	lister process.Lister,
	k killer.Killer,
	notify func(string, int32),
) *Monitor {
	return &Monitor{
		cfg:      cfg,
		lister:   lister,
		killer:   k,
		notify:   notify,
		offenses: make(map[int32]*offense),
	}
}

func (m *Monitor) Run(ctx context.Context) error {
	m.logStart()

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	return m.runLoop(ctx, ticker.C)
}

func (m *Monitor) logStart() {
	log.Info("Monitoring started",
		"cpu", m.cfg.CPUThreshold,
		"mem_mb", m.cfg.MemoryThreshold,
		"interval", m.cfg.Interval,
		"grace", m.cfg.GracePeriod,
		"dry_run", m.cfg.DryRun,
	)
}

func (m *Monitor) runLoop(ctx context.Context, ticks <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			log.Info("Shutting down monitor")
			return ctx.Err()
		case <-ticks:
			m.check(ctx)
		}
	}
}

func (m *Monitor) check(ctx context.Context) {
	procs, err := m.lister.List(ctx)
	if err != nil {
		log.Error("Failed to list processes", "error", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	seen := m.handleProcesses(ctx, procs)
	m.deleteGoneOffenses(seen)
}

func (m *Monitor) handleProcesses(ctx context.Context, procs []process.Process) map[int32]bool {
	seen := make(map[int32]bool)

	for _, p := range procs {
		seen[p.PID] = true
		m.handleProcess(ctx, p, procs)
	}

	return seen
}

func (m *Monitor) handleProcess(ctx context.Context, p process.Process, procs []process.Process) {
	if m.cfg.IsProtected(p.Name) {
		return
	}

	if !m.exceedsThreshold(p) {
		delete(m.offenses, p.PID)
		return
	}

	if m.recordNewOffense(p) {
		return
	}

	descendants := processtree.Descendants(procs, p.PID)
	m.killExpiredOffense(ctx, p, descendants)
}

func (m *Monitor) recordNewOffense(p process.Process) bool {
	_, exists := m.offenses[p.PID]
	if exists {
		return false
	}
	m.recordOffense(p)
	return true
}

func (m *Monitor) killExpiredOffense(
	ctx context.Context,
	p process.Process,
	descendants []process.Process,
) {
	off := m.offenses[p.PID]
	off.proc = p
	elapsed := time.Since(off.firstSeen)
	if elapsed < m.cfg.GracePeriod {
		return
	}

	m.killProcess(ctx, p, descendants, elapsed)
	delete(m.offenses, p.PID)
}

func (m *Monitor) deleteGoneOffenses(seen map[int32]bool) {
	for pid := range m.offenses {
		if !seen[pid] {
			delete(m.offenses, pid)
		}
	}
}

func (m *Monitor) exceedsThreshold(p process.Process) bool {
	cpuExceeded := p.CPUPercent > m.cfg.CPUThreshold
	memoryExceeded := p.MemoryMB > m.cfg.MemoryThreshold
	return cpuExceeded || memoryExceeded
}

func (m *Monitor) recordOffense(p process.Process) {
	m.offenses[p.PID] = &offense{
		firstSeen: time.Now(),
		proc:      p,
	}
	log.Warn("Process exceeding threshold",
		"pid", p.PID,
		"name", p.Name,
		"cpu", p.CPUPercent,
		"mem_mb", p.MemoryMB,
	)
}

func (m *Monitor) killProcess(
	ctx context.Context,
	p process.Process,
	descendants []process.Process,
	duration time.Duration,
) {
	m.logKill(p, duration)

	if m.cfg.DryRun {
		m.logDryRun(p)
		return
	}

	if !m.killTreeAndLog(ctx, p, descendants) {
		return
	}

	log.Info("Process terminated", "pid", p.PID, "name", p.Name)
	m.notifyKilled(p)
}

func (m *Monitor) logDryRun(p process.Process) {
	log.Info("Dry run - skipping kill", "pid", p.PID, "name", p.Name)
}

func (m *Monitor) killTreeAndLog(
	ctx context.Context,
	p process.Process,
	descendants []process.Process,
) bool {
	if err := m.killTree(ctx, p, descendants); err != nil {
		log.Error("Failed to kill process", "pid", p.PID, "name", p.Name, "error", err)
		return false
	}
	return true
}

func (m *Monitor) killTree(
	ctx context.Context,
	p process.Process,
	descendants []process.Process,
) error {
	for _, proc := range processtree.KillOrder(p, descendants) {
		if err := m.killer.Kill(ctx, proc.PID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) notifyKilled(p process.Process) {
	if m.notify != nil {
		m.notify(p.Name, p.PID)
	}
}

func (m *Monitor) logKill(p process.Process, duration time.Duration) {
	log.Warn("Killing process",
		"pid", p.PID,
		"name", p.Name,
		"reason", m.killReason(p),
		"cpu", p.CPUPercent,
		"mem_mb", p.MemoryMB,
		"duration", duration.Round(time.Second),
	)
}

func (m *Monitor) killReason(p process.Process) string {
	if p.CPUPercent > m.cfg.CPUThreshold {
		return "cpu"
	}
	if p.MemoryMB > m.cfg.MemoryThreshold {
		return "memory"
	}
	return "threshold exceeded"
}
