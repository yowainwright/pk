package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"path/filepath"
)

func (m *Manager) installLaunchd() error {
	if err := m.writeLaunchdPlist(); err != nil {
		return err
	}
	_ = m.runner.Run("launchctl", "bootout", m.launchdDomain(), m.servicePath())
	if err := m.runner.Run(
		"launchctl",
		"bootstrap",
		m.launchdDomain(),
		m.servicePath(),
	); err != nil {
		return fmt.Errorf("starting launchd service: %w", err)
	}
	return m.runner.Run("launchctl", "kickstart", "-k", m.launchdService())
}

func (m *Manager) uninstallLaunchd() error {
	_ = m.runner.Run("launchctl", "bootout", m.launchdDomain(), m.servicePath())
	return removeFile(m.servicePath())
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
