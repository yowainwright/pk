package cleanup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/process"
)

type Killer interface {
	Kill(ctx context.Context, target process.Process) error
}

type Recorder interface {
	Record(event audit.Event) error
}

type Result struct {
	Report  Report
	Process process.Process
	Applied bool
	Error   string
}

func Run(
	ctx context.Context,
	reports []Report,
	killer Killer,
	recorder Recorder,
	apply bool,
) ([]Result, error) {
	targets := Targets(reports)
	results := make([]Result, 0, len(targets))
	for _, report := range targets {
		current, err := runOne(ctx, report, killer, recorder, apply)
		if err != nil {
			return nil, err
		}
		results = append(results, current...)
	}
	return results, nil
}

func Targets(reports []Report) []Report {
	targets := make([]Report, 0, len(reports))
	for _, report := range reports {
		if isTarget(report) {
			targets = append(targets, report)
		}
	}
	return targets
}

func WriteResults(w io.Writer, results []Result) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(w, "No cleanup targets found.")
		return err
	}

	if _, err := fmt.Fprintln(w, "PID\tAPPLIED\tNAME\tERROR\tREASONS"); err != nil {
		return err
	}
	return writeRows(w, results)
}

func isTarget(report Report) bool {
	hasKillAction := report.Action == ActionKill
	hasHighConfidence := report.Confidence == ConfidenceHigh
	return hasKillAction && hasHighConfidence
}

func runOne(
	ctx context.Context,
	report Report,
	killer Killer,
	recorder Recorder,
	apply bool,
) ([]Result, error) {
	procs := process.KillOrder(report.Process, report.Descendants)
	results := make([]Result, 0, len(procs))
	for _, proc := range procs {
		result := runProcess(ctx, report, proc, killer, apply)
		if err := recordResult(recorder, result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func runProcess(
	ctx context.Context,
	report Report,
	proc process.Process,
	killer Killer,
	apply bool,
) Result {
	result := Result{Report: report, Process: proc, Applied: apply}
	if !apply {
		return result
	}
	if err := killer.Kill(ctx, proc); err != nil {
		result.Error = err.Error()
	}
	return result
}

func recordResult(recorder Recorder, result Result) error {
	if recorder == nil {
		return nil
	}
	event := eventForResult(result)
	if err := recorder.Record(event); err != nil {
		return fmt.Errorf("recording cleanup event: %w", err)
	}
	return nil
}

func eventForResult(result Result) audit.Event {
	proc := result.Process
	return audit.Event{
		Command:     "cleanup",
		Action:      string(result.Report.Action),
		Applied:     result.Applied,
		PID:         proc.PID,
		Name:        proc.Name,
		CommandLine: proc.CommandLine,
		Cwd:         proc.Cwd,
		Reasons:     result.Report.Reasons,
		Error:       result.Error,
	}
}

func writeRows(w io.Writer, results []Result) error {
	for _, result := range results {
		if err := writeRow(w, result); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, result Result) error {
	proc := result.Process
	_, err := fmt.Fprintf(w, "%d\t%t\t%s\t%s\t%s\n",
		proc.PID,
		result.Applied,
		proc.Name,
		result.Error,
		strings.Join(result.Report.Reasons, ","),
	)
	return err
}
