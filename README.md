# pk

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

**pk** (process killer) tracks processes in your zsh sessions and stops
unprotected leftovers when those sessions end.

[Quick start](#quick-start) · [Why pk exists](#why-pk-exists) ·
[How cleanup works](#how-cleanup-works) · [Check on it](#check-on-it) ·
[Common commands](#common-commands) · [Uninstall](#uninstall) ·
[Contributing](#contributing)

## Quick start

<!-- install commands derived from go.mod, .github/workflows/update-homebrew.yml, cmd/pk/main.go, internal/service/service.go, and internal/shell/shell.go -->

Install pk with Homebrew, then enable background cleanup:

```sh
brew install yowainwright/tap/pk
pk install --apply
```

`pk install --apply` starts the background service and adds a hook to your
`.zshrc`. The `--apply` flag allows pk to stop tracked processes automatically.

**Open a new zsh tab after installation.** Tabs that were already open may not
have loaded the hook. Start your dev servers and other tools in the new tab.

Background cleanup uses interactive zsh on macOS or Linux. Linux requires a
user systemd service. The cleanup walkthrough below has been tested on macOS.
See the [service setup](internal/service) and [shell hook](internal/shell/pk.zsh).

To install from a checkout of this repository instead of Homebrew:

```sh
go install ./cmd/pk
pk install --apply
```

## Why pk exists

<!-- session tracking derived from internal/shell/pk.zsh and internal/daemon/daemon.go -->

Coding agents often start dev servers, test watchers, and other processes that
outlive the work they were started for. Finding and stopping them by hand gets
old.

pk keeps track of those processes while you work. When their shell session ends,
it stops the tracked leftovers:

```text
Open a zsh tab → run your tools → close the tab → pk cleans up leftovers
```

It runs quietly in the background. You can check what it is tracking with
`pk obs` and read cleanup records with `pk history`.

## How cleanup works

<!-- daemon ownership, protected names, defaults, and signal behavior derived from internal/daemon/daemon.go, internal/config/config.go, and internal/killer/killer.go -->

The background service checks each session's child processes every three seconds.

| What you do | What pk does |
| --- | --- |
| Leave a shell open at an idle prompt | Keeps its processes running |
| Close a tracked shell or its terminal tab | Stops its tracked, unprotected leftovers |
| Exit Codex but leave the shell open | Keeps tracking the shell; exiting Codex alone does not end it |
| Work in an older shell without the hook | Cannot track that shell's session |

pk checks both the process ID and creation time before sending a signal. It
sends `SIGTERM`, waits up to two seconds, then uses `SIGKILL` if needed. If it
cannot confirm the shell's identity, it defers cleanup.

Names on the [protected list](internal/config/config.go), including `zsh`,
`codex`, and `claude`, are skipped. Protection applies to each named process;
its children can still be cleaned up when their session ends.

pk can only clean up processes it saw before the session ended. It can miss a
process that detaches between checks. Separate agent lifecycles need their own
integration; the bundled hook reports zsh session events. See the
[daemon](internal/daemon/daemon.go) and [signal handling](internal/killer/killer.go).

## Check on it

<!-- observability commands and history output derived from cmd/pk/main.go, internal/diagnostics/diagnostics.go, and internal/audit/audit.go -->

```sh
pk doctor
pk obs
pk history
```

| Command | What to look for |
| --- | --- |
| `pk doctor` | A running background service and a readable audit log |
| `pk obs` | A recent `last tick`, tracked process counts, and any `last error` |
| `pk history` | Cleanup records with process IDs, reasons, and results |

An idle prompt does not count as an active session. Zero managed processes can
be normal. A recent heartbeat tells you the daemon is running; history tells
you whether it attempted cleanup.

History can be empty if nothing needed stopping. In v0.1.0, `pk history` may
print only "Loading cleanup history" and return to the prompt when there are
no records.

### Try it with a dev server

<!-- manual verification based on internal/shell/pk.zsh, internal/daemon/daemon.go, and internal/audit/audit.go -->

1. In a fresh zsh tab, start your project's usual dev server. Leave it running
   for at least six seconds so pk has time to discover it.
2. In another tab, run `pk obs`. Check that the managed process count has
   increased before closing the server's tab.
3. Close the entire tab that started the server. Leave the second tab open,
   wait a few seconds, then run `pk history` there.

Check that the server no longer responds. In history, find its `command_line`
or project path (`cwd`). The entry should have `"command":"daemon"`,
`"applied":true`, and no `"error"` field. The reason may be `tab-ended` or
`session-ended`.

Some servers exit on their own when a tab closes. If the server is gone but
there is no matching cleanup record, that test does not establish that pk
stopped it.

For a bug report, include `pk doctor` output. It leaves out paths, commands,
process details, and audit contents. See [Support](.github/SUPPORT.md).

## Common commands

<!-- CLI command usage implemented by cmd/pk/usage.go -->

You can inspect and clean up processes without installing the background
service. `scan` always previews. `cleanup` and `monitor` preview by default;
add `--apply` to let them stop processes.

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

## Uninstall

<!-- uninstall behavior derived from internal/service/service.go and internal/shell/shell.go -->

```sh
pk uninstall
```

This stops the service and removes the shell hook. Open a new shell afterward.
The binary and stored history remain. To remove a Homebrew installation too:

```sh
brew uninstall yowainwright/tap/pk
```

## Contributing

<!-- contributor checks derived from .mise.toml and .github/CONTRIBUTING.md -->

Follow the [setup instructions](.github/CONTRIBUTING.md), then run the checks
before opening a pull request:

```sh
mise run check
```

The [contributing guide](.github/CONTRIBUTING.md) also covers process tests and
release previews. Report vulnerabilities through the
[security policy](.github/SECURITY.md).

[MIT License](LICENSE)
