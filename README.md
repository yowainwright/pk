# pk

Background cleanup for your terminal. `pk` tracks processes in zsh sessions and
cleans up unprotected leftovers when those sessions end.

Install it once, open a new shell, and work normally. There is no command to run
for each session.

<!-- project badges matching GitHub repository, CI workflow, OpenSSF Scorecard, and Codecov upload -->

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

[Install once](#install-once) · [While you work](#while-you-work) ·
[Check on it](#check-on-it) · [Uninstall](#uninstall)

## Install once

<!-- install commands derived from go.mod, cmd/pk/main.go, internal/service/service.go, and internal/shell/shell.go -->

Requires **interactive zsh** on macOS (`launchd`) or Linux (`systemd --user`).
From a local checkout, with Go 1.26+ and Go’s install directory on your `PATH`:

```sh
go install ./cmd/pk
pk install --apply
```

This starts the background service and adds the session hook to your `.zshrc`.
`--apply` enables automatic process termination.

**Open a new zsh tab.** The hook loads there, and `pk` takes care of tracking
and cleanup in the background.

## While you work

<!-- daemon ownership, protected names, defaults, and signal behavior derived from internal/daemon/daemon.go, internal/config/config.go, and internal/killer/killer.go -->

```text
Open a zsh tab → run your tools → close the session → pk cleans up tracked leftovers
```

An idle prompt alone does not trigger cleanup. Processes on the
[protected-name list](internal/config/config.go), including `zsh`, `codex`, and
`claude`, are skipped. Protection applies to each process individually.

<details>
<summary>How tracking and cleanup work</summary>

The daemon observes child processes every three seconds. It can only clean up
processes it observed before their session ended; processes that detach between
checks can be missed.

Before terminating a target, it verifies the PID and creation time, sends
`SIGTERM`, then waits up to two seconds before using `SIGKILL`.
An unreadable shell identity defers cleanup.

The bundled integration tracks zsh sessions. Tracking separate agent, window,
or user-session lifecycles requires events from an integration.
See the [daemon](internal/daemon/daemon.go) and [zsh hook](internal/shell/pk.zsh).

</details>

## Check on it

<!-- observability commands derived from cmd/pk/main.go -->

For an occasional check:

```sh
pk obs
```

Look for a running daemon, a recent `last tick`, and a recorded session.
Zero managed processes is normal when nothing is running.
For troubleshooting, see [Support](.github/SUPPORT.md).

<!-- CLI command usage implemented by cmd/pk/usage.go -->

Manual cleanup and diagnostic tools are available through `pk help`.

## Uninstall

<!-- uninstall behavior derived from internal/service/service.go and internal/shell/shell.go -->

```sh
pk uninstall
```

This stops the service and removes the shell hook. Open a new shell afterward.
The binary and stored history remain.

[Contributing](.github/CONTRIBUTING.md) · [Security](.github/SECURITY.md)
