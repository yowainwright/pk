package workflow_test

import "testing"

func TestDependencyUpdatesUseTrustedTriggers(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/update.yml")
	assertContains(t, workflow, "workflow_dispatch:")
	assertContains(t, workflow, "schedule:")
	assertMissing(t, workflow, "pull_request:")
	assertMissing(t, workflow, "pull_request_target:")
	assertContains(t, workflow, "permissions:\n  contents: read")
	assertContains(t, workflow, "persist-credentials: false")
	assertContains(t, workflow, "cancel-in-progress: false")
}

func TestDependencyUpdatesValidateBeforePR(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/update.yml")
	assertContains(t, workflow, "uses: yowainwright/codependence@")
	assertContains(t, workflow, "config: .codependencerc")
	assertContains(t, workflow, "targets: go docker github-actions")
	assertContains(t, workflow, `pull-request: "true"`)
	assertContains(t, workflow, "token: ${{ secrets.PR_CREATE_TOKEN }}")
	assertContains(t, workflow, "post-update-command: |")
	assertContains(t, workflow, "go mod tidy\n")
	assertContains(t, workflow, "go mod verify\n")
	assertContains(t, workflow, "go test -race ./...\n")
	assertContains(t, workflow, "git diff --check\n")
}

func TestScorecardPublishesResults(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/scorecard.yml")
	assertContains(t, workflow, "push:\n    branches: [main]")
	assertContains(t, workflow, "schedule:")
	assertContains(t, workflow, "uses: ossf/scorecard-action@")
	assertContains(t, workflow, "results_file: results.sarif")
	assertContains(t, workflow, "results_format: sarif")
	assertContains(t, workflow, "publish_results: true")
	assertContains(t, workflow, "id-token: write")
	assertContains(t, workflow, "security-events: write")
	assertContains(t, workflow, "uses: github/codeql-action/upload-sarif@")
	assertContains(t, workflow, "sarif_file: results.sarif")
}

func TestScorecardUsesRestrictedJob(t *testing.T) {
	workflow := readRepoFile(t, ".github/workflows/scorecard.yml")
	assertContains(t, workflow, "permissions:\n  contents: read")
	assertContains(t, workflow, "runs-on: ubuntu-latest")
	assertContains(t, workflow, "persist-credentials: false")
	assertMissing(t, workflow, "env:")
	assertMissing(t, workflow, "defaults:")
	assertMissing(t, workflow, "run:")
	assertMissing(t, workflow, "container:")
	assertMissing(t, workflow, "services:")
}
