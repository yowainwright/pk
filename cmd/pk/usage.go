package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

const rootUsage = `pk safely previews local process cleanup by default.

Usage:
  pk <command> [options]

Commands:
  scan                 Preview matching processes
  cleanup              Record cleanup targets without applying actions
  monitor              Watch process thresholds without applying actions
  history              Show cleanup audit events
  install --apply      Install active background cleanup
  status               Show background cleanup status
  uninstall            Remove background cleanup
  skills install       Install the bundled Codex skill
  skills path          Print the skill installation path
  version              Print the version

Destructive commands require --apply. Run "pk help <command>" for details.
`

const scanUsage = `Usage: pk scan [--cpu PERCENT] [--mem MB] [--protected NAMES]

Lists matching processes without terminating them.
`

const cleanupUsage = `Usage: pk cleanup [--apply] [--watch] [--scope SCOPE] [options]

Records high-confidence cleanup targets. --apply terminates matching process
trees and stops matching local containers. --scope accepts all, processes, or
containers. --watch repeats on the interval.
`

const monitorUsage = `Usage: pk monitor [--apply] [options]

Watches CPU and memory thresholds in preview mode. --apply terminates an
unprotected process after it exceeds a threshold for the grace period.
`

const installUsage = `Usage: pk install --apply

Installs continuous active cleanup for the current user. The explicit --apply
flag is required because the service terminates processes and containers.
`

const skillsUsage = `Usage:
  pk skills install [--dir PATH]
  pk skills path
`

func runInformational(args []string, out io.Writer) (bool, error) {
	if isVersionCommand(args) {
		return true, writeVersion(out)
	}
	topic, requested := helpTopic(args)
	if !requested {
		return false, nil
	}
	return true, writeUsage(out, topic)
}

func writeVersion(out io.Writer) error {
	_, err := fmt.Fprintln(out, "pk", displayVersion())
	return err
}

func displayVersion() string {
	if version != "dev" {
		return version
	}
	buildVersion, ok := readBuildVersion()
	if !ok {
		return version
	}
	return buildVersion
}

func readBuildVersion() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	buildVersion := info.Main.Version
	if buildVersion == "" {
		return "", false
	}
	if buildVersion == "(devel)" {
		return "", false
	}
	return buildVersion, true
}

func helpTopic(args []string) (string, bool) {
	if len(args) == 0 {
		return "", true
	}
	if args[0] == "help" {
		return strings.Join(args[1:], " "), true
	}
	for index, arg := range args {
		if isHelpFlag(arg) {
			return strings.Join(args[:index], " "), true
		}
	}
	return "", false
}

func isHelpFlag(arg string) bool {
	isShortHelp := arg == "-h"
	isLongHelp := arg == "--help"
	return isShortHelp || isLongHelp
}

func writeUsage(out io.Writer, topic string) error {
	usage, ok := usageForTopic(topic)
	if !ok {
		return fmt.Errorf("unknown help topic %q", topic)
	}
	_, err := fmt.Fprint(out, usage)
	return err
}

func usageForTopic(topic string) (string, bool) {
	usage, ok := primaryUsage(topic)
	if ok {
		return usage, true
	}
	return utilityUsage(topic)
}

func primaryUsage(topic string) (string, bool) {
	switch topic {
	case "":
		return rootUsage, true
	case "scan":
		return scanUsage, true
	case "cleanup":
		return cleanupUsage, true
	case "monitor":
		return monitorUsage, true
	default:
		return "", false
	}
}

func utilityUsage(topic string) (string, bool) {
	switch topic {
	case "install":
		return installUsage, true
	case "skills", "skills install", "skills path":
		return skillsUsage, true
	case "history":
		return "Usage: pk history\n", true
	case "status":
		return "Usage: pk status\n", true
	case "uninstall":
		return "Usage: pk uninstall\n", true
	case "version":
		return "Usage: pk version\n", true
	default:
		return "", false
	}
}
