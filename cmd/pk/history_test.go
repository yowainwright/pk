package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/lifecycle"
)

const historySummaryJSONContract = `{
  "schema_version": 1,
  "generated_at": "2026-10-18T19:30:00Z",
  "last_check_at": "2026-10-18T19:29:58Z",
  "service_state": "active",
  "tracked_processes": 2,
  "window_start": "2026-10-17T19:30:00Z",
  "window_end": "2026-10-18T19:30:00Z",
  "status": "ready",
  "coverage": "retained",
  "outcomes": {
    "processes": {
      "successes": 1,
      "failures": 1
    },
    "containers": {
      "successes": 1,
      "failures": 0
    }
  }
}
`

func TestHistorySummaryJSONContract(t *testing.T) {
	deps := historySummaryDeps(t)
	var out, formatted bytes.Buffer
	requireNoError(t, deps.run([]string{"history", "--summary", "--json"}, &out))
	requireNoError(t, json.Indent(&formatted, out.Bytes(), "", "  "))
	if formatted.String() != historySummaryJSONContract {
		t.Fatalf("unexpected history JSON:\n%s", formatted.String())
	}
}

func TestHistorySummaryZeroActivityWithoutRecordedCheck(t *testing.T) {
	deps := commandDeps(t)
	var out bytes.Buffer
	requireNoError(t, deps.run([]string{"history", "--summary", "--json"}, &out))
	summary := decodeHistorySnapshot(t, out.Bytes())
	if summary.LastCheckAt != nil {
		t.Fatal("invented a daemon check")
	}
	if summary.Outcomes == nil {
		t.Fatal("empty history should have measured zero outcomes")
	}
	if *summary.Outcomes != (audit.Outcomes{}) {
		t.Fatalf("got outcomes %+v, want zero activity", summary.Outcomes)
	}
}

func TestHistorySummaryRejectsFutureChecksWithoutInventingCounts(t *testing.T) {
	deps := historySummaryDeps(t)
	now := deps.dependencies.now()
	state := &deps.lifecycle.state
	state.Daemon.LastTickAt = now.Add(time.Second)
	var out bytes.Buffer
	if err := deps.run([]string{"history", "--summary", "--json"}, &out); err == nil {
		t.Fatal("accepted a future daemon check")
	}
	summary := decodeHistorySnapshot(t, out.Bytes())
	invalid := summary.Status != "unavailable" || summary.Outcomes != nil
	if invalid {
		t.Fatalf("invented cleanup outcomes: %+v", summary)
	}
	if summary.TrackedProcesses != nil {
		t.Fatal("invented a tracked-process count")
	}
}

func TestHistorySummaryJSONCountsAppliedAttempts(t *testing.T) {
	deps := commandDeps(t)
	at := time.Now().Add(-time.Minute)
	deps.audit.events = []audit.Event{{Time: at, Action: "kill", Applied: true, Name: "private"}}
	var out bytes.Buffer
	requireNoError(t, deps.run([]string{"history", "--summary", "--json"}, &out))
	snapshot := decodeHistorySnapshot(t, out.Bytes())
	want := audit.Outcomes{Processes: audit.Counts{Successes: 1}}
	if snapshot.Outcomes == nil {
		t.Fatal("missing ready outcomes")
	}
	if *snapshot.Outcomes != want {
		t.Fatalf("got %+v, want %+v", snapshot.Outcomes, want)
	}
}

func TestHistorySummaryWritesReadableOutcomes(t *testing.T) {
	deps := commandDeps(t)
	at := time.Now().Add(-time.Minute)
	deps.audit.events = []audit.Event{{Time: at, Action: "kill", Applied: true}}
	var out bytes.Buffer
	requireNoError(t, deps.run([]string{"history", "--summary"}, &out))
	assertMainOutputContains(t, out.String(), "Successes: 1")
	assertMainOutputContains(t, out.String(), "Failures: 0")
	assertMainOutputContains(t, out.String(), "retained history")
	assertMainOutputContains(t, out.String(), "Tracked processes now: 0")
}

func TestHistorySummaryIncludesCurrentTrackedProcesses(t *testing.T) {
	deps := commandDeps(t)
	deps.lifecycle.state.Processes = map[string]lifecycle.ManagedProcess{
		"one": {Name: "private-process-one"},
		"two": {Name: "private-process-two"},
	}
	var out bytes.Buffer
	requireNoError(t, deps.run([]string{"history", "--summary", "--json"}, &out))
	snapshot := decodeHistorySnapshot(t, out.Bytes())
	if snapshot.TrackedProcesses == nil {
		t.Fatal("missing current tracked-process count")
	}
	if *snapshot.TrackedProcesses != 2 {
		t.Fatalf("got tracked count %d, want 2", *snapshot.TrackedProcesses)
	}
}

