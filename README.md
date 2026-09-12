# pk

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

**pk** (process killer) tracks processes in your zsh sessions and stops unprotected leftovers when those sessions end.

[Quick start](#quick-start) · [Why it exists](#why-it-exists) ·
[How cleanup works](#how-cleanup-works) · [Common commands](#common-commands) ·
[Check on it](#check-on-it) · [Uninstall](#uninstall) · [Contributing](#contributing)

## Quick start

<!-- install commands derived from go.mod, .goreleaser.yaml, cmd/pk/main.go, internal/service/service.go, and internal/shell/shell.go; Homebrew availability tracked in GitHub issue #14 -->

pk's background cleanup currently requires interactive zsh on macOS or Linux. pk was built for Mac; so other OS support is not known.

### Install

brew
```sh
brew install yowainwright/tap/pk
```

go
```sh
go install ./cmd/pk
```

Then enable background cleanup:

to enable, run
```sh
pk install --apply
```

This starts the background service and adds a hook to your `.zshrc`.
`--apply` gives pk permission to terminate processes automatically.

Open a new zsh tab to start tracking. Setup details are in the
[service](internal/service/service.go) and [shell integration](internal/shell/shell.go).

## Why pk exists

<!-- session tracking derived from internal/shell/pk.zsh and internal/daemon/daemon.go -->

Coding agents often start dev servers, test watchers, and execut other processes that
outlive the work they were started for. Finding and stopping them by hand gets
old.

pk keeps track of processes while you work, so it can clean up after a session
ends:

```text
Open a zsh tab → run your tools → close the session → pk stops tracked leftovers
```

The [zsh hook](internal/shell/pk.zsh) reports session events to the background
service. You can leave pk running and check on it with `pk obs`.

## How cleanup works

<!-- daemon ownership, protected names, defaults, and signal behavior derived from internal/daemon/daemon.go, internal/config/config.go, and internal/killer/killer.go -->

The background service checks each session’s child processes every three seconds.

| What happens | What pk does |
| --- | --- |
| You leave a shell at an idle prompt | Keeps the session’s processes running |
| A session ends | Stops its tracked, unprotected processes |
| A process disappears from a check | Keeps tracking it until exit or PID reuse is confirmed |
| The shell’s identity cannot be read | Defers cleanup for that session |

Before sending a signal, pk checks the process ID and creation time. It sends
`SIGTERM` first, then waits up to two seconds before using `SIGKILL`.

Names on the [protected list](internal/config/config.go), including `zsh`,
`codex`, and `claude`, are skipped. That protection applies to each process;
it does not automatically cover its children.

There are limits. pk can only clean up processes it saw before their session
ended. It can miss processes that detach between checks. The bundled hook tracks
zsh sessions; separate agent or window lifecycles need their own integration.
See the [daemon](internal/daemon/daemon.go) and [signal handling](internal/killer/killer.go).

## Common commands

<!-- CLI command usage implemented by cmd/pk/usage.go -->

You can inspect and clean up processes without installing the background service.
`cleanup` and `monitor` preview by default; add `--apply` to let them stop
processes. `scan` is always a preview.

```sh
# Inspect matching processes
pk scan

# Preview process cleanup
pk cleanup --scope processes

# Apply process cleanup
pk cleanup --scope processes --apply

# Watch CPU and memory thresholds without stopping processes
pk monitor
```

Without `--scope processes`, cleanup also considers local Docker containers.
Use `pk help cleanup` or `pk help monitor` for options. The full command list is
in [`pk help`](cmd/pk/usage.go).

## Check on it

<!-- observability commands derived from cmd/pk/main.go -->

```sh
pk obs
```

Look for a running daemon and a recent `last tick`. An idle shell does not count
as an active session. Zero managed processes is normal when nothing is running.

To see recorded cleanup decisions or gather a diagnostic report:

```sh
pk history
pk doctor
```

The doctor report leaves out paths, commands, process details, and audit contents.
Include it when [reporting a bug](https://github.com/yowainwright/pk/issues/new/choose).
See [Support](.github/SUPPORT.md) for details.

## Uninstall

<!-- uninstall behavior derived from internal/service/service.go and internal/shell/shell.go -->

```sh
pk uninstall
```

This stops the service and removes the shell hook. Open a new shell afterward.
It leaves the binary and stored history in place. See the
[uninstall implementation](internal/service/service.go).

## Contributing

<!-- contributor checks derived from .mise.toml and .github/CONTRIBUTING.md -->

After [setting up the development tools](.github/CONTRIBUTING.md), run the local
checks before opening a pull request:

```sh
mise run check
```

The [contributing guide](.github/CONTRIBUTING.md) covers setup, process tests,
and release previews. Report vulnerabilities through the
[security policy](.github/SECURITY.md).

[MIT License](LICENSE)
