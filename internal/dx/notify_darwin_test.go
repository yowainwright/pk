package dx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunCommandExecutesCommand(t *testing.T) {
	if err := runCommand(t.Context(), "true"); err != nil {
		t.Fatalf("run command: %v", err)
	}
}

func TestSendRunsOsaScriptCommand(t *testing.T) {
	path := installNotificationTool(t, `printf '%s\n' "$@" > "$PK_NOTIFICATION_LOG"`)
	if err := Notify(t.Context(), "pk", "done"); err != nil {
		t.Fatalf("send notification: %v", err)
	}
	args, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "-e\ndisplay notification \"done\" with title \"pk\"\n"
	if string(args) != want {
		t.Fatalf("expected command arguments %q, got %q", want, args)
	}
}

func TestNotifyTimesOutBlockedCommand(t *testing.T) {
	installNotificationTool(t, ": > \"$PK_NOTIFICATION_LOG\"\nexec /bin/sleep 30\n")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := Notify(ctx, "pk", "done")
	if ctx.Err() != nil {
		t.Fatal("notification waited for the caller's deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected delivery deadline, got %v", err)
	}
}

func TestNotifyCancelsRunningCommand(t *testing.T) {
	path := installNotificationTool(t, ": > \"$PK_NOTIFICATION_LOG\"\nexec /bin/sleep 30\n")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- Notify(ctx, "pk", "done") }()
	waitForNotificationStart(t, path)
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("notification did not stop after cancellation")
	}
}

func TestNotificationScriptIncludesTitleAndMessage(t *testing.T) {
	script := notificationScript("pk", "Killed node")
	want := `display notification "Killed node" with title "pk"`

	if script != want {
		t.Fatalf("expected script %q, got %q", want, script)
	}
}

func TestNotificationScriptEscapesInjectionCharacters(t *testing.T) {
	script := notificationScript("pk", `Killed a" & do shell script "id" & "\`)
	want := `display notification "Killed a\" & do shell script \"id\" & \"\\" with title "pk"`

	if script != want {
		t.Fatalf("expected script %q, got %q", want, script)
	}
}

func installNotificationTool(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "notification.log")
	tool := filepath.Join(dir, "osascript")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("PK_NOTIFICATION_LOG", path)
	return path
}

func waitForNotificationStart(t *testing.T, path string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("notification helper did not start")
		}
	}
}
