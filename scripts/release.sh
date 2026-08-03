#!/usr/bin/env bash
set -euo pipefail

readonly PK_RELEASE_WORKFLOW="release-please.yml"
readonly PK_PUBLISH_WORKFLOW="release.yml"
readonly PK_CI_WORKFLOW="ci.yml"
readonly PK_GITHUB_REPOSITORY="yowainwright/pk"
readonly PK_RELEASE_POLL_LIMIT=120

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
  release_require svu || return
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

release_candidates() {
  PK_RELEASE_CURRENT="$(svu current)"
  PK_RELEASE_NEXT="$(svu next --always)"
  PK_RELEASE_PATCH="$(svu patch)"
  PK_RELEASE_MINOR="$(svu minor)"
  PK_RELEASE_MAJOR="$(svu major)"
  PK_RELEASE_RC="$(svu next --always --prerelease rc.1)"
  readonly PK_RELEASE_CURRENT PK_RELEASE_NEXT PK_RELEASE_PATCH PK_RELEASE_MINOR PK_RELEASE_MAJOR PK_RELEASE_RC
}

release_menu() {
  printf 'Current: %s\n\n' "$PK_RELEASE_CURRENT"
  printf '  1) %s  recommended\n' "$PK_RELEASE_NEXT"
  printf '  2) %s  patch\n' "$PK_RELEASE_PATCH"
  printf '  3) %s  minor\n' "$PK_RELEASE_MINOR"
  printf '  4) %s  major\n' "$PK_RELEASE_MAJOR"
  printf '  5) %s  release candidate\n' "$PK_RELEASE_RC"
  printf '  6) custom version\n\n'
}

release_custom_version() {
  local value
  read -r -p "Version: " value
  [[ "$value" == v* ]] || value="v$value"
  printf '%s' "$value"
}

release_validate_version() {
  local pattern
  pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
  [[ "$1" =~ $pattern ]] || {
    release_fail "Version must be v-prefixed SemVer: $1"
    return
  }
  [[ "$1" != "$PK_RELEASE_CURRENT" ]] || release_fail "Version matches the current release"
}

release_select_version() {
  local choice
  release_menu
  read -r -p "Choose a version [1]: " choice
  case "${choice:-1}" in
    1) PK_RELEASE_VERSION="$PK_RELEASE_NEXT" ;;
    2) PK_RELEASE_VERSION="$PK_RELEASE_PATCH" ;;
    3) PK_RELEASE_VERSION="$PK_RELEASE_MINOR" ;;
    4) PK_RELEASE_VERSION="$PK_RELEASE_MAJOR" ;;
    5) PK_RELEASE_VERSION="$PK_RELEASE_RC" ;;
    6) PK_RELEASE_VERSION="$(release_custom_version)" ;;
    *) release_fail "Unknown version choice: $choice"; return ;;
  esac
  release_validate_version "$PK_RELEASE_VERSION" || return
  readonly PK_RELEASE_VERSION
}

release_confirm() {
  local answer
  read -r -p "$1 [y/N] " answer
  [[ "$answer" =~ ^[Yy]$ ]] || release_fail "Release canceled"
}

release_preview() {
  release_info "Running the complete release preview"
  mise run release-preview
  release_success "Release preview passed"
}

release_latest_run() {
  local workflow="$1"
  local event="$2"
  local branch="$3"
  local arguments=(run list --workflow "$workflow" --event "$event")
  arguments+=(--branch "$branch" --limit 1 --json databaseId)
  arguments+=(--jq '.[0].databaseId // 0')
  gh "${arguments[@]}"
}

release_wait_for_run() {
  local workflow="$1"
  local previous="$2"
  local event="$3"
  local branch="$4"
  local attempt=0
  local current
  while (( attempt < PK_RELEASE_POLL_LIMIT )); do
    current="$(release_latest_run "$workflow" "$event" "$branch")"
    [[ "$current" != "0" && "$current" != "$previous" ]] && printf '%s' "$current" && return 0
    sleep 5
    ((attempt += 1))
  done
  release_fail "Timed out waiting for $workflow"
}

release_watch_run() {
  release_info "Watching $1"
  gh run watch "$2" --exit-status
}

release_find_pr() {
  local query
  query=".[] | select(.title == \"chore: release ${PK_RELEASE_VERSION#v}\") | [.number, .url, .headRefName] | @tsv"
  gh pr list --state open --limit 20 --json number,title,url,headRefName --jq "$query" | head -n 1
}

