package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/yowainwright/pk/internal/dx"
)

const rootUsage = `pk tracks local terminal sessions and cleans up ghost processes.

Usage:
  pk <command> [options]

Commands:
  status               Show daemon status
  obs                  Show daemon observability
  history              Show cleanup audit events
  install --apply      Install the daemon and shell lifecycle plugin
  uninstall            Remove the daemon and shell lifecycle plugin
  doctor               Print a shareable diagnostic report
  version              Print the version

Global options:
  --color MODE         Set color to auto, always, or never

Run "pk help <command>" for details.
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

const obsUsage = `Usage: pk obs

Shows daemon status, session/tab/window/agent/user counts, managed process
counts, lifecycle event count, and the last daemon decision.
`

const installUsage = `Usage: pk install --apply

Installs the supervised daemon and zsh lifecycle plugin for the current user.
The explicit --apply flag is required because the daemon can terminate
processes after session lifecycle signals.
`

func runInformational(args []string, ui *dx.UI) (bool, error) {
	if isVersionCommand(args) {
		return true, writeVersion(ui)
	}
	topic, requested := helpTopic(args)
	if !requested {
		return false, nil
	}
	return true, writeUsage(ui, topic)
}

func writeVersion(ui *dx.UI) error {
	value := fmt.Sprintf("pk %s", displayVersion())
	return ui.Value(value)
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
	if !hasHelpFlag(args) {
		return "", false
	}
	return strings.Join(leadingCommand(args), " "), true
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if isHelpFlag(arg) {
			return true
		}
	}
	return false
}

func leadingCommand(args []string) []string {
	for index, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return args[:index]
		}
	}
	return args
}

func isHelpFlag(arg string) bool {
	isShortHelp := arg == "-h"
	isLongHelp := arg == "--help"
	return isShortHelp || isLongHelp
}

func writeUsage(ui *dx.UI, topic string) error {
	usage, ok := usageForTopic(topic)
	if !ok {
		return fmt.Errorf("unknown help topic %q", topic)
	}
	return ui.Write(usage)
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
	case "obs":
		return obsUsage, true
	default:
		return "", false
	}
}

func utilityUsage(topic string) (string, bool) {
	switch topic {
	case "install":
		return installUsage, true
	case "history":
		return "Usage: pk history\n", true
	case "status":
		return "Usage: pk status\n", true
	case "doctor":
		return "Usage: pk doctor\n", true
	case "uninstall":
		return "Usage: pk uninstall\n", true
	case "version":
		return "Usage: pk version\n", true
	default:
		return "", false
	}
}
