# pk

Your terminal closed. Its processes didn’t. `pk` cleans up tracked, unprotected
processes after their terminal session ends.

<!-- project badges matching GitHub repository, CI workflow, OpenSSF Scorecard, and Codecov upload -->

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

## Contents

[Get running](#get-running) · [How cleanup works](#how-cleanup-works) ·
[See what’s happening](#see-whats-happening) · [Other commands](#other-commands) ·
[Uninstall](#uninstall) · [Develop and release](#develop-and-release)

## Get running

<!-- install commands matching go.mod module path and Homebrew cask release configuration -->

Automatic session tracking requires **interactive zsh**, with `launchd` on macOS
or `systemd --user` on Linux. From a local checkout, install with Go 1.26 or newer:

```sh
go install ./cmd/pk
```

Make sure Go’s install directory (`GOBIN`, or `GOPATH/bin` by default) is on your
`PATH`. Homebrew installation is pending a verified public release; the
[tap cask](https://github.com/yowainwright/homebrew-tap/blob/main/Casks/pk.rb)
currently references an unavailable release. See [installation issue #14](https://github.com/yowainwright/pk/issues/14).

Enable background cleanup:

```sh
pk install --apply
```

`--apply` authorizes automatic process termination. Installation starts the
daemon and adds the lifecycle plugin to your `.zshrc` (respecting `ZDOTDIR`).
**Open a new zsh tab** to load the plugin, run a command, then check:

```sh
pk status
pk obs
```

In `pk obs`, look for a recent `last tick` and a recorded session. `managed
processes` counts descendants the daemon has observed; zero is normal when
nothing is running. See the [service installer](internal/service/service.go)
and [zsh plugin](internal/shell/pk.zsh).

## How cleanup works

<!-- daemon ownership, protected names, defaults, and signal behavior derived from internal/daemon/daemon.go, internal/config/config.go, and internal/killer/killer.go -->

Every three seconds, the daemon records descendants of known shell sessions.
When a session ends, its tracked, unprotected processes become cleanup targets.

```text
session ends → verify target identity → SIGTERM → wait up to 2s → SIGKILL if needed
```

Processes are identified by **PID and creation time**, so a reused PID is not
treated as the original process. If a shell is missing from a snapshot, `pk`
checks its identity directly; an unreadable identity defers cleanup.

| Boundary | Installed behavior |
| --- | --- |
| Ownership | Only descendants observed before the session ends are tracked; processes that detach between polls can be missed. |
| Protection | Shells, editors, and agent CLIs such as `zsh`, `Code`, `codex`, and `claude` are protected by exact process name. Protection applies to individual processes. |
| Inactivity | Idle-session cleanup is disabled by default. An idle prompt alone does not trigger cleanup. |

The installer currently accepts only `--apply` and uses the
[daemon defaults and protected names](internal/config/config.go).
Agent, window, and user context tracking depends on identifiers supplied by
an integration; the bundled plugin emits shell and command events.
See the [cleanup decisions](internal/daemon/daemon.go) for the exact rules.

## See what's happening

<!-- observability and diagnostic commands derived from cmd/pk/main.go, internal/audit/audit.go, and internal/diagnostics/diagnostics.go -->

Use `pk obs` for session counts, tracked processes, the last tick, and the last
decision or error. `pk history` shows JSONL cleanup records, including targets,
reasons, and failures.

For a report to share in an issue:

```sh
pk doctor
```

`doctor` excludes paths, commands, process details, and audit contents.
History contains those details and should be reviewed before sharing.
On each write, the [audit log](internal/audit/audit.go) prunes events older than
48 hours and targets a 10 MiB size limit. Audit-write failures appear in `pk obs`.

## Other commands

<!-- CLI command usage and options implemented by cmd/pk/usage.go, cmd/pk/main.go, and internal/config/config.go -->

These commands use process heuristics or resource thresholds independently of
the daemon’s session tracking. They start in preview mode:

| Command | Purpose |
| --- | --- |
| `pk scan` | List process candidates and the reasons they matched. |
| `pk cleanup --scope processes` | Preview matching process trees. |
| `pk cleanup --scope containers` | Preview matching Compose and devcontainer containers. |
| `pk monitor` | Watch CPU and memory thresholds without terminating processes. |

Adding `--apply` to `cleanup` or `monitor` permits termination. Matching targets
can still be in use: a match does not prove a process or container was abandoned.
For example, plain `pk cleanup --apply` includes both processes and containers.
Read [command help](cmd/pk/usage.go) with `pk help cleanup` before applying.
Use `--color=auto|always|never` to control terminal color.

## Uninstall

<!-- uninstall behavior derived from internal/service/service.go and internal/shell/shell.go -->

Stop the service and remove the plugin and its `.zshrc` entry:

```sh
pk uninstall
```

Open a new shell afterward. Uninstall leaves the binary and stored history in
place; see the [uninstaller](internal/service/service.go).

## Develop and release

<!-- local setup and check commands derived from .mise.toml and scripts/setup.sh -->

Set up the tools, install repository hooks, and run the checks:

```sh
mise install
mise run setup
mise run check
```

The [contribution guide] covers prerequisites and validation; the
[release guide](.github/CONTRIBUTING.md#release) covers previewing and publishing.
For help, see the [support guide]. Report vulnerabilities privately using the
[security policy].

[contribution guide]: .github/CONTRIBUTING.md
[security policy]: .github/SECURITY.md
[support guide]: .github/SUPPORT.md
