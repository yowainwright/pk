package config

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	CPUThreshold    float64
	MemoryThreshold uint64
	Interval        time.Duration
	GracePeriod     time.Duration
	StaleLimit      time.Duration
	Protected       []string
	SessionSince    int64
	store           *Store
	baseProtected   []string
	preferencesPath string
}

// StateDir pins daemon lifecycle and audit data alongside explicit preferences.
func (c *Config) StateDir() string {
	if c.preferencesPath == "" {
		return ""
	}
	return filepath.Dir(c.preferencesPath)
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

func Default() (*Config, error) {
	cfg := &Config{
		CPUThreshold:    80,
		MemoryThreshold: 8192,
		Interval:        3 * time.Second,
		GracePeriod:     30 * time.Second,
		StaleLimit:      0,
	}
	return finishConfig(cfg, "")
}

func ParseArgsWithOutput(
	name string,
	args []string,
	output io.Writer,
	registerExtra func(*flag.FlagSet),
) (*Config, error) {
	cfg, flags, protectedStr := configFlags(name, output, registerExtra)
	if err := parseFlags(name, args, flags); err != nil {
		return nil, err
	}
	return finishSavedConfig(cfg, *protectedStr)
}

func finishSavedConfig(cfg *Config, protected string) (*Config, error) {
	if _, err := finishConfig(cfg, protected); err != nil {
		return nil, err
	}
	store, err := preferencesStore(cfg.preferencesPath)
	if err != nil {
		return nil, err
	}
	return cfg, cfg.UseStore(store)
}

func preferencesStore(path string) (*Store, error) {
	if path != "" {
		return NewStore(path), nil
	}
	return DefaultStore()
}

func (c *Config) UseStore(store *Store) error {
	c.store = store
	c.baseProtected = append([]string{}, c.Protected...)
	return c.Reload()
}

func (c *Config) Reload() error {
	if c.store == nil {
		return nil
	}
	names, err := c.store.Names()
	if err != nil {
		return fmt.Errorf("loading saved ignores: %w", err)
	}
	base := append([]string{}, c.baseProtected...)
	c.Protected = append(base, names...)
	return nil
}

func configFlags(
	name string,
	output io.Writer,
	registerExtra func(*flag.FlagSet),
) (*Config, *flag.FlagSet, *string) {
	cfg := &Config{}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	protectedStr := registerFlags(flags, cfg)
	registerDaemonFlags(name, flags, cfg)
	if registerExtra != nil {
		registerExtra(flags)
	}
	return cfg, flags, protectedStr
}

func registerDaemonFlags(name string, flags *flag.FlagSet, cfg *Config) {
	if name != "__daemon" {
		return
	}
	flags.StringVar(&cfg.preferencesPath, "config", "", "Saved preferences path")
	flags.Int64Var(&cfg.SessionSince, "since", 0, "Earliest shell creation time in milliseconds")
}

func parseFlags(name string, args []string, flags *flag.FlagSet) error {
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing %s flags: %w", name, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s does not accept positional arguments: %q", name, flags.Args())
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
	flags.DurationVar(&cfg.StaleLimit, "stale", 0, "Inactive agent session age before kill")
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
	if cfg.SessionSince < 0 {
		return fmt.Errorf("session start time must not be negative")
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}
	if cfg.GracePeriod < 0 {
		return fmt.Errorf("grace period must not be negative")
	}
	if cfg.StaleLimit < 0 {
		return fmt.Errorf("stale limit must not be negative")
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
