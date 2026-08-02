package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/killer"
	"github.com/yowainwright/pk/internal/process"
	"github.com/yowainwright/pk/internal/processtree"
)

type offense struct {
	firstSeen time.Time
	proc      process.Process
}

type Options struct {
	Apply  bool
	Logger *slog.Logger
}

type Monitor struct {
	cfg    *config.Config
	lister process.Lister
	killer killer.Killer
	notify func(name string, pid int32) error
	apply  bool
	logger *slog.Logger

	mu       sync.Mutex
	offenses map[int32]*offense
}

func New(
	cfg *config.Config,
	lister process.Lister,
	k killer.Killer,
	notify func(string, int32) error,
	options Options,
) *Monitor {
	return &Monitor{
		cfg:      cfg,
		lister:   lister,
		killer:   k,
		notify:   notify,
		apply:    options.Apply,
		logger:   monitorLogger(options.Logger),
		offenses: make(map[int32]*offense),
	}
}

func monitorLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.New(slog.DiscardHandler)
}

func (m *Monitor) Run(ctx context.Context) error {
	m.logStart()

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	return m.runLoop(ctx, ticker.C)
}

func (m *Monitor) logStart() {
	m.logger.Info("Monitoring started",
		"cpu", m.cfg.CPUThreshold,
		"mem_mb", m.cfg.MemoryThreshold,
		"interval", m.cfg.Interval,
		"grace", m.cfg.GracePeriod,
		"apply", m.apply,
	)
}

func (m *Monitor) runLoop(ctx context.Context, ticks <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Shutting down monitor")
			return ctx.Err()
		case <-ticks:
			m.check(ctx)
		}
	}
}

func (m *Monitor) check(ctx context.Context) {
	procs, err := m.lister.List(ctx)
	if err != nil {
		m.logger.Error("Failed to list processes", "error", err)
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

	descendants := m.killDescendants(procs, p.PID)
	m.killExpiredOffense(ctx, p, descendants)
}

func (m *Monitor) killDescendants(
	procs []process.Process,
	pid int32,
) []process.Process {
	descendants := processtree.Descendants(procs, pid)
	filtered := make([]process.Process, 0, len(descendants))
	for _, descendant := range descendants {
		if !m.cfg.IsProtected(descendant.Name) {
			filtered = append(filtered, descendant)
		}
	}
	return filtered
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
	m.logger.Warn("Process exceeding threshold",
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
	if !m.apply {
		m.logPreview(p)
		return
	}
	m.logKill(p, duration)

	if !m.killTreeAndLog(ctx, p, descendants) {
		return
	}

	m.logger.Info("Process terminated", "pid", p.PID, "name", p.Name)
	m.notifyKilled(p)
}

func (m *Monitor) logPreview(p process.Process) {
	m.logger.Info("Preview - skipping kill", "pid", p.PID, "name", p.Name)
}

func (m *Monitor) killTreeAndLog(
	ctx context.Context,
	p process.Process,
	descendants []process.Process,
) bool {
	rootKilled, err := m.killTree(ctx, p, descendants)
	if err != nil {
		m.logger.Error("Failed to kill process", "pid", p.PID, "name", p.Name, "error", err)
	}
	return rootKilled
}

func (m *Monitor) killTree(
	ctx context.Context,
	p process.Process,
	descendants []process.Process,
) (bool, error) {
	var firstErr error
	rootKilled := false
	for _, proc := range processtree.KillOrder(p, descendants) {
		if err := m.killer.Kill(ctx, proc.PID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if proc.PID == p.PID {
			rootKilled = true
		}
	}
	return rootKilled, firstErr
}

func (m *Monitor) notifyKilled(p process.Process) {
	if m.notify == nil {
		return
	}
	if err := m.notify(p.Name, p.PID); err != nil {
		m.logger.Warn("Notification failed", "pid", p.PID, "name", p.Name, "error", err)
	}
}

func (m *Monitor) logKill(p process.Process, duration time.Duration) {
	m.logger.Warn("Killing process",
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
