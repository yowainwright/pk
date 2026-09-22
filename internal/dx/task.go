package dx

import (
	"context"
	"errors"
	"fmt"
)

type Action func(context.Context) error

func (u *UI) Task(ctx context.Context, label string, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !u.rich {
		return action(ctx)
	}
	result := make(chan error, 1)
	go func() {
		result <- action(ctx)
	}()
	return u.waitForTask(ctx, label, result)
}

func (u *UI) waitForTask(ctx context.Context, label string, result <-chan error) error {
	timer := u.clock.NewTimer(u.timing.LoaderDelay)
	defer timer.Stop()
	select {
	case actionErr := <-result:
		statusErr := u.renderTaskCompletion(ctx, label, actionErr)
		return firstError(actionErr, statusErr)
	case <-ctx.Done():
		return errors.Join(ctx.Err(), <-result)
	case <-timer.C():
		return u.animateTask(ctx, label, result)
	}
}

func (u *UI) animateTask(ctx context.Context, label string, result <-chan error) error {
	startErr := u.startLoader(label)
	if startErr != nil {
		restoreErr := u.restoreTerminal()
		return u.waitAfterOutputError(ctx, result, firstError(startErr, restoreErr))
	}
	ticker := u.clock.NewTicker(u.frameInterval())
	defer ticker.Stop()
	task := runningTask{ctx: ctx, label: label, result: result, ticker: ticker, frame: 1}
	return u.runLoader(task)
}

type runningTask struct {
	ctx    context.Context
	label  string
	result <-chan error
	ticker Ticker
	frame  int
}

func (u *UI) runLoader(task runningTask) error {
	for {
		select {
		case err := <-task.result:
			return u.finishTask(task, err)
		case <-task.ctx.Done():
			return u.cancelTask(task)
		case <-task.ticker.C():
			if err := u.advanceLoader(&task); err != nil {
				return err
			}
		}
	}
}

func (u *UI) finishTask(task runningTask, actionErr error) error {
	restoreErr := u.restoreTerminal()
	statusErr := u.renderTaskCompletion(task.ctx, task.label, actionErr)
	return firstError(actionErr, restoreErr, statusErr)
}

func (u *UI) cancelTask(task runningTask) error {
	restoreErr := u.restoreTerminal()
	actionErr := <-task.result
	return errors.Join(task.ctx.Err(), actionErr, restoreErr)
}

func (u *UI) advanceLoader(task *runningTask) error {
	err := u.writeLoader(task.label, task.frame)
	if err != nil {
		restoreErr := u.restoreTerminal()
		outputErr := firstError(err, restoreErr)
		return u.waitAfterOutputError(task.ctx, task.result, outputErr)
	}
	task.frame++
	return nil
}

func (u *UI) startLoader(label string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, ansiHideCursor+loaderFrame(u.color, label, 0))
	if err != nil {
		return fmt.Errorf("starting loader: %w", err)
	}
	return nil
}

func (u *UI) writeLoader(label string, frame int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, loaderFrame(u.color, label, frame))
	if err != nil {
		return fmt.Errorf("updating loader: %w", err)
	}
	return nil
}

func (u *UI) restoreTerminal() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, ansiClearLine+ansiShowCursor)
	if err != nil {
		return fmt.Errorf("restoring terminal: %w", err)
	}
	return nil
}

func (u *UI) renderTaskCompletion(ctx context.Context, label string, actionErr error) error {
	if actionErr != nil {
		return u.Status(Failure, label)
	}
	return u.Shine(ctx, label)
}

func (u *UI) waitAfterOutputError(
	ctx context.Context,
	result <-chan error,
	outputErr error,
) error {
	actionErr := <-result
	return errors.Join(actionErr, ctx.Err(), outputErr)
}

func loaderFrame(color bool, label string, frame int) string {
	frames := [...]string{"|", "/", "-", "\\"}
	marker := frames[frame%len(frames)]
	styledMarker := style(color, Accent, marker)
	return fmt.Sprintf("%s%s %s", ansiClearLine, styledMarker, label)
}

const (
	ansiClearLine  = "\r\x1b[2K"
	ansiHideCursor = "\x1b[?25l"
	ansiShowCursor = "\x1b[?25h"
)
