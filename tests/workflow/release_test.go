package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoReleaserCreatesDraft(t *testing.T) {
	config := readRepoFile(t, ".goreleaser.yaml")
	assertContains(t, config, "draft: true")
	assertContains(t, config, "mode: keep-existing")
	assertContains(t, config, "artifacts: checksum")
	assertMissing(t, config, "homebrew_casks:")
}

func TestPublisherValidatesBeforePublication(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/release.yml")
	assertContains(t, workflow, "group: publish-release-${{ inputs.tag_name }}")
	assertContains(t, workflow, "cancel-in-progress: false")
	assertContains(t, workflow, "needs: preflight")
	assertContains(t, workflow, "sh scripts/verify-checksum-signature.sh assets")
	assertContains(t, workflow, "needs: build")
	assertContains(t, workflow, "gh release edit \"$TAG_NAME\" --draft=false")
	assertContains(t, workflow, "needs: publish")
	assertMissing(t, workflow, "restore-draft:")
	assertMissing(t, workflow, "gh release edit \"$TAG_NAME\" --draft=true")
}

func TestStableReleaseOpensFormulaPR(t *testing.T) {
	release := readRepoFile(t, ".github/workflows/release.yml")
	homebrew := readRepoFile(t, ".github/workflows/update-homebrew.yml")
	assertContains(t, release, "if: ${{ !contains(inputs.tag_name, '-') }}")
	assertContains(t, homebrew, `scripts/update-formula pk "$version"`)
	assertContains(t, homebrew, "peter-evans/create-pull-request@")
	assertContains(t, homebrew, "--auto --squash --match-head-commit")
	assertMissing(t, homebrew, "Casks/pk.rb")
	assertMissing(t, homebrew, "git push")
	assertMissing(t, homebrew, "cat > Formula/pk.rb")
}

func TestFormulaPRRequiresVerifiedInstall(t *testing.T) {
	homebrew := readRepoFile(t, ".github/workflows/update-homebrew.yml")
	assertContains(t, homebrew, "sh scripts/verify-checksum-signature.sh assets")
	assertContains(t, homebrew, "sh scripts/verify-release-assets.sh assets")
	assertContains(t, homebrew, "sh scripts/verify-homebrew-formula.sh tap/Formula/pk.rb")
	assertContains(
		t, homebrew,
		`install -m 644 tap/Formula/pk.rb "$(brew --repository yowainwright/tap)/Formula/pk.rb"`,
	)
	assertContains(t, homebrew, "brew install --formula yowainwright/tap/pk")
	assertContains(t, homebrew, "brew test yowainwright/tap/pk")
	assertContains(t, homebrew, "brew audit --strict yowainwright/tap/pk")
}

func TestCIExercisesReleaseAndSecurityPaths(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/ci.yml")
	script := readRepoFile(t, "scripts/release.sh")
	security := readRepoFile(t, "scripts/security.sh")
	assertCIWorkflowPaths(t, workflow)
	assertReleaseScriptPaths(t, script)
	assertSecurityToolVersions(t, security)
}

func assertCIWorkflowPaths(t *testing.T, workflow string) {
	t.Helper()
	assertContains(t, workflow, "workflow_dispatch:")
	assertContains(t, workflow, "go test -tags=e2e")
	assertContains(t, workflow, "sh scripts/lint.sh")
	assertContains(t, workflow, "shellcheck-legibility-0.2.1.tar.gz")
	assertContains(t, workflow, "137942db1000e72ce8f8e2fbe8c10e334c70dc554ad2ef08aa49f0778f0302c0")
	assertMissing(t, workflow, "lint-shell")
	assertContains(t, workflow, "sh scripts/security.sh")
	assertContains(t, workflow, "bash tests/scripts/setup_test.sh")
	assertContains(t, workflow, "release --snapshot --clean --skip=sign")
	assertContains(t, workflow, "codecov/codecov-action@")
}

func assertReleaseScriptPaths(t *testing.T, script string) {
	t.Helper()
	assertContains(t, script, `release_validate_version "$PK_RELEASE_VERSION"`)
	assertContains(t, script, `Version must be v-prefixed v0 SemVer`)
	assertContains(t, script, `release_require_available_version`)
	assertContains(t, script, `git ls-remote --exit-code --tags origin`)
	assertContains(t, script, `mise run release-preview`)
	assertContains(t, script, `release_select_version`)
	assertContains(t, script, `git tag "$PK_RELEASE_VERSION"`)
	assertContains(t, script, `git push origin "$PK_RELEASE_VERSION"`)
	assertContains(t, script, `gh workflow run "$PK_PUBLISH_WORKFLOW" --ref main`)
	assertMissing(t, script, "release"+"-please")
}

func assertSecurityToolVersions(t *testing.T, security string) {
	t.Helper()
	assertContains(t, security, "v1.2.0")
	assertContains(t, security, "v2.25.0")
}

func TestBugReportSupportsPreDoctorReleases(t *testing.T) {
	template := readRepoFile(t, ".github/ISSUE_TEMPLATE/bug_report.yml")
	assertContains(t, template, "id: version")
	assertContains(t, template, "Run `pk --version`.")
	assertContains(t, template, "Diagnostics (if available)")
	assertContains(t, template, "id: environment")
	assertContains(t, template, "Architecture: arm64 or amd64")
}

func TestAssetVerificationIsShared(t *testing.T) {
	ci := readRepoFile(t, ".github/workflows/ci.yml")
	release := readRepoFile(t, ".github/workflows/release.yml")
	mise := readRepoFile(t, ".mise.toml")
	verifier := readRepoFile(t, "scripts/verify-release-assets.sh")
	command := "sh scripts/verify-release-assets.sh"
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
