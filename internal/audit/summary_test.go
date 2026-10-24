package audit

import (
	"testing"
	"time"
)

func TestSummarizeAppliedProcessAttempts(t *testing.T) {
	now := testTime()
	events := []Event{
		{Time: now, Action: "kill", Applied: true},
		{Time: now, Action: "kill", TargetType: "process", Applied: true, Error: "denied"},
		{Time: now, Action: "kill", Applied: false},
	}
	got := Summarize(events, now.Add(-24*time.Hour), now)
	want := Counts{Successes: 1, Failures: 1}
	if got.Processes != want {
		t.Fatalf("process outcomes: %+v", got.Processes)
	}
}

func TestSummarizeContainerAttemptsAndRejectMismatchedTargets(t *testing.T) {
	now := testTime()
	events := []Event{
		{Time: now, Action: "stop", Applied: true},
		{Time: now, Action: "stop", TargetType: "container", Applied: true, Error: "denied"},
		{Time: now, Action: "stop", TargetType: "process", Applied: true},
		{Time: now, Action: "kill", TargetType: "container", Applied: true},
		{Time: now, Action: "skip", Applied: true},
	}
	got := Summarize(events, now.Add(-24*time.Hour), now)
	want := Outcomes{Containers: Counts{Successes: 1, Failures: 1}}
	if got != want {
		t.Fatalf("outcomes: got %+v, want %+v", got, want)
	}
}

func TestSummarizeWindowBoundariesAndRepeatedAttempts(t *testing.T) {
	now := testTime()
	start := now.Add(-24 * time.Hour)
	times := []time.Time{start.Add(-time.Nanosecond), start, now, now, now.Add(time.Nanosecond), {}}
	var events []Event
	for _, at := range times {
		events = append(events, Event{Time: at, Action: "kill", Applied: true, PID: 42})
	}
	got := Summarize(events, start, now)
	want := Outcomes{Processes: Counts{Successes: 3}}
	if got != want {
		t.Fatalf("outcomes: got %+v, want %+v", got, want)
	}
}
