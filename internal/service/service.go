package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/process"
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
	zdotdir    string
	uid        string
	runner     Runner
	configPath string
	since      int64
}

type commandRunner struct{}

func DefaultManager() (*Manager, error) {
	executable, err := installationExecutable()
	if err != nil {
		return nil, fmt.Errorf("finding executable: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home dir: %w", err)
	}
	manager := NewManager(runtime.GOOS, home, executable, currentUID(), commandRunner{})
	manager.zdotdir = os.Getenv("ZDOTDIR")
	return manager, manager.configurePreferences()
}

func installationExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	invoked, err := exec.LookPath(os.Args[0])
	if err != nil {
		return executable, nil
	}
	return stableExecutable(executable, invoked), nil
}

func stableExecutable(executable string, invoked string) string {
	absolute, err := filepath.Abs(invoked)
	if err != nil {
		return executable
	}
	actual, err := os.Stat(executable)
	if err != nil {
		return executable
	}
	// #nosec G703 -- Metadata only; SameFile verifies the invoked path is this executable before retaining it.
	entrypoint, err := os.Stat(absolute)
	if err != nil {
		return executable
	}
	if os.SameFile(actual, entrypoint) {
		return absolute
	}
	return executable
}

func (m *Manager) configurePreferences() error {
	store, err := config.DefaultStore()
	if err != nil {
		return err
	}
	m.configPath = store.Path()
	return nil
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
	return m.withLock(ctx, m.install)
}

func (m *Manager) install(ctx context.Context) error {
	if err := m.checkSupported(); err != nil {
		return err
	}
	previous, err := readRegistration(m.servicePath())
	if err != nil {
		return err
	}
	m.since, err = enableSince(ctx, previous)
	if err != nil {
		return err
	}
	if bytes.Equal(previous, m.registration()) {
		return m.resume(ctx)
	}
	return m.replaceService(ctx, previous)
}

func (m *Manager) replaceService(ctx context.Context, previous []byte) error {
	if err := m.installService(ctx); err != nil {
		return m.restoreRegistration(previous, err)
	}
	if err := m.waitRunning(ctx); err != nil {
		cause := m.rollbackServiceAfterShellInstall(err)
		return m.restoreRegistration(previous, cause)
	}
	return m.installShell(previous)
}

func (m *Manager) installShell(previous []byte) error {
	if err := m.shellInstaller().Install(); err != nil {
		cause := m.rollbackServiceAfterShellInstall(err)
		return m.restoreRegistration(previous, cause)
	}
	return nil
}

func (m *Manager) resume(ctx context.Context) error {
	if err := m.resumeService(ctx); err != nil {
		return err
	}
	return m.installShell(m.registration())
}

func (m *Manager) resumeService(ctx context.Context) error {
	if m.goos == "darwin" {
		if err := m.resumeLaunchd(ctx); err != nil {
			return err
		}
		return m.waitRunning(ctx)
	}
	if m.goos == "linux" {
		return m.resumeSystemd(ctx)
	}
	return unsupported(m.goos)
}

func (m *Manager) rollbackServiceAfterShellInstall(cause error) error {
	if m.goos == "darwin" {
		return m.rollbackLaunchdInstall(cause)
	}
	if m.goos == "linux" {
		return m.rollbackSystemdInstall(cause)
	}
	return lifecycleError(cause, "rolling back service install", unsupported(m.goos))
}

func (m *Manager) installService(ctx context.Context) error {
	if m.goos == "darwin" {
		return m.installLaunchd(ctx)
	}
	if m.goos == "linux" {
		return m.installSystemd(ctx)
	}
	return unsupported(m.goos)
}

func (m *Manager) Uninstall(ctx context.Context) error {
	return m.withLock(ctx, m.uninstall)
}

func (m *Manager) uninstall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	serviceErr := m.uninstallService(ctx)
	shellErr := m.shellInstaller().Uninstall()
	return errors.Join(serviceErr, shellErr)
}

func (m *Manager) uninstallService(ctx context.Context) error {
	if err := m.checkSupported(); err != nil {
		return err
	}
	if m.goos == "darwin" {
		return m.uninstallLaunchd(ctx)
	}
	if m.goos == "linux" {
		absent, err := m.systemdAbsent(ctx)
		finished := err != nil || absent
		if finished {
			return err
		}
		return m.uninstallSystemd(ctx)
	}
	return unsupported(m.goos)
}

func readRegistration(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Base(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

var sinceArgument = regexp.MustCompile(`--since(?:</string>\s*<string>|" ")([0-9]+)`)

func registrationSince(data []byte) int64 {
	match := sinceArgument.FindSubmatch(data)
	if len(match) == 2 {
		value, err := strconv.ParseInt(string(match[1]), 10, 64)
		valid := err == nil && value > 0
		if valid {
			return value
		}
	}
	return 0
}

func enableSince(ctx context.Context, previous []byte) (int64, error) {
	if since := registrationSince(previous); since > 0 {
		return since, nil
	}
	created, err := enablingProcessTime(ctx, os.Getpid())
	if err != nil {
		return 0, fmt.Errorf("reading enable process identity: %w", err)
	}
	// Compare identities from the same OS clock; Linux boot-time estimates can
	// differ from wall time. Exclude shells created in the enabling CLI's tick.
	return created + 1, nil
}

func enablingProcessTime(ctx context.Context, pid int) (int64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid enabling process ID: %d", pid)
	}
	if pid > math.MaxInt32 {
		return 0, fmt.Errorf("invalid enabling process ID: %d", pid)
	}
	return process.CreateTime(ctx, int32(pid))
}

func (m *Manager) registration() []byte {
	if m.goos == "darwin" {
		return launchdPlist(m.launchdDefinition())
	}
	return systemdUnitFile(m.command())
}

func (m *Manager) systemdAbsent(ctx context.Context) (bool, error) {
	_, statErr := os.Stat(m.servicePath())
	if statErr == nil {
		return false, nil
	}
	if !os.IsNotExist(statErr) {
		return false, statErr
	}
	output, err := m.runner.Output(ctx, "systemctl", "--user", "show", systemdUnit,
		"--property=LoadState", "--property=ActiveState")
	absent := strings.Contains(string(output), "LoadState=not-found\n")
	inactive := strings.Contains(string(output), "ActiveState=inactive\n")
	return absent && inactive, err
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
	err := exec.CommandContext(ctx, name, args...).Run()
	return commandContextError(ctx, err)
}

func (r commandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return output, commandContextError(ctx, err)
}

func commandContextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(ctxErr, err)
	}
	return err
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
	return []string{"__daemon"}
}

func (m *Manager) command() []string {
	command := []string{m.executable}
	command = append(command, serviceArgs()...)
	if m.configPath != "" {
		command = append(command, "--config", m.configPath)
	}
	if m.since > 0 {
		command = append(command, "--since", strconv.FormatInt(m.since, 10))
	}
	return command
}

func (m *Manager) shellInstaller() ShellInstaller {
	return ShellInstaller{
		Home:       m.home,
		ZDOTDIR:    m.zdotdir,
		Executable: m.executable,
	}
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
	escaped = strings.ReplaceAll(escaped, `%`, `%%`)
	escaped = strings.ReplaceAll(escaped, `$`, `$$`)
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

func (m *Manager) statusOutput(ctx context.Context) ([]byte, error) {
	if m.goos == "darwin" {
		return m.runner.Output(ctx, "launchctl", "print", m.launchdService())
	}
	return m.runner.Output(ctx, "systemctl", "--user", "is-active", systemdUnit)
}

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
