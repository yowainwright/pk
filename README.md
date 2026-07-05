# pk

Local process cleanup for agent and development work.

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

Show cleanup audit events:

```sh
pk history
```

Install the bundled agent skill:

```sh
pk skills install
```

Print where the skill will be installed:

```sh
pk skills path
```

Install background cleanup for the current user:

```sh
pk install
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

The monitor terminates matching processes by default. Use `-dry-run` when you
want to observe without killing.

Background cleanup uses `launchd` on macOS and `systemd --user` on Linux. It
runs `pk cleanup --apply --watch` with no external dependencies. Cleanup kills
target process trees child-first, infers agent/session-owned restartable
processes, stops matching local Docker Compose/devcontainer containers when
Docker is available, and writes bounded JSONL audit events. Set `PK_AUDIT_PATH`
to override the default audit file location. `pk skills install` writes the
bundled skill to `$PK_SKILLS_DIR`, `$CODEX_HOME/skills`, or `~/.codex/skills`.

## Development

<!-- local Go and legibility lint commands derived from go.mod, .mise.toml, .custom-gcl.yml, .golangci.yml, and .github/workflows/ci.yml -->

This repository uses Go 1.26 and a custom `golangci-lint` binary with the
`legibility` plugin.

Build and test:

```sh
go test ./...
go build ./...
```

Build the custom linter locally:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
golangci-lint custom
```

Run the same lint checks CI runs:

```sh
./bin/legibility-golangci-lint run ./...
./bin/legibility-golangci-lint fmt --diff ./...
```

`.custom-gcl.yml` configures `golangci-lint custom` to build
`./bin/legibility-golangci-lint` with
`github.com/yowainwright/golangci-lint-legibility`. `.golangci.yml` configures
the rules that binary runs.
