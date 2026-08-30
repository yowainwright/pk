#!/usr/bin/env bash
set -euo pipefail

readonly PK_PUBLISH_WORKFLOW="release.yml"
readonly PK_GITHUB_REPOSITORY="yowainwright/pk"

release_info() {
  if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-}" != "dumb" ]]; then
    printf '\033[36m%s\033[0m\n' "$*"
    return
  fi
  printf '%s\n' "$*"
}

release_success() {
  if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-}" != "dumb" ]]; then
    printf '\033[32m%s\033[0m\n' "$*"
    return
  fi
  printf '%s\n' "$*"
}

release_error() {
  if [[ -t 2 && -z "${NO_COLOR:-}" && "${TERM:-}" != "dumb" ]]; then
    printf '\033[31m%s\033[0m\n' "$*" >&2
    return
  fi
  printf '%s\n' "$*" >&2
}

release_fail() {
  release_error "$*"
  return 1
}

release_require() {
  command -v "$1" >/dev/null 2>&1 || release_fail "Required command not found: $1"
}

release_require_tools() {
  release_require git || return
  release_require gh || return
  release_require mise || return
}

release_require_clean_main() {
  [[ "$(git rev-parse --is-inside-work-tree)" == "true" ]] || {
    release_fail "Run from a Git repository"
    return
  }
  [[ -z "$(git status --porcelain)" ]] || {
    release_fail "Working tree must be clean"
    return
  }
  [[ "$(git branch --show-current)" == "main" ]] || release_fail "Release from main"
}

release_sync_main() {
  git fetch --quiet origin main --tags
  local local_head
  local remote_head
  local_head="$(git rev-parse HEAD)"
  remote_head="$(git rev-parse origin/main)"
  [[ "$local_head" == "$remote_head" ]] || release_fail "Local main must match origin/main"
}

release_preflight() {
  release_require_tools || return
  release_require_clean_main || return
  release_sync_main || return
  gh auth status >/dev/null
  local repository
  repository="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
  [[ "$repository" == "$PK_GITHUB_REPOSITORY" ]] || release_fail "Expected $PK_GITHUB_REPOSITORY, got $repository"
}

release_validate_version() {
  local pattern
  pattern='^v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
  [[ "$1" =~ $pattern ]] || {
    release_fail "Version must be v-prefixed v0 SemVer: $1"
    return
  }
}

release_parse_args() {
  PK_RELEASE_DRY_RUN=false
  PK_RELEASE_VERSION=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dry-run) PK_RELEASE_DRY_RUN=true ;;
      v*) PK_RELEASE_VERSION="$1" ;;
      *) release_fail "Usage: scripts/release.sh [--dry-run] [v0.x.y[-prerelease]]"; return ;;
    esac
    shift
  done
  if [[ -n "$PK_RELEASE_VERSION" ]]; then
    release_validate_version "$PK_RELEASE_VERSION" || return
  fi
  readonly PK_RELEASE_DRY_RUN
}

release_current_version() {
  local current
  current="$(git tag --list 'v0.*' --sort=-version:refname | head -n 1)"
  if [[ -n "$current" ]]; then
    printf '%s' "$current"
    return
  fi
  printf 'v0.0.0'
}

release_version_parts() {
  local core
  core="${1#v}"
  core="${core%%[-+]*}"
  IFS=. read -r PK_RELEASE_MAJOR PK_RELEASE_MINOR PK_RELEASE_PATCH_NUMBER <<< "$core"
}

release_increment_rc() {
  local version="$1"
  local core="${version%%-rc.*}"
  local suffix="${version##*-rc.}"
  if [[ "$version" == "$core" || ! "$suffix" =~ ^[0-9]+$ ]]; then
    printf '%s-rc.1' "$core"
    return
  fi
  printf '%s-rc.%s' "$core" "$((suffix + 1))"
}

