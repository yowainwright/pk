package widget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type publisherDiagnostics struct{ buffer bytes.Buffer }

const publishTimeout = 2 * time.Second

func Publish(ctx context.Context, executable string, snapshot Snapshot) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("widget publishing requires macOS")
	}
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("widget executable path must be absolute")
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encoding widget report: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--pk-publish")
	command.Stdin = bytes.NewReader(data)
	return runPublisher(ctx, command)
}

func runPublisher(ctx context.Context, command *exec.Cmd) error {
	var diagnostics publisherDiagnostics
	command.Stderr = &diagnostics
	command.WaitDelay = 100 * time.Millisecond
	err := errors.Join(command.Run(), ctx.Err())
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(diagnostics.buffer.String())
	return fmt.Errorf("publishing widget report: %w: %s", err, message)
}

func (writer *publisherDiagnostics) Write(data []byte) (int, error) {
	remaining := 4096 - writer.buffer.Len()
	count := min(len(data), remaining)
	_, err := writer.buffer.Write(data[:count])
	return len(data), err
}
