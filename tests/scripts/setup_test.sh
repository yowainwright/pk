#!/usr/bin/env bash
set -euo pipefail

TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly TEST_ROOT
TEST_TMP_ROOT="$TEST_ROOT/tmp/setup-tests"
readonly TEST_TMP_ROOT
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null

new_test_repository() {
  mkdir -p "$TEST_TMP_ROOT"
  mktemp -d "$TEST_TMP_ROOT/repository.XXXXXX"
}

cleanup_test_root() {
  case "$TEST_TMP_ROOT" in
  "$TEST_ROOT"/tmp/setup-tests) rm -rf -- "$TEST_TMP_ROOT" ;;
  *) printf 'Refusing to remove %s\n' "$TEST_TMP_ROOT" >&2; return 1 ;;
  esac
}

write_fake_mise() {
  path=${1:?fake mise path is required}
  cat > "$path" <<'EOF'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "$MISE_LOG"
EOF
  chmod 0755 "$path"
}

create_test_repository() {
  repository=${1:?repository is required}
  mkdir -p "$repository/scripts" "$repository/bin"
  cp "$TEST_ROOT/scripts/lint-session.sh" "$repository/scripts/lint-session.sh"
  cp "$TEST_ROOT/scripts/setup.sh" "$repository/scripts/setup.sh"
  cp -R "$TEST_ROOT/scripts/hooks" "$repository/scripts/hooks"
  cp "$TEST_ROOT/.custom-gcl.yml" "$repository/.custom-gcl.yml"
  cp "$TEST_ROOT/.golangci.yml" "$repository/.golangci.yml"
  write_fake_mise "$repository/bin/mise"
  git -C "$repository" init --quiet
}

run_setup() {
  repository=${1:?repository is required}
  shift
  case "${1:-}" in
  --hooks-only)
    shift
    MISE_LOG="$repository/mise.log" PATH="$repository/bin:$PATH" \
      sh "$repository/scripts/setup.sh" --hooks-only "$repository" "$@"
    return
    ;;
  esac
  MISE_LOG="$repository/mise.log" PATH="$repository/bin:$PATH" \
    sh "$repository/scripts/setup.sh" "$repository" "$@"
}

assert_installed_hook() {
  repository=${1:?repository is required}
  hook_name=${2:?hook name is required}
  installed="$repository/.git/hooks/$hook_name"
  [[ -x "$installed" ]]
  grep -Fxq '# Managed by pk scripts/setup.sh' "$installed"
}

assert_hook_command() {
  repository=${1:?repository is required}
  hook_name=${2:?hook name is required}
  command=${3:?hook command is required}
  grep -Fxq "$command" "$repository/.git/hooks/$hook_name"
}

assert_setup_commands() {
  repository=${1:?repository is required}
  grep -Fxq install "$repository/mise.log"
  grep -Fxq 'run lint' "$repository/mise.log"
  grep -Fxq 'run lint-legibility-setup' "$repository/mise.log"
  ! grep -Fq lint-shell "$repository/mise.log"
}

assert_setup_fails() {
  repository=${1:?repository is required}
  assert_command_fails run_setup "$repository"
}

assert_command_fails() {
  set +e
  "$@" >/dev/null 2>&1
  status=$?
  set -e
  [[ "$status" -ne 0 ]]
}

assert_agent_hooks_created() {
  repository=${1:?repository is required}
  [[ -f "$repository/.codex/hooks.json" ]]
  [[ -f "$repository/.claude/settings.json" ]]
  grep -Fq 'scripts/lint-session.sh' "$repository/.codex/hooks.json"
  grep -Fq 'scripts/lint-session.sh' "$repository/.claude/settings.json"
}

run_hook() {
  repository=${1:?repository is required}
  hook_name=${2:?hook name is required}
  shift 2
  cd "$repository"
  MISE_LOG="$repository/hook.log" PATH="$repository/bin:$PATH" \
    ".git/hooks/$hook_name" "$@"
}

test_setup_installs_hooks_and_runs_mise() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"

  run_setup "$repository" >/dev/null

  assert_installed_hook "$repository" pre-commit
  assert_installed_hook "$repository" commit-msg
  assert_installed_hook "$repository" pre-push
  assert_hook_command "$repository" pre-commit '  exec mise run hook-pre-commit'
  assert_hook_command "$repository" pre-push '  exec mise run check'
  assert_agent_hooks_created "$repository"
  grep -Fq 'conventional commits format' "$repository/.git/hooks/commit-msg"
  assert_setup_commands "$repository"
  [[ ! -e "$repository/.githooks" ]]
}

