package shell

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	pluginMode            = 0o600
	shellDirMode          = 0o700
	sourceMarker          = "# pk"
	executablePlaceholder = "__PK_EXECUTABLE__"
)

//go:embed pk.zsh
var zshPlugin string

type Installer struct {
	Home       string
	ZDOTDIR    string
	Executable string
}

func (i Installer) Install() error {
	if err := i.validate(); err != nil {
		return err
	}
	previous, err := readPlugin(i.PluginPath())
	if err != nil {
		return err
	}
	if err := writePlugin(i.PluginPath(), i.Executable); err != nil {
		return rollbackPluginInstall(i.PluginPath(), previous, err)
	}
	if err := appendSourceLine(i.ZshrcPath(), i.SourceLine()); err != nil {
		return rollbackPluginInstall(i.PluginPath(), previous, err)
	}
	return nil
}

func (i Installer) Uninstall() error {
	if err := removeSourceLine(i.ZshrcPath(), i.SourceLine()); err != nil {
		return err
	}
	return removePlugin(i.PluginPath())
}

func (i Installer) PluginPath() string {
	return filepath.Join(i.Home, ".config", "pk", "shell", "pk.zsh")
}

func (i Installer) ZshrcPath() string {
	if i.ZDOTDIR != "" {
		return filepath.Join(i.ZDOTDIR, ".zshrc")
	}
	return filepath.Join(i.Home, ".zshrc")
}

func (i Installer) SourceLine() string {
	path := "$HOME/.config/pk/shell/pk.zsh"
	return fmt.Sprintf(`[ -r "%s" ] && source "%s" %s`, path, path, sourceMarker)
}

func (i Installer) validate() error {
	if i.Home == "" {
		return fmt.Errorf("home is required")
	}
	if i.Executable == "" {
		return fmt.Errorf("executable is required")
	}
	return nil
}

func writePlugin(path string, executable string) error {
	if err := os.MkdirAll(filepath.Dir(path), shellDirMode); err != nil {
		return fmt.Errorf("creating shell plugin dir: %w", err)
	}
	content := strings.ReplaceAll(zshPlugin, executablePlaceholder, quoteShellLiteral(executable))
	if err := os.WriteFile(path, []byte(content), pluginMode); err != nil {
		return fmt.Errorf("writing shell plugin: %w", err)
	}
	return os.Chmod(path, pluginMode)
}

func quoteShellLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "'", "'\"'\"'")
	quoted := "'" + escaped + "'"
	return quoted
}

func appendSourceLine(path string, line string) error {
	current, err := readOptional(path)
	if err != nil {
		return err
	}
	if sourceLineExists(current, line) {
		return nil
	}
	next := appendLine(current, line)
	if err := os.MkdirAll(filepath.Dir(path), shellDirMode); err != nil {
		return fmt.Errorf("creating shell rc dir: %w", err)
	}
	return writeShellRC(path, []byte(next))
}

func readOptional(path string) (string, error) {
	// #nosec G304 -- User-selected shell rc; external dotfile symlinks are supported.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading shell rc: %w", err)
	}
	return string(data), nil
}

func sourceLineExists(contents string, line string) bool {
	lines := strings.Split(contents, "\n")
	return slices.Contains(lines, line)
}

func appendLine(contents string, line string) string {
	needsNewline := contents != "" && !strings.HasSuffix(contents, "\n")
	if needsNewline {
		contents += "\n"
	}
	next := contents + line + "\n"
	return next
}

func removeSourceLine(path string, line string) error {
	current, err := readOptional(path)
	if err != nil {
		return err
	}
	if current == "" {
		return nil
	}
	next := withoutSourceLine(current, line)
	return writeShellRC(path, []byte(next))
}

func writeShellRC(path string, data []byte) error {
	return os.WriteFile(path, data, shellRCMode(path))
}

func shellRCMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return pluginMode
	}
	return info.Mode().Perm()
}

func withoutSourceLine(contents string, line string) string {
	lines := strings.Split(contents, "\n")
	kept := make([]string, 0, len(lines))
	for _, current := range lines {
		if current == line {
			continue
		}
		kept = append(kept, current)
	}
	return strings.Join(kept, "\n")
}

func removePlugin(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readPlugin(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Base(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func rollbackPluginInstall(path string, previous []byte, cause error) error {
	if previous != nil {
		return errors.Join(cause, os.WriteFile(path, previous, pluginMode))
	}
	if err := removePlugin(path); err != nil {
		return errors.Join(cause, fmt.Errorf("removing shell plugin: %w", err))
	}
	return cause
}
