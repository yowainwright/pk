package workflow_test

import "testing"

func TestCommunityHealthFilesExist(t *testing.T) {
	paths := []string{
		".github/CODEOWNERS",
		".github/CODE_OF_CONDUCT.md",
		".github/CONTRIBUTING.md",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/SECURITY.md",
		".github/SUPPORT.md",
		".github/ISSUE_TEMPLATE/bug_report.yml",
		".github/ISSUE_TEMPLATE/feature_request.yml",
		".github/dependabot.yml",
	}
	for _, path := range paths {
		if readRepoFile(t, path) == "" {
			t.Fatalf("%s is empty", path)
		}
	}
}

func TestOwnershipAndSecurityRouting(t *testing.T) {
	owners := readRepoFile(t, ".github/CODEOWNERS")
	security := readRepoFile(t, ".github/SECURITY.md")
	issues := readRepoFile(t, ".github/ISSUE_TEMPLATE/config.yml")
	assertContains(t, owners, "@yowainwright")
	assertContains(t, security, "GitHub Security Advisories")
	assertContains(t, issues, "/security/advisories/new")
}
