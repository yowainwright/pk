package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const (
	launchdLabel = "com.jeffrywainwright.pk"
	systemdUnit  = "pk.service"
)

type Runner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type Manager struct {
	goos       string
	home       string
	executable string
	uid        string
	runner     Runner
}

type commandRunner struct{}

func DefaultManager() (*Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding executable: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home dir: %w", err)
	}
	return NewManager(runtime.GOOS, home, executable, currentUID(), commandRunner{}), nil
}

func NewManager(
	goos string,
	home string,
	executable string,
	uid string,
	runner Runner,
) *Manager {
	return &Manager{goos: goos, home: home, executable: executable, uid: uid, runner: runner}
}

func (m *Manager) Install() error {
	if m.goos == "darwin" {
		return m.installLaunchd()
	}
	if m.goos == "linux" {
		return m.installSystemd()
	}
	return unsupported(m.goos)
}

func (m *Manager) Uninstall() error {
	if m.goos == "darwin" {
		return m.uninstallLaunchd()
	}
	if m.goos == "linux" {
		return m.uninstallSystemd()
	}
	return unsupported(m.goos)
}

func (m *Manager) Status() (string, error) {
	if err := m.checkSupported(); err != nil {
		return "", err
	}
	if !m.installed() {
		return "not installed", nil
	}
	output, err := m.statusOutput()
	if err != nil {
		return "installed but not running", nil
	}
	return strings.TrimSpace(string(output)), nil
}

func (r commandRunner) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (r commandRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func currentUID() string {
	return strconv.Itoa(os.Getuid())
}

func unsupported(goos string) error {
	return fmt.Errorf("background service is not supported on %s", goos)
}

func (m *Manager) checkSupported() error {
	if m.goos == "darwin" {
		return nil
	}
	if m.goos == "linux" {
		return nil
	}
	return unsupported(m.goos)
}

func serviceArgs() []string {
	return []string{"cleanup", "--apply", "--watch"}
}

func (m *Manager) command() []string {
	command := []string{m.executable}
	return append(command, serviceArgs()...)
}

func (m *Manager) installed() bool {
	_, err := os.Stat(m.servicePath())
	return err == nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func removeFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func quoteSystemdArg(arg string) string {
	escaped := strings.ReplaceAll(arg, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	quote := `"`
	quoted := quote + escaped
	return quoted + quote
}

func systemdExecStart(command []string) string {
	quoted := make([]string, 0, len(command))
	for _, arg := range command {
		quoted = append(quoted, quoteSystemdArg(arg))
	}
	return strings.Join(quoted, " ")
}

func writeLine(buf *bytes.Buffer, line string) {
	buf.WriteString(line)
	buf.WriteByte('\n')
}
