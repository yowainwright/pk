package service

import "path/filepath"

func (m *Manager) servicePath() string {
	if m.goos == "darwin" {
		return filepath.Join(m.home, "Library", "LaunchAgents", launchdLabel+".plist")
	}
	return filepath.Join(m.home, ".config", "systemd", "user", systemdUnit)
}

func (m *Manager) logDir() string {
	if m.goos == "darwin" {
		return filepath.Join(m.home, "Library", "Logs", "pk")
	}
	return filepath.Join(m.home, ".local", "state", "pk")
}

func (m *Manager) launchdDomain() string {
	return "gui/" + m.uid
}

func (m *Manager) launchdService() string {
	domain := m.launchdDomain()
	prefix := domain + "/"
	return prefix + launchdLabel
}

func (m *Manager) statusOutput() ([]byte, error) {
	if m.goos == "darwin" {
		return m.runner.Output("launchctl", "print", m.launchdService())
	}
	return m.runner.Output("systemctl", "--user", "is-active", systemdUnit)
}
