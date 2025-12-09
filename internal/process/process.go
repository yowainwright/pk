package process

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/process"
)

type Process struct {
	PID        int32
	Name       string
	CPUPercent float64
	MemoryMB   uint64
}

type Lister interface {
	List(ctx context.Context) ([]Process, error)
}

type GopsutilLister struct{}

func NewLister() *GopsutilLister {
	return &GopsutilLister{}
}

func (l *GopsutilLister) List(ctx context.Context) ([]Process, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}

	result := make([]Process, 0, len(procs))
	for _, p := range procs {
		info, err := getProcessInfo(ctx, p)
		if err != nil {
			continue
		}
		result = append(result, info)
	}

	return result, nil
}

func getProcessInfo(ctx context.Context, p *process.Process) (Process, error) {
	name, err := p.NameWithContext(ctx)
	if err != nil {
		return Process{}, err
	}

	cpu, err := p.CPUPercentWithContext(ctx)
	if err != nil {
		cpu = 0
	}

	memInfo, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return Process{}, err
	}

	memMB := memInfo.RSS / (1024 * 1024)

	return Process{
		PID:        p.Pid,
		Name:       name,
		CPUPercent: cpu,
		MemoryMB:   memMB,
	}, nil
}
