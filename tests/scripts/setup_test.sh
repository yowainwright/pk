#!/usr/bin/env bash
set -euo pipefail

TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly TEST_ROOT
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null

create_test_repository() {
  local repository="$1"
  mkdir -p "$repository/scripts"
  cp "$TEST_ROOT/scripts/setup.sh" "$repository/scripts/setup.sh"
  git -C "$repository" init --quiet
}

assert_installed_hook() {
  local repository="$1"
  local hook_name="$2"
  local installed="$repository/.git/hooks/$hook_name"
  [[ -x "$installed" ]]
  grep -Fxq '# Managed by pk scripts/setup.sh' "$installed"
  case "$hook_name" in
  pre-commit) grep -Fxq 'mise run hook-pre-commit' "$installed" ;;
  pre-push) grep -Fxq 'mise run check' "$installed" ;;
  esac
}

test_setup_installs_hooks() (
  repository="$(mktemp -d)"
  trap 'rm -rf -- "$repository"' EXIT
  create_test_repository "$repository"

  sh "$repository/scripts/setup.sh" >/dev/null

  assert_installed_hook "$repository" pre-commit
  assert_installed_hook "$repository" pre-push
  [[ ! -e "$repository/.githooks" ]]
)

test_setup_is_idempotent() (
  repository="$(mktemp -d)"
  trap 'rm -rf -- "$repository"' EXIT
  create_test_repository "$repository"
  sh "$repository/scripts/setup.sh" >/dev/null

  sh "$repository/scripts/setup.sh" >/dev/null

  assert_installed_hook "$repository" pre-commit
  assert_installed_hook "$repository" pre-push
)

test_setup_preserves_unmanaged_hooks() (
  repository="$(mktemp -d)"
  trap 'rm -rf -- "$repository"' EXIT
  create_test_repository "$repository"
  printf '#!/bin/sh\nprintf custom\\n' >"$repository/.git/hooks/pre-commit"

  if sh "$repository/scripts/setup.sh" >/dev/null 2>&1; then
    return 1
  fi

  grep -Fq 'printf custom' "$repository/.git/hooks/pre-commit"
  [[ ! -e "$repository/.git/hooks/pre-push" ]]
)

test_setup_preserves_configured_hooks_path() (
  repository="$(mktemp -d)"
  trap 'rm -rf -- "$repository"' EXIT
  create_test_repository "$repository"
  git -C "$repository" config core.hooksPath shared-hooks

  if sh "$repository/scripts/setup.sh" >/dev/null 2>&1; then
    return 1
  fi

  [[ ! -e "$repository/shared-hooks/pre-commit" ]]
)

run_test() {
  local name="$1"
  if "$name"; then
    printf 'ok - %s\n' "$name"
    return
  fi
  printf 'not ok - %s\n' "$name" >&2
  return 1
}

failures=0
for test_name in \
  test_setup_installs_hooks \
  test_setup_is_idempotent \
  test_setup_preserves_unmanaged_hooks \
  test_setup_preserves_configured_hooks_path; do
  run_test "$test_name" || ((failures += 1))
done

[[ "$failures" -eq 0 ]]
