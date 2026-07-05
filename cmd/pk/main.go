package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"

	"github.com/jeffrywainwright/pk/internal/audit"
	"github.com/jeffrywainwright/pk/internal/cleanup"
	"github.com/jeffrywainwright/pk/internal/config"
	"github.com/jeffrywainwright/pk/internal/docker"
	"github.com/jeffrywainwright/pk/internal/killer"
	"github.com/jeffrywainwright/pk/internal/monitor"
	"github.com/jeffrywainwright/pk/internal/notify"
	"github.com/jeffrywainwright/pk/internal/process"
	"github.com/jeffrywainwright/pk/internal/scan"
	"github.com/jeffrywainwright/pk/internal/service"
	"github.com/jeffrywainwright/pk/internal/skillinstall"
)

var version = "dev"

type processScanner interface {
	Scan(context.Context) ([]scan.Report, error)
}

type auditStore interface {
	Record(audit.Event) error
	Events() ([]audit.Event, error)
}

type monitorRunner interface {
	Run(context.Context) error
}

type backgroundManager interface {
	Install() error
	Uninstall() error
	Status() (string, error)
}

type skillInstaller interface {
	Install(string) (string, error)
	DefaultRoot() (string, error)
}

type cleanupOptions struct {
	apply bool
	watch bool
}

var (
	newProcessLister     = func() process.Lister { return process.NewLister() }
	newProcessScanner    = func(cfg *config.Config, lister process.Lister) processScanner { return scan.New(cfg, lister) }
	newAuditStore        = func() (auditStore, error) { return audit.DefaultLog() }
	newProcessKiller     = func() killer.Killer { return killer.New() }
	newDockerClient      = func() docker.Client { return docker.NewClient() }
	newMonitorRunner     = func(cfg *config.Config) monitorRunner { return newMonitor(cfg) }
	newBackgroundManager = func() (backgroundManager, error) { return service.DefaultManager() }
	newSkillInstaller    = func() skillInstaller { return defaultSkillInstaller{} }
	sendNotification     = notify.Send
	handleShutdownSignal = handleSignals
	notifySignal         = signal.Notify
	exitProcess          = os.Exit
)

const (
	reportTimestamps = true
	defaultApply     = false
	defaultWatch     = false
)

func main() {
	err := run(os.Args[1:], os.Stdout)
	exitOnError(err)
}

func run(args []string, out io.Writer) error {
	if isVersionCommand(args) {
		_, err := fmt.Fprintln(out, "pk", version)
		return err
	}

	command, commandArgs := splitCommand(args)
	return dispatch(command, commandArgs, out)
}

func dispatch(command string, args []string, out io.Writer) error {
	switch command {
	case "", "monitor":
		return runMonitor(args)
	case "scan":
		return runScan(args, out)
	case "cleanup":
		return runCleanup(args, out)
	case "history":
		return runHistory(out)
	case "install":
		return runInstall(out)
	case "uninstall":
		return runUninstall(out)
	case "status":
		return runStatus(out)
	case "skills":
		return runSkills(args, out)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runMonitor(args []string) error {
	cfg, err := config.ParseArgs("monitor", args)
	if err != nil {
		return err
	}

	log.SetLevel(log.InfoLevel)
	log.SetReportTimestamp(reportTimestamps)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handleShutdownSignal(cancel)
	m := newMonitorRunner(cfg)
	return m.Run(ctx)
}

func runScan(args []string, out io.Writer) error {
	cfg, err := config.ParseArgs("scan", args)
	if err != nil {
		return err
	}
	reports, err := scanReports(context.Background(), cfg)
	if err != nil {
		return err
	}
	return scan.WriteReports(out, reports)
}

func runCleanup(args []string, out io.Writer) error {
	cfg, options, err := cleanupConfig(args)
	if err != nil {
		return err
	}
	if options.watch {
		return runCleanupWatch(cfg, options, out)
	}
	return runCleanupOnce(context.Background(), cfg, options, out)
}

func runCleanupOnce(
	ctx context.Context,
	cfg *config.Config,
	options cleanupOptions,
	out io.Writer,
) error {
	reports, err := scanReports(ctx, cfg)
	if err != nil {
		return err
	}
	log, err := newAuditStore()
	if err != nil {
		return err
	}
	results, err := cleanup.Run(ctx, reports, newProcessKiller(), log, options.apply)
	if err != nil {
		return err
	}
	containerResults, err := runDockerCleanup(ctx, log, options.apply)
	if err != nil {
		return err
	}
	return writeCleanupResults(out, results, containerResults)
}

func runDockerCleanup(
	ctx context.Context,
	log auditStore,
	apply bool,
) ([]docker.Result, error) {
	client := newDockerClient()
	if !client.Available() {
		return nil, nil
	}
	return docker.Run(ctx, client, log, apply)
}

func writeCleanupResults(
	out io.Writer,
	results []cleanup.Result,
	containerResults []docker.Result,
) error {
	if len(results) == 0 && len(containerResults) == 0 {
		return cleanup.WriteResults(out, results)
	}
	if err := writeProcessCleanupResults(out, results); err != nil {
		return err
	}
	return docker.WriteResults(out, containerResults)
}

func writeProcessCleanupResults(out io.Writer, results []cleanup.Result) error {
	if len(results) == 0 {
		return nil
	}
	return cleanup.WriteResults(out, results)
}

func runCleanupWatch(
	cfg *config.Config,
	options cleanupOptions,
	out io.Writer,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handleShutdownSignal(cancel)
	if err := runCleanupOnce(ctx, cfg, options, out); err != nil {
		return err
	}
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	return cleanupLoop(ctx, ticker.C, cfg, options, out)
}

func cleanupLoop(
	ctx context.Context,
	ticks <-chan time.Time,
	cfg *config.Config,
	options cleanupOptions,
	out io.Writer,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticks:
			if err := runCleanupOnce(ctx, cfg, options, out); err != nil {
				return err
			}
		}
	}
}

