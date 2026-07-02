package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"

	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/killer"
	"github.com/jeffrywainwright/pk/internal/monitor"
	"github.com/jeffrywainwright/pk/internal/notify"
	"github.com/jeffrywainwright/pk/internal/process"
)

var version = "dev"

const reportTimestamps = true

func main() {
	if isVersionCommand(os.Args) {
		fmt.Println("pk", version)
		return
	}

	err := run(config.Parse())
	exitOnError(err)
}

func run(cfg *config.Config) error {
	log.SetLevel(log.InfoLevel)
	log.SetReportTimestamp(reportTimestamps)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handleSignals(cancel)
	m := newMonitor(cfg)
	return m.Run(ctx)
}

func handleSignals(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("Received shutdown signal")
		cancel()
	}()
}

func newMonitor(cfg *config.Config) *monitor.Monitor {
	lister := process.NewLister()
	processKiller := killer.New()
	return monitor.New(cfg, lister, processKiller, notifyKilled)
}

func notifyKilled(name string, pid int32) {
	msg := fmt.Sprintf("Killed %s (PID %d)", name, pid)
	if err := notify.Send("pk", msg); err != nil {
		log.Debug("Notification failed", "error", err)
	}
}

func exitOnError(err error) {
	if err == nil {
		return
	}
	if err == context.Canceled {
		return
	}

	log.Error("Monitor error", "error", err)
	os.Exit(1)
}

func isVersionCommand(args []string) bool {
	if len(args) <= 1 {
		return false
	}
	return args[1] == "--version"
}
