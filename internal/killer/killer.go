package killer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	gopsutilProcess "github.com/shirou/gopsutil/v4/process"
	appProcess "github.com/yowainwright/pk/internal/process"
)

const defaultPollInterval = 100 * time.Millisecond

type Killer interface {
	Kill(ctx context.Context, target appProcess.Process) error
}

type processHandle interface {
	Signal(os.Signal) error
}

type SignalKiller struct {
	termTimeout  time.Duration
	pollInterval time.Duration
}

type targetState uint8

const (
	targetGone targetState = iota
	targetCurrent
	targetReused
)

var findProcess = func(pid int32) (processHandle, error) {
	return os.FindProcess(int(pid))
}

var readProcessCreateTime = func(ctx context.Context, pid int32) (int64, error) {
	proc, err := gopsutilProcess.NewProcessWithContext(ctx, pid)
	if err != nil {
		return 0, err
	}
	return proc.CreateTimeWithContext(ctx)
}

func New() *SignalKiller {
	return &SignalKiller{
		termTimeout:  2 * time.Second,
		pollInterval: defaultPollInterval,
	}
}

func (k *SignalKiller) Kill(ctx context.Context, target appProcess.Process) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	terminated, err := signalInitialTarget(ctx, target)
	if err != nil {
		return err
	}
	if terminated {
		return nil
	}
	return k.waitAndKill(ctx, target)
}

func (k *SignalKiller) waitAndKill(ctx context.Context, target appProcess.Process) error {
	terminated, err := k.waitForExit(ctx, target)
	if err != nil {
		return err
	}
	if terminated {
		return nil
	}

	_, err = signalTarget(ctx, target, syscall.SIGKILL)
	return err
}

func (k *SignalKiller) waitForExit(ctx context.Context, target appProcess.Process) (bool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, k.termTimeout)
	defer cancel()
	ticker := time.NewTicker(k.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return waitFinished(ctx)
		case <-ticker.C:
			state, err := inspectTarget(ctx, target)
			if err != nil {
				return false, err
			}
			if state != targetCurrent {
				return true, nil
			}
		}
	}
}

func waitFinished(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func signalInitialTarget(ctx context.Context, target appProcess.Process) (bool, error) {
	state, err := signalTarget(ctx, target, syscall.SIGTERM)
	if err != nil {
		return false, err
	}
	if state == targetGone {
		return true, nil
	}
	if state == targetReused {
		return false, fmt.Errorf("process %d identity changed", target.PID)
	}
	return false, nil
}

func signalTarget(
	ctx context.Context,
	target appProcess.Process,
	signal syscall.Signal,
) (targetState, error) {
	state, err := inspectTarget(ctx, target)
	if err != nil {
		return state, err
	}
	if state != targetCurrent {
		return state, nil
	}

	proc, err := findProcess(target.PID)
	if err != nil {
		return state, fmt.Errorf("finding process %d: %w", target.PID, err)
	}
	return state, signalProcess(proc, target.PID, signal)
}

func inspectTarget(ctx context.Context, target appProcess.Process) (targetState, error) {
	if target.CreateTime <= 0 {
		return targetGone, fmt.Errorf("process %d has no creation time", target.PID)
	}
	createTime, err := readProcessCreateTime(ctx, target.PID)
	if processGone(err) {
		return targetGone, nil
	}
	if err != nil {
		return targetGone, fmt.Errorf("reading process %d creation time: %w", target.PID, err)
	}
	if createTime != target.CreateTime {
		return targetReused, nil
	}
	return targetCurrent, nil
}

func processGone(err error) bool {
	if errors.Is(err, gopsutilProcess.ErrorProcessNotRunning) {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return errors.Is(err, syscall.ESRCH)
}

func signalProcess(proc processHandle, pid int32, signal syscall.Signal) error {
	err := proc.Signal(signal)
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	if processGone(err) {
		return nil
	}
	return fmt.Errorf("sending %s to %d: %w", signal, pid, err)
}
