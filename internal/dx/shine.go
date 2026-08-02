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
	if err := u.startShine(label); err != nil {
		return err
	}
	ticker := u.clock.NewTicker(u.frameInterval())
	defer ticker.Stop()
	return u.runShine(ctx, label, duration, ticker)
}

func (u *UI) runShine(ctx context.Context, label string, duration Timer, ticker Ticker) error {
	frame := 1
	for {
		event := nextShineEvent(ctx, duration, ticker)
		switch event {
		case shineCanceled:
			u.stopLoader()
			return ctx.Err()
		case shineFinished:
			u.stopLoader()
			return u.Status(Gold, label)
		case shineTicked:
			if err := u.writeShine(label, frame); err != nil {
				u.stopLoader()
				return err
			}
			frame++
		}
	}
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
	return err
}

func (u *UI) writeShine(label string, frame int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, shineFrame(u.color, label, frame))
	return err
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
