//go:build e2e

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

const reaperCommand = "reap"

func main() {
	if isReaper(os.Args) {
		runReaper(os.Args[2], os.Args[3], os.Args[4])
		return
	}
	waitForTermination()
}

func isReaper(args []string) bool {
	if len(args) != 5 {
		return false
	}
	return args[1] == reaperCommand
}

func runReaper(executable string, workingDir string, pidPath string) {
	command := exec.Command(executable)
	command.Dir = workingDir
	if err := command.Start(); err != nil {
		fail("starting target", err)
	}
	pid := fmt.Sprintf("%d\n", command.Process.Pid)
	if err := os.WriteFile(pidPath, []byte(pid), 0o600); err != nil {
		_ = command.Process.Kill()
		fail("writing target pid", err)
	}
	_ = command.Wait()
}

func waitForTermination() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
}

func fail(action string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	os.Exit(1)
}
