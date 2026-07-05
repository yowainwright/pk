package notify

import "testing"

func TestRunCommandExecutesCommand(t *testing.T) {
	if err := runCommand("true"); err != nil {
		t.Fatalf("run command: %v", err)
	}
}

func TestSendRunsOsaScriptCommand(t *testing.T) {
	originalRunCommand := runCommand
	defer func() {
		runCommand = originalRunCommand
	}()

	var commandName string
	var commandArgs []string
	runCommand = func(name string, args ...string) error {
		commandName = name
		commandArgs = append(commandArgs, args...)
		return nil
	}

	if err := Send("pk", "done"); err != nil {
		t.Fatalf("send notification: %v", err)
	}
	assertNotificationCommand(t, commandName, commandArgs)
}

func TestNotificationScriptIncludesTitleAndMessage(t *testing.T) {
	script := notificationScript("pk", "Killed node")
	want := `display notification "Killed node" with title "pk"`

	if script != want {
		t.Fatalf("expected script %q, got %q", want, script)
	}
}

func assertNotificationCommand(t *testing.T, name string, args []string) {
	t.Helper()
	if name != "osascript" {
		t.Fatalf("expected osascript, got %q", name)
	}
	if len(args) != 2 {
		t.Fatalf("expected two args, got %#v", args)
	}
	if args[0] != "-e" {
		t.Fatalf("expected -e arg, got %q", args[0])
	}
}