release_candidates() {
  PK_RELEASE_CURRENT="$(release_current_version)"
  release_version_parts "$PK_RELEASE_CURRENT"
  if [[ "$PK_RELEASE_CURRENT" == *-* ]]; then
    PK_RELEASE_NEXT="${PK_RELEASE_CURRENT%%-*}"
    PK_RELEASE_RC="$(release_increment_rc "$PK_RELEASE_CURRENT")"
  else
    PK_RELEASE_NEXT="v0.$((PK_RELEASE_MINOR + 1)).0-rc.1"
    PK_RELEASE_RC="$PK_RELEASE_NEXT"
  fi
  PK_RELEASE_PATCH="v0.$PK_RELEASE_MINOR.$((PK_RELEASE_PATCH_NUMBER + 1))"
  PK_RELEASE_MINOR_VERSION="v0.$((PK_RELEASE_MINOR + 1)).0"
  readonly PK_RELEASE_CURRENT PK_RELEASE_NEXT PK_RELEASE_RC
  readonly PK_RELEASE_PATCH PK_RELEASE_MINOR_VERSION
}

release_menu() {
  printf 'Current: %s\n\n' "$PK_RELEASE_CURRENT"
  printf '  1) %s  recommended\n' "$PK_RELEASE_NEXT"
  printf '  2) %s  patch\n' "$PK_RELEASE_PATCH"
  printf '  3) %s  minor\n' "$PK_RELEASE_MINOR_VERSION"
  printf '  4) %s  release candidate\n' "$PK_RELEASE_RC"
  printf '  5) custom version\n\n'
}

release_custom_version() {
  local value
  read -r -p "Version: " value
  [[ "$value" == v* ]] || value="v$value"
  printf '%s' "$value"
}

release_select_version() {
  [[ -z "$PK_RELEASE_VERSION" ]] || return 0
  local choice
  release_candidates
  release_menu
  read -r -p "Choose a version [1]: " choice
  case "${choice:-1}" in
    1) PK_RELEASE_VERSION="$PK_RELEASE_NEXT" ;;
    2) PK_RELEASE_VERSION="$PK_RELEASE_PATCH" ;;
    3) PK_RELEASE_VERSION="$PK_RELEASE_MINOR_VERSION" ;;
    4) PK_RELEASE_VERSION="$PK_RELEASE_RC" ;;
    5) PK_RELEASE_VERSION="$(release_custom_version)" ;;
    *) release_fail "Unknown version choice: $choice"; return ;;
  esac
  release_validate_version "$PK_RELEASE_VERSION" || return
}

release_require_available_version() {
  local remote_status
  if git rev-parse -q --verify "refs/tags/$PK_RELEASE_VERSION" >/dev/null; then
    release_fail "Local tag already exists: $PK_RELEASE_VERSION"
    return
  fi
  remote_status=0
  git ls-remote --exit-code --tags origin "refs/tags/$PK_RELEASE_VERSION" >/dev/null 2>&1 || remote_status=$?
  if [[ "$remote_status" == "0" ]]; then
    release_fail "Remote tag already exists: $PK_RELEASE_VERSION"
    return
  fi
  if [[ "$remote_status" != "2" ]]; then
    release_fail "Could not check remote tag: $PK_RELEASE_VERSION"
    return
  fi
  if gh release view "$PK_RELEASE_VERSION" >/dev/null 2>&1; then
    release_fail "GitHub release already exists: $PK_RELEASE_VERSION"
    return
  fi
}

release_preview() {
  release_info "Running the complete release preview"
  mise run release-preview
  release_success "Release preview passed"
}

release_confirm() {
  local answer
  read -r -p "$1 [y/N] " answer
  [[ "$answer" =~ ^[Yy]$ ]] || release_fail "Release canceled"
}

release_publish() {
  git tag "$PK_RELEASE_VERSION"
  git push origin "$PK_RELEASE_VERSION"
  gh workflow run "$PK_PUBLISH_WORKFLOW" --ref main --field tag_name="$PK_RELEASE_VERSION"
}

release_main() {
  release_parse_args "$@" || return
  release_preflight || return
  release_select_version || return
  release_require_available_version || return
  release_preview || return
  release_info "Selected $PK_RELEASE_VERSION"
  if [[ "$PK_RELEASE_DRY_RUN" == "true" ]]; then
    release_success "Dry run complete; no GitHub state changed"
    return
  fi
  release_confirm "Tag, push, and dispatch $PK_RELEASE_VERSION?"
  release_publish
  release_success "Dispatched release workflow for $PK_RELEASE_VERSION"
}

if [[ "${_PK_RELEASE_SOURCED:-false}" != "true" ]]; then
  release_main "$@"
fi
