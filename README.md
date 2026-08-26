# pk

<!-- project badges matching GitHub repository, CI workflow, OpenSSF Scorecard, and Codecov upload -->

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

Safe-by-default local process cleanup CLI for agent and development work.

## Installation

<!-- install commands matching go.mod module path and Homebrew cask release configuration -->

Install the latest release with Homebrew:

```sh
brew install --cask yowainwright/tap/pk
```

Or install with Go 1.26 or newer:

```sh
go install github.com/yowainwright/pk/cmd/pk@latest
```

## CLI

`pk` previews local process cleanup by default. Direct commands below assume
the Homebrew or Go install shown above.

> Run `pk --help` or `pk help <command>` for current command help.

<!-- CLI command usage and options implemented by cmd/pk/usage.go, cmd/pk/main.go, and internal/config/config.go -->

### Commands

```sh
Usage:
  pk <command> [options]

Commands:
  scan                 Preview matching processes
  cleanup              Record cleanup targets without applying actions
  monitor              Watch process thresholds without applying actions
  history              Show cleanup audit events
  install --apply      Install active background cleanup
  status               Show background cleanup status
  doctor               Print a shareable, privacy-safe diagnostic report
  uninstall            Remove background cleanup
  skills install       Install the bundled Codex skill
  skills path          Print the skill installation path
  version              Print the version
```

### Common Flows

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

Install, check, or remove active background cleanup:

```sh
pk install --apply
pk status
pk uninstall
```

Inspect history, diagnostics, and bundled skills:

```sh
pk history
pk doctor
pk skills install
pk skills path
```

### Option Reference

Every destructive mode requires an explicit `--apply` flag.

Global options:

- `--color=auto|always|never` controls terminal color.

Process options:

- `--cpu PERCENT` sets the CPU threshold. Default: `80`.
- `--mem MB` sets the memory threshold. Default: `8192`.
- `--interval DURATION` sets the check interval. Default: `3s`.
- `--grace DURATION` sets the grace period before termination. Default: `30s`.
- `--protected NAMES` appends comma-separated process names to the protected
  set.

Cleanup options:

- `cleanup --scope all|processes|containers` limits cleanup. Default: `all`.
- `cleanup --watch` repeats cleanup on the configured interval.

Utility options:

- `skills install --dir PATH` installs the bundled skill to a custom root.

Background cleanup uses `launchd` on macOS and `systemd --user` on Linux. It
runs `pk cleanup --apply --watch`. Cleanup writes bounded JSONL audit events;
set `PK_AUDIT_PATH` to override the default audit file. `doctor` excludes
paths, commands, process details, and audit contents.

## Development

<!-- local setup and check commands derived from .mise.toml and scripts/setup.sh -->

Set up tools and hooks:

```sh
mise install
mise run setup
```

Run the local check suite:

```sh
mise run check
```

Useful focused commands:

```sh
mise run build
mise run lint
mise run test-e2e
mise run test-process-e2e
mise run security
```

ShellCheck and `shellcheck-legibility` are required for `mise run lint`.

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
