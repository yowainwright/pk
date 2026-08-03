package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

func (m *Manager) installSystemd(ctx context.Context) error {
	if err := m.writeSystemdUnit(); err != nil {
		return err
	}
	if err := m.reloadSystemd(ctx); err != nil {
		cause := fmt.Errorf("reloading systemd user manager: %w", err)
		return m.rollbackSystemdInstall(cause)
	}
	if err := m.enableSystemd(ctx); err != nil {
		cause := fmt.Errorf("starting systemd service: %w", err)
		return m.rollbackSystemdInstall(cause)
	}
	return nil
}

func (m *Manager) uninstallSystemd(ctx context.Context) error {
	disableErr := m.disableSystemd(ctx)
	if err := ctx.Err(); err != nil {
		return m.restoreSystemd(err)
	}
	if disableErr != nil {
		cause := fmt.Errorf("stopping systemd service: %w", disableErr)
		return m.restoreSystemd(cause)
	}
	if err := removeFile(m.servicePath()); err != nil {
		return m.restoreSystemd(err)
	}
	if err := m.reloadSystemd(ctx); err != nil {
		return m.finishSystemdUninstall(err)
	}
	return nil
}

func (m *Manager) rollbackSystemdInstall(cause error) error {
	ctx, cancel := recoveryContext()
	defer cancel()
	disableErr := m.disableSystemd(ctx)
	if disableErr != nil {
		recovery := errors.Join(disableErr, m.reloadSystemd(ctx), m.enableSystemd(ctx))
		return lifecycleError(cause, "restoring systemd install", recovery)
	}
	recovery := errors.Join(removeFile(m.servicePath()), m.reloadSystemd(ctx))
	return lifecycleError(cause, "rolling back systemd install", recovery)
}

func (m *Manager) restoreSystemd(cause error) error {
	ctx, cancel := recoveryContext()
	defer cancel()
	recovery := errors.Join(m.reloadSystemd(ctx), m.enableSystemd(ctx))
	return lifecycleError(cause, "restoring systemd service", recovery)
}

func (m *Manager) finishSystemdUninstall(cause error) error {
	ctx, cancel := recoveryContext()
	defer cancel()
	recovery := m.reloadSystemd(ctx)
	return lifecycleError(cause, "finishing systemd uninstall", recovery)
}

func (m *Manager) reloadSystemd(ctx context.Context) error {
	return m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
}

func (m *Manager) enableSystemd(ctx context.Context) error {
	return m.runner.Run(ctx, "systemctl", "--user", "enable", "--now", systemdUnit)
}

func (m *Manager) disableSystemd(ctx context.Context) error {
	return m.runner.Run(ctx, "systemctl", "--user", "disable", "--now", systemdUnit)
}

func (m *Manager) writeSystemdUnit() error {
	if err := ensureDir(filepath.Dir(m.servicePath())); err != nil {
		return err
	}
	if err := ensureDir(m.logDir()); err != nil {
		return err
	}
	return writeFile(m.servicePath(), systemdUnitFile(m.command()))
}

func systemdUnitFile(command []string) []byte {
	var buf bytes.Buffer
	writeLine(&buf, "[Unit]")
	writeLine(&buf, "Description=pk background cleanup")
	writeLine(&buf, "")
	writeLine(&buf, "[Service]")
	writeLine(&buf, "ExecStart="+systemdExecStart(command))
	writeLine(&buf, "Restart=always")
	writeLine(&buf, "RestartSec=5")
	writeLine(&buf, "")
	writeLine(&buf, "[Install]")
	writeLine(&buf, "WantedBy=default.target")
	return buf.Bytes()
}
