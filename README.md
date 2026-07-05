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

Show cleanup audit events:

```sh
pk history
```

Run the existing threshold monitor:

```sh
pk monitor
```

Dry-run is the default. Use `-dry-run=false` only when you want the monitor to
terminate matching processes.

Cleanup writes bounded JSONL audit events. Set `PK_AUDIT_PATH` to override the
default audit file location.

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
