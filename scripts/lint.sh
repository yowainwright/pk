#!/usr/bin/env sh
set -eu

. ./scripts/lib/go-tool.sh

strict=0
all=0
setup_only=0
lint_base_rev="${LINT_BASE_REV:-HEAD}"

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

usage() {
  printf '%s\n' "usage: sh scripts/lint.sh [--agent] [--all] [--changed] [--setup-only]" >&2
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
    --agent) strict=1 ;;
    --all) all=1 ;;
    --changed) all=0 ;;
    --setup-only) setup_only=1 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; fail "Unknown lint option: $1" ;;
    esac
    shift
  done
}

read_linter_version() {
  linter_version="$(sed -n 's/^version:[[:space:]]*//p' .custom-gcl.yml)"
  [ -n "$linter_version" ] || fail "missing linter version in .custom-gcl.yml"
  printf '%s\n' "$linter_version"
}

config_checksum() {
  cksum .custom-gcl.yml
}

custom_linter_stamp_path() {
  custom_linter_path=${1:?custom linter path is required}
  printf '%s\n' "$custom_linter_path.config.cksum"
}

custom_linter_config_changed() {
  custom_linter_path=${1:?custom linter path is required}
  stamp_path="$(custom_linter_stamp_path "$custom_linter_path")"
  [ -f "$stamp_path" ] || return 0
  current_checksum="$(config_checksum)"
  saved_checksum="$(sed -n '1p' "$stamp_path")"
  [ "$current_checksum" != "$saved_checksum" ]
}

custom_linter_needs_build() {
  custom_linter_path=${1:?custom linter path is required}
  [ ! -x "$custom_linter_path" ] && return 0
  custom_linter_config_changed "$custom_linter_path" && return 0
  built_go_version="$(
    go version -m "$custom_linter_path" 2>/dev/null | awk 'NR == 1 { print $2 }'
  )"
  current_go_version="$(go env GOVERSION)"
  [ "$built_go_version" != "$current_go_version" ]
}

run_shell_lint() {
  require_command shellcheck
  require_command shellcheck-legibility
  set -- scripts/lint-session.sh scripts/lint.sh scripts/lib/go-tool.sh scripts/setup.sh
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
  config_checksum > "$(custom_linter_stamp_path "$custom_linter_path")"
}

has_lint_base_rev() {
  git rev-parse --verify "$lint_base_rev" >/dev/null 2>&1
}

has_changed_go_inputs() {
  has_lint_base_rev || return 0
  git diff --name-only --diff-filter=ACMR "$lint_base_rev" -- \
    '*.go' go.mod go.sum .golangci.yml .custom-gcl.yml scripts/lint-session.sh scripts/lint.sh scripts/setup.sh |
    grep -q . && return 0
  git ls-files --others --exclude-standard -- '*.go' | grep -q .
}

should_run_legibility() {
  [ "$all" -eq 1 ] && return 0
  has_changed_go_inputs
}

should_lint_changed_only() {
  [ "$all" -eq 0 ] || return 1
  has_lint_base_rev
}

prepare_custom_linter() {
  require_command cksum
  linter_version="$(read_linter_version)"
  linter_package="github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$linter_version"
  linter_path="$PWD/bin/tools/golangci-lint-$linter_version/golangci-lint"
  custom_linter_path="$PWD/bin/legibility-golangci-lint"
  install_go_tool "$linter_path" "$linter_package"
  build_custom_linter_if_needed "$linter_path" "$custom_linter_path"
}

run_legibility() {
  issues_exit_code=${1:-1}
  should_run_legibility || return 0
  run_changed_legibility_if_needed "$issues_exit_code" && return
  run_all_legibility "$issues_exit_code"
}

run_changed_legibility_if_needed() {
  issues_exit_code=${1:?issues exit code is required}
  should_lint_changed_only || return 1
  run_changed_legibility "$issues_exit_code"
}

run_changed_legibility() {
  issues_exit_code=${1:?issues exit code is required}
  "$custom_linter_path" run "--issues-exit-code=$issues_exit_code" --enable-only=legibility "--new-from-rev=$lint_base_rev" ./...
}

run_all_legibility() {
  issues_exit_code=${1:?issues exit code is required}
  "$custom_linter_path" run "--issues-exit-code=$issues_exit_code" --enable-only=legibility ./...
}

run_optional_legibility() {
  run_legibility 0
}

run_strict_or_optional_legibility() {
  [ "$strict" -eq 0 ] && {
    run_optional_legibility
    return
  }
  run_legibility
}

run_go_lint() {
  prepare_custom_linter
  [ "$setup_only" -eq 1 ] && return
  go vet ./...
  "$custom_linter_path" run --disable=legibility ./...
  "$custom_linter_path" fmt --diff ./...
  run_strict_or_optional_legibility
}

main() {
  parse_args "$@"
  [ "$setup_only" -eq 0 ] || {
    run_go_lint
    return
  }
  run_shell_lint
  run_go_lint
}

main "$@"
