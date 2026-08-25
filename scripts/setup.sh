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
  pre-commit|commit-msg|pre-push) printf '%s\n' "$root/scripts/hooks/$hook_name" ;;
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

install_hooks() {
  root=${1:?repository root is required}
  hooks_path=${2:?hooks path is required}
  mkdir -p "$hooks_path"
  validate_hook "$hooks_path/pre-commit"
  validate_hook "$hooks_path/commit-msg"
  validate_hook "$hooks_path/pre-push"
  install_hook "$root" "$hooks_path" pre-commit
  install_hook "$root" "$hooks_path" commit-msg
  install_hook "$root" "$hooks_path" pre-push
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
}

setup_repository() {
  root=${1:?repository root is required}
  mode=${2:-}
  cd "$root"
  ensure_git_repo "$root"
  reject_configured_hooks_path "$root"
  hooks_path="$(hooks_dir "$root")"
  install_hooks "$root" "$hooks_path"
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
