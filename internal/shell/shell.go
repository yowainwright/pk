package shell

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	if err := writePlugin(i.PluginPath(), i.Executable); err != nil {
		return err
	}
	if err := appendSourceLine(i.ZshrcPath(), i.SourceLine()); err != nil {
		return rollbackPluginInstall(i.PluginPath(), err)
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
	content := strings.ReplaceAll(zshPlugin, executablePlaceholder, executable)
	if err := os.WriteFile(path, []byte(content), pluginMode); err != nil {
		return fmt.Errorf("writing shell plugin: %w", err)
	}
	return os.Chmod(path, pluginMode)
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
	rootDir, name := splitRootPath(path)
	root, err := os.OpenRoot(rootDir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("opening shell rc dir: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.Open(name)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading shell rc: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("reading shell rc: %w", err)
	}
	return string(data), nil
}

func splitRootPath(path string) (string, string) {
	dir, name := filepath.Split(path)
	if dir == "" {
		return ".", name
	}
	return filepath.Clean(dir), name
}

func sourceLineExists(contents string, line string) bool {
	for _, current := range strings.Split(contents, "\n") {
		if current == line {
			return true
		}
	}
	return false
}

func appendLine(contents string, line string) string {
	var builder strings.Builder
	if contents != "" {
		builder.WriteString(contents)
		if !strings.HasSuffix(contents, "\n") {
			builder.WriteByte('\n')
		}
	}
	builder.WriteString(line)
	builder.WriteByte('\n')
	return builder.String()
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
	mode := shellRCMode(path)
	return os.WriteFile(path, data, mode)
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

func rollbackPluginInstall(path string, cause error) error {
	if err := removePlugin(path); err != nil {
		return errors.Join(cause, fmt.Errorf("removing shell plugin: %w", err))
	}
	return cause
}
