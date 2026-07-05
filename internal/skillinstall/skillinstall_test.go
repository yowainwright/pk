package skillinstall

import (
	"os"
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

func TestSkillPathUsesPKDirectory(t *testing.T) {
	path := SkillPath("/tmp/skills")

	if path != "/tmp/skills/pk/SKILL.md" {
		t.Fatalf("unexpected path %q", path)
	}
}