func runHistory(out io.Writer) error {
	log, err := newAuditStore()
	if err != nil {
		return err
	}
	events, err := log.Events()
	if err != nil {
		return err
	}
	return audit.WriteEvents(out, events)
}

func runInstall(out io.Writer) error {
	manager, err := newBackgroundManager()
	if err != nil {
		return err
	}
	if err := manager.Install(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "installed")
	return err
}

func runUninstall(out io.Writer) error {
	manager, err := newBackgroundManager()
	if err != nil {
		return err
	}
	if err := manager.Uninstall(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "uninstalled")
	return err
}

func runStatus(out io.Writer) error {
	manager, err := newBackgroundManager()
	if err != nil {
		return err
	}
	status, err := manager.Status()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, status)
	return err
}

func runSkills(args []string, out io.Writer) error {
	command, commandArgs := splitCommand(args)
	switch command {
	case "install":
		return runSkillsInstall(commandArgs, out)
	case "path":
		return runSkillsPath(out)
	default:
		return fmt.Errorf("unknown skills command %q", command)
	}
}

func runSkillsInstall(args []string, out io.Writer) error {
	var root string
	flags := flag.NewFlagSet("skills install", flag.ContinueOnError)
	flags.StringVar(&root, "dir", "", "Skills root directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	path, err := newSkillInstaller().Install(root)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, path)
	return err
}

func runSkillsPath(out io.Writer) error {
	root, err := newSkillInstaller().DefaultRoot()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, skillinstall.SkillPath(root))
	return err
}

func scanReports(ctx context.Context, cfg *config.Config) ([]scan.Report, error) {
	lister := newProcessLister()
	scanner := newProcessScanner(cfg, lister)
	return scanner.Scan(ctx)
}

func cleanupConfig(args []string) (*config.Config, cleanupOptions, error) {
	var options cleanupOptions
	cfg, err := config.ParseArgsWith("cleanup", args, func(flags *flag.FlagSet) {
		flags.BoolVar(&options.apply, "apply", defaultApply, "Kill cleanup targets")
		flags.BoolVar(&options.watch, "watch", defaultWatch, "Run cleanup on interval")
	})
	if err != nil {
		return nil, cleanupOptions{}, err
	}
	return cfg, options, nil
}

func splitCommand(args []string) (string, []string) {
	args = trimSeparator(args)
	if len(args) == 0 {
		return "", args
	}
	if isFlag(args[0]) {
		return "", args
	}
	return args[0], args[1:]
}

func trimSeparator(args []string) []string {
	if len(args) == 0 {
		return args
	}
	if args[0] == "--" {
		return args[1:]
	}
	return args
}

func isFlag(arg string) bool {
	hasValue := len(arg) > 0
	if !hasValue {
		return false
	}
	return arg[0] == '-'
}

func handleSignals(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	notifySignal(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("Received shutdown signal")
		cancel()
	}()
}

func newMonitor(cfg *config.Config) *monitor.Monitor {
	lister := newProcessLister()
	processKiller := newProcessKiller()
	return monitor.New(cfg, lister, processKiller, notifyKilled)
}

func notifyKilled(name string, pid int32) {
	msg := fmt.Sprintf("Killed %s (PID %d)", name, pid)
	if err := sendNotification("pk", msg); err != nil {
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
	exitProcess(1)
}

func isVersionCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	isVersionFlag := args[0] == "--version"
	isVersionSubcommand := args[0] == "version"
	return isVersionFlag || isVersionSubcommand
}

type defaultSkillInstaller struct{}

func (i defaultSkillInstaller) Install(root string) (string, error) {
	return skillinstall.Install(root)
}

func (i defaultSkillInstaller) DefaultRoot() (string, error) {
	return skillinstall.DefaultRoot()
}
