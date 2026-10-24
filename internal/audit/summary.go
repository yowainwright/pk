package audit

import "time"

type Counts struct {
	Successes int `json:"successes"`
	Failures  int `json:"failures"`
}

type Outcomes struct {
	Processes  Counts `json:"processes"`
	Containers Counts `json:"containers"`
}

func Summarize(events []Event, start time.Time, end time.Time) Outcomes {
	var outcomes Outcomes
	for _, event := range events {
		if !appliedWithinWindow(event, start, end) {
			continue
		}
		counts := outcomes.targetCounts(event)
		if counts != nil {
			counts.record(event.Error)
		}
	}
	return outcomes
}

func appliedWithinWindow(event Event, start time.Time, end time.Time) bool {
	withinWindow := !event.Time.Before(start) && !event.Time.After(end)
	hasTime := !event.Time.IsZero()
	counted := event.Applied && hasTime && withinWindow
	return counted
}

func (o *Outcomes) targetCounts(event Event) *Counts {
	processTarget := event.TargetType == "" || event.TargetType == "process"
	isProcess := event.Action == "kill" && processTarget
	if isProcess {
		return &o.Processes
	}
	containerTarget := event.TargetType == "" || event.TargetType == "container"
	isContainer := event.Action == "stop" && containerTarget
	if isContainer {
		return &o.Containers
	}
	return nil
}

func (c *Counts) record(recordedError string) {
	if recordedError != "" {
		c.Failures++
		return
	}
	c.Successes++
}
