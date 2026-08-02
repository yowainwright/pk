package dx_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/dx"
)

func TestTaskReturnsActionErrorUnchangedWithoutTTY(t *testing.T) {
	var output bytes.Buffer
	expected := errors.New("action failed")
	called := false
	ui := quietUI(&output)
	action := func(context.Context) error {
		called = true
		return expected
	}
	err := ui.Task(context.Background(), "Running action", action)
	if !called {
		t.Fatal("expected action to run")
	}
	if err != expected {
		t.Fatalf("expected original error, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("expected quiet non-interactive task, got %q", output.String())
	}
}

func TestTaskDelaysLoaderUntilThreshold(t *testing.T) {
	output, clock, ui := delayedTaskUI()
	release := make(chan struct{})
	finished := startBlockedTask(ui, release)
	delay := clock.nextTimer(t)
	if delay.duration != 200*time.Millisecond {
		t.Fatalf("expected loader delay, got %s", delay.duration)
	}
	assertOutput(t, output, "")
	delay.timer.fire()
	clock.nextTicker(t)
	close(release)
	if err := <-finished; err != nil {
		t.Fatalf("run task: %v", err)
	}
	expected := "\x1b[?25l\r\x1b[2K| Running action\r\x1b[2K\x1b[?25h** Running action\n"
	assertOutput(t, output, expected)
}

func TestTaskRestoresCursorWhenCanceled(t *testing.T) {
	output, clock, ui := delayedTaskUI()
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	finished := startCancelableTask(ctx, ui, release)
	delay := clock.nextTimer(t)
	delay.timer.fire()
	clock.nextTicker(t)
	cancel()
	err := waitForTask(t, finished)
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled task, got %v", err)
	}
	if !strings.Contains(output.String(), "\x1b[?25h") {
		t.Fatalf("expected restored cursor, got %q", output.String())
	}
}

func quietUI(output *bytes.Buffer) *dx.UI {
	return dx.New(dx.Config{
		Err:          output,
		Capabilities: &dx.Capabilities{},
		LookupEnv:    emptyEnv,
	})
}

func delayedTaskUI() (*lockedBuffer, *manualClock, *dx.UI) {
	output := newLockedBuffer()
	clock := newManualClock()
	timing := dx.Timing{LoaderDelay: 200 * time.Millisecond, FrameInterval: 50 * time.Millisecond}
	return output, clock, animationUI(output, clock, &timing)
}

func startBlockedTask(ui *dx.UI, release <-chan struct{}) <-chan error {
	finished := make(chan error, 1)
	go func() {
		finished <- ui.Task(context.Background(), "Running action", func(context.Context) error {
			<-release
			return nil
		})
	}()
	return finished
}

func startCancelableTask(ctx context.Context, ui *dx.UI, release <-chan struct{}) <-chan error {
	finished := make(chan error, 1)
	go func() {
		finished <- ui.Task(ctx, "Running action", func(context.Context) error {
			<-release
			return nil
		})
	}()
	return finished
}

func waitForTask(t *testing.T, finished <-chan error) error {
	t.Helper()
	select {
	case err := <-finished:
		return err
	case <-time.After(time.Second):
		t.Fatal("expected task to finish")
		return nil
	}
}

func assertOutput(t *testing.T, output *lockedBuffer, expected string) {
	t.Helper()
	if output.String() != expected {
		t.Fatalf("expected %q, got %q", expected, output.String())
	}
}

func waitForOutput(t *testing.T, output *lockedBuffer, expected string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !strings.Contains(output.String(), expected) {
		select {
		case <-output.writes:
		case <-deadline.C:
			t.Fatalf("expected %q in %q", expected, output.String())
		}
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
	writes chan struct{}
}

func newLockedBuffer() *lockedBuffer {
	return &lockedBuffer{writes: make(chan struct{}, 16)}
}

func animationUI(output *lockedBuffer, clock *manualClock, timing *dx.Timing) *dx.UI {
	capabilities := dx.Capabilities{ErrorTTY: true}
	return dx.New(dx.Config{
		Err:          output,
		Color:        dx.ColorNever,
		Capabilities: &capabilities,
		LookupEnv:    emptyEnv,
		Clock:        clock,
		Timing:       timing,
	})
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	written, err := b.Buffer.Write(value)
	b.mu.Unlock()
	select {
	case b.writes <- struct{}{}:
	default:
	}
	return written, err
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

type timerRequest struct {
	duration time.Duration
	timer    *manualTimer
}

type tickerRequest struct {
	duration time.Duration
	ticker   *manualTicker
}

type manualClock struct {
	timers  chan timerRequest
	tickers chan tickerRequest
}

func newManualClock() *manualClock {
	return &manualClock{
		timers:  make(chan timerRequest, 4),
		tickers: make(chan tickerRequest, 4),
	}
}

func (c *manualClock) NewTimer(duration time.Duration) dx.Timer {
	timer := &manualTimer{ticks: make(chan time.Time, 1)}
	c.timers <- timerRequest{duration: duration, timer: timer}
	return timer
}

func (c *manualClock) NewTicker(duration time.Duration) dx.Ticker {
	ticker := &manualTicker{ticks: make(chan time.Time, 1)}
	c.tickers <- tickerRequest{duration: duration, ticker: ticker}
	return ticker
}

func (c *manualClock) nextTimer(t *testing.T) timerRequest {
	t.Helper()
	select {
	case request := <-c.timers:
		return request
	case <-time.After(time.Second):
		t.Fatal("expected timer")
		return timerRequest{}
	}
}

func (c *manualClock) nextTicker(t *testing.T) tickerRequest {
	t.Helper()
	select {
	case request := <-c.tickers:
		return request
	case <-time.After(time.Second):
		t.Fatal("expected ticker")
		return tickerRequest{}
	}
}

type manualTimer struct {
	ticks chan time.Time
}

func (t *manualTimer) C() <-chan time.Time {
	return t.ticks
}

func (t *manualTimer) Stop() {}

func (t *manualTimer) fire() {
	t.ticks <- time.Now()
}

type manualTicker struct {
	ticks chan time.Time
}

func (t *manualTicker) C() <-chan time.Time {
	return t.ticks
}

func (t *manualTicker) Stop() {}

func (t *manualTicker) fire() {
	t.ticks <- time.Now()
}
