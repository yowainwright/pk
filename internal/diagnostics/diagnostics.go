package diagnostics

import (
	"fmt"
	"io"
	"runtime"
	"strings"
)

type Input struct {
	Version         string
	ServiceStatus   string
	ServiceErr      error
	DockerAvailable bool
	AuditEvents     int
	AuditErr        error
	AuditOverride   bool
}

type Report struct {
	Version       string
	Platform      string
	GoVersion     string
	Service       string
	Docker        string
	Audit         string
	AuditOverride string
}

func New(input Input) Report {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	return Report{
		Version:       input.Version,
		Platform:      platform,
		GoVersion:     runtime.Version(),
		Service:       serviceState(input.ServiceStatus, input.ServiceErr),
		Docker:        availability(input.DockerAvailable),
		Audit:         auditState(input.AuditEvents, input.AuditErr),
		AuditOverride: settingState(input.AuditOverride),
	}
}

func Write(w io.Writer, report Report) error {
	lines := reportLines(report)
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func reportLines(report Report) []string {
	version := "version: " + report.Version
	platform := "platform: " + report.Platform
	goVersion := "go: " + report.GoVersion
	service := "background service: " + report.Service
	docker := "docker CLI: " + report.Docker
	audit := "audit log: " + report.Audit
	auditOverride := "audit path override: " + report.AuditOverride
	return []string{
		"pk doctor",
		version,
		platform,
		goVersion,
		service,
		docker,
		audit,
		auditOverride,
		"privacy: excludes paths, commands, process details, and audit contents",
	}
}

func serviceState(status string, err error) string {
	if err != nil {
		return "error"
	}
	switch status {
	case "":
		return "unknown"
	case "not installed", "installed but not running", "active", "running":
		return status
	}
	state := launchdState(status)
	if state != "" {
		return state
	}
	return "unknown"
}

func launchdState(status string) string {
	for line := range strings.SplitSeq(status, "\n") {
		switch strings.TrimSpace(line) {
		case "state = running":
			return "running"
		case "state = not running":
			return "installed but not running"
		}
	}
	return ""
}

func availability(available bool) string {
	if available {
		return "available"
	}
	return "unavailable"
}

func settingState(set bool) string {
	if set {
		return "set"
	}
	return "unset"
}

func auditState(events int, err error) string {
	if err != nil {
		return "unreadable"
	}
	return fmt.Sprintf("readable (%d events)", events)
}
