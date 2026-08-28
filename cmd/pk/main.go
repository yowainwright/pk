package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/cleanup"
	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/diagnostics"
	"github.com/yowainwright/pk/internal/docker"
	"github.com/yowainwright/pk/internal/dx"
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
	Install(context.Context) error
	Uninstall(context.Context) error
	Status(context.Context) (string, error)
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

type globalOptions struct {
	color dx.ColorMode
}

type colorArgument struct {
	value    string
	consumed int
	found    bool
}

type application struct {
	ctx context.Context
	ui  *dx.UI
	out io.Writer
}

var (
	newProcessLister  = func() process.Lister { return process.NewLister() }
	newProcessScanner = func(cfg *config.Config, lister process.Lister) processScanner { return scan.New(cfg, lister) }
	newAuditStore     = func() (auditStore, error) { return audit.DefaultLog() }
	newProcessKiller  = func() killer.Killer { return killer.New() }
	newDockerClient   = func() docker.Client { return docker.NewClient() }
	newMonitorRunner  = func(
		cfg *config.Config,
		options monitorOptions,
		logger *slog.Logger,
	) monitorRunner {
		return newMonitor(cfg, options, logger)
	}
	newBackgroundManager = func() (backgroundManager, error) { return service.DefaultManager() }
	installSkill         = skillinstall.Install
	defaultSkillRoot     = skillinstall.DefaultRoot
	sendNotification     = notify.Send
	handleShutdownSignal = handleSignals
	notifySignal         = signal.Notify
	stopSignal           = signal.Stop
	resetSignals         = signal.Reset
	exitProcess          = os.Exit
)

