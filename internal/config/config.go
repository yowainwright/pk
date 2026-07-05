package config

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	CPUThreshold    float64
	MemoryThreshold uint64
	Interval        time.Duration
	GracePeriod     time.Duration
	DryRun          bool
	Protected       []string
}

var defaultProtected = []string{
	"kernel_task",
	"launchd",
	"WindowServer",
	"loginwindow",
	"Finder",
	"Terminal",
	"iTerm2",
	"Ghostty",
	"Code",
	"Code Helper",
	"Code Helper (Plugin)",
	"Code Helper (Renderer)",
	"Cursor",
	"Cursor Helper",
	"Cursor Helper (Plugin)",
	"Cursor Helper (Renderer)",
	"Zed",
	"tmux",
	"bash",
	"zsh",
	"fish",
	"codex",
	"claude",
	"pk",
}

const defaultDryRun = true

func ParseArgs(name string, args []string) (*Config, error) {
	return ParseArgsWith(name, args, nil)
}

func ParseArgsWith(
	name string,
	args []string,
	registerExtra func(*flag.FlagSet),
) (*Config, error) {
	cfg := &Config{}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	protectedStr := registerFlags(flags, cfg)
	if registerExtra != nil {
		registerExtra(flags)
	}

	if err := flags.Parse(args); err != nil {
		return nil, fmt.Errorf("parsing %s flags: %w", name, err)
	}

	cfg.Protected = protectedNames(*protectedStr)
	return cfg, nil
}

func registerFlags(flags *flag.FlagSet, cfg *Config) *string {
	var protectedStr string
	flags.Float64Var(&cfg.CPUThreshold, "cpu", 80, "CPU percentage threshold")
	flags.Uint64Var(&cfg.MemoryThreshold, "mem", 8192, "Memory threshold in MB")
	flags.DurationVar(&cfg.Interval, "interval", 3*time.Second, "Check interval")
	flags.DurationVar(&cfg.GracePeriod, "grace", 30*time.Second, "Grace period before kill")
	flags.BoolVar(&cfg.DryRun, "dry-run", defaultDryRun, "Log actions without killing")
	flags.StringVar(&protectedStr, "protected", "", "Comma-separated process names to protect")
	return &protectedStr
}

func protectedNames(protectedStr string) []string {
	protected := append([]string{}, defaultProtected...)
	if protectedStr != "" {
		for _, p := range strings.Split(protectedStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				protected = append(protected, p)
			}
		}
	}

	return protected
}

func (c *Config) IsProtected(name string) bool {
	for _, p := range c.Protected {
		if p == name {
			return true
		}
	}
	return false
}
