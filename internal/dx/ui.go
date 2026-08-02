package dx

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

func ParseColorMode(value string) (ColorMode, error) {
	mode := ColorMode(value)
	switch mode {
	case ColorAuto, ColorAlways, ColorNever:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid color mode %q: use auto, always, or never", value)
	}
}

type Kind uint8

const (
	Accent Kind = iota
	Success
	Warning
	Failure
	Muted
	Gold
)

type Capabilities struct {
	InputTTY  bool
	OutputTTY bool
	ErrorTTY  bool
}

type Config struct {
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
	Color        ColorMode
	Capabilities *Capabilities
	LookupEnv    func(string) (string, bool)
	Clock        Clock
	Timing       *Timing
	Timestamps   bool
}

type UI struct {
	in           *bufio.Reader
	out          io.Writer
	err          io.Writer
	color        bool
	rich         bool
	capabilities Capabilities
	lookupEnv    func(string) (string, bool)
	clock        Clock
	timing       Timing
	timestamps   bool
	mu           sync.Mutex
}

func New(config Config) *UI {
	config = withDefaults(config)
	capabilities := detectCapabilities(config)
	return &UI{
		in:           bufio.NewReader(config.In),
		out:          config.Out,
		err:          config.Err,
		color:        colorEnabled(config, capabilities),
		rich:         richTerminal(config.LookupEnv, capabilities),
		capabilities: capabilities,
		lookupEnv:    config.LookupEnv,
		clock:        config.Clock,
		timing:       *config.Timing,
		timestamps:   config.Timestamps,
	}
}

func (u *UI) Status(kind Kind, message string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprintln(u.err, formatStatus(u.color, kind, message))
	return err
}

func (u *UI) Text(kind Kind, text string) string {
	return style(u.color, kind, text)
}

func (u *UI) Value(value any) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprintln(u.out, value)
	return err
}

func withDefaults(config Config) Config {
	config = withDefaultStreams(config)
	return withDefaultRuntime(config)
}

func withDefaultStreams(config Config) Config {
	if config.In == nil {
		config.In = os.Stdin
	}
	if config.Out == nil {
		config.Out = os.Stdout
	}
	if config.Err == nil {
		config.Err = os.Stderr
	}
	return config
}

func withDefaultRuntime(config Config) Config {
	if config.Color == "" {
		config.Color = ColorAuto
	}
	if config.LookupEnv == nil {
		config.LookupEnv = os.LookupEnv
	}
	if config.Clock == nil {
		config.Clock = systemClock{}
	}
	if config.Timing == nil {
		timing := DefaultTiming()
		config.Timing = &timing
	}
	return config
}

func detectCapabilities(config Config) Capabilities {
	if config.Capabilities != nil {
		return *config.Capabilities
	}
	return Capabilities{
		InputTTY:  isTerminal(config.In),
		OutputTTY: isTerminal(config.Out),
		ErrorTTY:  isTerminal(config.Err),
	}
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func colorEnabled(config Config, capabilities Capabilities) bool {
	if config.Color == ColorAlways {
		return true
	}
	if config.Color == ColorNever {
		return false
	}
	return richTerminal(config.LookupEnv, capabilities)
}

func richTerminal(lookupEnv func(string) (string, bool), capabilities Capabilities) bool {
	if !capabilities.ErrorTTY {
		return false
	}
	if _, disabled := lookupEnv("NO_COLOR"); disabled {
		return false
	}
	return terminalEnvironment(lookupEnv)
}

func terminalEnvironment(lookupEnv func(string) (string, bool)) bool {
	if environmentEnabled(lookupEnv, "CI") {
		return false
	}
	term, _ := lookupEnv("TERM")
	return !strings.EqualFold(term, "dumb")
}

func environmentEnabled(lookupEnv func(string) (string, bool), name string) bool {
	value, ok := lookupEnv(name)
	if !ok {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	isEmpty := normalized == ""
	isZero := normalized == "0"
	isFalse := normalized == "false"
	isDisabled := isEmpty || isZero
	isDisabled = isDisabled || isFalse
	return !isDisabled
}

func formatStatus(color bool, kind Kind, message string) string {
	label := style(color, kind, statusLabel(kind))
	return fmt.Sprintf("%s %s", label, message)
}

func style(enabled bool, kind Kind, text string) string {
	if !enabled {
		return text
	}
	return fmt.Sprintf("%s%s%s", ansiColor(kind), text, ansiReset)
}

func statusLabel(kind Kind) string {
	switch kind {
	case Success:
		return "OK"
	case Warning:
		return "WARN"
	case Failure:
		return "ERR"
	case Muted:
		return "--"
	case Gold:
		return "**"
	default:
		return "::"
	}
}

func ansiColor(kind Kind) string {
	switch kind {
	case Success:
		return "\x1b[32m"
	case Warning:
		return "\x1b[33m"
	case Failure:
		return "\x1b[31m"
	case Muted:
		return "\x1b[90m"
	case Gold:
		return "\x1b[38;5;220m"
	default:
		return "\x1b[36m"
	}
}

const ansiReset = "\x1b[0m"
