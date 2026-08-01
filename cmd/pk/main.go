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

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/cleanup"
	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/docker"
	"github.com/yowainwright/pk/internal/killer"
	"github.com/yowainwright/pk/internal/monitor"
	"github.com/yowainwright/pk/internal/notify"
	"github.com/yowainwright/pk/internal/process"
	"github.com/yowainwright/pk/internal/scan"
	"github.com/yowainwright/pk/internal/service"
	"github.com/yowainwright/pk/internal/skillinstall"
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

type cleanupOptions struct {
	apply bool
	watch bool
	scope cleanupScope
}

type cleanupScope string

type monitorOptions struct {
	apply bool
}

type installOptions struct {
	apply bool
}

var (
	newProcessLister  = func() process.Lister { return process.NewLister() }
	newProcessScanner = func(cfg *config.Config, lister process.Lister) processScanner { return scan.New(cfg, lister) }
	newAuditStore     = func() (auditStore, error) { return audit.DefaultLog() }
	newProcessKiller  = func() killer.Killer { return killer.New() }
	newDockerClient   = func() docker.Client { return docker.NewClient() }
	newMonitorRunner  = func(cfg *config.Config, options monitorOptions) monitorRunner {
		return newMonitor(cfg, options)
	}
	newBackgroundManager = func() (backgroundManager, error) { return service.DefaultManager() }
	installSkill         = skillinstall.Install
	defaultSkillRoot     = skillinstall.DefaultRoot
	sendNotification     = notify.Send
	handleShutdownSignal = handleSignals
	notifySignal         = signal.Notify
	exitProcess          = os.Exit
)

const (
	reportTimestamps       = true
	defaultApply           = false
	defaultWatch           = false
	cleanupScopeAll        = cleanupScope("all")
	cleanupScopeProcesses  = cleanupScope("processes")
	cleanupScopeContainers = cleanupScope("containers")
)

func main() {
	err := run(os.Args[1:], os.Stdout)
	exitOnError(err)
}

func run(args []string, out io.Writer) error {
	handled, err := runInformational(args, out)
	if handled {
		return err
	}
	command, commandArgs := splitCommand(args)
	return dispatch(command, commandArgs, out)
}

func dispatch(command string, args []string, out io.Writer) error {
	handled, err := dispatchPrimary(command, args, out)
	if handled {
		return err
	}
	return dispatchUtility(command, args, out)
}

func dispatchPrimary(command string, args []string, out io.Writer) (bool, error) {
	switch command {
	case "monitor":
		return true, runMonitor(args)
	case "scan":
		return true, runScan(args, out)
	case "cleanup":
		return true, runCleanup(args, out)
	case "history":
		return true, runHistory(out)
	default:
		return false, nil
	}
}

