package dx

import (
	"context"
	"fmt"
)

type Action func(context.Context) error

func (u *UI) Task(ctx context.Context, label string, action Action) error {
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
	case err := <-result:
		return u.completeTask(ctx, label, err)
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C():
		return u.animateTask(ctx, label, result)
	}
}

func (u *UI) animateTask(ctx context.Context, label string, result <-chan error) error {
	if err := u.startLoader(label); err != nil {
		return u.waitAfterOutputError(ctx, result, err)
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

type taskEvent struct {
	kind taskEventKind
	err  error
}

type taskEventKind uint8

const (
	taskFinished taskEventKind = iota
	taskCanceled
	taskTicked
)

func (u *UI) runLoader(task runningTask) error {
	for {
		event := nextTaskEvent(task)
		switch event.kind {
		case taskFinished:
			return u.finishTask(task, event.err)
		case taskCanceled:
			return u.cancelTask(task.ctx)
		case taskTicked:
			if err := u.advanceLoader(&task); err != nil {
				return err
			}
		}
	}
}

func nextTaskEvent(task runningTask) taskEvent {
	select {
	case err := <-task.result:
		return taskEvent{kind: taskFinished, err: err}
	case <-task.ctx.Done():
		return taskEvent{kind: taskCanceled}
	case <-task.ticker.C():
		return taskEvent{kind: taskTicked}
	}
}

func (u *UI) finishTask(task runningTask, actionErr error) error {
	u.stopLoader()
	return u.completeTask(task.ctx, task.label, actionErr)
}

func (u *UI) cancelTask(ctx context.Context) error {
	u.stopLoader()
	return ctx.Err()
}

func (u *UI) advanceLoader(task *runningTask) error {
	err := u.writeLoader(task.label, task.frame)
	if err != nil {
		u.stopLoader()
		return u.waitAfterOutputError(task.ctx, task.result, err)
	}
	task.frame++
	return nil
}

func (u *UI) startLoader(label string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, ansiHideCursor+loaderFrame(u.color, label, 0))
	return err
}

func (u *UI) writeLoader(label string, frame int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprint(u.err, loaderFrame(u.color, label, frame))
	return err
}

func (u *UI) stopLoader() {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, _ = fmt.Fprint(u.err, ansiClearLine+ansiShowCursor)
}

func (u *UI) completeTask(ctx context.Context, label string, actionErr error) error {
	if actionErr != nil {
		_ = u.Status(Failure, label)
		return actionErr
	}
	return u.Shine(ctx, label)
}

func (u *UI) waitAfterOutputError(
	ctx context.Context,
	result <-chan error,
	outputErr error,
) error {
	select {
	case actionErr := <-result:
		if actionErr != nil {
			return actionErr
		}
		return outputErr
	case <-ctx.Done():
		return ctx.Err()
	}
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
