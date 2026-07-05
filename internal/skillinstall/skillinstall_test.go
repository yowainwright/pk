package skillinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWritesSkillFile(t *testing.T) {
	root := t.TempDir()

	path, err := Install(root)
	if err != nil {
		t.Fatalf("install skill: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(data), "name: pk") {
		t.Fatalf("expected pk skill, got %s", string(data))
	}
}

func TestInstallUsesDefaultRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PK_SKILLS_DIR", root)

	path, err := Install("")
	if err != nil {
		t.Fatalf("install skill: %v", err)
	}
	if path != SkillPath(root) {
		t.Fatalf("expected default path, got %q", path)
	}
}

func TestInstallReturnsDirectoryErrors(t *testing.T) {
	file := writeTestFile(t)

	_, err := Install(file)

	if err == nil {
		t.Fatal("expected directory error")
	}
}

func TestDefaultRootUsesOverride(t *testing.T) {
	t.Setenv("PK_SKILLS_DIR", "/tmp/pk-skills")

	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("default root: %v", err)
	}
	if root != "/tmp/pk-skills" {
		t.Fatalf("expected override root, got %q", root)
	}
}

func TestDefaultRootUsesCodexHome(t *testing.T) {
	t.Setenv("PK_SKILLS_DIR", "")
	t.Setenv("CODEX_HOME", "/tmp/codex")

	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("default root: %v", err)
	}
	if root != "/tmp/codex/skills" {
		t.Fatalf("expected codex skills root, got %q", root)
	}
}

func TestDefaultRootUsesHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PK_SKILLS_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", home)

	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("default root: %v", err)
	}
	expected := filepath.Join(home, ".codex", "skills")
	if root != expected {
		t.Fatalf("expected home fallback, got %q", root)
	}
}

func TestSkillPathUsesPKDirectory(t *testing.T) {
	path := SkillPath("/tmp/skills")

	if path != "/tmp/skills/pk/SKILL.md" {
		t.Fatalf("unexpected path %q", path)
	}
}

func writeTestFile(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "file")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return file.Name()
}
