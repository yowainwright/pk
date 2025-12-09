package config

import (
	"flag"
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
	"pk",
}

func Parse() *Config {
	cfg := &Config{}

	var protectedStr string

	flag.Float64Var(&cfg.CPUThreshold, "cpu", 80, "CPU percentage threshold")
	flag.Uint64Var(&cfg.MemoryThreshold, "mem", 8192, "Memory threshold in MB")
	flag.DurationVar(&cfg.Interval, "interval", 3*time.Second, "Check interval")
	flag.DurationVar(&cfg.GracePeriod, "grace", 30*time.Second, "Grace period before kill")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Log actions without killing")
	flag.StringVar(&protectedStr, "protected", "", "Comma-separated process names to protect")

	flag.Parse()

	cfg.Protected = defaultProtected
	if protectedStr != "" {
		for _, p := range strings.Split(protectedStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.Protected = append(cfg.Protected, p)
			}
		}
	}

	return cfg
}

func (c *Config) IsProtected(name string) bool {
	for _, p := range c.Protected {
		if p == name {
			return true
		}
	}
	return false
}
