package service

import (
	"bytes"
	"fmt"
	"path/filepath"
)

func (m *Manager) installSystemd() error {
	if err := m.writeSystemdUnit(); err != nil {
		return err
	}
	if err := m.runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("reloading systemd user manager: %w", err)
	}
	err := m.runner.Run("systemctl", "--user", "enable", "--now", systemdUnit)
	if err != nil {
		return fmt.Errorf("starting systemd service: %w", err)
	}
	return nil
}

func (m *Manager) uninstallSystemd() error {
	_ = m.runner.Run("systemctl", "--user", "disable", "--now", systemdUnit)
	if err := removeFile(m.servicePath()); err != nil {
		return err
	}
	return m.runner.Run("systemctl", "--user", "daemon-reload")
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
