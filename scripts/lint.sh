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
custom_linter_path="$PWD/bin/legibility-golangci-lint"

custom_linter_needs_build() {
  if [ ! -x "$custom_linter_path" ] || [ .custom-gcl.yml -nt "$custom_linter_path" ]; then
    return 0
  fi
  built_go_version="$(go version -m "$custom_linter_path" 2>/dev/null | awk 'NR == 1 { print $2 }')"
  current_go_version="$(go env GOVERSION)"
  [ "$built_go_version" != "$current_go_version" ]
}

install_go_tool "$linter_path" "$linter_package"
go vet ./...
if custom_linter_needs_build; then
  "$linter_path" custom
fi
"$custom_linter_path" run ./...
"$custom_linter_path" fmt --diff ./...
