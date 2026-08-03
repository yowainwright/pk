package dx_test

import (
	"bytes"
	"testing"

	"github.com/yowainwright/pk/internal/dx"
)

func TestLoggerWritesStructuredHumanOutput(t *testing.T) {
	var output bytes.Buffer
	ui := dx.New(dx.Config{
		Err:          &output,
		Color:        dx.ColorNever,
		Capabilities: &dx.Capabilities{},
		LookupEnv:    emptyEnv,
	})

	apply := false
	ui.Logger().Info("Monitoring started", "cpu", 90, "apply", apply)

	expected := "INF Monitoring started cpu=90 apply=false\n"
	if output.String() != expected {
		t.Fatalf("expected %q, got %q", expected, output.String())
	}
}