func dispatchUtility(command string, args []string, out io.Writer) error {
	switch command {
	case "install":
		return runInstall(args, out)
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
	cfg, options, err := monitorConfig(args)
	if err != nil {
		return err
	}

	log.SetLevel(log.InfoLevel)
	log.SetReportTimestamp(reportTimestamps)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handleShutdownSignal(cancel)
	m := newMonitorRunner(cfg, options)
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
	log, err := newAuditStore()
	if err != nil {
		return err
	}
	results, err := runProcessCleanup(ctx, cfg, options, log)
	if err != nil {
		return err
	}
	containerResults, err := runDockerCleanup(ctx, log, options)
	if err != nil {
		return err
	}
	return writeCleanupResults(out, results, containerResults)
}

func runProcessCleanup(
	ctx context.Context,
	cfg *config.Config,
	options cleanupOptions,
	log auditStore,
) ([]cleanup.Result, error) {
	if !options.includesProcesses() {
		return nil, nil
	}
	reports, err := scanReports(ctx, cfg)
	if err != nil {
		return nil, err
	}
	results, err := cleanup.Run(ctx, reports, newProcessKiller(), log, options.apply)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func runDockerCleanup(
	ctx context.Context,
	log auditStore,
	options cleanupOptions,
) ([]docker.Result, error) {
	if !options.includesContainers() {
		return nil, nil
	}
	client := newDockerClient()
	if !client.Available() {
		return nil, nil
	}
	return executeDockerCleanup(ctx, client, log, options.apply)
}

func executeDockerCleanup(
	ctx context.Context,
	client docker.Client,
	log auditStore,
	apply bool,
) ([]docker.Result, error) {
	results, err := docker.Run(ctx, client, log, apply)
	if err != nil {
		if docker.IsDaemonUnavailable(err) {
			return nil, nil
		}
		return nil, err
	}
	return results, nil
}

func writeCleanupResults(
	out io.Writer,
	results []cleanup.Result,
	containerResults []docker.Result,
) error {
	noResults := len(results) == 0
	noContainerResults := len(containerResults) == 0
	noCleanupResults := noResults && noContainerResults
	if noCleanupResults {
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

func runInstall(args []string, out io.Writer) error {
	options, err := parseInstallOptions(args)
	if err != nil {
		return err
	}
	if !options.apply {
		return fmt.Errorf("install requires --apply to enable destructive background cleanup")
	}
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
	path, err := installSkill(root)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, path)
	return err
}

func runSkillsPath(out io.Writer) error {
	root, err := defaultSkillRoot()
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
	var scope string
	register := func(flags *flag.FlagSet) { registerCleanupFlags(flags, &options, &scope) }
	cfg, err := config.ParseArgsWith("cleanup", args, register)
	if err != nil {
		return nil, cleanupOptions{}, err
	}
	options.scope, err = parseCleanupScope(scope)
	if err != nil {
		return nil, cleanupOptions{}, err
	}
	return cfg, options, nil
}

func registerCleanupFlags(flags *flag.FlagSet, options *cleanupOptions, scope *string) {
	flags.BoolVar(&options.apply, "apply", defaultApply, "Kill cleanup targets")
	flags.BoolVar(&options.watch, "watch", defaultWatch, "Run cleanup on interval")
	flags.StringVar(
		scope,
		"scope",
		string(cleanupScopeAll),
		"Cleanup scope: all, processes, or containers",
	)
}

func parseCleanupScope(value string) (cleanupScope, error) {
	scope := cleanupScope(value)
	switch scope {
	case cleanupScopeAll, cleanupScopeProcesses, cleanupScopeContainers:
		return scope, nil
	default:
		return "", fmt.Errorf("invalid cleanup scope %q", value)
	}
}

func (o cleanupOptions) includesProcesses() bool {
	return o.scope != cleanupScopeContainers
}

func (o cleanupOptions) includesContainers() bool {
	return o.scope != cleanupScopeProcesses
}

func monitorConfig(args []string) (*config.Config, monitorOptions, error) {
	var options monitorOptions
	cfg, err := config.ParseArgsWith("monitor", args, func(flags *flag.FlagSet) {
		flags.BoolVar(
			&options.apply,
			"apply",
			defaultApply,
			"Terminate processes after the grace period",
		)
	})
	if err != nil {
		return nil, monitorOptions{}, err
	}
	return cfg, options, nil
}

func parseInstallOptions(args []string) (installOptions, error) {
	var options installOptions
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.BoolVar(&options.apply, "apply", defaultApply, "Enable destructive background cleanup")
	if err := flags.Parse(args); err != nil {
		return installOptions{}, err
	}
	return options, nil
}

func splitCommand(args []string) (string, []string) {
	args = trimSeparator(args)
	if len(args) == 0 {
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

func handleSignals(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	notifySignal(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("Received shutdown signal")
		cancel()
	}()
}

func newMonitor(cfg *config.Config, options monitorOptions) *monitor.Monitor {
	lister := newProcessLister()
	processKiller := newProcessKiller()
	return monitor.New(cfg, lister, processKiller, notifyKilled, options.apply)
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

	log.Error("pk error", "error", err)
	exitProcess(1)
}

func isVersionCommand(args []string) bool {
	if len(args) != 1 {
		return false
	}
	isVersionFlag := args[0] == "--version"
	isVersionSubcommand := args[0] == "version"
	return isVersionFlag || isVersionSubcommand
}