release_require_pr() {
  local attempt=0
  local result
  while (( attempt < PK_RELEASE_POLL_LIMIT )); do
    result="$(release_find_pr)"
    [[ -n "$result" ]] && printf '%s' "$result" && return 0
    sleep 5
    ((attempt += 1))
  done
  release_fail "Timed out waiting for the release pull request"
}

release_dispatch_pr() {
  local previous="$1"
  gh workflow run "$PK_RELEASE_WORKFLOW" --ref main --field release_as="${PK_RELEASE_VERSION#v}"
  local run_id
  run_id="$(release_wait_for_run "$PK_RELEASE_WORKFLOW" "$previous" workflow_dispatch main)"
  release_watch_run "release preparation" "$run_id"
}

release_check_pr() {
  local pr_number="$1"
  local head_ref="$2"
  local previous run_id
  previous="$(release_latest_run "$PK_CI_WORKFLOW" workflow_dispatch "$head_ref")"
  gh workflow run "$PK_CI_WORKFLOW" --ref "$head_ref"
  run_id="$(release_wait_for_run "$PK_CI_WORKFLOW" "$previous" workflow_dispatch "$head_ref")"
  release_watch_run "release pull request checks" "$run_id"
  local merge_state
  merge_state="$(gh pr view "$pr_number" --json mergeStateStatus --jq .mergeStateStatus)"
  [[ "$merge_state" == "CLEAN" ]] || release_fail "Release pull request is $merge_state"
}

release_merge_pr() {
  local pr_number="$1"
  release_confirm "Merge $PK_RELEASE_VERSION and publish it?"
  gh pr merge "$pr_number" --squash --delete-branch
}

release_watch_publication() {
  local release_please_before="$1"
  local publish_before="$2"
  local release_please_run
  local publish_run
  release_please_run="$(release_wait_for_run "$PK_RELEASE_WORKFLOW" "$release_please_before" push main)"
  release_watch_run "tag and draft creation" "$release_please_run"
  publish_run="$(release_wait_for_run "$PK_PUBLISH_WORKFLOW" "$publish_before" workflow_dispatch main)"
  release_watch_run "release validation and publication" "$publish_run"
}

release_verify() {
  local draft
  draft="$(gh release view "$PK_RELEASE_VERSION" --json isDraft --jq .isDraft)"
  [[ "$draft" == "false" ]] || {
    release_fail "$PK_RELEASE_VERSION is still a draft"
    return
  }
  gh release view "$PK_RELEASE_VERSION" --json url --jq .url
}

release_execute() {
  local initial_run="$1"
  release_dispatch_pr "$initial_run"
  local pr_line pr_number pr_url head_ref
  pr_line="$(release_require_pr)" || return
  IFS=$'\t' read -r pr_number pr_url head_ref <<< "$pr_line"
  [[ -n "$pr_number" && -n "$head_ref" ]] || {
    release_fail "Release pull request lookup returned no result"
    return
  }
  release_info "Release pull request: $pr_url"
  release_check_pr "$pr_number" "$head_ref"
  local release_please_before publish_before
  release_please_before="$(release_latest_run "$PK_RELEASE_WORKFLOW" push main)"
  publish_before="$(release_latest_run "$PK_PUBLISH_WORKFLOW" workflow_dispatch main)"
  release_merge_pr "$pr_number"
  release_watch_publication "$release_please_before" "$publish_before"
  release_verify
}

release_parse_args() {
  PK_RELEASE_DRY_RUN=false
  case "${1:-}" in
    "") ;;
    --dry-run) PK_RELEASE_DRY_RUN=true ;;
    *) release_fail "Usage: scripts/release.sh [--dry-run]"; return ;;
  esac
  [[ $# -le 1 ]] || release_fail "Usage: scripts/release.sh [--dry-run]"
  readonly PK_RELEASE_DRY_RUN
}

release_main() {
  release_parse_args "$@" || return
  release_preflight || return
  release_candidates || return
  release_select_version || return
  release_preview || return
  release_info "Selected $PK_RELEASE_VERSION"
  if [[ "$PK_RELEASE_DRY_RUN" == "true" ]]; then
    release_success "Dry run complete; no GitHub state changed"
    return
  fi
  release_confirm "Prepare $PK_RELEASE_VERSION?"
  local initial_run
  initial_run="$(release_latest_run "$PK_RELEASE_WORKFLOW" workflow_dispatch main)"
  release_execute "$initial_run"
  release_success "Published $PK_RELEASE_VERSION"
}

if [[ "${_PK_RELEASE_SOURCED:-false}" != "true" ]]; then
  release_main "$@"
fi
