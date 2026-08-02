package dx_test

import (
	"context"
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

func shineUI() (*lockedBuffer, *manualClock, *dx.UI) {
	output := newLockedBuffer()
	clock := newManualClock()
	timing := dx.Timing{FrameInterval: 50 * time.Millisecond, ShineDuration: 200 * time.Millisecond}
	return output, clock, animationUI(output, clock, &timing)
}

func startShine(ui *dx.UI) <-chan error {
	finished := make(chan error, 1)
	go func() {
		finished <- ui.Shine(context.Background(), "Done")
	}()
	return finished
}
