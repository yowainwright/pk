package notify

import (
	"fmt"
	"os/exec"
)

var runCommand = func(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func Send(title, message string) error {
	script := notificationScript(title, message)
	return runCommand("osascript", "-e", script)
}

func notificationScript(title, message string) string {
	return fmt.Sprintf(`display notification "%s" with title "%s"`, message, title)
}
