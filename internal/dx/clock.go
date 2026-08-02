package dx

import "time"

const (
	defaultLoaderDelay   = 200 * time.Millisecond
	defaultFrameInterval = 80 * time.Millisecond
	defaultShineDuration = 240 * time.Millisecond
)

type Timing struct {
	LoaderDelay   time.Duration
	FrameInterval time.Duration
	ShineDuration time.Duration
}

func DefaultTiming() Timing {
	return Timing{
		LoaderDelay:   defaultLoaderDelay,
		FrameInterval: defaultFrameInterval,
		ShineDuration: defaultShineDuration,
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
