package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/yowainwright/pk/internal/audit"
)

const historyOutcomesFormat = `Successes: %d
Failures: %d
Processes: %d successes, %d failures
Containers: %d successes, %d failures
`

const historyDatesFormat = `Service: %s
Tracked processes now: %s
Coverage: retained history (may be incomplete)
Window: %s through %s
Last recorded daemon check: %s
Data as of: %s
`

func writeHistorySnapshot(out io.Writer, snapshot historySummary, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(snapshot)
	}
	if err := writeHistoryOutcomes(out, snapshot.Outcomes); err != nil {
		return err
	}
	return writeHistoryDates(out, snapshot)
}

func writeHistoryOutcomes(out io.Writer, outcomes *audit.Outcomes) error {
	if outcomes == nil {
		_, err := fmt.Fprintln(out, "Cleanup outcomes: unavailable")
		return err
	}
	p, c := outcomes.Processes, outcomes.Containers
	successes := p.Successes + c.Successes
	failures := p.Failures + c.Failures
	_, err := fmt.Fprintf(out, historyOutcomesFormat,
		successes, failures, p.Successes, p.Failures, c.Successes, c.Failures)
	return err
}

func writeHistoryDates(out io.Writer, snapshot historySummary) error {
	tracked := "unavailable"
	if snapshot.TrackedProcesses != nil {
		tracked = fmt.Sprint(*snapshot.TrackedProcesses)
	}
	check := "none recorded"
	if snapshot.LastCheckAt != nil {
		check = snapshot.LastCheckAt.Format(time.RFC3339Nano)
	}
	start := snapshot.WindowStart.Format(time.RFC3339Nano)
	end := snapshot.WindowEnd.Format(time.RFC3339Nano)
	generated := snapshot.GeneratedAt.Format(time.RFC3339Nano)
	_, err := fmt.Fprintf(out, historyDatesFormat,
		snapshot.ServiceState, tracked, start, end, check, generated)
	return err
}
