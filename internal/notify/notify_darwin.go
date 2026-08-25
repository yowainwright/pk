package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

var runCommand = func(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func Send(title, message string) error {
	script := notificationScript(title, message)
	return runCommand("osascript", "-e", script)
}

func notificationScript(title, message string) string {
	escaped := escapeScriptString(message)
	escapedTitle := escapeScriptString(title)
	return fmt.Sprintf(`display notification "%s" with title "%s"`, escaped, escapedTitle)
}

func escapeScriptString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(value)
}
