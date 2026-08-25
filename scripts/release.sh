#!/usr/bin/env bash
set -euo pipefail

readonly PK_RELEASE_WORKFLOW="release-please.yml"
readonly PK_PUBLISH_WORKFLOW="release.yml"
readonly PK_CI_WORKFLOW="ci.yml"
readonly PK_CI_REQUIRED_CHECK="Build, Lint, and Test"
readonly PK_GITHUB_REPOSITORY="yowainwright/pk"
readonly PK_RELEASE_POLL_LIMIT=120
readonly PK_RELEASE_PR_POLL_LIMIT=12

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
  while (( attempt < PK_RELEASE_PR_POLL_LIMIT )); do
    result="$(release_find_pr)"
    [[ -n "$result" ]] && printf '%s' "$result" && return 0
    sleep 5
    ((attempt += 1))
  done
  release_error "No release pull request for $PK_RELEASE_VERSION"
  release_fail "release-please found no changelog entries since the last tag"
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
  release_validate_pr_run "$pr_number" "$run_id"
}

release_validate_pr_run() {
  local pr_number="$1"
  local run_id="$2"
  local run_head pr_info pr_head pr_base mergeable
  run_head="$(gh run view "$run_id" --json headSha --jq .headSha)"
  pr_info="$(gh pr view "$pr_number" --json headRefOid,baseRefOid,mergeable \
    --jq '[.headRefOid,.baseRefOid,.mergeable] | @tsv')"
  IFS=$'\t' read -r pr_head pr_base mergeable <<< "$pr_info"
  release_require_tested_head "$run_head" "$pr_head" || return
  [[ "$mergeable" == "MERGEABLE" ]] || {
    release_fail "Release pull request is $mergeable"
    return
  }
  release_require_current_base "$pr_base" "$pr_head" || return
  PK_RELEASE_VALIDATED_HEAD="$pr_head"
  PK_RELEASE_VALIDATED_BASE="$pr_base"
}

release_require_tested_head() {
  local run_head="$1"
  local pr_head="$2"
  [[ -n "$pr_head" && "$run_head" == "$pr_head" ]] || {
    release_fail "Release pull request changed after validation"
    return
  }
}

release_require_current_base() {
  local base="$1"
  local head="$2"
  local comparison
  comparison="$(release_compare_revision "$base" "$head")" || return
  [[ "$comparison" == "ahead" || "$comparison" == "identical" ]] || {
    release_fail "Release pull request does not contain the current base"
    return
  }
}

release_compare_revision() {
  local base head endpoint
  base="$1"
  head="$2"
  endpoint="repos/$PK_GITHUB_REPOSITORY/compare/$base...$head"
  gh api "$endpoint" --jq .status
}

release_review_threads() {
  local owner repository query filter pr_number
  pr_number="$1"
  owner="${PK_GITHUB_REPOSITORY%%/*}"
  repository="${PK_GITHUB_REPOSITORY#*/}"
  query="query(\$owner:String!,\$repository:String!,\$number:Int!){"
  query+="repository(owner:\$owner,name:\$repository){"
  query+="pullRequest(number:\$number){reviewThreads(first:100){"
  query+='nodes{isResolved}pageInfo{hasNextPage}}}}}'
  filter='[.data.repository.pullRequest.reviewThreads.pageInfo.hasNextPage,'
  filter+='([.data.repository.pullRequest.reviewThreads.nodes[]'
  filter+=' | select(.isResolved == false)] | length)] | @tsv'
  gh api graphql -F owner="$owner" -F repository="$repository" \
    -F number="$pr_number" -f query="$query" --jq "$filter"
}

release_require_clear_reviews() {
  local pr_number decision thread_info has_more unresolved
  pr_number="$1"
  decision="$(gh pr view "$pr_number" --json reviewDecision --jq '.reviewDecision // ""')" || return
  [[ -z "$decision" || "$decision" == "APPROVED" ]] || {
    release_fail "Release pull request requires review approval"
    return
  }
  thread_info="$(release_review_threads "$pr_number")" || return
  IFS=$'\t' read -r has_more unresolved <<< "$thread_info"
  [[ "$has_more" == "false" && "$unresolved" == "0" ]] || \
    release_fail "Release pull request has unresolved or unverified conversations"
}

release_required_contexts() {
  local endpoint
  endpoint="repos/$PK_GITHUB_REPOSITORY/branches/main/protection/required_status_checks"
  gh api "$endpoint" --jq '[(.contexts // [])[], (.checks // [])[].context] | unique[]'
}

release_require_no_rulesets() {
  local endpoint count
  endpoint="repos/$PK_GITHUB_REPOSITORY/rules/branches/main"
  count="$(gh api "$endpoint" --jq 'length')" || return
  [[ "$count" == "0" ]] || release_fail "Release pull request is blocked by a ruleset"
}

