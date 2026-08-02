package dx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNotInteractive = errors.New("prompt requires an interactive terminal")

type Prompt struct {
	Label    string
	Default  string
	Hint     string
	Validate func(string) error
}

type Confirmation struct {
	Label   string
	Default bool
}

func (u *UI) Confirm(ctx context.Context, confirmation Confirmation) (bool, error) {
	answer, err := u.Ask(ctx, confirmationPrompt(confirmation))
	if err != nil {
		return false, err
	}
	return isYes(answer), nil
}

func confirmationPrompt(confirmation Confirmation) Prompt {
	defaultAnswer := "no"
	hint := "y/N"
	if confirmation.Default {
		defaultAnswer = "yes"
		hint = "Y/n"
	}
	return Prompt{
		Label:    confirmation.Label,
		Default:  defaultAnswer,
		Hint:     hint,
		Validate: validateConfirmation,
	}
}

func validateConfirmation(answer string) error {
	value := strings.ToLower(answer)
	isShortNo := value == "n"
	isLongNo := value == "no"
	isNo := isShortNo || isLongNo
	isConfirmation := isYes(value) || isNo
	if isConfirmation {
		return nil
	}
	return fmt.Errorf("invalid confirmation %q: enter yes or no", answer)
}

func isYes(answer string) bool {
	value := strings.ToLower(answer)
	isShortYes := value == "y"
	isLongYes := value == "yes"
	return isShortYes || isLongYes
}

func (u *UI) Ask(ctx context.Context, prompt Prompt) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !u.canPrompt() {
		return "", ErrNotInteractive
	}
	return u.readPrompt(ctx, prompt)
}

func (u *UI) readPrompt(ctx context.Context, prompt Prompt) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, err := fmt.Fprint(u.err, formatPrompt(u.color, prompt)); err != nil {
		return "", fmt.Errorf("writing prompt: %w", err)
	}
	answer, err := u.in.ReadString('\n')
	if promptReadFailed(err, answer) {
		return "", fmt.Errorf("reading prompt: %w", err)
	}
	return resolveAnswer(ctx, prompt, answer)
}

func promptReadFailed(err error, answer string) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, io.EOF) {
		return true
	}
	return answer == ""
}

func (u *UI) canPrompt() bool {
	inputRedirected := !u.capabilities.InputTTY
	errorRedirected := !u.capabilities.ErrorTTY
	terminalRedirected := inputRedirected || errorRedirected
	if terminalRedirected {
		return false
	}
	return terminalEnvironment(u.lookupEnv)
}

func resolveAnswer(ctx context.Context, prompt Prompt, answer string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := strings.TrimSpace(answer)
	if value == "" {
		value = prompt.Default
	}
	if prompt.Validate != nil {
		return value, prompt.Validate(value)
	}
	return value, nil
}

func formatPrompt(color bool, prompt Prompt) string {
	marker := style(color, Accent, "?")
	hint := prompt.Hint
	hintMissing := hint == ""
	hasDefault := prompt.Default != ""
	useDefault := hintMissing && hasDefault
	if useDefault {
		hint = prompt.Default
	}
	if hint != "" {
		hint = " " + style(color, Muted, "["+hint+"]")
	}
	pointer := style(color, Accent, ">")
	return fmt.Sprintf("%s %s%s %s ", marker, prompt.Label, hint, pointer)
}
