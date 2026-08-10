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
	if err := signalRequiredTarget(ctx, target, syscall.SIGTERM); err != nil {
		return err
	}

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
			matches, err := targetMatches(ctx, target)
			if err != nil {
				return false, err
			}
			if !matches {
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

func signalRequiredTarget(
	ctx context.Context,
	target appProcess.Process,
	signal syscall.Signal,
) error {
	signaled, err := signalTarget(ctx, target, signal)
	if err != nil {
		return err
	}
	if !signaled {
		return fmt.Errorf("process %d identity changed", target.PID)
	}
	return nil
}

func signalTarget(
	ctx context.Context,
	target appProcess.Process,
	signal syscall.Signal,
) (bool, error) {
	matches, err := targetMatches(ctx, target)
	if err != nil {
		return false, err
	}
	if !matches {
		return false, nil
	}

	proc, err := findProcess(target.PID)
	if err != nil {
		return false, fmt.Errorf("finding process %d: %w", target.PID, err)
	}
	return true, signalProcess(proc, target.PID, signal)
}

func targetMatches(ctx context.Context, target appProcess.Process) (bool, error) {
	if target.CreateTime <= 0 {
		return false, fmt.Errorf("process %d has no creation time", target.PID)
	}
	createTime, err := readProcessCreateTime(ctx, target.PID)
	if processGone(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading process %d creation time: %w", target.PID, err)
	}
	return createTime == target.CreateTime, nil
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
	return fmt.Errorf("sending %s to %d: %w", signal, pid, err)
}
