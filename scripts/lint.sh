#!/usr/bin/env sh
set -eu

. ./scripts/lib/go-tool.sh

fail() {
  message=${1:?message is required}
  printf '%s\n' "$message" >&2
  exit 1
}

require_command() {
  command_name=${1:?command name is required}
  command -v "$command_name" >/dev/null 2>&1 && return
  fail "$command_name is required"
}

read_linter_version() {
  linter_version="$(sed -n 's/^version:[[:space:]]*//p' .custom-gcl.yml)"
  [ -n "$linter_version" ] || fail "missing linter version in .custom-gcl.yml"
  printf '%s\n' "$linter_version"
}

custom_linter_needs_build() {
  custom_linter_path=${1:?custom linter path is required}
  [ ! -x "$custom_linter_path" ] && return 0
  [ .custom-gcl.yml -nt "$custom_linter_path" ] && return 0
  built_go_version="$(
    go version -m "$custom_linter_path" 2>/dev/null | awk 'NR == 1 { print $2 }'
  )"
  current_go_version="$(go env GOVERSION)"
  [ "$built_go_version" != "$current_go_version" ]
}

run_shell_lint() {
  require_command shellcheck
  require_command shellcheck-legibility
  set -- scripts/lint.sh scripts/lib/go-tool.sh scripts/setup.sh
  set -- "$@" scripts/hooks/commit-msg scripts/hooks/pre-commit
  set -- "$@" scripts/hooks/pre-push tests/scripts/setup_test.sh
  shellcheck -x "$@"
  shellcheck-legibility check "$@" scripts/hooks
}

build_custom_linter_if_needed() {
  linter_path=${1:?linter path is required}
  custom_linter_path=${2:?custom linter path is required}
  custom_linter_needs_build "$custom_linter_path" || return 0
  "$linter_path" custom
}

run_go_lint() {
  linter_version="$(read_linter_version)"
  linter_package="github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$linter_version"
  linter_path="$PWD/bin/tools/golangci-lint-$linter_version/golangci-lint"
  custom_linter_path="$PWD/bin/legibility-golangci-lint"
  install_go_tool "$linter_path" "$linter_package"
  go vet ./...
  build_custom_linter_if_needed "$linter_path" "$custom_linter_path"
  "$custom_linter_path" run ./...
  "$custom_linter_path" fmt --diff ./...
}

main() {
  run_shell_lint
  run_go_lint
}

main "$@"
