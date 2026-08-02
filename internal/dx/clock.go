package dx

import "time"

type Timing struct {
	LoaderDelay   time.Duration
	FrameInterval time.Duration
	ShineDuration time.Duration
}

func DefaultTiming() Timing {
	loaderDelay := 200 * time.Millisecond
	frameInterval := 80 * time.Millisecond
	shineDuration := 240 * time.Millisecond
	return Timing{
		LoaderDelay:   loaderDelay,
		FrameInterval: frameInterval,
		ShineDuration: shineDuration,
	}
}

type Clock interface {
	NewTimer(time.Duration) Timer
	NewTicker(time.Duration) Ticker
}

type Timer interface {
	C() <-chan time.Time
	Stop()
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type systemClock struct{}

func (systemClock) NewTimer(duration time.Duration) Timer {
	return systemTimer{Timer: time.NewTimer(duration)}
}

func (systemClock) NewTicker(duration time.Duration) Ticker {
	return systemTicker{Ticker: time.NewTicker(duration)}
}

type systemTimer struct {
	*time.Timer
}

func (t systemTimer) C() <-chan time.Time {
	return t.Timer.C
}

func (t systemTimer) Stop() {
	t.Timer.Stop()
}

type systemTicker struct {
	*time.Ticker
}

func (t systemTicker) C() <-chan time.Time {
	return t.Ticker.C
}

func (t systemTicker) Stop() {
	t.Ticker.Stop()
}
