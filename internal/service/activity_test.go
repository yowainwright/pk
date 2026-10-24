package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestActivityReportsRunningLaunchAgent(t *testing.T) {
	runner := &fakeRunner{output: []byte("service = {\n\tstate = running\n}\n")}
	manager := testManager(t, "darwin", runner)
	writeTestServiceFile(t, manager)
	assertActivity(t, manager, "active")
}

func TestActivityReportsStoppedLaunchAgent(t *testing.T) {
	runner := &fakeRunner{output: []byte("state = not running\n")}
	manager := testManager(t, "darwin", runner)
	writeTestServiceFile(t, manager)
	assertActivity(t, manager, "inactive")
}

func TestActivityReportsAbsentInstallation(t *testing.T) {
	manager := testManager(t, "darwin", &fakeRunner{})
	assertActivity(t, manager, "inactive")
}

func TestActivityDoesNotTreatReadFailuresAsInactive(t *testing.T) {
	runner := &fakeRunner{err: errors.New("permission denied")}
	manager := testManager(t, "darwin", runner)
	writeTestServiceFile(t, manager)
	state, err := manager.Activity(t.Context())
	wrongState := state != "unknown"
	wrongError := !errors.Is(err, runner.err)
	wrongResult := wrongState || wrongError
	if wrongResult {
		t.Fatalf("got %q, %v", state, err)
	}
}

func TestActivityReportsUnreadableRegistrationAsUnknown(t *testing.T) {
	manager := testManager(t, "darwin", &fakeRunner{})
	writeTestServiceFile(t, manager)
	directory := filepath.Dir(manager.servicePath())
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	if err := os.Chmod(directory, 0); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(manager.servicePath())
	if !errors.Is(err, os.ErrPermission) {
		t.Skip("directory permissions are not enforced for this user")
	}
	state, err := manager.Activity(t.Context())
	if state != "unknown" {
		t.Fatalf("got %q, want unknown", state)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected registration permission error, got %v", err)
	}
}

func TestActivityRejectsUnrecognizedLaunchdOutput(t *testing.T) {
	manager := testManager(t, "darwin", &fakeRunner{output: []byte("service = enabled\n")})
	writeTestServiceFile(t, manager)
	state, err := manager.Activity(t.Context())
	wrongResult := state != "unknown" || err == nil
	if wrongResult {
		t.Fatalf("got %q, %v", state, err)
	}
}

func TestActivityReportsRunningSystemdService(t *testing.T) {
	manager := testManager(t, "linux", &fakeRunner{output: []byte("active\n")})
	writeTestServiceFile(t, manager)
	assertActivity(t, manager, "active")
}

func assertActivity(t *testing.T, manager *Manager, want string) {
	t.Helper()
	state, err := manager.Activity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state != want {
		t.Fatalf("got %q, want %q", state, want)
	}
}
