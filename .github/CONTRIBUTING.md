# Contributing

## Setup

Requirements:

- Go and mise versions declared in `.mise.toml`
- Docker for isolated process tests
- GoReleaser for release-sensitive changes
- zsh for shell integration tests; jq for agent hook setup

<!-- contributor setup commands derived from .mise.toml and scripts/setup.sh -->

```sh
git clone https://github.com/yowainwright/pk.git
cd pk
mise install
mise run setup
mise run build
```

Setup installs pinned ShellCheck and shellcheck-legibility releases through mise,
builds the Go legibility plugin from [`.custom-gcl.yml`](../.custom-gcl.yml),
and installs Git, Codex, and Claude hooks. On macOS, setup installs Homebrew Bash
when the available Bash is older than 4.3. Custom Go builds stay in `tmp/lint-build`.

## Workflow

1. Branch from `main`.
2. Keep the change focused.
3. Add tests for behavior and regressions.
4. Update documentation for user-facing changes.
5. Run `mise run check`.
6. Open a pull request.

## Validation

```sh
mise run fmt-check
mise run lint
mise run test
mise run test-e2e
mise run test-process-e2e
mise run security
mise run release-preview
```

Run the release preview for workflow, packaging, versioning, or Homebrew changes.

### Code style

<!-- lint policy derived from scripts/lint.sh, scripts/lint-session.sh, and scripts/setup.sh -->

| Command | Readability findings | Scope |
| --- | --- | --- |
| `mise run lint` | Advisory | Changed shell files and new Go findings |
| `mise run lint-all` | Advisory | All shell files and Go packages; used by CI |
| `mise run lint-agent` | Fail | Changed shell files and new Go findings |
| `mise run lint-agent-all` | Fail | All shell files and Go packages |

ShellCheck, Go vet, standard Go lint, and formatting errors fail in every mode.
ShellCheck covers tracked and untracked `.sh`, `.bash`, and Git hook scripts;
the shell readability checker also covers `.zsh`. ShellCheck does not support zsh.
Changed checks compare against `HEAD`; set `LINT_BASE_REV` to use another revision.
Shell readability checks the whole changed file, while Go readability filters new findings.

Agent edit hooks and completion hooks run `scripts/lint-session.sh` in strict mode.
A failed completion check returns exit code 2 with diagnostics, blocking completion.
Restart the agent session after setup to load the project hooks.
The shared rules live in [`.golangci.yml`](../.golangci.yml) and
[`scripts/.shellcheck-legibility.toml`](../scripts/.shellcheck-legibility.toml).

## Release

<!-- release commands and publication behavior derived from .mise.toml, scripts/release.sh, .goreleaser.yaml, and .github/workflows/release.yml -->

Preview all checks and generated artifacts before publishing:

```sh
mise run release-preview
```

The preview runs code, CLI, isolated-process, and security checks; builds four
macOS/Linux binaries for amd64/arm64; and verifies the generated Homebrew cask.
On macOS it also smoke-tests a local cask installation.

From a clean `main` synchronized with GitHub, select a release candidate:

```sh
mise run release
```

The script accepts `v0` semantic versions, checks that the version is unused,
runs the complete preview, and asks before tagging, pushing, and dispatching
the release workflow. Pass `v0.1.0-rc.1` to choose that version explicitly.

The [publisher](workflows/release.yml) verifies binaries, checksums, and the
keyless cosign signature before publishing. Stable releases update the
[Homebrew tap](https://github.com/yowainwright/homebrew-tap); prereleases do not.
The macOS binaries are not notarized; the generated cask removes quarantine
from the staged binary. See [.goreleaser.yaml](../.goreleaser.yaml).

## Project Constraints

- Preserve preview-first behavior for destructive actions.
- Require explicit `--apply` authorization before changing processes or system services.
- Keep terminal output clear and restrained.
- Do not add runtime dependencies without a concrete portability or correctness need.
- Keep functions small, focused, and covered by tests.

## Security

Do not report vulnerabilities in public issues. Follow the [security policy].

[security policy]: SECURITY.md
