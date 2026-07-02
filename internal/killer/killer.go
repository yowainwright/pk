package killer

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

type Killer interface {
	Kill(ctx context.Context, pid int32) error
}

type SignalKiller struct {
	termTimeout time.Duration
}

func New() *SignalKiller {
	return &SignalKiller{
		termTimeout: 2 * time.Second,
	}
}

func (k *SignalKiller) Kill(ctx context.Context, pid int32) error {
	proc, err := os.FindProcess(int(pid))
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
	ticker := time.NewTicker(100 * time.Millisecond)
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
	proc, err := os.FindProcess(int(pid))
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func signalProcess(proc *os.Process, pid int32, signal syscall.Signal) error {
	err := proc.Signal(signal)
	if err == nil {
		return nil
	}
	if err == os.ErrProcessDone {
		return nil
	}
	return fmt.Errorf("sending %s to %d: %w", signal, pid, err)
}
