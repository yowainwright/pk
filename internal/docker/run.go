package docker

import (
	"context"
	"fmt"

	"github.com/yowainwright/pk/internal/audit"
)

func runReports(
	ctx context.Context,
	reports []Report,
	client Client,
	recorder Recorder,
	apply bool,
) ([]Result, error) {
	results := make([]Result, 0, len(reports))
	for _, report := range reports {
		result := runReport(ctx, report, client, apply)
		if err := recordResult(recorder, result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func runReport(ctx context.Context, report Report, client Client, apply bool) Result {
	result := Result{Report: report, Applied: apply}
	if !apply {
		return result
	}
	if err := client.Stop(ctx, report.Container.ID); err != nil {
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
		return fmt.Errorf("recording docker cleanup event: %w", err)
	}
	return nil
}

func eventForResult(result Result) audit.Event {
	container := result.Report.Container
	return audit.Event{
		Command:     "cleanup",
		Action:      result.Report.Action,
		TargetType:  "container",
		Applied:     result.Applied,
		Name:        container.Name,
		ContainerID: container.ID,
		Image:       container.Image,
		CommandLine: container.Command,
		Reasons:     result.Report.Reasons,
		Error:       result.Error,
	}
}
