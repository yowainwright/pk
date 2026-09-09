#!/usr/bin/env bash
set -euo pipefail

readonly PK_PUBLISH_WORKFLOW="release.yml"
readonly PK_GITHUB_REPOSITORY="yowainwright/pk"

release_color_enabled() {
  local descriptor="${1:?}"
  [[ -t "$descriptor" ]] && [[ -z "${NO_COLOR:-}" ]] && [[ "${TERM:-}" != "dumb" ]]
}

release_print() {
  local descriptor="${1:?}"
  local color="${2:?}"
  local use_color=false
  shift 2
  release_color_enabled "$descriptor" && use_color=true
  case "$use_color" in
  true) printf '\033[%sm%s\033[0m\n' "$color" "$*" ;;
  *) printf '%s\n' "$*" ;;
  esac
}

release_info() {
  release_print 1 36 "$@"
}

release_success() {
  release_print 1 32 "$@"
}

release_error() {
  release_print 2 31 "$@" >&2
}

release_fail() {
  release_error "$*"
  return 1
}

release_require() {
  command -v "$1" > /dev/null 2>&1 || release_fail "Required command not found: $1"
}

release_require_tools() {
  release_require git || return
  release_require gh || return
  release_require mise || return
}

release_require_repository() {
  [[ "$(git rev-parse --is-inside-work-tree)" == "true" ]] && return 0
  release_fail "Run from a Git repository"
}

release_require_clean_tree() {
  [[ -z "$(git status --porcelain)" ]] && return 0
  release_fail "Working tree must be clean"
}

release_require_clean_main() {
  release_require_repository || return
  release_require_clean_tree || return
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
  gh auth status > /dev/null
  local repository
  repository="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
  [[ "$repository" == "$PK_GITHUB_REPOSITORY" ]] || release_fail "Expected $PK_GITHUB_REPOSITORY, got $repository"
}

release_validate_version() {
  local pattern
  pattern='^v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
  [[ "$1" =~ $pattern ]] && return 0
  release_fail "Version must be v-prefixed v0 SemVer: $1"
}

release_parse_args() {
  PK_RELEASE_DRY_RUN=false
  PK_RELEASE_VERSION=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
    --dry-run) PK_RELEASE_DRY_RUN=true ;;
    v*) PK_RELEASE_VERSION="$1" ;;
    *)
      release_fail "Usage: scripts/release.sh [--dry-run] [v0.x.y[-prerelease]]"
      return
      ;;
    esac
    shift
  done
  [[ -z "$PK_RELEASE_VERSION" ]] || release_validate_version "$PK_RELEASE_VERSION" || return
  readonly PK_RELEASE_DRY_RUN
}

release_current_version() {
  local current
  current="$(git tag --list 'v0.*' --sort=-version:refname | head -n 1)"
  printf '%s' "${current:-v0.0.0}"
}

release_version_parts() {
  local core
  core="${1#v}"
  core="${core%%[-+]*}"
  IFS=. read -r _ PK_RELEASE_MINOR PK_RELEASE_PATCH_NUMBER <<< "$core"
}

release_increment_rc() {
  local version="${1:?}"
  local core="${version%%-rc.*}"
  local suffix="${version##*-rc.}"
  [[ "$version" != "$core" ]] || suffix=0
  [[ "$suffix" =~ ^[0-9]+$ ]] || suffix=0
  printf '%s-rc.%s' "$core" "$((suffix + 1))"
}

release_next_candidate() {
  case "$PK_RELEASE_CURRENT" in
  *-*)
    PK_RELEASE_NEXT="${PK_RELEASE_CURRENT%%-*}"
    PK_RELEASE_RC="$(release_increment_rc "$PK_RELEASE_CURRENT")"
    ;;
  *)
    PK_RELEASE_NEXT="v0.$((PK_RELEASE_MINOR + 1)).0-rc.1"
    PK_RELEASE_RC="$PK_RELEASE_NEXT"
    ;;
  esac
}

release_candidates() {
  PK_RELEASE_CURRENT="$(release_current_version)"
  release_version_parts "$PK_RELEASE_CURRENT"
  release_next_candidate
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
  *)
    release_fail "Unknown version choice: $choice"
    return
    ;;
  esac
  release_validate_version "$PK_RELEASE_VERSION" || return
}

release_require_local_tag_available() {
  ! git rev-parse -q --verify "refs/tags/$PK_RELEASE_VERSION" > /dev/null && return 0
  release_fail "Local tag already exists: $PK_RELEASE_VERSION"
}

release_require_remote_tag_available() {
  local remote_status
  remote_status=0
  git ls-remote --exit-code --tags origin "refs/tags/$PK_RELEASE_VERSION" > /dev/null 2>&1 || remote_status=$?
  case "$remote_status" in
  2) return 0 ;;
  0) release_fail "Remote tag already exists: $PK_RELEASE_VERSION" ;;
  *) release_fail "Could not check remote tag: $PK_RELEASE_VERSION" ;;
  esac
}

release_require_available_version() {
  release_require_local_tag_available || return
  release_require_remote_tag_available || return
  ! gh release view "$PK_RELEASE_VERSION" > /dev/null 2>&1 && return 0
  release_fail "GitHub release already exists: $PK_RELEASE_VERSION"
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
  case "$PK_RELEASE_DRY_RUN" in
  true)
    release_success "Dry run complete; no GitHub state changed"
    return
    ;;
  esac
  release_confirm "Tag, push, and dispatch $PK_RELEASE_VERSION?"
  release_publish
  release_success "Dispatched release workflow for $PK_RELEASE_VERSION"
}

release_dispatch() {
  [[ "${_PK_RELEASE_SOURCED:-false}" != "true" ]] || return 0
  release_main "$@"
}

release_dispatch "$@"