test_setup_is_idempotent() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"
  run_setup "$repository" >/dev/null

  run_setup "$repository" >/dev/null

  assert_installed_hook "$repository" pre-commit
  assert_installed_hook "$repository" commit-msg
  assert_installed_hook "$repository" pre-push
}

test_setup_hooks_only_skips_mise() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"

  run_setup "$repository" --hooks-only >/dev/null

  assert_installed_hook "$repository" pre-commit
  assert_agent_hooks_created "$repository"
  [[ ! -e "$repository/mise.log" ]]
}

test_installed_hooks_execute_expected_tasks() {
  repository="$(new_test_repository)"
  message_path="$repository/message.txt"
  create_test_repository "$repository"
  run_setup "$repository" --hooks-only >/dev/null

  run_hook "$repository" pre-commit
  run_hook "$repository" pre-push
  printf 'feat: add setup\n' > "$message_path"
  run_hook "$repository" commit-msg "$message_path"
  printf 'bad header\n' > "$message_path"
  assert_command_fails run_hook "$repository" commit-msg "$message_path"
  grep -Fxq 'run hook-pre-commit' "$repository/hook.log"
  grep -Fxq 'run check' "$repository/hook.log"
}

test_setup_preserves_unmanaged_hooks() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"
  printf '#!/bin/sh\nprintf custom\\n' > "$repository/.git/hooks/commit-msg"

  assert_setup_fails "$repository"

  grep -Fq 'printf custom' "$repository/.git/hooks/commit-msg"
  [[ ! -e "$repository/.git/hooks/pre-commit" ]]
  [[ ! -e "$repository/.git/hooks/pre-push" ]]
}

test_setup_preserves_configured_hooks_path() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"
  git -C "$repository" config core.hooksPath shared-hooks

  assert_setup_fails "$repository"

  [[ ! -e "$repository/shared-hooks/pre-commit" ]]
}

test_setup_merges_existing_claude_settings() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"
  mkdir -p "$repository/.claude"
  printf '{"permissions":{"allow":["Read"]}}\n' > "$repository/.claude/settings.json"

  run_setup "$repository" --hooks-only >/dev/null

  grep -Fq 'scripts/lint-session.sh' "$repository/.claude/settings.json"
  grep -Fq '"permissions"' "$repository/.claude/settings.json"
}

test_setup_uses_local_settings_for_claude_symlink() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"
  mkdir -p "$repository/.claude"
  ln -s "$repository/missing-settings.json" "$repository/.claude/settings.json"
  printf '{"hooks":{"PostToolUse":[]}}\n' > "$repository/.claude/settings.local.json"

  run_setup "$repository" --hooks-only >/dev/null

  [[ -L "$repository/.claude/settings.json" ]]
  grep -Fq 'scripts/lint-session.sh' "$repository/.claude/settings.local.json"
}

test_setup_requires_go_legibility_config() {
  repository="$(new_test_repository)"
  create_test_repository "$repository"
  printf 'version: v2.12.2\n' > "$repository/.custom-gcl.yml"

  assert_setup_fails "$repository"
}

run_test() {
  name=${1:?test name is required}
  set +e
  (set -euo pipefail; "$name")
  status=$?
  set -e
  [ "$status" -eq 0 ] || return_failed_test "$name"
  passed_test "$name"
}

passed_test() {
  name=${1:?test name is required}
  printf 'ok - %s\n' "$name"
}

failed_test() {
  name=${1:?test name is required}
  printf 'not ok - %s\n' "$name" >&2
}

return_failed_test() {
  name=${1:?test name is required}
  failed_test "$name"
  return 1
}

test_names() {
  printf '%s\n' \
    test_setup_installs_hooks_and_runs_mise \
    test_setup_is_idempotent \
    test_setup_hooks_only_skips_mise \
    test_installed_hooks_execute_expected_tasks \
    test_setup_preserves_unmanaged_hooks \
    test_setup_preserves_configured_hooks_path \
    test_setup_merges_existing_claude_settings \
    test_setup_uses_local_settings_for_claude_symlink \
    test_setup_requires_go_legibility_config
}

main() {
  failures=0
  cleanup_test_root
  for test_name in $(test_names); do
    set +e
    run_test "$test_name"
    status=$?
    set -e
    [ "$status" -eq 0 ] || ((failures += 1))
  done

  cleanup_test_root
  [[ "$failures" -eq 0 ]]
}

main "$@"
