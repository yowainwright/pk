package cleanup

import (
	"strings"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/process"
)

func BenchmarkMatchesAnyCommand(b *testing.B) {
	arguments := strings.Repeat(" --argument=value", 100)
	cases := []commandMatchCase{
		{"name", "node", "node server.js", true},
		{"command", "server", "/usr/bin/node server.js", true},
		{"long command", "server", "/usr/bin/node" + arguments, true},
		{"no match", "server", "/usr/bin/postgres" + arguments, false},
	}
	for _, tc := range cases {
		b.Run(tc.label, func(b *testing.B) { benchmarkCommandMatch(b, tc) })
	}
}

func benchmarkCommandMatch(b *testing.B, tc commandMatchCase) {
	commands := []string{"node", "bun", "npm", "pnpm", "yarn", "go", "python", "python3"}
	b.ReportAllocs()
	for b.Loop() {
		got := matchesAnyCommand(tc.name, tc.command, commands)
		if got != tc.want {
			b.Fatalf("match = %v, want %v", got, tc.want)
		}
	}
}

func benchmarkProcessTree() []process.Process {
	procs := make([]process.Process, 129)
	procs[0] = overCPUProcess()
	for i := 1; i < len(procs); i++ {
		procs[i] = process.Process{PID: int32(i + 42), ParentPID: 42, Name: "node"}
	}
	procs[1].Name = "Code"
	return procs
}

func BenchmarkUnprotectedDescendants(b *testing.B) {
	procs := benchmarkProcessTree()
	tree := process.NewIndex(procs)
	cfg := baseConfig()
	b.ReportAllocs()
	for b.Loop() {
		descendants := unprotectedDescendants(cfg, procs[0], tree)
		if len(descendants) != 127 {
			b.Fatalf("got %d descendants, want 127", len(descendants))
		}
	}
}

func BenchmarkMonitorHandleProcess(b *testing.B) {
	b.Run("grace", func(b *testing.B) { benchmarkMonitorProcess(b, applyMode, time.Hour) })
	b.Run("preview", func(b *testing.B) { benchmarkMonitorProcess(b, previewMode, 0) })
}

func benchmarkMonitorProcess(b *testing.B, apply bool, grace time.Duration) {
	procs := benchmarkProcessTree()
	tree := process.NewIndex(procs)
	cfg := baseConfig()
	cfg.GracePeriod = grace
	monitor := testMonitor(cfg)
	monitor.apply = apply
	off := &offense{firstSeen: time.Now(), proc: procs[0]}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		monitor.offenses[procs[0].PID] = off
		monitor.handleProcess(ctx, procs[0], tree)
	}
}
