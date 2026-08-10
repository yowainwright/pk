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
	ChangelogSections []changelogSection `json:"changelog-sections"`
	Draft             bool               `json:"draft"`
	ForceTagCreation  bool               `json:"force-tag-creation"`
}

type changelogSection struct {
	Type   string `json:"type"`
	Hidden bool   `json:"hidden"`
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

func TestReleasePleaseIncludesMaintenanceCommits(t *testing.T) {
	var config releaseConfig
	data := readRepoFile(t, "release-please-config.json")
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		t.Fatalf("parse release config: %v", err)
	}
	for _, section := range config.Packages["."].ChangelogSections {
		isVisibleChore := section.Type == "chore" && !section.Hidden
		if isVisibleChore {
			return
		}
	}
	t.Fatal("expected visible chore changelog section")
}

func TestReleasePleaseDispatchesPublisher(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/release-please.yml")
	assertContains(t, workflow, "gh workflow run release.yml")
	assertContains(t, workflow, "release-as: ${{ inputs.release_as }}")
	assertContains(t, workflow, "needs: release-please")
	assertContains(t, workflow, "cancel-in-progress: false")
	assertMissing(t, workflow, "uses: ./.github/workflows/release.yml")
}

func TestGoReleaserTargetsExistingDraft(t *testing.T) {
	config := readRepoFile(t, ".goreleaser.yaml")
	assertContains(t, config, "use_existing_draft: true")
	assertContains(t, config, "mode: keep-existing")
	assertContains(t, config, "artifacts: checksum")
	assertContains(t, config, "homebrew_casks:")
	assertContains(t, config, "skip_upload: true")
}

func TestPublisherValidatesBeforePublication(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/release.yml")
	assertContains(t, workflow, "group: publish-release-${{ inputs.tag_name }}")
	assertContains(t, workflow, "cancel-in-progress: false")
	assertContains(t, workflow, "needs: [preflight, homebrew-cask]")
	assertContains(t, workflow, "sh scripts/verify-checksum-signature.sh assets")
	assertContains(t, workflow, "needs: build")
	assertContains(t, workflow, "gh release edit \"$TAG_NAME\" --draft=false")
	assertContains(t, workflow, "needs: publish")
	assertContains(t, workflow, "needs: [publish, update-homebrew]")
	assertContains(t, workflow, "needs.update-homebrew.result == 'failure'")
	assertContains(t, workflow, "gh release edit \"$TAG_NAME\" --draft=true")
}

func TestStableReleaseUpdatesGeneratedCask(t *testing.T) {
	release := readRepoFile(t, ".github/workflows/release.yml")
	homebrew := readRepoFile(t, ".github/workflows/update-homebrew.yml")
	assertContains(t, release, "if: ${{ !contains(inputs.tag_name, '-') }}")
	assertContains(t, homebrew, "actions/download-artifact@")
	assertContains(t, homebrew, "cp generated/pk.rb tap/Casks/pk.rb")
	assertContains(t, homebrew, "git -C tap rm --ignore-unmatch Formula/pk.rb")
	assertContains(t, homebrew, "brew tap pk-release/verify")
	assertContains(t, homebrew, "brew install --cask pk-release/verify/pk")
	assertMissing(t, homebrew, "cat > Formula/pk.rb")
	assertMissing(t, homebrew, `brew install --cask "$PWD/generated/pk.rb"`)
}

func TestCIExercisesReleaseAndSecurityPaths(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/ci.yml")
	script := readRepoFile(t, "scripts/release.sh")
	security := readRepoFile(t, "scripts/security.sh")
	assertContains(t, workflow, "workflow_dispatch:")
	assertContains(t, workflow, "go test -tags=e2e")
	assertContains(t, workflow, "sh scripts/lint.sh")
	assertContains(t, workflow, "sh scripts/security.sh")
	assertContains(t, security, "v1.2.0")
	assertContains(t, security, "v2.25.0")
	assertContains(t, workflow, "release --snapshot --clean --skip=sign")
	assertContains(t, workflow, "codecov/codecov-action@")
	assertContains(t, script, `gh workflow run "$PK_CI_WORKFLOW" --ref "$head_ref"`)
	assertContains(t, script, `--match-head-commit "$PK_RELEASE_VALIDATED_HEAD"`)
	assertContains(t, script, `PK_RELEASE_VALIDATED_BASE="$pr_base"`)
	assertContains(t, script, `compare/$base...$head`)
	assertContains(t, script, `release_require_admin_merge "$pr_number"`)
	assertContains(t, script, `.restrictions != null`)
	assertContains(t, script, "reviewThreads(first:100)")
	assertContains(t, script, `PK_CI_REQUIRED_CHECK="Build, Lint, and Test"`)
	assertContains(t, script, `arguments+=(--admin)`)
}

func TestBugReportSupportsPreDoctorReleases(t *testing.T) {
	template := readRepoFile(t, ".github/ISSUE_TEMPLATE/bug_report.yml")
	assertContains(t, template, "id: version")
	assertContains(t, template, "Run `pk --version`.")
	assertContains(t, template, "Diagnostics (if available)")
	assertContains(t, template, "id: environment")
	assertContains(t, template, "Architecture: arm64 or amd64")
}

func TestCaskVerificationIsShared(t *testing.T) {
	ci := readRepoFile(t, ".github/workflows/ci.yml")
	release := readRepoFile(t, ".github/workflows/release.yml")
	mise := readRepoFile(t, ".mise.toml")
	verifier := readRepoFile(t, "scripts/verify-homebrew-cask.sh")
	command := "sh scripts/verify-homebrew-cask.sh"
	assertContains(t, ci, command)
	assertContains(t, release, command)
	assertContains(t, mise, command)
	assertContains(t, verifier, "expected_platforms=4")
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
