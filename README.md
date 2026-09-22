# pk C|=>

[![GitHub release](https://img.shields.io/github/v/release/yowainwright/pk?sort=semver)](https://github.com/yowainwright/pk/releases)
[![CI](https://github.com/yowainwright/pk/actions/workflows/ci.yml/badge.svg)](https://github.com/yowainwright/pk/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/yowainwright/pk/badge)](https://scorecard.dev/viewer/?uri=github.com/yowainwright/pk)
[![codecov](https://codecov.io/gh/yowainwright/pk/branch/main/graph/badge.svg)](https://codecov.io/gh/yowainwright/pk)

**pk**, process killer, tracks processes in zsh sessions and kills
unprotected leftovers when tracked sessions end.

This can provide significant benefit during agentic coding where, for any number of reasons, processes can be abandoned.

## Quick start

Install pk with Homebrew, then enable background cleanup.

Step 1. Install pk.
```sh
brew install yowainwright/tap/pk
```

Step 2. apply it.
```sh
pk install --apply
```

Step 3. Open a new zsh tab. That's it!

## What's going on under the hood?

`pk install --apply` starts the background service and adds a hook to your
`.zshrc`. The `--apply` flag allows pk to stop tracked processes automatically.

Background cleanup runs through launchd on macOS or user systemd on Linux.
Session tracking uses the [interactive zsh hook](internal/shell/pk.zsh).
See the [service setup](internal/service).

## Install

Homebrew is the preferred method:

```sh
brew install yowainwright/tap/pk
```

To install from a checkout of this repository instead of Homebrew:

```sh
go install ./cmd/pk
pk install --apply
```

## Why pk exists

Coding agents often start dev servers, test watchers, and other processes that
can outlive the work they were started for. This takes processing power from your computer! Finding and kill background processes that are no longer used can help. pk aims to make this thoughtless with a simple api. Feedback welcome!

```text
Open a zsh tab → run your tools → close the tab → pk cleans up leftovers
```

Once applied via `pk install --apply` pk runs quietly in the background.
You can check what it is tracking with
`pk obs` and read cleanup records with `pk history`.

## Commands

Run `pk <command> [options]`.

### `pk scan`

`pk scan` inspects processes; their PIDs, proposed actions, confidence,
and reasons. A `kill` action in this output is a proposal; scanning never sends
signals.

```sh
pk scan
pk scan --cpu 90 --mem 4096
```

The [scanner](internal/scan/scan.go) considers process names, ancestry, working
directories, and resource use. This is a broader process scan than the session
counts shown by `pk obs`.

### `pk cleanup`

`pk cleanup` previews processes and local Docker containers selected for cleanup.

> [!NOTE]
> you can add the `--apply` option to stop the selected targets and their unprotected process descendants.
> This can stop a dev server even while its shell session is still open.

Cleanup records both previews and applied results in [history](#pk-history).

```sh
pk cleanup --scope processes
pk cleanup --scope processes --apply
```

The default cleanup scope is `all` which includes containers. Process cleanup uses the
scanner's high-confidence targets. To avoid containers being cleaned up, a label
`pk.protected=true` can be set.

See the [process](internal/cleanup/cleanup.go)
and [container](internal/docker/docker.go) selection rules.

### `pk monitor`

`pk monitor` watches process CPU and memory use in the foreground. By default, pk logs what it
would stop. With `--apply`, it stops an unprotected process and its unprotected
descendants after the process stays above either threshold for the grace period.

```sh
pk monitor --cpu 90 --mem 4096 --grace 1m
pk monitor --cpu 90 --mem 4096 --grace 1m --apply
```
`Ctrl` + `C` stops monitoring. 

`pk monitor` command uses resource thresholds across visible processes, independently of whether their terminal sessions have ended.

### pk install

`pk install` installs and starts the background service for the current user. `--apply` is required because the service can stop tracked leftovers.

```sh
pk install --apply
```

After running, open a fresh zsh tab. The [installer](internal/service) uses launchd
on macOS and user systemd on Linux. It accepts `--apply`; the options for `monitor` and `cleanup` do not configure the installed service.

### pk uninstall

Stop and remove the background service and remove the shell hook by running `pk uninstall`. Open a fresh shell afterward. The executable and stored history remain.

```sh
pk uninstall
```

To also remove an installation made through Homebrew:

```sh
brew uninstall yowainwright/tap/pk
```

## pk api args

### `pk status`

`pk status` Show the operating system's status for the installed background service. On
macOS, this includes the launchd service details.

Use [doctor](#pk-doctor) for a shorter report or [obs](#pk-obs) for tracking counts.

### `pk obs`

`pk obs` shows service status, session counts, tracked processes, the last daemon tick,
and any recorded decision or error.

A recent `last tick` shows the daemon is running. An idle shell can have zero
active sessions while its processes remain tracked.

Try the [dev-server recipe](#check-cleanup-with-a-dev-server) to check cleanup.

### `pk history`

Print the retained cleanup records as JSON lines. Each record includes the
action, target, reasons, and whether cleanup was applied. Errors appear in an
`error` field when present.

The log includes daemon actions and manual `cleanup` previews and actions.
`"applied":false` means a preview or skipped action. `"applied":true` means
cleanup was attempted; check the `error` field for failures. An empty log has no
records to print. See the [audit record format](internal/audit/audit.go).

### `pk doctor`

`pk doctor` prints the installed version of pk, platform, service status, Docker CLI availability, and whether the audit log is readable.
The report leaves out paths, commands, process details, and audit contents.

### `pk version`

`pk version` prints the version of the executable you are running. `--version` is an alias.

### `pk help`

`pk help` shows the command list, or a usage summary for one command.
Running `pk` with no arguments also shows the command list.

```sh
pk help
pk help cleanup
```

### `pk enable`

`pk enable` starts pk.

```sh
pk enable
```

After running, open a fresh zsh tab.

### `pk disable`

`pk disable` stops pk. 
The executable, saved ignores, and history remain.

```sh
pk disable
```

### `pk ignore [...svc]`

`pk ignore` saves process names to protect from cleanup. 
Names must match exactly, including case. `--list` shows saved names.

```sh
pk ignore postgres redis-server
pk ignore --list
```

### `pk unignore`

`pk unignore` removes saved process-name ignores. 
Built-in protections remain.

```sh
pk unignore postgres
```

## pk api opts

Put command options after the command name. `--color` can go before or after it.
These options apply to the command you run; they do not change the configuration
of an already running background service.

### `--apply`

`--apply` can be added after `cleanup` or `monitor` to stop selected targets. Both commands default to preview mode. `install` requires this flag to enable background cleanup.

examples
```sh
pk cleanup --scope processes --apply
pk monitor --apply
pk install --apply
```

`scan` does not accept `--apply`. See the [command parsers](cmd/pk/main.go).

### `--scope`

Choose what `cleanup` considers. The default is `all`.

Use `processes` to leave containers alone, or `containers` to check only local
Docker containers. `all` checks both.

<!-- Docker endpoint validation and pinning from internal/docker/docker.go -->

Docker cleanup requires a Unix socket endpoint. Remote SSH and TCP endpoints
are rejected. Each cleanup pass uses the same endpoint for listing and stopping
containers.

example
```sh
pk cleanup --scope containers
```

This remains a preview until you add `--apply`. If Docker is unavailable,
[cleanup skips the container check](cmd/pk/main.go).

### `--watch`

Repeat `cleanup` until you press Ctrl-C. It runs once immediately, then repeats
at `--interval`. The default is off.

example
```sh
pk cleanup --scope processes --watch --interval 5s
```

Add `--apply` to act on each pass. `monitor` already runs continuously and does
not accept `--watch`. See the [cleanup loop](cmd/pk/main.go).

### `--cpu`

Set the CPU percentage threshold for `scan`, process `cleanup`, and `monitor`.
The default is `80`.

<!-- CPU sampling behavior from internal/process/process.go -->

CPU usage measures activity between samples. The first scan takes two samples
at least 100 ms apart; `monitor` then samples at its configured interval.
A reused process ID starts with a fresh baseline.

example
```sh
pk monitor --cpu 90
```

In `monitor`, a reading above the threshold starts the grace period. In `scan`
and `cleanup`, it adds a `high-cpu` reason; that reason alone does not make a
process a cleanup target. See the [selection rules](internal/scan/scan.go).

### `--mem`

Set the resident-memory threshold for `scan`, process `cleanup`, and `monitor`.
The default is `8192`. Values use MiB (1,048,576 bytes), labeled MB in the CLI.

example
```sh
pk monitor --mem 4096
```

In `monitor`, exceeding either `--cpu` or `--mem` starts the grace period. In
`scan` and `cleanup`, exceeding this threshold adds a `high-memory` reason;
other evidence is needed to select the process for cleanup. See
[memory measurement](internal/process/process.go) and
[threshold handling](internal/monitor/monitor.go).

### `--interval`

Set the time between checks for `monitor` or `cleanup --watch`. The default is
`3s`; the value must be greater than zero. Durations can use units such as `ms`,
`s`, or `m`.

example
```sh
pk monitor --interval 5s
```

The [shared parser](internal/config/config.go) also accepts this option on
`scan` and a single `cleanup` run, where it has no effect on scheduling.

### `--grace`

Set how long a process must stay above a resource threshold before `monitor`
acts. The default is `30s`. Zero is allowed; negative durations are rejected.

example
```sh
pk monitor --cpu 90 --grace 1m
```

A reading at or below both thresholds resets the timer. Without `--apply`, the
monitor only reports what it would stop. `scan` and `cleanup` accept this option
through the shared parser but do not use it. See the
[monitor](internal/monitor/monitor.go).

### `--protected`

Add a comma-separated list of process names to the built-in protected list for
`scan`, process `cleanup`, or `monitor`. Names must match exactly, including
case. Extra names are added to the defaults, not substituted for them.

example
```sh
pk cleanup --scope processes --protected postgres,redis-server
```

This protects the named processes, not their entire descendant trees. It does
not set Docker container protection or update the installed daemon. See the
[default list and matching rules](internal/config/config.go).

### `--stale`

Set the age at which an inactive, explicitly identified agent session becomes
eligible for cleanup. The default is `0`, which disables this behavior. A value
such as `--stale 10m` means ten minutes; negative durations are rejected.

Only the internal `__daemon` command uses this setting. The shared parser
accepts it on `scan`, `cleanup`, and `monitor`, but it has no effect there.
`pk install` does not accept it, so the installed service keeps the default.
The bundled zsh hook does not identify agent sessions unless configured by an
integration. See the [daemon's stale-session rules](internal/daemon/daemon.go).

### `--color`

Choose `auto`, `always`, or `never`. The default is `auto`, which enables color
for a suitable terminal and respects `NO_COLOR` and CI detection.

examples
```sh
pk --color never scan
pk scan --color=always
```

### `--help, -h`

Show help for the command, then exit without running it.

examples
```sh
pk cleanup --help
pk monitor -h
```

Use `pk --help` for the command list. These flags use the same
[help text](cmd/pk/usage.go) as `pk help`.

### `--version`

Print the executable's version and exit. Use it in place of a subcommand.

example
```sh
pk --version
```

This is equivalent to [`pk version`](#pk-version).

## Recipes

<!-- command combinations derived from cmd/pk/main.go, internal/config/config.go, internal/monitor/monitor.go, and internal/daemon/daemon.go -->

Start with previews when choosing cleanup targets or resource thresholds.

### Preview cleanup while you work

Repeat the process cleanup preview every ten seconds. This leaves processes
and Docker containers running, and records proposed cleanup in `pk history`.

```sh
pk cleanup --scope processes --watch --interval 10s
```

Press Ctrl-C when you have seen enough. See [cleanup](#pk-cleanup) for how targets
are selected.

### Watch resource use while keeping databases running

Preview processes that stay above 90% CPU or 4 GiB of resident memory for a
minute. Add your database process names to the protected list:

```sh
pk monitor --cpu 90 --mem 4096 --grace 1m --protected postgres,redis-server
```

This prints monitoring results without stopping anything. The
[protected names](#--protected) must match the process names on your machine.

### Check cleanup with a dev server

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
