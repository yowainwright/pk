package workflow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type releaseConfig struct {
	Packages map[string]packageConfig `json:"packages"`
}

type packageConfig struct {
	Draft            bool `json:"draft"`
	ForceTagCreation bool `json:"force-tag-creation"`
}

func TestReleasePleaseCreatesTaggedDrafts(t *testing.T) {
	var config releaseConfig
	data := readRepoFile(t, "release-please-config.json")
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		t.Fatalf("parse release config: %v", err)
	}
	root := config.Packages["."]
	if !root.Draft {
		t.Fatalf("expected draft configuration, got %#v", root)
	}
	if !root.ForceTagCreation {
		t.Fatalf("expected tagged draft configuration, got %#v", root)
	}
}

func TestReleasePleaseDispatchesPublisher(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/release-please.yml")
	assertContains(t, workflow, "gh workflow run release.yml")
	assertContains(t, workflow, "actions: write")
	assertContains(t, workflow, "cancel-in-progress: false")
	assertMissing(t, workflow, "uses: ./.github/workflows/release.yml")
}

func TestPublisherIsNonCancelingAndPublishesLast(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/release.yml")
	assertContains(t, workflow, "group: publish-release-${{ inputs.tag_name }}")
	assertContains(t, workflow, "cancel-in-progress: false")
	assertContains(t, workflow, "gh release edit \"$VERSION\" --draft=false --latest")
	assertContains(t, workflow, "needs: publish")
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertContains(t *testing.T, value string, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected %q in workflow", expected)
	}
}

func assertMissing(t *testing.T, value string, unexpected string) {
	t.Helper()
	if strings.Contains(value, unexpected) {
		t.Fatalf("unexpected %q in workflow", unexpected)
	}
}
