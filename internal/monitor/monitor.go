package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/killer"
	"github.com/jeffrywainwright/pk/internal/process"
)

type offense struct {
	firstSeen time.Time
	proc      process.Process
}

type Monitor struct {
	cfg     *config.Config
	lister  process.Lister
	killer  killer.Killer
	notify  func(name string, pid int32)

	mu       sync.Mutex
	offenses map[int32]*offense
}

func New(cfg *config.Config, lister process.Lister, k killer.Killer, notify func(string, int32)) *Monitor {
	return &Monitor{
		cfg:      cfg,
		lister:   lister,
		killer:   k,
		notify:   notify,
		offenses: make(map[int32]*offense),
	}
}

func (m *Monitor) Run(ctx context.Context) error {
	log.Info("Monitoring started",
		"cpu", m.cfg.CPUThreshold,
		"mem_mb", m.cfg.MemoryThreshold,
		"interval", m.cfg.Interval,
		"grace", m.cfg.GracePeriod,
		"dry_run", m.cfg.DryRun,
	)

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Shutting down monitor")
			return ctx.Err()
		case <-ticker.C:
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

	seen := make(map[int32]bool)

	for _, p := range procs {
		seen[p.PID] = true

		if m.cfg.IsProtected(p.Name) {
			continue
		}

		exceeds := p.CPUPercent > m.cfg.CPUThreshold || p.MemoryMB > m.cfg.MemoryThreshold
		if !exceeds {
			delete(m.offenses, p.PID)
			continue
		}

		off, exists := m.offenses[p.PID]
		if !exists {
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
			continue
		}

		off.proc = p

		elapsed := time.Since(off.firstSeen)
		if elapsed < m.cfg.GracePeriod {
			continue
		}

		m.killProcess(ctx, p, elapsed)
		delete(m.offenses, p.PID)
	}

	for pid := range m.offenses {
		if !seen[pid] {
			delete(m.offenses, pid)
		}
	}
}

func (m *Monitor) killProcess(ctx context.Context, p process.Process, duration time.Duration) {
	reason := "threshold exceeded"
	if p.CPUPercent > m.cfg.CPUThreshold {
		reason = "cpu"
	} else if p.MemoryMB > m.cfg.MemoryThreshold {
		reason = "memory"
	}

	log.Warn("Killing process",
		"pid", p.PID,
		"name", p.Name,
		"reason", reason,
		"cpu", p.CPUPercent,
		"mem_mb", p.MemoryMB,
		"duration", duration.Round(time.Second),
	)

	if m.cfg.DryRun {
		log.Info("Dry run - skipping kill", "pid", p.PID, "name", p.Name)
		return
	}

	if err := m.killer.Kill(ctx, p.PID); err != nil {
		log.Error("Failed to kill process", "pid", p.PID, "name", p.Name, "error", err)
		return
	}

	log.Info("Process terminated", "pid", p.PID, "name", p.Name)

	if m.notify != nil {
		m.notify(p.Name, p.PID)
	}
}