func TestHistorySummaryIncludesServiceActivityAndLastCheck(t *testing.T) {
	deps := commandDeps(t)
	deps.background.status = "active"
	now := time.Now().UTC()
	check := now.Add(-time.Minute)
	state := &deps.lifecycle.state
	state.Daemon.LastTickAt = check
	var out bytes.Buffer
	requireNoError(t, deps.run([]string{"history", "--summary", "--json"}, &out))
	snapshot := decodeHistorySnapshot(t, out.Bytes())
	if snapshot.ServiceState != "active" {
		t.Fatalf("got service state %q", snapshot.ServiceState)
	}
	if snapshot.LastCheckAt == nil {
		t.Fatal("missing last check")
	}
	if !snapshot.LastCheckAt.Equal(check) {
		t.Fatalf("got last check %v, want %v", snapshot.LastCheckAt, check)
	}
}

func TestHistorySummaryTimestampsAfterSourceReads(t *testing.T) {
	deps := commandDeps(t)
	app := deps.app()
	now := time.Now().UTC()
	check := now.Add(time.Second)
	state := &deps.lifecycle.state
	state.Daemon.LastTickAt = check
	app.deps.now = func() time.Time { return now }
	app.deps.newLifecycle = func() (lifecycleStore, error) {
		now = check
		return deps.lifecycle, nil
	}
	var out bytes.Buffer
	app.out = &out
	requireNoError(t, app.run([]string{"history", "--summary", "--json"}))
	snapshot := decodeHistorySnapshot(t, out.Bytes())
	if !snapshot.GeneratedAt.Equal(check) {
		t.Fatalf("report generated before source read: %v", snapshot.GeneratedAt)
	}
}

func TestHistorySummaryDoesNotInventCountsOnReadFailure(t *testing.T) {
	deps := commandDeps(t)
	deps.audit.err = errors.New("audit unreadable")
	var out bytes.Buffer
	err := deps.run([]string{"history", "--summary", "--json"}, &out)
	if !errors.Is(err, deps.audit.err) {
		t.Fatalf("got error %v", err)
	}
	snapshot := decodeHistorySnapshot(t, out.Bytes())
	missing := snapshot.Status != "unavailable" || snapshot.Outcomes != nil
	if missing {
		t.Fatalf("got snapshot %+v", snapshot)
	}
	if snapshot.TrackedProcesses != nil {
		t.Fatal("invented tracking count on read failure")
	}
}

func TestHistorySummaryKeepsCountsWhenServiceStateIsUnknown(t *testing.T) {
	deps := commandDeps(t)
	deps.background.err = errors.New("service unreadable")
	var out bytes.Buffer
	err := deps.run([]string{"history", "--summary", "--json"}, &out)
	if !errors.Is(err, deps.background.err) {
		t.Fatalf("got error %v", err)
	}
	snapshot := decodeHistorySnapshot(t, out.Bytes())
	missing := snapshot.ServiceState != "unknown" || snapshot.Outcomes == nil
	if missing {
		t.Fatalf("got snapshot %+v", snapshot)
	}
}

func TestHistorySummaryRejectsInvalidUsageBeforeReading(t *testing.T) {
	deps := commandDeps(t)
	deps.audit.err = errors.New("must not read")
	var out bytes.Buffer
	err := deps.run([]string{"history", "--summary", "--since", "0s"}, &out)
	var usage historyUsageError
	if !errors.As(err, &usage) {
		t.Fatalf("got error %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestHistoryUsageHasDistinctExitStatus(t *testing.T) {
	app := commandDeps(t).app()
	var code int
	app.deps.exitFunc = func(status int) { code = status }
	app.exitOnError(historyUsageError{errors.New("invalid history flags")})
	if code != 2 {
		t.Fatalf("got exit code %d, want 2", code)
	}
}

func historySummaryDeps(t *testing.T) *commandTestDeps {
	t.Helper()
	deps := commandDeps(t)
	now := time.Date(2026, time.October, 18, 19, 30, 0, 0, time.UTC)
	deps.dependencies.now = func() time.Time { return now }
	deps.background.status = "active"
	state := &deps.lifecycle.state
	state.Daemon.LastTickAt = now.Add(-2 * time.Second)
	state.Processes = map[string]lifecycle.ManagedProcess{"one": {}, "two": {}}
	deps.audit.events = []audit.Event{
		{Time: now, Action: "kill", Applied: true, Name: "private-process"},
		{Time: now, Action: "kill", Applied: true, Error: "private error"},
		{Time: now, Action: "stop", TargetType: "container", Applied: true},
	}
	return deps
}

func decodeHistorySnapshot(t *testing.T, data []byte) historySummary {
	t.Helper()
	var snapshot historySummary
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("invalid snapshot JSON: %v\n%s", err, data)
	}
	return snapshot
}
