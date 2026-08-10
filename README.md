# pk

Safe-by-default local process cleanup CLI for agent and development work.

## Installation

Install the latest release with Homebrew:

```sh
brew install --cask yowainwright/tap/pk
```

Or install with Go 1.26 or newer:

```sh
go install github.com/yowainwright/pk/cmd/pk@latest
```

## Usage

<!-- CLI commands implemented by cmd/pk/main.go -->

Scan restartable development processes without killing anything:

```sh
pk scan
```

Record cleanup targets without killing anything:

```sh
pk cleanup
```

Kill high-confidence cleanup targets:

```sh
pk cleanup --apply
```

Run cleanup continuously on the configured interval:

```sh
pk cleanup --apply --watch
```

Limit cleanup to processes or containers:

```sh
pk cleanup --scope processes
pk cleanup --scope containers
```

Show cleanup audit events:

```sh
pk history
```

Print a privacy-safe diagnostic report to paste into a bug report:

```sh
pk doctor
```

The report includes versions and component availability, but excludes paths,
commands, process details, and audit contents.

Install the bundled agent skill:

```sh
pk skills install
```

Print where the skill will be installed:

```sh
pk skills path
```

Install active background cleanup for the current user:

```sh
pk install --apply
```

Check or remove background cleanup:

```sh
pk status
pk uninstall
```

Run the existing threshold monitor:

```sh
pk monitor
```

The monitor previews matching processes by default. Apply threshold-based
termination explicitly:

```sh
pk monitor --apply
```

Background cleanup uses `launchd` on macOS and `systemd --user` on Linux. It
runs `pk cleanup --apply --watch` with no external dependencies. Cleanup kills
target process trees child-first, infers agent/session-owned restartable
processes, stops matching local Docker Compose/devcontainer containers when
Docker is available, and writes bounded JSONL audit events. Set `PK_AUDIT_PATH`
to override the default audit file location. `pk skills install` writes the
bundled skill to `$PK_SKILLS_DIR`, `$CODEX_HOME/skills`, or `~/.codex/skills`.

Running `pk` without a command prints help. Every destructive mode requires an
explicit `--apply` flag.

Color is automatic on interactive terminals and can be controlled globally:

```sh
pk --color=always scan
pk cleanup --color=never
```

`--color` accepts `auto`, `always`, or `never`. Rich output is disabled for
pipes, CI, `TERM=dumb`, and `NO_COLOR`. Data remains plain on stdout; prompts,
loaders, completion shimmer, and structured status output use stderr. The UI
layer is implemented with the Go standard library and adds no dependencies.

## Development

<!-- local setup, Go, and legibility commands derived from scripts/setup.sh, go.mod, .mise.toml, .custom-gcl.yml, .golangci.yml, and .github/workflows/ci.yml -->

This repository pins Go, GoReleaser, and `svu` in `.mise.toml` and the custom
linter in `.custom-gcl.yml`. Install the tools and repository Git hooks:

```sh
mise install
mise run setup
```

Setup writes managed hooks directly to `.git/hooks` and preserves existing
unmanaged hooks. The pre-commit hook checks formatting and runs the Go tests;
the pre-push hook runs the complete local check suite.

Build and test:

```sh
mise run build
mise run check
```

Run black-box CLI tests plus isolated process termination in Docker:

```sh
./tests/e2e/test.sh
```

Run the same pinned custom lint checks CI runs:

```sh
mise run lint
```

`.custom-gcl.yml` configures `golangci-lint custom` to build
`./bin/legibility-golangci-lint` with
`github.com/yowainwright/golangci-lint-legibility`. `.golangci.yml` configures
the rules that binary runs.

## Release

Start an interactive release from a clean, synchronized `main` branch:

```sh
mise run release
```

`svu` derives the recommended version from conventional commits and supplies
patch, minor, major, release-candidate, and custom choices. The command runs the
complete release preview, opens the exact Release Please PR, dispatches and
waits for its CI, verifies the tested commit, asks before merging, and follows
publication to completion. Bot-created PRs whose required check event is
suppressed by GitHub are merged with a commit-pinned admin override only after
that exact commit passes CI.
Preview without changing GitHub state with
`bash scripts/release.sh --dry-run`.

Release Please owns the changelog, version commit, tag, and draft release.
GoReleaser builds four binaries, generates checksums and a Homebrew cask, and
signs the checksum with keyless cosign. The non-canceling publisher verifies
the draft, assets, signature, and generated cask before publication. Stable
releases update `yowainwright/homebrew-tap`; prereleases do not.
Maintenance commits use `chore:` and remain eligible for patch releases.

## Contributing and Support

See the [contribution guide], [support guide], and [security policy]. Report
vulnerabilities privately through GitHub Security Advisories.

[contribution guide]: .github/CONTRIBUTING.md
[security policy]: .github/SECURITY.md
[support guide]: .github/SUPPORT.md
