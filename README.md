# pk

<!-- project badges matching GitHub repository, CI workflow, OpenSSF Scorecard, and Codecov upload -->

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

Background process lifecycle cleanup for agent and terminal sessions.

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

`pk` runs as a supervised background daemon. Terminal lifecycle events bind
processes to the tab/session that started them; the daemon cleans up tracked
ghost processes when their owner disappears or exceeds the inactive limit.
The lifecycle store tracks terminal sessions, tabs, windows, agent sessions,
and user sessions separately.

> Run `pk --help` or `pk help <command>` for current command help.

<!-- CLI command usage and options implemented by cmd/pk/usage.go, cmd/pk/main.go, and internal/config/config.go -->

### Commands

```sh
Usage:
  pk <command> [options]

Commands:
  status               Show daemon status
  obs                  Show daemon observability
  history              Show cleanup audit events
  install --apply      Install the daemon and shell lifecycle plugin
  uninstall            Remove the daemon and shell lifecycle plugin
  doctor               Print a shareable diagnostic report
  version              Print the version
```

### Common Flows

Install, check, observe, or remove the daemon:

```sh
pk install --apply
pk status
pk obs
pk uninstall
```

Inspect history or diagnostics:

```sh
pk history
pk doctor
```

### Option Reference

Global options:

- `--color=auto|always|never` controls terminal color.

Daemon options:

- `--interval DURATION` sets the check interval. Default: `3s`.
- `--stale DURATION` sets inactive agent session age before cleanup. Default:
  disabled.
- `--protected NAMES` appends comma-separated process names to the protected
  set.

`pk install --apply` uses `launchd` on macOS and `systemd --user` on Linux.
It also installs a zsh plugin that emits session lifecycle events. Cleanup
writes bounded JSONL audit events; set `PK_AUDIT_PATH` to override the default
audit file. `doctor` excludes paths, commands, process details, and audit
contents.

## Development

<!-- local setup and check commands derived from .mise.toml and scripts/setup.sh -->

Set up tools and run checks:

```sh
mise install
mise run setup
mise run check
```

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
