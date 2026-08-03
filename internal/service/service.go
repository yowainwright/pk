package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const (
	launchdLabel = "com.yowainwright.pk"
	systemdUnit  = "pk.service"
)

type Runner interface {
	Run(context.Context, string, ...string) error
	Output(context.Context, string, ...string) ([]byte, error)
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

func (m *Manager) Install(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.goos == "darwin" {
		return m.installLaunchd(ctx)
	}
	if m.goos == "linux" {
		return m.installSystemd(ctx)
	}
	return unsupported(m.goos)
}

func (m *Manager) Uninstall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.goos == "darwin" {
		return m.uninstallLaunchd(ctx)
	}
	if m.goos == "linux" {
		return m.uninstallSystemd(ctx)
	}
	return unsupported(m.goos)
}

func (m *Manager) Status(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := m.checkSupported(); err != nil {
		return "", err
	}
	if !m.installed() {
		return "not installed", nil
	}
	output, err := m.statusOutput(ctx)
	return resolveStatus(output, err)
}

func resolveStatus(output []byte, err error) (string, error) {
	if errors.Is(err, context.Canceled) {
		return "", err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	if err != nil {
		return "installed but not running", nil
	}
	return strings.TrimSpace(string(output)), nil
}

func (r commandRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func (r commandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
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
	return os.MkdirAll(path, 0o750)
}

func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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
