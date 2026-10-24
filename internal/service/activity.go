package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (m *Manager) Activity(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "unknown", err
	}
	if err := m.checkSupported(); err != nil {
		return "unknown", err
	}
	_, err := os.Stat(m.servicePath())
	if os.IsNotExist(err) {
		return "inactive", nil
	}
	if err != nil {
		return "unknown", fmt.Errorf("reading service registration: %w", err)
	}
	output, err := m.statusOutput(ctx)
	if m.goos == "darwin" {
		return launchdActivity(output, err)
	}
	return systemdActivity(output, err)
}

func launchdActivity(output []byte, err error) (string, error) {
	if launchdMissing(err) {
		return "inactive", nil
	}
	if err != nil {
		return "unknown", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		state, found := strings.CutPrefix(trimmed, "state = ")
		if found {
			return launchdStateActivity(state)
		}
	}
	return "unknown", fmt.Errorf("launchd response has no service state")
}

func launchdStateActivity(state string) (string, error) {
	switch state {
	case "running":
		return "active", nil
	case "not running", "waiting", "spawn scheduled", "spawn requested":
		return "inactive", nil
	default:
		return "unknown", fmt.Errorf("unrecognized launchd service state %q", state)
	}
}

func systemdActivity(output []byte, err error) (string, error) {
	readFailed := err != nil
	unexpectedFailure := readFailed && !systemdInactive(err)
	if unexpectedFailure {
		return "unknown", err
	}
	state := strings.TrimSpace(string(output))
	switch state {
	case "active", "reloading":
		return "active", nil
	case "inactive", "failed", "activating", "deactivating", "unknown":
		return "inactive", nil
	default:
		return "unknown", fmt.Errorf("unrecognized systemd service state %q", state)
	}
}

func systemdInactive(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	code := exit.ExitCode()
	inactive := code == 3 || code == 4
	return inactive
}
