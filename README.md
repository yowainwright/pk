# pk

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

**pk** (process kill) cleans up after your terminal. It tracks processes in zsh
sessions and cleans up unprotected leftovers when those sessions end.

Install it once, open a new shell, and get to work. pk tracks each session
automatically. Check in when you like.

[Install once](#install-once) · [While you work](#while-you-work) ·
[Check on it](#check-on-it) · [Uninstall](#uninstall)

---

## Install once

<!-- install commands derived from go.mod, .goreleaser.yaml, cmd/pk/main.go, internal/service/service.go, and internal/shell/shell.go; Homebrew availability tracked in GitHub issue #14 -->

Requires **interactive zsh** on macOS (`launchd`) or Linux (`systemd --user`).

Homebrew installation is [currently blocked](https://github.com/yowainwright/pk/issues/14).
Once a new release updates the tap, install on macOS with:

```sh
brew install yowainwright/tap/pk
```

For now, install from a local checkout with Go 1.26+ and Go’s install directory
on your `PATH`:

```sh
go install ./cmd/pk
```

After either install, enable background cleanup:

```sh
pk install --apply
```

This starts the background service and adds the session hook to your `.zshrc`.
`--apply` enables automatic process termination.

**Open a new zsh tab** to load the hook.

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

The daemon checks child processes every three seconds. It can only clean up
processes it saw before their session ended; it can miss processes that detach
between checks.

A process missing from a check stays tracked until pk confirms it exited or
its PID was reused.

Before terminating a target, it verifies the PID and creation time, sends
`SIGTERM`, then waits up to two seconds before using `SIGKILL`.
If it cannot read a shell’s identity, it waits before cleaning up that session.

The bundled integration tracks zsh sessions. Tracking separate agent, window,
or user-session lifecycles requires events from an integration.
See the [daemon](internal/daemon/daemon.go) and [zsh hook](internal/shell/pk.zsh).

</details>

## Check on it

<!-- observability commands derived from cmd/pk/main.go -->

```sh
pk obs
```

Look for a running daemon and a recent `last tick`.
Idle sessions are not counted as active. Zero managed processes is normal when
nothing is running.
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
