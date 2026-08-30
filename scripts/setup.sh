#!/usr/bin/env sh
set -eu

managed_marker="# Managed by pk scripts/setup.sh"

fail() {
  message=${1:?message is required}
  printf '%s\n' "$message" >&2
  exit 1
}

log() {
  message=${1:?message is required}
  printf '%s\n' "$message"
}

require_command() {
  command_name=${1:?command name is required}
  command -v "$command_name" >/dev/null 2>&1 && return
  fail "$command_name is required"
}

repository_root() {
  script_dir="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
  dirname "$script_dir"
}

ensure_git_repo() {
  root=${1:?repository root is required}
  git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1 && return
  fail "Run setup from a Git worktree"
}

configured_hooks_path() {
  root=${1:?repository root is required}
  git -C "$root" config --get core.hooksPath 2>/dev/null || return 0
}

reject_configured_hooks_path() {
  root=${1:?repository root is required}
  configured_hooks="$(configured_hooks_path "$root")"
  [ -n "$configured_hooks" ] || return 0
  fail "Refusing to replace configured core.hooksPath: $configured_hooks"
}

hooks_dir() {
  root=${1:?repository root is required}
  git -C "$root" rev-parse --path-format=absolute --git-path hooks
}

managed_hook() {
  path=${1:?hook path is required}
  grep -Fq "$managed_marker" "$path"
}

validate_existing_hook() {
  path=${1:?hook path is required}
  managed_hook "$path" && return
  fail "Refusing to replace unmanaged hook: $path"
}

validate_hook() {
  hook_path=${1:?hook path is required}
  [ -L "$hook_path" ] && fail "Refusing to replace symlink: $hook_path"
  [ -e "$hook_path" ] || return 0
  validate_existing_hook "$hook_path"
}

hook_template() {
  root=${1:?repository root is required}
  hook_name=${2:?hook name is required}
  case "$hook_name" in
  pre-commit|commit-msg) printf '%s\n' "$root/scripts/hooks/$hook_name" ;;
  *) fail "Unknown hook: $hook_name" ;;
  esac
}

install_hook() {
  root=${1:?repository root is required}
  hooks_path=${2:?hooks path is required}
  hook_name=${3:?hook name is required}
  hook_path="$hooks_path/$hook_name"
  temporary_path="$hooks_path/.pk-$hook_name.$$"
  template_path="$(hook_template "$root" "$hook_name")"
  validate_hook "$hook_path"
  cp "$template_path" "$temporary_path"
  chmod 0755 "$temporary_path"
  mv "$temporary_path" "$hook_path"
}

remove_retired_hook() {
  hooks_path=${1:?hooks path is required}
  hook_name=${2:?hook name is required}
  hook_path="$hooks_path/$hook_name"
  [ -e "$hook_path" ] || return 0
  [ ! -L "$hook_path" ] || return 0
  managed_hook "$hook_path" || return 0
  rm -f "$hook_path"
}

install_hooks() {
  root=${1:?repository root is required}
  hooks_path=${2:?hooks path is required}
  mkdir -p "$hooks_path"
  remove_retired_hook "$hooks_path" pre-push
  validate_hook "$hooks_path/pre-commit"
  validate_hook "$hooks_path/commit-msg"
  install_hook "$root" "$hooks_path" pre-commit
  install_hook "$root" "$hooks_path" commit-msg
}

codex_hooks_path() {
  root=${1:?repository root is required}
  printf '%s\n' "$root/.codex/hooks.json"
}

claude_settings_path() {
  root=${1:?repository root is required}
  claude_local_settings_path_if_needed "$root" && return
  printf '%s\n' "$root/.claude/settings.json"
}

claude_local_settings_path_if_needed() {
  root=${1:?repository root is required}
  [ -L "$root/.claude/settings.json" ] || return 1
  printf '%s\n' "$root/.claude/settings.local.json"
}

lint_session_command() {
  # shellcheck disable=SC2016
  printf '%s\n' 'sh \"$(git rev-parse --show-toplevel)/scripts/lint-session.sh\"'
}

claude_lint_session_command() {
  # shellcheck disable=SC2016
  printf '%s\n' 'sh \"$CLAUDE_PROJECT_DIR/scripts/lint-session.sh\"'
}

claude_lint_session_command_raw() {
  # shellcheck disable=SC2016
  printf '%s\n' 'sh "$CLAUDE_PROJECT_DIR/scripts/lint-session.sh"'
}

claude_direct_agent_lint_command_raw() {
  # shellcheck disable=SC2016
  printf '%s\n' 'sh "$CLAUDE_PROJECT_DIR/scripts/lint.sh" --agent'
}

codex_hooks_prefix() {
  printf '%s\n' \
    '{' \
    '  "description": "Strict Go legibility lint for agent edits in this workspace.",' \
    '  "hooks": {' \
    '    "PostToolUse": [' \
    '      {' \
    '        "matcher": "apply_patch|Edit|MultiEdit|Write",' \
    '        "hooks": [' \
    '          {' \
    '            "type": "command",'
}

codex_hooks_suffix() {
  printf '%s\n' \
    '            "command": "'"$command"'",' \
    '            "timeout": 120,' \
    '            "statusMessage": "Checking Go legibility"' \
    '          }' \
    '        ]' \
    '      }' \
    '    ]' \
    '  }' \
    '}'
}

