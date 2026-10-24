package widget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPublisherDeliversExactInput(t *testing.T) {
	command := publisherTestCommand(t.Context(), "copy")
	want := []byte("{\"schema_version\":1}\n")
	command.Stdin = bytes.NewReader(want)
	var output bytes.Buffer
	command.Stdout = &output
	if err := runPublisher(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("publisher changed stdin: %q", output.String())
	}
}

func TestPublisherBoundsFailureDiagnostics(t *testing.T) {
	command := publisherTestCommand(t.Context(), "fail")
	err := runPublisher(t.Context(), command)
	if err == nil {
		t.Fatal("publisher ignored child failure")
	}
	expected := strings.Repeat("x", 4096)
	if !strings.HasSuffix(err.Error(), expected) {
		t.Fatal("missing expected failure diagnostics")
	}
	if strings.Contains(err.Error(), expected+"x") {
		t.Fatal("publisher retained excessive failure diagnostics")
	}
}

func TestPublisherStopsAtCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	command := publisherTestCommand(ctx, "wait")
	started := time.Now()
	err := runPublisher(ctx, command)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("publisher failed to stop promptly")
	}
}

func TestPublisherRejectsRelativeExecutable(t *testing.T) {
	err := Publish(t.Context(), "Widget", Snapshot{})
	if err == nil {
		t.Fatal("accepted a relative executable")
	}
}

func TestWidgetPublisherProcess(t *testing.T) {
	switch os.Getenv("PK_WIDGET_PUBLISHER_TEST") {
	case "copy":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	case "fail":
		if _, err := io.WriteString(os.Stderr, strings.Repeat("x", 8192)); err != nil {
			t.Fatal(err)
		}
		os.Exit(1)
	case "wait":
		time.Sleep(time.Minute)
	default:
		t.Skip("subprocess helper")
	}
}

func publisherTestCommand(ctx context.Context, mode string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWidgetPublisherProcess$")
	command.Env = append(os.Environ(), "PK_WIDGET_PUBLISHER_TEST="+mode)
	return command
}
