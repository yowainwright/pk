package audit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordKeepsOnlyRecentEvents(t *testing.T) {
	now := testTime()
	log := New(filepath.Join(t.TempDir(), "events.jsonl"))
	log.now = func() time.Time { return now }

	recordTestEvent(t, log, testEvent("old", now.Add(-72*time.Hour)))
	recordTestEvent(t, log, testEvent("new", now))

	events := readTestEvents(t, log)
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Name != "new" {
		t.Fatalf("expected new event, got %q", events[0].Name)
	}
}

func recordTestEvent(t *testing.T, log *Log, event Event) {
	t.Helper()
	if err := log.Record(event); err != nil {
		t.Fatalf("record event: %v", err)
	}
}

func TestTrimEventsKeepsNewestEventsWithinSize(t *testing.T) {
	now := testTime()
	events := make([]Event, 0, 2)
	events = append(events, testEvent("old", now))
	events = append(events, testEvent("new", now))
	maxBytes := encodedSize(events[1])

	trimmed := trimEvents(events, maxBytes)

	if len(trimmed) != 1 {
		t.Fatalf("expected one event, got %d", len(trimmed))
	}
	if trimmed[0].Name != "new" {
		t.Fatalf("expected newest event, got %q", trimmed[0].Name)
	}
}

func TestTrimEventsPreservesChronologicalOrder(t *testing.T) {
	now := testTime()
	events := make([]Event, 0, 3)
	events = append(events, testEvent("old", now))
	events = append(events, testEvent("middle", now))
	events = append(events, testEvent("new", now))

	trimmed := trimEvents(events, 4096)

	if trimmed[0].Name != "old" {
		t.Fatalf("expected oldest event first, got %q", trimmed[0].Name)
	}
	if trimmed[2].Name != "new" {
		t.Fatalf("expected newest event last, got %q", trimmed[2].Name)
	}
}

func TestWriteEventsWritesJSONLines(t *testing.T) {
	var out bytes.Buffer
	event := testEvent("node", testTime())
	events := make([]Event, 0, 1)
	events = append(events, event)

	if err := WriteEvents(&out, events); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"name":"node"`)) {
		t.Fatalf("expected event JSON, got %s", out.String())
	}
}

func TestRecordAddsTimestamp(t *testing.T) {
	now := testTime()
	log := New(filepath.Join(t.TempDir(), "events.jsonl"))
	log.now = func() time.Time { return now }

	recordTestEvent(t, log, Event{Name: "node"})

	events := readTestEvents(t, log)
	if !events[0].Time.Equal(now) {
		t.Fatalf("expected timestamp %s, got %s", now, events[0].Time)
	}
}

func TestEventsReturnsEmptyForMissingLog(t *testing.T) {
	log := New(filepath.Join(t.TempDir(), "missing.jsonl"))

	events := readTestEvents(t, log)

	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func TestEventsReturnsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeTestFile(t, path, []byte("not-json\n"))
	log := New(path)

	_, err := log.Events()

	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestTrimEventsKeepsNewestOversizedEvent(t *testing.T) {
	now := testTime()
	events := []Event{testEvent("new", now)}
	var maxBytes int64 = 1

	trimmed := trimEvents(events, maxBytes)

	if len(trimmed) != 1 {
		t.Fatalf("expected one oversized event, got %d", len(trimmed))
	}
}

func TestWriteEventsReturnsWriterError(t *testing.T) {
	events := []Event{testEvent("node", testTime())}

	err := WriteEvents(failingWriter{}, events)

	if err == nil {
		t.Fatal("expected writer error")
	}
}

func TestDefaultLogUsesDefaultPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("PK_AUDIT_PATH", path)

	log, err := DefaultLog()
	if err != nil {
		t.Fatalf("default log: %v", err)
	}
	if log.path != path {
		t.Fatalf("expected path %q, got %q", path, log.path)
	}
}

func TestDefaultPathUsesAuditPathOverride(t *testing.T) {
	t.Setenv("PK_AUDIT_PATH", "/tmp/pk-events.jsonl")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if path != "/tmp/pk-events.jsonl" {
		t.Fatalf("expected override path, got %q", path)
	}
}

func TestDefaultPathBuildsConfigPath(t *testing.T) {
	t.Setenv("PK_AUDIT_PATH", "")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	want := filepath.Join("pk", "events.jsonl")
	hasPKDir := filepath.Base(filepath.Dir(path)) == "pk"
	hasEventsFile := filepath.Base(path) == "events.jsonl"
	hasExpectedPath := hasPKDir && hasEventsFile
	if !hasExpectedPath {
		t.Fatalf("expected path ending in %q, got %q", want, path)
	}
}

func TestRecordReturnsCreateDirectoryErrors(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "file")
	writeTestFile(t, filePath, nil)
	log := New(filepath.Join(filePath, "events.jsonl"))

	err := log.Record(testEvent("node", testTime()))

	if err == nil {
		t.Fatal("expected create directory error")
	}
}

func testTime() time.Time {
	return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
}

func testEvent(name string, at time.Time) Event {
	var event Event
	event.Time = at
	event.Command = "cleanup"
	event.Action = "kill"
	event.Name = name
	return event
}

func readTestEvents(t *testing.T, log *Log) []Event {
	t.Helper()
	events, err := log.Events()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return events
}

type failingWriter struct{}

func (w failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}
