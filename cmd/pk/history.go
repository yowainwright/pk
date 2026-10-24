package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/lifecycle"
	"github.com/yowainwright/pk/internal/widget"
)

type historyOptions struct {
	summary bool
	json    bool
	since   time.Duration
}

type historyUsageError struct{ error }

const (
	defaultHistoryWindow  = 24 * time.Hour
	defaultHistorySummary = false
	defaultHistoryJSON    = false
)

func (a application) runHistoryOptions(args []string) error {
	options, err := parseHistoryOptions(args)
	if err != nil {
		return historyUsageError{err}
	}
	if !options.summary {
		return a.runHistory()
	}
	return a.runHistorySummary(options)
}

func parseHistoryOptions(args []string) (historyOptions, error) {
	var options historyOptions
	flags := historyFlags(&options)
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("history does not accept positional arguments")
	}
	err := validateHistoryOptions(options, historyFlagSet(flags, "since"))
	return options, err
}

func historyFlags(options *historyOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.summary, "summary", defaultHistorySummary, "Summarize cleanup attempts")
	flags.BoolVar(&options.json, "json", defaultHistoryJSON, "Write the summary as JSON")
	flags.DurationVar(&options.since, "since", defaultHistoryWindow, "Summary window, such as 24h")
	return flags
}

func historyFlagSet(flags *flag.FlagSet, name string) bool {
	provided := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == name {
			provided = true
		}
	})
	return provided
}

func validateHistoryOptions(options historyOptions, windowSet bool) error {
	hasSummaryFlags := options.json || windowSet
	missingSummary := !options.summary && hasSummaryFlags
	if missingSummary {
		return fmt.Errorf("--json and --since require --summary")
	}
	if options.since <= 0 {
		return fmt.Errorf("--since must be a positive duration")
	}
	return nil
}

func (a application) runHistorySummary(options historyOptions) error {
	state, stateErr := a.historyServiceState()
	snapshot, readErr := a.loadHistorySnapshot(options.since)
	snapshot.ServiceState = state
	writeErr := writeHistorySnapshot(a.out, snapshot, options.json)
	return errors.Join(readErr, stateErr, operationError("writing history summary", writeErr))
}

func (a application) historyServiceState() (string, error) {
	manager, err := a.deps.newBackground()
	if err != nil {
		return "unknown", operationError("opening background manager", err)
	}
	state, err := manager.Activity(a.ctx)
	return state, operationError("reading service activity", err)
}

func (a application) loadHistorySnapshot(window time.Duration) (widget.Snapshot, error) {
	events, err := a.historyEvents()
	if err != nil {
		return widget.UnavailableSnapshot(a.deps.now(), window), err
	}
	state, err := a.historyLifecycleState()
	now := a.deps.now()
	if err != nil {
		return widget.UnavailableSnapshot(now, window), err
	}
	snapshot, err := widget.NewSnapshot(events, state.Daemon.LastTickAt, now, window)
	if err != nil {
		return snapshot, err
	}
	tracked := len(state.Processes)
	snapshot.TrackedProcesses = &tracked
	return snapshot, nil
}

func (a application) historyEvents() ([]audit.Event, error) {
	log, err := a.deps.newAudit()
	if err != nil {
		return nil, operationError("opening audit store", err)
	}
	events, err := log.Events()
	return events, operationError("reading cleanup history", err)
}

func (a application) historyLifecycleState() (lifecycle.State, error) {
	store, err := a.deps.newLifecycle()
	if err != nil {
		return lifecycle.State{}, operationError("opening lifecycle store", err)
	}
	state, err := store.LoadState()
	return state, operationError("reading lifecycle state", err)
}
