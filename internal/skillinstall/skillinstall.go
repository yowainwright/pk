package skillinstall

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yowainwright/pk/skills"
)

const skillName = "pk"

func DefaultRoot() (string, error) {
	if override := os.Getenv("PK_SKILLS_DIR"); override != "" {
		return override, nil
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding home dir: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "skills"), nil
}

func Install(root string) (string, error) {
	if root == "" {
		defaultRoot, err := DefaultRoot()
		if err != nil {
			return "", err
		}
		root = defaultRoot
	}
	path := SkillPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating skill dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(skills.PKSkill), 0o644); err != nil {
		return "", fmt.Errorf("writing skill: %w", err)
	}
	return path, nil
}

func SkillPath(root string) string {
	return filepath.Join(root, skillName, "SKILL.md")
}
