package service

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
)

func (m *Manager) installSystemd(ctx context.Context) error {
	if err := m.writeSystemdUnit(); err != nil {
		return err
	}
	if err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("reloading systemd user manager: %w", err)
	}
	err := m.runner.Run(ctx, "systemctl", "--user", "enable", "--now", systemdUnit)
	if err != nil {
		return fmt.Errorf("starting systemd service: %w", err)
	}
	return nil
}

func (m *Manager) uninstallSystemd(ctx context.Context) error {
	_ = m.runner.Run(ctx, "systemctl", "--user", "disable", "--now", systemdUnit)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := removeFile(m.servicePath()); err != nil {
		return err
	}
	return m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
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
