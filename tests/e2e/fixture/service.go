//go:build e2e

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The fixture runs real daemons while emulating only systemctl's command boundary.
// It is used exclusively inside the disposable process-test container.
func runService(args []string) {
	if err := validateServiceArguments(args); err != nil {
		fail("service arguments", err)
	}
	if err := serviceCommand(args[1]); err != nil {
		fail("service command", err)
	}
}

func validateServiceArguments(args []string) error {
	command := strings.Join(args, " ")
	switch command {
	case "--user daemon-reload", "--user enable --now pk.service":
		return nil
	case "--user restart pk.service", "--user disable --now pk.service":
		return nil
	case "--user is-active pk.service":
		return nil
	case "--user show pk.service --property=LoadState --property=ActiveState":
		return nil
	default:
		return fmt.Errorf("unexpected systemctl arguments: %s", command)
	}
}

func serviceCommand(command string) error {
	switch command {
	case "daemon-reload":
		return nil
	case "enable":
		return startService()
	case "restart":
		return restartService()
	case "disable":
		return stopService()
	case "is-active":
		return serviceStatus()
	case "show":
		return showService()
	default:
		return fmt.Errorf("unexpected systemctl command %s", command)
	}
}

func restartService() error {
	if err := stopService(); err != nil {
		return err
	}
	return startService()
}

func servicePIDPath() string {
	return filepath.Join(os.Getenv("HOME"), "service.pid")
}

func servicePID() int {
	data, err := os.ReadFile(servicePIDPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func serviceAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	path := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return !strings.Contains(string(data), "State:\tZ")
}

func serviceStatus() error {
	if !serviceAlive(servicePID()) {
		return fmt.Errorf("inactive")
	}
	fmt.Println("active")
	return nil
}

func showService() error {
	unit := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "pk.service")
	_, err := os.Stat(unit)
	if os.IsNotExist(err) {
		fmt.Println("LoadState=not-found")
	} else {
		fmt.Println("LoadState=loaded")
	}
	if serviceAlive(servicePID()) {
		fmt.Println("ActiveState=active")
	} else {
		fmt.Println("ActiveState=inactive")
	}
	return nil
}

func stopService() error {
	pid := servicePID()
	if !serviceAlive(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for serviceAlive(pid) {
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon %d did not exit", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func startService() error {
	if serviceAlive(servicePID()) {
		return nil
	}
	args, err := serviceArguments()
	if err != nil {
		return err
	}
	logPath := filepath.Join(os.Getenv("HOME"), "service-test.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	command := exec.Command(args[0], args[1:]...)
	// Emulate a user manager with a different config environment from the CLI.
	serviceConfig := filepath.Join(os.Getenv("HOME"), "service environment")
	command.Env = append(os.Environ(), "XDG_CONFIG_HOME="+serviceConfig)
	command.Stdout, command.Stderr = log, log
	return launchService(command)
}

func launchService(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	pid := strconv.Itoa(command.Process.Pid)
	if err := os.WriteFile(servicePIDPath(), []byte(pid), 0o600); err != nil {
		_ = command.Process.Kill()
		return err
	}
	return command.Process.Release()
}

var quotedArgument = regexp.MustCompile(`"(?:\\.|[^"\\])*"`)

func serviceArguments() ([]string, error) {
	path := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "pk.service")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			return unquoteArguments(line)
		}
	}
	return nil, fmt.Errorf("unit has no ExecStart")
}

func unquoteArguments(line string) ([]string, error) {
	var args []string
	for _, quoted := range quotedArgument.FindAllString(line, -1) {
		arg, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, err
		}
		arg = strings.ReplaceAll(arg, "%%", "%")
		args = append(args, strings.ReplaceAll(arg, "$$", "$"))
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("empty ExecStart")
	}
	return args, nil
}
