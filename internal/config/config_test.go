package config

import (
	"flag"
	"testing"
	"time"
)

func TestParseArgsUsesSafeDefaults(t *testing.T) {
	cfg := mustParse(t)

	if cfg.CPUThreshold != 80 {
		t.Fatalf("expected default cpu threshold, got %f", cfg.CPUThreshold)
	}
	if cfg.MemoryThreshold != 8192 {
		t.Fatalf("expected default memory threshold, got %d", cfg.MemoryThreshold)
	}
	if cfg.Interval != 3*time.Second {
		t.Fatalf("expected default interval, got %s", cfg.Interval)
	}
	if !cfg.DryRun {
		t.Fatal("expected dry-run default")
	}
}

func TestParseArgsAddsProtectedNames(t *testing.T) {
	cfg := mustParse(t, "-protected", "postgres, redis")

	if !cfg.IsProtected("postgres") {
		t.Fatal("expected postgres to be protected")
	}
	if !cfg.IsProtected("redis") {
		t.Fatal("expected redis to be protected")
	}
}

func TestParseArgsWithRegistersExtraFlags(t *testing.T) {
	var apply bool
	defaultApply := false
	cfg, err := ParseArgsWith("test", testArgs("--apply"), func(flags *flag.FlagSet) {
		flags.BoolVar(&apply, "apply", defaultApply, "apply")
	})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
	if !apply {
		t.Fatal("expected apply flag")
	}
}

func TestParseArgsReturnsFlagErrors(t *testing.T) {
	_, err := ParseArgs("test", testArgs("-bad"))

	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseArgsRejectsNonPositiveIntervals(t *testing.T) {
	_, err := ParseArgs("test", testArgs("-interval", "0s"))

	if err == nil {
		t.Fatal("expected interval error")
	}
}

func TestParseArgsRejectsNegativeGracePeriods(t *testing.T) {
	_, err := ParseArgs("test", testArgs("-grace", "-1s"))

	if err == nil {
		t.Fatal("expected grace error")
	}
}

func mustParse(t *testing.T, args ...string) *Config {
	t.Helper()
	cfg, err := ParseArgs("test", args)
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	return cfg
}

func testArgs(args ...string) []string {
	result := make([]string, 0, len(args))
	result = append(result, args...)
	return result
}
