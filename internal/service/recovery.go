package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const recoveryTimeout = 5 * time.Second

func (m *Manager) withLock(ctx context.Context, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(m.home, ".config", "pk", "service.lock")
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := openServiceLock(path)
	if err != nil {
		return fmt.Errorf("opening service lock: %w", err)
	}
	defer func() { _ = file.Close() }()
	return withServiceLock(ctx, file, operation)
}

func withServiceLock(
	ctx context.Context,
	file *os.File,
	operation func(context.Context) error,
) error {
	fd, err := serviceDescriptor(file)
	if err != nil {
		return err
	}
	if err := lockService(ctx, fd); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()
	return operation(ctx)
}

func openServiceLock(path string) (*os.File, error) {
	// #nosec G304 -- The path is fixed under the user's pk directory; reject symlink leaves.
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
}

func serviceDescriptor(file *os.File) (int, error) {
	fd := file.Fd()
	if fd > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("file descriptor out of range: %d", fd)
	}
	return int(fd), nil
}

func lockService(ctx context.Context, fd int) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) restoreRegistration(previous []byte, cause error) error {
	if previous == nil {
		return cause
	}
	if err := writeFile(m.servicePath(), previous); err != nil {
		return lifecycleError(cause, "restoring service registration", err)
	}
	ctx, cancel := recoveryContext()
	defer cancel()
	if m.goos == "darwin" {
		return lifecycleError(cause, "restoring launchd service", m.resumeLaunchd(ctx))
	}
	recovery := errors.Join(m.reloadSystemd(ctx), m.enableSystemd(ctx))
	return lifecycleError(cause, "restoring systemd service", recovery)
}

func (m *Manager) waitRunning(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, recoveryTimeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		output, err := m.statusOutput(ctx)
		running := err == nil && m.running(string(output))
		if running {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("background cleanup did not start; run pk status: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *Manager) running(output string) bool {
	if m.goos == "darwin" {
		return strings.Contains(output, "state = running")
	}
	return strings.TrimSpace(output) == "active"
}

func recoveryContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), recoveryTimeout)
}

func lifecycleError(cause error, action string, recovery error) error {
	if recovery == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("%s: %w", action, recovery))
}
