package process

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/process"
)

const bytesPerMegabyte = 1024 * 1024

type Process struct {
	PID         int32
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

type GopsutilLister struct{}

type systemProcess interface {
	NameWithContext(context.Context) (string, error)
	MemoryInfoWithContext(context.Context) (*process.MemoryInfoStat, error)
	PpidWithContext(context.Context) (int32, error)
	CmdlineWithContext(context.Context) (string, error)
	CwdWithContext(context.Context) (string, error)
	CPUPercentWithContext(context.Context) (float64, error)
}

var listProcesses = process.ProcessesWithContext

func NewLister() *GopsutilLister {
	return &GopsutilLister{}
}

func (l *GopsutilLister) List(ctx context.Context) ([]Process, error) {
	procs, err := listProcesses(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}

	result := make([]Process, 0, len(procs))
	for _, p := range procs {
		info, err := getProcessInfo(ctx, p.Pid, p)
		if err != nil {
			continue
		}
		result = append(result, info)
	}

	return result, nil
}

func getProcessInfo(ctx context.Context, pid int32, p systemProcess) (Process, error) {
	name, err := p.NameWithContext(ctx)
	if err != nil {
		return Process{}, err
	}

	memMB, err := memoryMB(ctx, p)
	if err != nil {
		return Process{}, err
	}

	metadata := processMetadata(ctx, p)
	return newProcess(pid, name, memMB, metadata), nil
}

type metadata struct {
	parentPID   int32
	commandLine string
	cwd         string
	cpuPercent  float64
}

func processMetadata(ctx context.Context, p systemProcess) metadata {
	return metadata{
		parentPID:   parentPID(ctx, p),
		commandLine: commandLine(ctx, p),
		cwd:         cwd(ctx, p),
		cpuPercent:  cpuPercent(ctx, p),
	}
}

func newProcess(pid int32, name string, memMB uint64, metadata metadata) Process {
	return Process{
		PID:         pid,
		ParentPID:   metadata.parentPID,
		Name:        name,
		CommandLine: metadata.commandLine,
		Cwd:         metadata.cwd,
		CPUPercent:  metadata.cpuPercent,
		MemoryMB:    memMB,
	}
}

func cpuPercent(ctx context.Context, p systemProcess) float64 {
	cpu, err := p.CPUPercentWithContext(ctx)
	if err != nil {
		return 0
	}
	return cpu
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
