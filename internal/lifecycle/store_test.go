package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAppendsAndReadsEvents(t *testing.T) {
	store := NewStore(t.TempDir())
	event := testEvent(t, KindSessionStart)

	if err := store.Append(event); err != nil {
		t.Fatalf("append event: %v", err)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].EventID != event.EventID {
		t.Fatalf("expected event id %q, got %q", event.EventID, events[0].EventID)
	}
}

func TestStoreRejectsUnknownEventKind(t *testing.T) {
	store := NewStore(t.TempDir())
	event := testEvent(t, "missing")

	err := store.Append(event)

	if err == nil {
		t.Fatal("expected unknown kind error")
	}
}

func TestStoreTakesAndAcknowledgesEvents(t *testing.T) {
	store := NewStore(t.TempDir())
	event := testEvent(t, KindSessionStart)

	if err := store.Append(event); err != nil {
		t.Fatalf("append event: %v", err)
	}
	events, err := store.TakeEvents()
	if err != nil {
		t.Fatalf("take events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one taken event, got %d", len(events))
	}
	if err := store.AcknowledgeEvents(); err != nil {
		t.Fatalf("acknowledge events: %v", err)
	}
	assertNoEvents(t, store)
}

func TestStoreKeepsNewEventsAfterAcknowledgingTakenEvents(t *testing.T) {
	store := NewStore(t.TempDir())
	appendTestEvent(t, store, "taken-event")
	if _, err := store.TakeEvents(); err != nil {
		t.Fatalf("take events: %v", err)
	}
	appendTestEvent(t, store, "next-event")
	if err := store.AcknowledgeEvents(); err != nil {
		t.Fatalf("acknowledge events: %v", err)
	}

	events, err := store.TakeEvents()
	if err != nil {
		t.Fatalf("take next events: %v", err)
	}
	if events[0].EventID != "next-event" {
		t.Fatalf("expected next event, got %#v", events)
	}
}

func TestStoreSavesPrivateState(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	state := State{}
	state.Ensure()
	state.Sessions["session"] = TerminalSession{ID: "session"}

	if err := store.SaveState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	assertMode(t, dir, privateDirMode)
	assertMode(t, filepath.Join(dir, "lifecycle-state.json"), privateFileMode)
}

func TestStoreLoadsMissingState(t *testing.T) {
	store := NewStore(t.TempDir())

	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	state.Ensure()
	if len(state.Sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(state.Sessions))
	}
}

func appendTestEvent(t *testing.T, store *Store, eventID string) {
	t.Helper()
	event := testEvent(t, KindSessionStart)
	event.EventID = eventID
	if err := store.Append(event); err != nil {
		t.Fatalf("append event: %v", err)
	}
}

func assertNoEvents(t *testing.T, store *Store) {
	t.Helper()
	events, err := store.Events()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func testEvent(t *testing.T, kind string) Event {
	t.Helper()
	eventID, err := NewEventID()
	if err != nil {
		t.Fatalf("new event id: %v", err)
	}
	return Event{
		Version:           Version,
		EventID:           eventID,
		Kind:              kind,
		ObservedAt:        time.Now(),
		Source:            "zsh",
		TerminalSessionID: "session",
		ShellPID:          123,
		ShellCreateTime:   456,
	}
}

func assertMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != expected {
		t.Fatalf("expected mode %o, got %o", expected, info.Mode().Perm())
	}
}