const (
	defaultApply           = false
	defaultWatch           = false
	cleanupScopeAll        = cleanupScope("all")
	cleanupScopeProcesses  = cleanupScope("processes")
	cleanupScopeContainers = cleanupScope("containers")
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restoreSignals := handleShutdownSignal(cancel)
	defer restoreSignals()
	app, args, err := newApplication(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err == nil {
		err = app.run(args)
	}
	app.exitOnError(err)
}

func newApplication(
	ctx context.Context,
	args []string,
	in io.Reader,
	out io.Writer,
	errOut io.Writer,
) (application, []string, error) {
	commandArgs, options, err := parseGlobalOptions(args)
	ui := dx.New(dx.Config{
		In:         in,
		Out:        out,
		Err:        errOut,
		Color:      options.color,
		Timestamps: true,
	})
	return application{ctx: ctx, ui: ui, out: out}, commandArgs, err
}

func (a application) run(args []string) error {
	handled, err := runInformational(args, a.ui)
	if handled {
		return err
	}
	command, commandArgs := splitCommand(args)
	return a.dispatch(command, commandArgs)
}

func (a application) dispatch(command string, args []string) error {
	handled, err := a.dispatchPrimary(command, args)
	if handled {
		return err
	}
	return a.dispatchUtility(command, args)
}

func (a application) dispatchPrimary(command string, args []string) (bool, error) {
	switch command {
	case "monitor":
		return true, a.runMonitor(args)
	case "scan":
		return true, a.runScan(args)
	case "cleanup":
		return true, a.runCleanup(args)
	case "history":
		return true, a.runHistory()
	default:
		return false, nil
	}
}

func (a application) dispatchUtility(command string, args []string) error {
	switch command {
	case "install":
		return a.runInstall(args)
	case "uninstall":
		return a.runUninstall()
	case "status":
		return a.runStatus()
	case "doctor":
		return a.runDoctor()
	case "skills":
		return a.runSkills(args)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func (a application) runMonitor(args []string) error {
	cfg, options, err := monitorConfigWithOutput(args, a.ui.ErrorWriter())
	if err != nil {
		return err
	}
	m := newMonitorRunner(cfg, options, a.ui.Logger())
	return m.Run(a.ctx)
}

func (a application) runScan(args []string) error {
	cfg, err := config.ParseArgsWithOutput("scan", args, a.ui.ErrorWriter(), nil)
	if err != nil {
		return err
	}
	var reports []scan.Report
	err = a.ui.Task(a.ctx, "Scanning processes", func(ctx context.Context) error {
		var scanErr error
		reports, scanErr = scanReports(ctx, cfg)
		return operationError("scanning processes", scanErr)
	})
	if err != nil {
		return err
	}
	return operationError("writing scan results", scan.WriteReports(a.out, reports))
}

func (a application) runCleanup(args []string) error {
	cfg, options, err := cleanupConfigWithOutput(args, a.ui.ErrorWriter())
	if err != nil {
		return err
	}
	if options.watch {
		return runCleanupWatch(a.ctx, cfg, options, a.out)
	}
	return a.runBoundedCleanup(cfg, options)
}

func (a application) runBoundedCleanup(cfg *config.Config, options cleanupOptions) error {
	var results cleanupResults
	label := cleanupTaskLabel(options.apply)
	err := a.ui.Task(a.ctx, label, func(ctx context.Context) error {
		var cleanupErr error
		results, cleanupErr = collectCleanupResults(ctx, cfg, options)
		return cleanupErr
	})
	if err != nil {
		return err
	}
	writeErr := writeCleanupResults(a.out, results.processes, results.containers)
	return operationError("writing cleanup results", writeErr)
}

func runCleanupOnce(
	ctx context.Context,
	cfg *config.Config,
	options cleanupOptions,
	out io.Writer,
) error {
	results, err := collectCleanupResults(ctx, cfg, options)
	if err != nil {
		return err
	}
	writeErr := writeCleanupResults(out, results.processes, results.containers)
	return operationError("writing cleanup results", writeErr)
}

type cleanupResults struct {
	processes  []cleanup.Result
	containers []docker.Result
}

func collectCleanupResults(
	ctx context.Context,
	cfg *config.Config,
	options cleanupOptions,
) (cleanupResults, error) {
	log, err := newAuditStore()
	if err != nil {
		return cleanupResults{}, operationError("opening audit store", err)
	}
	results, err := runProcessCleanup(ctx, cfg, options, log)
	if err != nil {
		return cleanupResults{}, operationError("cleaning processes", err)
	}
	containerResults, err := runDockerCleanup(ctx, log, options)
	if err != nil {
		return cleanupResults{}, operationError("cleaning containers", err)
	}
	return cleanupResults{processes: results, containers: containerResults}, nil
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
		return nil, operationError("running Docker cleanup", err)
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
	ctx context.Context,
	cfg *config.Config,
	options cleanupOptions,
	out io.Writer,
) error {
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

func (a application) runHistory() error {
	var events []audit.Event
	err := a.ui.Task(a.ctx, "Loading cleanup history", func(context.Context) error {
		log, logErr := newAuditStore()
		if logErr != nil {
			return operationError("opening audit store", logErr)
		}
		var eventsErr error
		events, eventsErr = log.Events()
		return operationError("reading cleanup history", eventsErr)
	})
	if err != nil {
		return err
	}
	return operationError("writing cleanup history", audit.WriteEvents(a.out, events))
}

func (a application) runInstall(args []string) error {
	options, err := parseInstallOptionsWithOutput(args, a.ui.ErrorWriter())
	if err != nil {
		return operationError("parsing install options", err)
	}
	if !options.apply {
		return fmt.Errorf("install requires --apply to enable destructive background cleanup")
	}
	err = a.ui.Task(a.ctx, "Installing background cleanup", installBackground)
	if err != nil {
		return err
	}
	return a.ui.Value("installed")
}

func installBackground(ctx context.Context) error {
	manager, err := newBackgroundManager()
	if err != nil {
		return operationError("opening background manager", err)
	}
	return operationError("installing background cleanup", manager.Install(ctx))
}

func (a application) runUninstall() error {
	err := a.ui.Task(a.ctx, "Uninstalling background cleanup", uninstallBackground)
	if err != nil {
		return err
	}
	return a.ui.Value("uninstalled")
}

func uninstallBackground(ctx context.Context) error {
	manager, err := newBackgroundManager()
	if err != nil {
		return operationError("opening background manager", err)
	}
	return operationError("uninstalling background cleanup", manager.Uninstall(ctx))
}

func (a application) runStatus() error {
	var status string
	err := a.ui.Task(
		a.ctx,
		"Checking background cleanup",
		func(ctx context.Context) error {
			manager, managerErr := newBackgroundManager()
			if managerErr != nil {
				return operationError("opening background manager", managerErr)
			}
			var statusErr error
			status, statusErr = manager.Status(ctx)
			return operationError("reading background status", statusErr)
		},
	)
	if err != nil {
		return err
	}
	return a.ui.Value(status)
}

func (a application) runDoctor() error {
	serviceStatus, serviceErr := diagnosticServiceStatus(a.ctx)
	auditEvents, auditErr := diagnosticAuditEvents()
	input := diagnostics.Input{
		Version:         displayVersion(),
		ServiceStatus:   serviceStatus,
		ServiceErr:      serviceErr,
		DockerAvailable: newDockerClient().Available(),
		AuditEvents:     auditEvents,
		AuditErr:        auditErr,
		AuditOverride:   os.Getenv("PK_AUDIT_PATH") != "",
	}
	return operationError("writing diagnostics", diagnostics.Write(a.out, diagnostics.New(input)))
}

func diagnosticServiceStatus(ctx context.Context) (string, error) {
	manager, err := newBackgroundManager()
	if err != nil {
		return "", err
	}
	return manager.Status(ctx)
}

func diagnosticAuditEvents() (int, error) {
	log, err := newAuditStore()
	if err != nil {
		return 0, err
	}
	events, err := log.Events()
	return len(events), err
}

func (a application) runSkills(args []string) error {
	command, commandArgs := splitCommand(args)
	switch command {
	case "install":
		return a.runSkillsInstall(commandArgs)
	case "path":
		return a.runSkillsPath()
	default:
		return fmt.Errorf("unknown skills command %q", command)
	}
}

func (a application) runSkillsInstall(args []string) error {
	var root string
	flags := flag.NewFlagSet("skills install", flag.ContinueOnError)
	flags.SetOutput(a.ui.ErrorWriter())
	flags.StringVar(&root, "dir", "", "Skills root directory")
	if err := flags.Parse(args); err != nil {
		return operationError("parsing skill install options", err)
	}
	var path string
	err := a.ui.Task(a.ctx, "Installing Codex skill", func(context.Context) error {
		var installErr error
		path, installErr = installSkill(root)
		return operationError("installing Codex skill", installErr)
	})
	if err != nil {
		return err
	}
	return a.ui.Value(path)
}

func (a application) runSkillsPath() error {
	root, err := defaultSkillRoot()
	if err != nil {
		return operationError("finding skill root", err)
	}
	return a.ui.Value(skillinstall.SkillPath(root))
}

func scanReports(ctx context.Context, cfg *config.Config) ([]scan.Report, error) {
	lister := newProcessLister()
	scanner := newProcessScanner(cfg, lister)
	return scanner.Scan(ctx)
}

func cleanupConfigWithOutput(
	args []string,
	output io.Writer,
) (*config.Config, cleanupOptions, error) {
	var options cleanupOptions
	var scope string
	register := func(flags *flag.FlagSet) { registerCleanupFlags(flags, &options, &scope) }
	cfg, err := config.ParseArgsWithOutput("cleanup", args, output, register)
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

func monitorConfigWithOutput(
	args []string,
	output io.Writer,
) (*config.Config, monitorOptions, error) {
	var options monitorOptions
	cfg, err := config.ParseArgsWithOutput("monitor", args, output, func(flags *flag.FlagSet) {
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

func parseInstallOptionsWithOutput(
	args []string,
	output io.Writer,
) (installOptions, error) {
	var options installOptions
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&options.apply, "apply", defaultApply, "Enable destructive background cleanup")
	if err := flags.Parse(args); err != nil {
		return installOptions{}, err
	}
	return options, nil
}

func parseGlobalOptions(args []string) ([]string, globalOptions, error) {
	options := globalOptions{color: dx.ColorAuto}
	commandArgs := make([]string, 0, len(args))
	return collectGlobalOptions(args, commandArgs, options)
}

func collectGlobalOptions(
	args []string,
	commandArgs []string,
	options globalOptions,
) ([]string, globalOptions, error) {
	if len(args) == 0 {
		return commandArgs, options, nil
	}
	if args[0] == "--" {
		return append(commandArgs, args...), options, nil
	}
	color, err := colorOption(args)
	if err != nil {
		return nil, options, err
	}
	return collectColorOption(args, commandArgs, options, color)
}

func collectColorOption(
	args []string,
	commandArgs []string,
	options globalOptions,
	color colorArgument,
) ([]string, globalOptions, error) {
	if !color.found {
		return collectGlobalOptions(args[1:], append(commandArgs, args[0]), options)
	}
	mode, err := dx.ParseColorMode(color.value)
	if err != nil {
		return nil, options, err
	}
	options.color = mode
	return collectGlobalOptions(args[color.consumed:], commandArgs, options)
}

func colorOption(args []string) (colorArgument, error) {
	value, found := strings.CutPrefix(args[0], "--color=")
	if found {
		return colorArgument{value: value, consumed: 1, found: true}, nil
	}
	if args[0] != "--color" {
		return colorArgument{}, nil
	}
	if len(args) < 2 {
		return colorArgument{}, fmt.Errorf("--color requires auto, always, or never")
	}
	return colorArgument{value: args[1], consumed: 2, found: true}, nil
}

func cleanupTaskLabel(apply bool) string {
	if apply {
		return "Applying cleanup"
	}
	return "Previewing cleanup"
}

func operationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
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

func handleSignals(cancel context.CancelFunc) func() {
	sigCh := make(chan os.Signal, 1)
	notifySignal(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	var once sync.Once
	restore := func() {
		once.Do(func() {
			stopSignal(sigCh)
			resetSignals(syscall.SIGINT, syscall.SIGTERM)
			close(done)
		})
	}
	go waitForSignal(sigCh, done, restore, cancel)
	return restore
}

func waitForSignal(
	sigCh <-chan os.Signal,
	done <-chan struct{},
	restore func(),
	cancel context.CancelFunc,
) {
	select {
	case <-sigCh:
		restore()
		cancel()
	case <-done:
	}
}

func newMonitor(
	cfg *config.Config,
	options monitorOptions,
	logger *slog.Logger,
) *monitor.Monitor {
	lister := newProcessLister()
	processKiller := newProcessKiller()
	monitorConfig := monitor.Options{
		Apply:  options.apply,
		Logger: logger,
	}
	return monitor.New(cfg, lister, processKiller, notifyKilled, monitorConfig)
}

func notifyKilled(name string, pid int32) error {
	msg := fmt.Sprintf("Killed %s (PID %d)", name, pid)
	return operationError("sending kill notification", sendNotification("pk", msg))
}

func (a application) exitOnError(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}

	a.ui.Logger().Error("pk error", "error", err)
	a.ui.Logger().Info("Run pk doctor to create a shareable diagnostic report")
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
