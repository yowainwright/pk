package widget

import (
	"fmt"
	"time"

	"github.com/yowainwright/pk/internal/audit"
)

type Snapshot struct {
	SchemaVersion    int             `json:"schema_version"`
	GeneratedAt      time.Time       `json:"generated_at"`
	LastCheckAt      *time.Time      `json:"last_check_at"`
	ServiceState     string          `json:"service_state"`
	TrackedProcesses *int            `json:"tracked_processes"`
	WindowStart      time.Time       `json:"window_start"`
	WindowEnd        time.Time       `json:"window_end"`
	Status           string          `json:"status"`
	Coverage         string          `json:"coverage"`
	Outcomes         *audit.Outcomes `json:"outcomes"`
}

func NewSnapshot(
	events []audit.Event,
	lastCheck, now time.Time,
	window time.Duration,
) (Snapshot, error) {
	snapshot := UnavailableSnapshot(now, window)
	if window <= 0 {
		return snapshot, fmt.Errorf("summary window must be positive")
	}
	if lastCheck.After(now) {
		return snapshot, fmt.Errorf("last recorded check is later than report generation")
	}
	outcomes := audit.Summarize(events, snapshot.WindowStart, snapshot.WindowEnd)
	snapshot.Outcomes = &outcomes
	snapshot.Status = "ready"
	snapshot.LastCheckAt = optionalCheckTime(lastCheck)
	return snapshot, nil
}

func UnavailableSnapshot(now time.Time, window time.Duration) Snapshot {
	generated := now.UTC()
	start := generated.Add(-window)
	return Snapshot{
		SchemaVersion: 1,
		GeneratedAt:   generated,
		ServiceState:  "unknown",
		WindowStart:   start,
		WindowEnd:     generated,
		Status:        "unavailable",
		Coverage:      "retained",
	}
}

func optionalCheckTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	utc := at.UTC()
	return &utc
}
