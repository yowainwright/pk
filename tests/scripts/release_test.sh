#!/usr/bin/env bash
set -euo pipefail

TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly TEST_ROOT
_PK_RELEASE_SOURCED=true
source "$TEST_ROOT/scripts/release.sh"

test_equal() {
  [[ "$1" == "$2" ]] || {
    printf 'expected %q, got %q\n' "$2" "$1" >&2
    return 1
  }
}

test_candidates_use_svu() (
  svu() {
    case "$*" in
      current) printf 'v1.2.3' ;;
      'next --always') printf 'v1.3.0' ;;
      patch) printf 'v1.2.4' ;;
      minor) printf 'v1.3.0' ;;
      major) printf 'v2.0.0' ;;
      'next --always --prerelease rc.1') printf 'v1.3.0-rc.1' ;;
    esac
  }
  release_candidates
  test_equal "$PK_RELEASE_CURRENT" "v1.2.3"
  test_equal "$PK_RELEASE_NEXT" "v1.3.0"
  test_equal "$PK_RELEASE_PATCH" "v1.2.4"
  test_equal "$PK_RELEASE_MAJOR" "v2.0.0"
  test_equal "$PK_RELEASE_RC" "v1.3.0-rc.1"
)

test_version_validation() (
  PK_RELEASE_CURRENT="v1.2.3"
  release_validate_version "v2.0.0-rc.1"
  if release_validate_version "2.0.0" >/dev/null 2>&1; then
    return 1
  fi
  if release_validate_version "v1.2.3" >/dev/null 2>&1; then
    return 1
  fi
)

test_dry_run_does_not_dispatch() (
  release_preflight() { :; }
  release_candidates() {
    PK_RELEASE_CURRENT="v1.0.0"
    PK_RELEASE_NEXT="v1.1.0"
    PK_RELEASE_PATCH="v1.0.1"
    PK_RELEASE_MINOR="v1.1.0"
    PK_RELEASE_MAJOR="v2.0.0"
    PK_RELEASE_RC="v1.1.0-rc.1"
  }
  release_preview() { :; }
  release_execute() { return 93; }
  release_main --dry-run <<< "1"
)

test_unknown_option_fails() (
  if release_parse_args --publish >/dev/null 2>&1; then
    return 1
  fi
)

write_fake_cosign() {
  local path="$1"
  local body
  body="printf '%s\\n' \"\$*\" > \"\$COSIGN_LOG\""
  printf '%s\n' '#!/bin/sh' "$body" > "$path"
  chmod +x "$path"
}

test_checksum_signature_policy() (
  test_dir="$(mktemp -d)"
  trap 'rm -rf -- "$test_dir"' EXIT
  asset_dir="$test_dir/assets"
  fake_dir="$test_dir/bin"
  log_path="$test_dir/cosign.log"
  mkdir -p "$asset_dir" "$fake_dir"
  printf 'checksums\n' > "$asset_dir/SHA256SUMS"
  printf 'bundle\n' > "$asset_dir/SHA256SUMS.bundle"
  write_fake_cosign "$fake_dir/cosign"
  COSIGN_LOG="$log_path" PATH="$fake_dir:$PATH" \
    sh "$TEST_ROOT/scripts/verify-checksum-signature.sh" "$asset_dir"
  identity='https://github.com/yowainwright/pk/.github/workflows/release.yml@refs/heads/main'
  grep -Fq -- "--certificate-identity $identity" "$log_path"
  grep -Fq -- '--certificate-oidc-issuer https://token.actions.githubusercontent.com' "$log_path"
)

test_release_pr_dispatches_ci() (
  log_path="$(mktemp)"
  trap 'rm -f -- "$log_path"' EXIT
  gh() {
    printf '%s\n' "$*" >> "$log_path"
    case "$*" in
      'pr view '*) printf 'CLEAN\n' ;;
    esac
  }
  release_latest_run() { printf '10'; }
  release_wait_for_run() { printf '11'; }
  release_watch_run() { :; }
  release_check_pr 42 release-please--branches--main
  expected='workflow run ci.yml --ref release-please--branches--main'
  grep -Fxq "$expected" "$log_path"
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
  test_candidates_use_svu \
  test_version_validation \
  test_dry_run_does_not_dispatch \
  test_unknown_option_fails \
  test_checksum_signature_policy \
  test_release_pr_dispatches_ci; do
  run_test "$test_name" || ((failures += 1))
done

[[ "$failures" -eq 0 ]]
