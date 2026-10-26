package dx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const notificationTimeout = 2 * time.Second

func Notify(ctx context.Context, title, message string) error {
	ctx, cancel := context.WithTimeout(ctx, notificationTimeout)
	defer cancel()
	script := notificationScript(title, message)
	return runCommand(ctx, "osascript", "-e", script)
}

func runCommand(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = 100 * time.Millisecond
	err := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(ctxErr, err)
	}
	return err
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
