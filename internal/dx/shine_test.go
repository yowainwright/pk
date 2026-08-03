package dx_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/dx"
)

func TestShineAnimatesAndRestoresTheCursor(t *testing.T) {
	output, clock, ui := shineUI()
	finished := startShine(ui)
	duration := clock.nextTimer(t)
	ticker := clock.nextTicker(t)
	ticker.ticker.fire()
	waitForOutput(t, output, "+ Done +")
	duration.timer.fire()
	if err := <-finished; err != nil {
		t.Fatalf("shine: %v", err)
	}
	expected := "\x1b[?25l\r\x1b[2K* Done *\r\x1b[2K+ Done +\r\x1b[2K\x1b[?25h** Done\n"
	assertOutput(t, output, expected)
}

func TestShineUsesStaticFallbackWithoutTTY(t *testing.T) {
	var output bytes.Buffer
	ui := dx.New(dx.Config{Err: &output, Capabilities: &dx.Capabilities{}, LookupEnv: emptyEnv})

	if err := ui.Shine(context.Background(), "Done"); err != nil {
		t.Fatalf("shine: %v", err)
	}
	if output.String() != "** Done\n" {
		t.Fatalf("unexpected static shine %q", output.String())
	}
}

func TestShineRestoresCursorWhenCanceled(t *testing.T) {
	output, clock, ui := shineUI()
	ctx, cancel := context.WithCancel(context.Background())
	finished := startShineWithContext(ctx, ui)
	clock.nextTimer(t)
	clock.nextTicker(t)
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled shine, got %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("\x1b[?25h")) {
		t.Fatalf("expected restored cursor, got %q", output.String())
	}
}

func TestShineRestoresCursorAfterStartError(t *testing.T) {
	expected := errors.New("write failed")
	output, ui := failingShineUI(expected)
	err := ui.Shine(context.Background(), "Done")
	if !errors.Is(err, expected) {
		t.Fatalf("expected writer error, got %v", err)
	}
	if output.String() != "\r\x1b[2K\x1b[?25h" {
		t.Fatalf("expected restored cursor, got %q", output.String())
	}
}

func failingShineUI(writeErr error) (*failFirstWriter, *dx.UI) {
	output := newFailFirstWriter(writeErr)
	timing := dx.Timing{FrameInterval: time.Millisecond, ShineDuration: time.Millisecond}
	capabilities := dx.Capabilities{ErrorTTY: true}
	config := dx.Config{
		Err: output, Capabilities: &capabilities, LookupEnv: emptyEnv,
		Clock: newManualClock(), Timing: &timing,
	}
	return output, dx.New(config)
}

type failFirstWriter struct {
	err        error
	calls      int
	firstWrite chan struct{}
	bytes.Buffer
}

func newFailFirstWriter(writeErr error) *failFirstWriter {
	return &failFirstWriter{err: writeErr, firstWrite: make(chan struct{})}
}

func (w *failFirstWriter) Write(value []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		close(w.firstWrite)
		return 0, w.err
	}
	return w.Buffer.Write(value)
}

func shineUI() (*lockedBuffer, *manualClock, *dx.UI) {
	output := newLockedBuffer()
	clock := newManualClock()
	timing := dx.Timing{FrameInterval: 50 * time.Millisecond, ShineDuration: 200 * time.Millisecond}
	return output, clock, animationUI(output, clock, &timing)
}

func startShine(ui *dx.UI) <-chan error {
	return startShineWithContext(context.Background(), ui)
}

func startShineWithContext(ctx context.Context, ui *dx.UI) <-chan error {
	finished := make(chan error, 1)
	go func() {
		finished <- ui.Shine(ctx, "Done")
	}()
	return finished
}
