package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

const bytesPerMegabyte = 1024 * 1024

type Process struct {
	PID         int32
	CreateTime  int64
	ParentPID   int32
	Name        string
	CommandLine string
	Cwd         string
	CPUPercent  float64
	MemoryMB    uint64
}

type Lister interface {
	List(ctx context.Context) ([]Process, error)
}

type GopsutilLister struct {
	mu      sync.Mutex
	samples map[int32]cpuSample
	now     func() time.Time
}

type systemProcess interface {
	NameWithContext(context.Context) (string, error)
	CreateTimeWithContext(context.Context) (int64, error)
	MemoryInfoWithContext(context.Context) (*process.MemoryInfoStat, error)
	PpidWithContext(context.Context) (int32, error)
	CmdlineWithContext(context.Context) (string, error)
	CwdWithContext(context.Context) (string, error)
	TimesWithContext(context.Context) (*cpu.TimesStat, error)
}

var listProcesses = process.ProcessesWithContext

func NewLister() *GopsutilLister {
	return &GopsutilLister{now: time.Now}
}

func CreateTime(ctx context.Context, pid int32) (int64, error) {
	proc, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return 0, fmt.Errorf("opening process %d: %w", pid, err)
	}
	createTime, err := proc.CreateTimeWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading process %d create time: %w", pid, err)
	}
	return createTime, nil
}

func IsGone(err error) bool {
	if errors.Is(err, process.ErrorProcessNotRunning) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return errors.Is(err, syscall.ESRCH)
}

func (l *GopsutilLister) List(ctx context.Context) ([]Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.samples == nil {
		if _, err := l.list(ctx); err != nil {
			return nil, err
		}
		if err := waitForCPUSample(ctx); err != nil {
			return nil, err
		}
	}
	return l.list(ctx)
}

func (l *GopsutilLister) list(ctx context.Context) ([]Process, error) {
	procs, err := listProcesses(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}
	return l.snapshot(ctx, procs), nil
}

func (l *GopsutilLister) snapshot(ctx context.Context, procs []*process.Process) []Process {
	result := make([]Process, 0, len(procs))
	samples := make(map[int32]cpuSample, len(procs))
	for _, p := range procs {
		info, err := getProcessInfo(ctx, p.Pid, p)
		if err != nil {
			continue
		}
		info.CPUPercent = l.sampleCPU(ctx, info, p, samples)
		result = append(result, info)
	}
	l.samples = samples
	return result
}

func getProcessInfo(ctx context.Context, pid int32, p systemProcess) (info Process, err error) {
	info.PID = pid
	info.Name, err = p.NameWithContext(ctx)
	if err != nil {
		return Process{}, err
	}
	info.MemoryMB, err = memoryMB(ctx, p)
	if err != nil {
		return Process{}, err
	}
	info.CreateTime, err = p.CreateTimeWithContext(ctx)
	if err != nil {
		return Process{}, err
	}
	info.ParentPID = parentPID(ctx, p)
	info.CommandLine = commandLine(ctx, p)
	info.Cwd = cwd(ctx, p)
	return info, nil
}

func memoryMB(ctx context.Context, p systemProcess) (uint64, error) {
	memInfo, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return 0, err
	}
	rssMB := memInfo.RSS / bytesPerMegabyte
	return rssMB, nil
}

func parentPID(ctx context.Context, p systemProcess) int32 {
	parentPID, err := p.PpidWithContext(ctx)
	if err != nil {
		return 0
	}
	return parentPID
}

func commandLine(ctx context.Context, p systemProcess) string {
	commandLine, err := p.CmdlineWithContext(ctx)
	if err != nil {
		return ""
	}
	return commandLine
}

func cwd(ctx context.Context, p systemProcess) string {
	cwd, err := p.CwdWithContext(ctx)
	if err != nil {
		return ""
	}
	return cwd
}

type cpuSample struct {
	createTime int64
	total      float64
	at         time.Time
}

func (l *GopsutilLister) sampleCPU(
	ctx context.Context,
	proc Process,
	p systemProcess,
	next map[int32]cpuSample,
) float64 {
	times, err := p.TimesWithContext(ctx)
	if err != nil {
		return 0
	}
	total := times.User + times.System
	current := cpuSample{createTime: proc.CreateTime, total: total, at: l.now()}
	next[proc.PID] = current
	return intervalCPU(l.samples[proc.PID], current)
}

func intervalCPU(previous cpuSample, current cpuSample) float64 {
	needsBaseline := previous.createTime != current.createTime || previous.at.IsZero()
	if needsBaseline {
		return 0
	}
	elapsed := current.at.Sub(previous.at).Seconds()
	used := current.total - previous.total
	invalidDelta := elapsed <= 0 || used < 0
	if invalidDelta {
		return 0
	}
	percent := 100 * (used / elapsed)
	return percent
}

// A short first interval gives one-shot scans a recent CPU reading too.
const initialCPUInterval = 100 * time.Millisecond

func waitForCPUSample(ctx context.Context) error {
	timer := time.NewTimer(initialCPUInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
