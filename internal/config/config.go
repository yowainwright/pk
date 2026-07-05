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

const defaultDryRun = false

func ParseArgs(name string, args []string) (*Config, error) {
	return ParseArgsWith(name, args, nil)
}

func ParseArgsWith(
	name string,
	args []string,
	registerExtra func(*flag.FlagSet),
) (*Config, error) {
	cfg, flags, protectedStr := configFlags(name, registerExtra)
	if err := parseFlags(name, args, flags); err != nil {
		return nil, err
	}
	return finishConfig(cfg, *protectedStr)
}

func configFlags(
	name string,
	registerExtra func(*flag.FlagSet),
) (*Config, *flag.FlagSet, *string) {
	cfg := &Config{}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	protectedStr := registerFlags(flags, cfg)
	if registerExtra != nil {
		registerExtra(flags)
	}
	return cfg, flags, protectedStr
}

func parseFlags(name string, args []string, flags *flag.FlagSet) error {
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing %s flags: %w", name, err)
	}
	return nil
}

func finishConfig(cfg *Config, protectedStr string) (*Config, error) {
	cfg.Protected = protectedNames(protectedStr)
	if err := validate(cfg); err != nil {
		return nil, err
	}
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

func validate(cfg *Config) error {
	if cfg.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}
	if cfg.GracePeriod < 0 {
		return fmt.Errorf("grace period must not be negative")
	}
	return nil
}

func (c *Config) IsProtected(name string) bool {
	for _, p := range c.Protected {
		if p == name {
			return true
		}
	}
	return false
}