release_require_no_classic_blockers() {
  local endpoint filter blocked
  endpoint="repos/$PK_GITHUB_REPOSITORY/branches/main/protection"
  filter='[.restrictions != null,'
  filter+='(.required_signatures.enabled // false),'
  filter+='(.lock_branch.enabled // false)] | any'
  blocked="$(gh api "$endpoint" --jq "$filter")" || return
  [[ "$blocked" == "false" ]] || \
    release_fail "Release pull request has unrelated branch protections"
}

release_required_check_rows() {
  local pr_number check_count rows check_status
  pr_number="$1"
  check_status=0
  check_count="$(gh pr view "$pr_number" --json statusCheckRollup \
    --jq '.statusCheckRollup | length')" || return
  [[ "$check_count" != "0" ]] || return
  rows="$(gh pr checks "$pr_number" --required --json name,bucket,event \
    --jq '.[] | [.name,.bucket,.event] | @tsv')" || check_status=$?
  if [[ -z "$rows" && "$check_status" != "0" ]]; then
    release_fail "Could not inspect required checks"
    return
  fi
  printf '%s' "$rows"
}

release_check_state() {
  local expected_name expected_event rows name bucket event state
  expected_name="$1"
  expected_event="$2"
  rows="$3"
  state="missing"
  while IFS=$'\t' read -r name bucket event; do
    [[ "$name" == "$expected_name" ]] || continue
    [[ -z "$expected_event" || "$event" == "$expected_event" ]] || continue
    [[ "$bucket" != "fail" && "$bucket" != "cancel" ]] || {
      printf 'fail'
      return
    }
    [[ "$bucket" != "pending" && "$bucket" != "skipping" ]] || state="pending"
    [[ "$bucket" != "pass" || "$state" != "missing" ]] || state="pass"
  done <<< "$rows"
  printf '%s' "$state"
}

release_require_other_checks() {
  local contexts rows context state
  contexts="$1"
  rows="$2"
  while IFS= read -r context; do
    [[ -n "$context" && "$context" != "$PK_CI_REQUIRED_CHECK" ]] || continue
    state="$(release_check_state "$context" "" "$rows")"
    [[ "$state" == "pass" ]] || {
      release_fail "Required check is not passing: $context"
      return
    }
  done <<< "$contexts"
}

release_require_suppressed_ci() {
  local pr_number contexts rows ci_state
  pr_number="$1"
  contexts="$(release_required_contexts)" || return
  grep -Fxq "$PK_CI_REQUIRED_CHECK" <<< "$contexts" || {
    release_fail "Required CI check is not protected"
    return
  }
  rows="$(release_required_check_rows "$pr_number")" || return
  ci_state="$(release_check_state "$PK_CI_REQUIRED_CHECK" pull_request "$rows")"
  [[ "$ci_state" == "missing" ]] || {
    release_fail "Required CI check was not suppressed"
    return
  }
  release_require_other_checks "$contexts" "$rows"
}

release_require_admin_merge() {
  release_require_clear_reviews "$1" || return
  release_require_no_rulesets || return
  release_require_no_classic_blockers || return
  release_require_suppressed_ci "$1"
}

release_require_validated_revision() {
  local pr_number revision current_head current_base
  pr_number="$1"
  revision="$(gh pr view "$pr_number" --json headRefOid,baseRefOid \
    --jq '[.headRefOid,.baseRefOid] | @tsv')" || return
  IFS=$'\t' read -r current_head current_base <<< "$revision"
  [[ "$current_head" == "$PK_RELEASE_VALIDATED_HEAD" && \
    "$current_base" == "$PK_RELEASE_VALIDATED_BASE" ]] || \
    release_fail "Release pull request or base changed after validation"
}

release_merge_pr() {
  local pr_number="$1"
  local merge_state
  local arguments=(pr merge "$pr_number" --squash --delete-branch)
  [[ -n "${PK_RELEASE_VALIDATED_HEAD:-}" && -n "${PK_RELEASE_VALIDATED_BASE:-}" ]] || {
    release_fail "Release pull request was not validated"
    return
  }
  arguments+=(--match-head-commit "$PK_RELEASE_VALIDATED_HEAD")
  release_confirm "Merge $PK_RELEASE_VERSION and publish it?"
  merge_state="$(gh pr view "$pr_number" --json mergeStateStatus --jq .mergeStateStatus)"
  if [[ "$merge_state" == "BLOCKED" ]]; then
    release_require_admin_merge "$pr_number" || return
    arguments+=(--admin)
  elif [[ "$merge_state" != "CLEAN" ]]; then
    release_fail "Release pull request is $merge_state"
    return
  fi
  release_require_validated_revision "$pr_number" || return
  gh "${arguments[@]}"
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
