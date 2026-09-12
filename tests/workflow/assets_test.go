package workflow_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	verificationPass = true
	verificationFail = false
)

var releasePlatforms = []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"}

func TestReleaseAssetsAcceptCompleteMatrix(t *testing.T) {
	dir := releaseFixture(t)
	runVerifier(t, verificationPass, "verify-release-assets.sh", dir)
}

func TestReleaseAssetsRejectIncompleteOrCorruptMatrix(t *testing.T) {
	for _, fault := range []string{"corrupt", "missing", "duplicate", "unlisted"} {
		t.Run(fault, func(t *testing.T) {
			dir := releaseFixture(t)
			damageRelease(t, dir, fault)
			runVerifier(t, verificationFail, "verify-release-assets.sh", dir)
		})
	}
}

func TestReleaseAssetsResolveSnapshotPaths(t *testing.T) {
	dir := releaseFixture(t)
	var artifacts []string
	for _, platform := range releasePlatforms {
		name := "pk-" + platform
		path := filepath.Join(dir, "build-"+platform)
		err := os.Rename(filepath.Join(dir, name), path)
		if err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, fmt.Sprintf(`{"name":%q,"path":%q}`, name, path))
	}
	writeFixture(t, dir, "artifacts.json", "["+strings.Join(artifacts, ",")+"]")
	runVerifier(t, verificationPass, "verify-release-assets.sh", dir)
}

func TestFormulaMatchesRelease(t *testing.T) {
	dir := releaseFixture(t)
	formula := formulaFixture(t, dir)
	runVerifier(t, verificationPass, "verify-homebrew-formula.sh", formula, "v0.1.0", dir)
	runVerifier(t, verificationFail, "verify-homebrew-formula.sh", formula, "v1.1.1", dir)
}

func TestFormulaRejectsWrongChecksum(t *testing.T) {
	dir := releaseFixture(t)
	formula := formulaFixture(t, dir)
	writeFixture(t, dir, "pk-darwin-arm64", "replacement binary")
	writeChecksums(t, dir)
	runVerifier(t, verificationFail, "verify-homebrew-formula.sh", formula, "v0.1.0", dir)
}

func releaseFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, platform := range releasePlatforms {
		writeFixture(t, dir, "pk-"+platform, platform)
	}
	writeChecksums(t, dir)
	return dir
}

func writeChecksums(t *testing.T, dir string) {
	t.Helper()
	var checksums strings.Builder
	for _, platform := range releasePlatforms {
		name := "pk-" + platform
		data := fixtureContent(t, filepath.Join(dir, name))
		fmt.Fprintf(&checksums, "%x  %s\n", sha256.Sum256([]byte(data)), name)
	}
	writeFixture(t, dir, "SHA256SUMS", checksums.String())
}

func damageRelease(t *testing.T, dir, fault string) {
	t.Helper()
	checksums := fixtureContent(t, filepath.Join(dir, "SHA256SUMS"))
	switch fault {
	case "corrupt":
		writeFixture(t, dir, "pk-linux-arm64", "corrupt")
	case "missing":
		err := os.Remove(filepath.Join(dir, "pk-linux-arm64"))
		if err != nil {
			t.Fatal(err)
		}
	case "duplicate":
		writeFixture(t, dir, "SHA256SUMS", checksums+checksums)
	case "unlisted":
		writeFixture(t, dir, "SHA256SUMS", strings.ReplaceAll(checksums, "pk-linux-arm64", "other"))
	}
}

func formulaFixture(t *testing.T, dir string) string {
	t.Helper()
	var formula strings.Builder
	formula.WriteString("class Pk < Formula\n")
	for _, platform := range releasePlatforms {
		name := "pk-" + platform
		checksum := sha256.Sum256([]byte(platform))
		url := "https://github.com/yowainwright/pk/releases/download/v0.1.0/" + name
		fmt.Fprintf(&formula, "  url %q\n", url)
		fmt.Fprintf(&formula, "  sha256 \"%x\"\n", checksum)
	}
	formula.WriteString("end\n")
	writeFixture(t, dir, "pk.rb", formula.String())
	return filepath.Join(dir, "pk.rb")
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func fixtureContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runVerifier(t *testing.T, wantSuccess bool, script string, args ...string) {
	t.Helper()
	path := filepath.Join("..", "..", "scripts", script)
	command := exec.Command("sh", append([]string{path}, args...)...)
	output, err := command.CombinedOutput()
	success := err == nil
	if success != wantSuccess {
		t.Fatalf("%s: success=%v, want %v: %s", script, success, wantSuccess, output)
	}
}
