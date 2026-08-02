package dx_test

import (
	"testing"
	"time"

	"github.com/yowainwright/pk/internal/dx"
)

func TestDefaultTiming(t *testing.T) {
	timing := dx.DefaultTiming()

	if timing.LoaderDelay != 200*time.Millisecond {
		t.Fatalf("unexpected loader delay %s", timing.LoaderDelay)
	}
	if timing.FrameInterval != 80*time.Millisecond {
		t.Fatalf("unexpected frame interval %s", timing.FrameInterval)
	}
	if timing.ShineDuration != 240*time.Millisecond {
		t.Fatalf("unexpected shine duration %s", timing.ShineDuration)
	}
}
