package killer

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

const defaultPollInterval = 100 * time.Millisecond

type Killer interface {
	Kill(ctx context.Context, pid int32) error
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

func New() *SignalKiller {
	return &SignalKiller{
		termTimeout:  2 * time.Second,
		pollInterval: defaultPollInterval,
	}
}

func (k *SignalKiller) Kill(ctx context.Context, pid int32) error {
	proc, err := findProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}

	if err := signalProcess(proc, pid, syscall.SIGTERM); err != nil {
		return err
	}

	terminated := k.waitForExit(ctx, pid, k.termTimeout)
	if terminated {
		return nil
	}

	return signalProcess(proc, pid, syscall.SIGKILL)
}

func (k *SignalKiller) waitForExit(ctx context.Context, pid int32, timeout time.Duration) bool {
	deadline := time.After(timeout)
	ticker := time.NewTicker(k.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case <-ticker.C:
			if !processExists(pid) {
				return true
			}
		}
	}
}

func processExists(pid int32) bool {
	proc, err := findProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func signalProcess(proc processHandle, pid int32, signal syscall.Signal) error {
	err := proc.Signal(signal)
	if err == nil {
		return nil
	}
	if err == os.ErrProcessDone {
		return nil
	}
	return fmt.Errorf("sending %s to %d: %w", signal, pid, err)
}
