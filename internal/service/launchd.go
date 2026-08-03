package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
)

func (m *Manager) installLaunchd(ctx context.Context) error {
	if err := m.writeLaunchdPlist(); err != nil {
		return err
	}
	if err := m.stopLaunchd(ctx); err != nil {
		return m.rollbackLaunchdInstall(err)
	}
	if err := m.bootstrapLaunchd(ctx); err != nil {
		cause := fmt.Errorf("starting launchd service: %w", err)
		return m.rollbackLaunchdInstall(cause)
	}
	if err := m.kickstartLaunchd(ctx); err != nil {
		cause := fmt.Errorf("restarting launchd service: %w", err)
		return m.rollbackLaunchdInstall(cause)
	}
	return nil
}

func (m *Manager) uninstallLaunchd(ctx context.Context) error {
	if err := m.stopLaunchd(ctx); err != nil {
		return m.restoreLaunchd(err)
	}
	if err := removeFile(m.servicePath()); err != nil {
		return m.restoreLaunchd(err)
	}
	return nil
}

func (m *Manager) rollbackLaunchdInstall(cause error) error {
	ctx, cancel := recoveryContext()
	defer cancel()
	if err := m.stopLaunchd(ctx); err != nil {
		return lifecycleError(cause, "rolling back launchd install", err)
	}
	recovery := removeFile(m.servicePath())
	return lifecycleError(cause, "rolling back launchd install", recovery)
}

func (m *Manager) restoreLaunchd(cause error) error {
	ctx, cancel := recoveryContext()
	defer cancel()
	recovery := errors.Join(m.bootstrapLaunchd(ctx), m.kickstartLaunchd(ctx))
	return lifecycleError(cause, "restoring launchd service", recovery)
}

func (m *Manager) stopLaunchd(ctx context.Context) error {
	err := m.runner.Run(ctx, "launchctl", "bootout", m.launchdDomain(), m.servicePath())
	commandStopped := err == nil
	contextCanceled := ctx.Err() != nil
	operationFinished := commandStopped || contextCanceled
	if operationFinished {
		return err
	}
	_, statusErr := m.runner.Output(ctx, "launchctl", "print", m.launchdService())
	if statusErr != nil {
		return nil
	}
	return fmt.Errorf("stopping launchd service: %w", err)
}

func (m *Manager) bootstrapLaunchd(ctx context.Context) error {
	return m.runner.Run(ctx, "launchctl", "bootstrap", m.launchdDomain(), m.servicePath())
}

func (m *Manager) kickstartLaunchd(ctx context.Context) error {
	return m.runner.Run(ctx, "launchctl", "kickstart", "-k", m.launchdService())
}

func (m *Manager) writeLaunchdPlist() error {
	if err := ensureDir(filepath.Dir(m.servicePath())); err != nil {
		return err
	}
	if err := ensureDir(m.logDir()); err != nil {
		return err
	}
	return writeFile(m.servicePath(), launchdPlist(m.launchdDefinition()))
}

func (m *Manager) launchdDefinition() launchdDefinition {
	return launchdDefinition{
		label:  launchdLabel,
		args:   m.command(),
		stdout: filepath.Join(m.logDir(), "service.log"),
		stderr: filepath.Join(m.logDir(), "service.err.log"),
	}
}

func launchdPlist(def launchdDefinition) []byte {
	var buf bytes.Buffer
	runAtLoad := true
	keepAlive := true
	writeLaunchdHeader(&buf)
	writeKeyString(&buf, "Label", def.label)
	writeProgramArguments(&buf, def.args)
	writeKeyString(&buf, "StandardOutPath", def.stdout)
	writeKeyString(&buf, "StandardErrorPath", def.stderr)
	writeKeyBool(&buf, "RunAtLoad", runAtLoad)
	writeKeyBool(&buf, "KeepAlive", keepAlive)
	writeLaunchdFooter(&buf)
	return buf.Bytes()
}

func writeLaunchdHeader(buf *bytes.Buffer) {
	writeLine(buf, xml.Header)
	writeLine(
		buf,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
	)
	writeLine(buf, `<plist version="1.0">`)
	writeLine(buf, `<dict>`)
}

func writeLaunchdFooter(buf *bytes.Buffer) {
	writeLine(buf, `</dict>`)
	writeLine(buf, `</plist>`)
}

func writeKeyString(buf *bytes.Buffer, key string, value string) {
	writeLine(buf, "\t<key>"+escapeXML(key)+"</key>")
	writeLine(buf, "\t<string>"+escapeXML(value)+"</string>")
}

func writeKeyBool(buf *bytes.Buffer, key string, value bool) {
	writeLine(buf, "\t<key>"+escapeXML(key)+"</key>")
	if value {
		writeLine(buf, "\t<true/>")
		return
	}
	writeLine(buf, "\t<false/>")
}

func writeProgramArguments(buf *bytes.Buffer, args []string) {
	writeLine(buf, "\t<key>ProgramArguments</key>")
	writeLine(buf, "\t<array>")
	for _, arg := range args {
		writeLine(buf, "\t\t<string>"+escapeXML(arg)+"</string>")
	}
	writeLine(buf, "\t</array>")
}

func escapeXML(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

type launchdDefinition struct {
	label  string
	args   []string
	stdout string
	stderr string
}
