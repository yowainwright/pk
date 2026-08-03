package dx

import (
	"context"
	"fmt"
	"time"
)

func (u *UI) Shine(ctx context.Context, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	notRich := !u.rich
	noDuration := u.timing.ShineDuration <= 0
	useStaticOutput := notRich || noDuration
	if useStaticOutput {
		return u.Status(Gold, label)
	}
	return u.animateShine(ctx, label)
}

func (u *UI) animateShine(ctx context.Context, label string) error {
	duration := u.clock.NewTimer(u.timing.ShineDuration)
	defer duration.Stop()
	startErr := u.startShine(label)
	if startErr != nil {
		restoreErr := u.restoreTerminal()
		return firstError(startErr, restoreErr)
	}
	ticker := u.clock.NewTicker(u.frameInterval())
	defer ticker.Stop()
	return u.runShine(ctx, label, duration, ticker)
}

func (u *UI) runShine(ctx context.Context, label string, duration Timer, ticker Ticker) error {
	frame := 1
	for {
		event := nextShineEvent(ctx, duration, ticker)
		if event == shineCanceled {
			return u.cancelShine(ctx)
		}
		if event == shineFinished {
			return u.completeShine(label)
		}
		if err := u.updateShine(label, frame); err != nil {
			return err
		}
		frame++
	}
}

func (u *UI) cancelShine(ctx context.Context) error {
	restoreErr := u.restoreTerminal()
	return firstError(ctx.Err(), restoreErr)
}

func (u *UI) completeShine(label string) error {
	restoreErr := u.restoreTerminal()
	statusErr := u.Status(Gold, label)
	return firstError(restoreErr, statusErr)
}

func (u *UI) updateShine(label string, frame int) error {
	err := u.writeShine(label, frame)
	if err == nil {
		return nil
	}
	restoreErr := u.restoreTerminal()
	return firstError(err, restoreErr)
}

type shineEvent uint8

const (
	shineCanceled shineEvent = iota
	shineFinished
	shineTicked
)

func nextShineEvent(ctx context.Context, duration Timer, ticker Ticker) shineEvent {
	select {
	case <-ctx.Done():
		return shineCanceled
	case <-duration.C():
		return shineFinished
	case <-ticker.C():
		return shineTicked
	}
}

func (u *UI) startShine(label string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, ansiHideCursor+shineFrame(u.color, label, 0))
	if err != nil {
		return fmt.Errorf("starting shine: %w", err)
	}
	return nil
}

func (u *UI) writeShine(label string, frame int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, shineFrame(u.color, label, frame))
	if err != nil {
		return fmt.Errorf("updating shine: %w", err)
	}
	return nil
}

func (u *UI) frameInterval() time.Duration {
	if u.timing.FrameInterval > 0 {
		return u.timing.FrameInterval
	}
	return DefaultTiming().FrameInterval
}

func shineFrame(color bool, label string, frame int) string {
	markers := [...]string{"*", "+", "."}
	marker := style(color, Gold, markers[frame%len(markers)])
	text := shimmer(color, label, frame)
	return fmt.Sprintf("%s%s %s %s", ansiClearLine, marker, text, marker)
}

func shimmer(color bool, label string, frame int) string {
	letters := []rune(label)
	colorDisabled := !color
	labelEmpty := len(letters) == 0
	usePlainText := colorDisabled || labelEmpty
	if usePlainText {
		return label
	}
	index := frame % len(letters)
	before := string(letters[:index])
	colorEnabled := true
	highlight := style(colorEnabled, Gold, string(letters[index]))
	after := string(letters[index+1:])
	prefix := before + highlight
	return prefix + after
}
