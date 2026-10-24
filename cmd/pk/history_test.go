package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/lifecycle"
	"github.com/yowainwright/pk/internal/widget"
)

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

func decodeHistorySnapshot(t *testing.T, data []byte) widget.Snapshot {
	t.Helper()
	var snapshot widget.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("invalid snapshot JSON: %v\n%s", err, data)
	}
	return snapshot
}
