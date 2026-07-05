package process

import (
	"context"
	"errors"
	"os"
	"testing"

	gopsutilProcess "github.com/shirou/gopsutil/v4/process"
)

func TestNewProcessMapsMetadata(t *testing.T) {
	data := metadata{
		parentPID:   7,
		commandLine: "node server.js",
		cwd:         "/Users/jeff/code/app",
		cpuPercent:  42,
	}

	proc := newProcess(9, "node", 128, data)

	if proc.PID != 9 {
		t.Fatalf("expected pid 9, got %d", proc.PID)
	}
	if proc.ParentPID != 7 {
		t.Fatalf("expected parent pid 7, got %d", proc.ParentPID)
	}
	assertProcessMetadata(t, proc)
}

func TestNewListerReturnsGopsutilLister(t *testing.T) {
	lister := NewLister()

	if lister == nil {
		t.Fatal("expected lister")
	}
}

func TestGetProcessInfoReadsCurrentProcess(t *testing.T) {
	proc := currentGopsutilProcess(t)

	info, err := getProcessInfo(context.Background(), proc.Pid, proc)
	if err != nil {
		t.Fatalf("get process info: %v", err)
	}
	if info.PID != int32(os.Getpid()) {
		t.Fatalf("expected current pid, got %d", info.PID)
	}
}

func TestGetProcessInfoReturnsNameErrors(t *testing.T) {
	proc := testSystemProcess()
	proc.nameErr = errors.New("name denied")

	_, err := getProcessInfo(context.Background(), 42, proc)

	if err == nil {
		t.Fatal("expected name error")
	}
}

func TestGetProcessInfoReturnsMemoryErrors(t *testing.T) {
	proc := testSystemProcess()
	proc.memoryErr = errors.New("memory denied")

	_, err := getProcessInfo(context.Background(), 42, proc)

	if err == nil {
		t.Fatal("expected memory error")
	}
}

func TestGetProcessInfoDefaultsOptionalMetadataErrors(t *testing.T) {
	proc := testSystemProcess()
	proc.cpuErr = errors.New("cpu denied")
	proc.ppidErr = errors.New("ppid denied")
	proc.cmdlineErr = errors.New("command denied")
	proc.cwdErr = errors.New("cwd denied")

	info, err := getProcessInfo(context.Background(), 42, proc)
	if err != nil {
		t.Fatalf("get process info: %v", err)
	}
	assertOptionalMetadataDefaults(t, info)
}

func TestGopsutilListerListsProcesses(t *testing.T) {
	proc := currentGopsutilProcess(t)
	restore := replaceListProcesses(t, []*gopsutilProcess.Process{proc}, nil)
	defer restore()
	lister := NewLister()

	procs, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("list processes: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected processes")
	}
}

func TestGopsutilListerReturnsListErrors(t *testing.T) {
	restore := replaceListProcesses(t, nil, errors.New("denied"))
	defer restore()
	lister := NewLister()

	_, err := lister.List(context.Background())

	if err == nil {
		t.Fatal("expected list error")
	}
}

type fakeSystemProcess struct {
	name       string
	rss        uint64
	ppid       int32
	cmdline    string
	cwd        string
	cpu        float64
	nameErr    error
	memoryErr  error
	ppidErr    error
	cmdlineErr error
	cwdErr     error
	cpuErr     error
}

func (p *fakeSystemProcess) NameWithContext(ctx context.Context) (string, error) {
	return p.name, p.nameErr
}

func (p *fakeSystemProcess) MemoryInfoWithContext(
	ctx context.Context,
) (*gopsutilProcess.MemoryInfoStat, error) {
	if p.memoryErr != nil {
		return nil, p.memoryErr
	}
	return &gopsutilProcess.MemoryInfoStat{RSS: p.rss}, nil
}

func (p *fakeSystemProcess) PpidWithContext(ctx context.Context) (int32, error) {
	return p.ppid, p.ppidErr
}

func (p *fakeSystemProcess) CmdlineWithContext(ctx context.Context) (string, error) {
	return p.cmdline, p.cmdlineErr
}

func (p *fakeSystemProcess) CwdWithContext(ctx context.Context) (string, error) {
	return p.cwd, p.cwdErr
}

func (p *fakeSystemProcess) CPUPercentWithContext(ctx context.Context) (float64, error) {
	return p.cpu, p.cpuErr
}

func testSystemProcess() *fakeSystemProcess {
	return &fakeSystemProcess{
		name:    "node",
		rss:     128 * bytesPerMegabyte,
		ppid:    7,
		cmdline: "node server.js",
		cwd:     "/Users/jeff/code/app",
		cpu:     42,
	}
}

func assertOptionalMetadataDefaults(t *testing.T, proc Process) {
	t.Helper()
	if proc.ParentPID != 0 {
		t.Fatalf("expected default parent pid, got %d", proc.ParentPID)
	}
	if proc.CommandLine != "" {
		t.Fatalf("expected empty command line, got %q", proc.CommandLine)
	}
	if proc.Cwd != "" {
		t.Fatalf("expected empty cwd, got %q", proc.Cwd)
	}
	if proc.CPUPercent != 0 {
		t.Fatalf("expected default cpu, got %f", proc.CPUPercent)
	}
}

func assertProcessMetadata(t *testing.T, proc Process) {
	t.Helper()
	if proc.CommandLine != "node server.js" {
		t.Fatalf("expected command line, got %q", proc.CommandLine)
	}
	if proc.Cwd != "/Users/jeff/code/app" {
		t.Fatalf("expected cwd, got %q", proc.Cwd)
	}
	if proc.CPUPercent != 42 {
		t.Fatalf("expected cpu percent, got %f", proc.CPUPercent)
	}
	if proc.MemoryMB != 128 {
		t.Fatalf("expected memory mb, got %d", proc.MemoryMB)
	}
}

func currentGopsutilProcess(t *testing.T) *gopsutilProcess.Process {
	t.Helper()
	proc, err := gopsutilProcess.NewProcess(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("new process: %v", err)
	}
	return proc
}

func replaceListProcesses(
	t *testing.T,
	procs []*gopsutilProcess.Process,
	err error,
) func() {
	t.Helper()
	oldListProcesses := listProcesses
	listProcesses = func(ctx context.Context) ([]*gopsutilProcess.Process, error) {
		return procs, err
	}
	return func() {
		listProcesses = oldListProcesses
	}
}
