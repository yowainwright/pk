#!/usr/bin/env sh
set -eu

. ./scripts/lib/go-tool.sh

linter_version="$(awk '$1 == "version:" { print $2; exit }' .custom-gcl.yml)"
if [ -z "$linter_version" ]; then
  printf 'missing linter version in .custom-gcl.yml\n' >&2
  exit 1
fi

linter_package="github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$linter_version"
linter_dir="$PWD/bin/tools/golangci-lint-$linter_version"
linter_path="$linter_dir/golangci-lint"
install_go_tool "$linter_path" "$linter_package"
go vet ./...
"$linter_path" custom
./bin/legibility-golangci-lint run ./...
./bin/legibility-golangci-lint fmt --diff ./...