codex_hooks_content() {
  command="$(lint_session_command)"
  codex_hooks_prefix
  codex_hooks_suffix
}

claude_settings_content() {
  command="$(claude_lint_session_command)"
  printf '%s\n' \
    '{' \
    '  "hooks": {' \
    '    "PostToolUse": [' \
    '      {' \
    '        "matcher": "Edit|MultiEdit|Write",' \
    '        "hooks": [' \
    '          {' \
    '            "type": "command",' \
    '            "command": "'"$command"'",' \
    '            "timeout": 120' \
    '          }' \
    '        ]' \
    '      }' \
    '    ]' \
    '  }' \
    '}'
}

claude_settings_remove_filter() {
  # shellcheck disable=SC2016
  printf '%s\n' \
    'def session_hook($current; $direct):' \
    '  [.hooks[]?.command // ""] | any(. == $current or . == $direct);' \
    '.hooks.PostToolUse = ((.hooks.PostToolUse // []) | map(select(session_hook($current; $direct) | not)))'
}

claude_settings_add_filter() {
  # shellcheck disable=SC2016
  printf '%s\n' '.hooks.PostToolUse = ((.hooks.PostToolUse // []) + [$hook])'
}

claude_lint_session_hook_json() {
  command="$(claude_lint_session_command_raw)"
  jq -n --arg command "$command" '{matcher:"Edit|MultiEdit|Write",hooks:[{type:"command",command:$command,timeout:120}]}'
}

file_contains_lint_session() {
  path=${1:?path is required}
  [ -f "$path" ] && grep -Fq 'scripts/lint-session.sh' "$path"
}

file_contains_direct_agent_lint() {
  path=${1:?path is required}
  [ -f "$path" ] && grep -Fq 'scripts/lint.sh' "$path" && grep -Fq -- '--agent' "$path"
}

write_codex_hooks() {
  path=${1:?codex hooks path is required}
  [ -f "$path" ] && file_contains_lint_session "$path" && return
  [ ! -f "$path" ] || file_contains_direct_agent_lint "$path" || fail "Refusing to replace existing Codex hooks: $path"
  mkdir -p "$(dirname "$path")"
  codex_hooks_content > "$path"
}

write_claude_settings() {
  path=${1:?claude settings path is required}
  [ -L "$path" ] && fail "Refusing to replace Claude settings symlink: $path"
  write_new_claude_settings_if_missing "$path"
  file_contains_lint_session "$path" && return
  merge_claude_settings "$path"
}

write_new_claude_settings_if_missing() {
  path=${1:?claude settings path is required}
  [ -f "$path" ] && return
  mkdir -p "$(dirname "$path")"
  claude_settings_content > "$path"
}

merge_claude_settings() {
  path=${1:?claude settings path is required}
  require_command jq
  temporary_path="$path.pk-agent.$$"
  remove_claude_session_hooks "$path" "$temporary_path"
  append_claude_hook "$temporary_path" "$path"
  rm -f "$temporary_path"
}

remove_claude_session_hooks() {
  source_path=${1:?source path is required}
  target_path=${2:?target path is required}
  current_command="$(claude_lint_session_command_raw)"
  direct_command="$(claude_direct_agent_lint_command_raw)"
  filter="$(claude_settings_remove_filter)"
  jq --arg current "$current_command" --arg direct "$direct_command" "$filter" "$source_path" > "$target_path"
}

append_claude_hook() {
  source_path=${1:?source path is required}
  target_path=${2:?target path is required}
  hook_json="$(claude_lint_session_hook_json)"
  filter="$(claude_settings_add_filter)"
  jq --argjson hook "$hook_json" "$filter" "$source_path" > "$target_path"
}

install_agent_hooks() {
  root=${1:?repository root is required}
  write_codex_hooks "$(codex_hooks_path "$root")"
  write_claude_settings "$(claude_settings_path "$root")"
}

has_golangci_legibility_plugin() {
  grep -Fq 'github.com/yowainwright/golangci-lint-legibility' .custom-gcl.yml
}

has_golangci_legibility_linter() {
  grep -Eq '^[[:space:]]+- legibility$' .golangci.yml
}

verify_go_legibility_config() {
  has_golangci_legibility_plugin || fail "Missing golangci-lint-legibility plugin config"
  has_golangci_legibility_linter || fail "Missing golangci legibility linter config"
}

install_mise_tools() {
  require_command mise
  log "Installing mise-managed tools..."
  mise install
}

run_setup_checks() {
  log "Checking repository lint..."
  mise run lint
  log "Checking Go legibility configuration..."
  verify_go_legibility_config
  log "Checking Go legibility binary..."
  mise run lint-legibility-setup
}

setup_repository() {
  root=${1:?repository root is required}
  mode=${2:-}
  cd "$root"
  ensure_git_repo "$root"
  reject_configured_hooks_path "$root"
  hooks_path="$(hooks_dir "$root")"
  install_hooks "$root" "$hooks_path"
  install_agent_hooks "$root"
  log "Installed managed hooks in $hooks_path"
  [ "$mode" = "--hooks-only" ] && return
  install_mise_tools
  run_setup_checks
}

main() {
  mode=""
  root="${1:-}"
  case "$root" in
  --hooks-only)
    mode="--hooks-only"
    root="${2:-}"
    ;;
  esac
  [ -n "$root" ] || root="$(repository_root)"
  setup_repository "$root" "$mode"
  log "Setup complete."
}

main "$@"
