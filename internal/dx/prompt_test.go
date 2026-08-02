package dx_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yowainwright/pk/internal/dx"
)

func TestAskReadsAnInteractivePrompt(t *testing.T) {
	var output bytes.Buffer
	ui := promptUI("Ada\n", &output)
	answer, err := ui.Ask(context.Background(), dx.Prompt{Label: "Name"})
	if err != nil {
		t.Fatalf("ask prompt: %v", err)
	}
	if answer != "Ada" {
		t.Fatalf("expected answer Ada, got %q", answer)
	}
	if output.String() != "? Name > " {
		t.Fatalf("unexpected prompt %q", output.String())
	}
}

func TestAskRejectsNonInteractiveInput(t *testing.T) {
	var output bytes.Buffer
	ui := dx.New(dx.Config{
		In:           strings.NewReader("yes\n"),
		Err:          &output,
		Capabilities: &dx.Capabilities{},
		LookupEnv:    emptyEnv,
	})

	_, err := ui.Ask(context.Background(), dx.Prompt{Label: "Continue"})
	if !errors.Is(err, dx.ErrNotInteractive) {
		t.Fatalf("expected non-interactive error, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("expected no prompt output, got %q", output.String())
	}
}

func TestConfirmRejectsInvalidAnswer(t *testing.T) {
	var output bytes.Buffer
	ui := promptUI("maybe\n", &output)

	_, err := ui.Confirm(context.Background(), dx.Confirmation{Label: "Continue"})
	if err == nil {
		t.Fatal("expected invalid confirmation error")
	}
	if !strings.Contains(err.Error(), "enter yes or no") {
		t.Fatalf("unexpected confirmation error %v", err)
	}
}

type confirmationCase struct {
	name         string
	input        string
	defaultValue bool
	expected     bool
	hint         string
}

func TestConfirmComposesPromptDefaults(t *testing.T) {
	tests := []confirmationCase{
		{name: "default yes", input: "\n", defaultValue: true, expected: true, hint: "Y/n"},
		{name: "explicit no", input: "no\n", defaultValue: true, expected: false, hint: "Y/n"},
		{name: "default no", input: "\n", expected: false, hint: "y/N"},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) { testConfirmation(t, current) })
	}
}

func testConfirmation(t *testing.T, current confirmationCase) {
	t.Helper()
	var output bytes.Buffer
	ui := promptUI(current.input, &output)
	confirmation := dx.Confirmation{Label: "Continue", Default: current.defaultValue}
	confirmed, err := ui.Confirm(context.Background(), confirmation)
	if err != nil {
		t.Fatalf("confirm prompt: %v", err)
	}
	if confirmed != current.expected {
		t.Fatalf("expected %t, got %t", current.expected, confirmed)
	}
	expectedPrompt := "? Continue [" + current.hint + "] > "
	if output.String() != expectedPrompt {
		t.Fatalf("expected prompt %q, got %q", expectedPrompt, output.String())
	}
}

func promptUI(input string, output *bytes.Buffer) *dx.UI {
	capabilities := dx.Capabilities{InputTTY: true, ErrorTTY: true}
	return dx.New(dx.Config{
		In:           strings.NewReader(input),
		Err:          output,
		Color:        dx.ColorNever,
		Capabilities: &capabilities,
		LookupEnv:    emptyEnv,
	})
}
