#!/usr/bin/env sh
set -eu

managed_marker="# Managed by pk scripts/setup.sh"
script_dir="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
repository_root="$(dirname "$script_dir")"
# shellcheck disable=SC2016
pre_commit_hook='#!/usr/bin/env sh
# Managed by pk scripts/setup.sh
set -eu

repository_root="$(git rev-parse --show-toplevel)"
cd "$repository_root"

if ! command -v mise >/dev/null 2>&1; then
  printf "%s\n" "mise is required to run the pk pre-commit hook" >&2
  exit 1
fi

mise run hook-pre-commit'
# shellcheck disable=SC2016
pre_push_hook='#!/usr/bin/env sh
# Managed by pk scripts/setup.sh
set -eu

repository_root="$(git rev-parse --show-toplevel)"
cd "$repository_root"

if ! command -v mise >/dev/null 2>&1; then
  printf "%s\n" "mise is required to run the pk pre-push hook" >&2
  exit 1
fi

mise run check'

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

validate_hook() {
  hook_name="$1"
  target_path="$hooks_dir/$hook_name"
  [ ! -L "$target_path" ] || fail "Refusing to replace symlink: $target_path"
  if [ -e "$target_path" ] && ! grep -Fq "$managed_marker" "$target_path"; then
    fail "Refusing to replace unmanaged hook: $target_path"
  fi
}

hook_body() {
  case "$1" in
  pre-commit) printf '%s\n' "$pre_commit_hook" ;;
  pre-push) printf '%s\n' "$pre_push_hook" ;;
  *) fail "Unknown hook: $1" ;;
  esac
}

install_hook() {
  hook_name="$1"
  target_path="$hooks_dir/$hook_name"
  temporary_path="$hooks_dir/.pk-$hook_name.$$"
  hook_body "$hook_name" >"$temporary_path"
  chmod 0755 "$temporary_path"
  mv "$temporary_path" "$target_path"
}

git -C "$repository_root" rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  fail "Run setup from a Git worktree"
}

configured_hooks="$(git -C "$repository_root" config --get core.hooksPath || true)"
[ -z "$configured_hooks" ] || fail "Refusing to replace configured core.hooksPath: $configured_hooks"

hooks_dir="$(git -C "$repository_root" rev-parse --path-format=absolute --git-path hooks)"
mkdir -p "$hooks_dir"

for hook_name in pre-commit pre-push; do
  validate_hook "$hook_name"
done
for hook_name in pre-commit pre-push; do
  install_hook "$hook_name"
done

printf 'Installed pre-commit and pre-push hooks in %s\n' "$hooks_dir"
