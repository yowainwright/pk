package widget

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/audit"
)

func TestNewSnapshotMatchesReadyContract(t *testing.T) {
	now := snapshotTime()
	check := now.Add(-2 * time.Second)
	events := []audit.Event{
		{Time: now, Action: "kill", Applied: true, Name: "private-process"},
		{Time: now, Action: "kill", Applied: true, Error: "private error"},
		{Time: now, Action: "stop", TargetType: "container", Applied: true},
	}
	snapshot, err := NewSnapshot(events, check, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tracked := 12
	snapshot.TrackedProcesses = &tracked
	assertSnapshotFixture(t, snapshot, "ready.json")
}

func TestNewSnapshotDistinguishesEmptyHistoryFromMissingCheck(t *testing.T) {
	now := snapshotTime()
	checked, err := NewSnapshot(nil, now.Add(-2*time.Second), now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotFixture(t, checked, "zero.json")
	unrecorded, err := NewSnapshot(nil, time.Time{}, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotFixture(t, unrecorded, "no-check.json")
}

func TestNewSnapshotRejectsFutureChecksWithoutInventingCounts(t *testing.T) {
	now := snapshotTime()
	future := now.Add(time.Second)
	snapshot, err := NewSnapshot(nil, future, now, 24*time.Hour)
	if err == nil {
		t.Fatal("expected future check error")
	}
	assertSnapshotFixture(t, snapshot, "unavailable.json")
}

func snapshotTime() time.Time {
	return time.Date(2026, time.October, 18, 19, 30, 0, 0, time.UTC)
}

func assertSnapshotFixture(t *testing.T, snapshot Snapshot, name string) {
	t.Helper()
	got, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../tests/fixtures/widget/" + name)
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if string(got) != string(want) {
		t.Fatalf("snapshot contract mismatch:\ngot: %s\nwant: %s", got, want)
	}
}
