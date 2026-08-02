package dx_test

import (
	"bytes"
	"testing"

	"github.com/yowainwright/pk/internal/dx"
)

func TestStatusUsesSemanticColorOnTTY(t *testing.T) {
	var output bytes.Buffer
	ui := dx.New(dx.Config{
		Err:          &output,
		Color:        dx.ColorAuto,
		Capabilities: &dx.Capabilities{ErrorTTY: true},
		LookupEnv:    emptyEnv,
	})

	err := ui.Status(dx.Success, "ready")
	if err != nil {
		t.Fatalf("write status: %v", err)
	}
	if output.String() != "\x1b[32mOK\x1b[0m ready\n" {
		t.Fatalf("unexpected status %q", output.String())
	}
}

func TestTextComposesSemanticColorWithoutWriting(t *testing.T) {
	var output bytes.Buffer
	ui := dx.New(dx.Config{Err: &output, Color: dx.ColorAlways, LookupEnv: emptyEnv})

	text := ui.Text(dx.Accent, "path")

	if text != "\x1b[36mpath\x1b[0m" {
		t.Fatalf("unexpected text %q", text)
	}
	if output.Len() != 0 {
		t.Fatalf("expected pure formatting, got %q", output.String())
	}
}

func TestValueRemainsStableWhenColorIsForced(t *testing.T) {
	var output bytes.Buffer
	ui := dx.New(dx.Config{Out: &output, Color: dx.ColorAlways, LookupEnv: emptyEnv})

	if err := ui.Value("active"); err != nil {
		t.Fatalf("write value: %v", err)
	}
	if output.String() != "active\n" {
		t.Fatalf("unexpected value %q", output.String())
	}
}

func TestAutomaticColorRespectsTerminalEnvironment(t *testing.T) {
	for _, current := range automaticColorCases() {
		t.Run(current.name, func(t *testing.T) { testAutomaticColor(t, current) })
	}
}

type automaticColorCase struct {
	name         string
	capabilities dx.Capabilities
	environment  map[string]string
}

func automaticColorCases() []automaticColorCase {
	return []automaticColorCase{
		{name: "pipe"},
		{
			name:         "no color",
			capabilities: dx.Capabilities{ErrorTTY: true},
			environment:  map[string]string{"NO_COLOR": "1"},
		},
		{
			name:         "ci",
			capabilities: dx.Capabilities{ErrorTTY: true},
			environment:  map[string]string{"CI": "true"},
		},
		{
			name:         "dumb terminal",
			capabilities: dx.Capabilities{ErrorTTY: true},
			environment:  map[string]string{"TERM": "dumb"},
		},
	}
}

func testAutomaticColor(t *testing.T, current automaticColorCase) {
	t.Helper()
	var output bytes.Buffer
	ui := dx.New(dx.Config{
		Err:          &output,
		Color:        dx.ColorAuto,
		Capabilities: &current.capabilities,
		LookupEnv:    environment(current.environment),
	})
	if err := ui.Status(dx.Warning, "careful"); err != nil {
		t.Fatalf("write status: %v", err)
	}
	if output.String() != "WARN careful\n" {
		t.Fatalf("unexpected status %q", output.String())
	}
}

func TestParseColorMode(t *testing.T) {
	tests := []struct {
		value    string
		expected dx.ColorMode
		wantErr  bool
	}{
		{value: "auto", expected: dx.ColorAuto},
		{value: "always", expected: dx.ColorAlways},
		{value: "never", expected: dx.ColorNever},
		{value: "sometimes", wantErr: true},
	}
	for _, current := range tests {
		t.Run(current.value, func(t *testing.T) {
			mode, err := dx.ParseColorMode(current.value)
			expectedErrorMissing := current.wantErr && err == nil
			if expectedErrorMissing {
				t.Fatal("expected color mode error")
			}
			unexpectedError := !current.wantErr && err != nil
			if unexpectedError {
				t.Fatalf("parse color mode: %v", err)
			}
			if mode != current.expected {
				t.Fatalf("expected %q, got %q", current.expected, mode)
			}
		})
	}
}

func emptyEnv(string) (string, bool) {
	return "", false
}

func environment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
