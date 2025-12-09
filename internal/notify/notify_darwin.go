package notify

import (
	"fmt"
	"os/exec"
)

func Send(title, message string) error {
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, message, title)
	return exec.Command("osascript", "-e", script).Run()
}
