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

test_version_validation_accepts_v0_prerelease() (
  release_validate_version "v0.1.0-rc.1"
)

test_version_validation_rejects_v1() (
  if release_validate_version "v1.0.0" >/dev/null 2>&1; then
    return 1
  fi
)

test_version_validation_requires_v_prefix() (
  if release_validate_version "0.1.0" >/dev/null 2>&1; then
    return 1
  fi
)

test_parse_args_accepts_dry_run_and_version() (
  release_parse_args --dry-run v0.1.0-rc.1
  test_equal "$PK_RELEASE_DRY_RUN" "true"
  test_equal "$PK_RELEASE_VERSION" "v0.1.0-rc.1"
)

test_parse_args_allows_interactive_selection() (
  release_parse_args --dry-run
  test_equal "$PK_RELEASE_DRY_RUN" "true"
  test_equal "$PK_RELEASE_VERSION" ""
)

test_candidates_default_to_first_rc() (
  git() {
    case "$*" in
      'tag --list v0.* --sort=-version:refname') return 0 ;;
    esac
    return 1
  }
  release_candidates
  test_equal "$PK_RELEASE_CURRENT" "v0.0.0"
  test_equal "$PK_RELEASE_NEXT" "v0.1.0-rc.1"
  test_equal "$PK_RELEASE_PATCH" "v0.0.1"
  test_equal "$PK_RELEASE_MINOR_VERSION" "v0.1.0"
)

test_candidates_promote_current_rc() (
  git() {
    case "$*" in
      'tag --list v0.* --sort=-version:refname') printf 'v0.1.0-rc.1\n' ;;
    esac
  }
  release_candidates
  test_equal "$PK_RELEASE_CURRENT" "v0.1.0-rc.1"
  test_equal "$PK_RELEASE_NEXT" "v0.1.0"
  test_equal "$PK_RELEASE_RC" "v0.1.0-rc.2"
)

test_select_version_uses_menu_default() (
  git() {
    case "$*" in
      'tag --list v0.* --sort=-version:refname') return 0 ;;
    esac
    return 1
  }
  PK_RELEASE_VERSION=""
  release_select_version <<< ""
  test_equal "$PK_RELEASE_VERSION" "v0.1.0-rc.1"
)

test_unknown_option_fails() (
  if release_parse_args --publish >/dev/null 2>&1; then
    return 1
  fi
)

test_available_version_rejects_existing_local_tag() (
  git() {
    case "$*" in
      'rev-parse -q --verify refs/tags/v0.1.0') return 0 ;;
    esac
    return 1
  }
  gh() { return 1; }
  PK_RELEASE_VERSION="v0.1.0"
  if release_require_available_version >/dev/null 2>&1; then
    return 1
  fi
)

test_available_version_rejects_existing_github_release() (
  git() { return 1; }
  gh() {
    case "$*" in
      'release view v0.1.0') return 0 ;;
    esac
    return 1
  }
  PK_RELEASE_VERSION="v0.1.0"
  if release_require_available_version >/dev/null 2>&1; then
    return 1
  fi
)

test_available_version_rejects_remote_lookup_error() (
  git() {
    case "$*" in
      'rev-parse -q --verify refs/tags/v0.1.0') return 1 ;;
      'ls-remote --exit-code --tags origin refs/tags/v0.1.0') return 128 ;;
    esac
    return 1
  }
  gh() { return 1; }
  PK_RELEASE_VERSION="v0.1.0"
  if release_require_available_version >/dev/null 2>&1; then
    return 1
  fi
)

test_dry_run_does_not_publish() (
  release_preflight() { :; }
  release_require_available_version() { :; }
  release_preview() { :; }
  release_publish() { return 93; }
  output="$(release_main --dry-run v0.1.0 2>&1)"
  grep -Fq 'Dry run complete; no GitHub state changed' <<< "$output"
)

test_publish_tags_pushes_and_dispatches() (
  log_path="$(mktemp)"
  trap 'rm -f -- "$log_path"' EXIT
  git() { printf 'git %s\n' "$*" >> "$log_path"; }
  gh() { printf 'gh %s\n' "$*" >> "$log_path"; }
  PK_RELEASE_VERSION="v0.1.0-rc.1"
  release_publish
  grep -Fxq "git tag v0.1.0-rc.1" "$log_path"
  grep -Fxq "git push origin v0.1.0-rc.1" "$log_path"
  grep -Fxq "gh workflow run release.yml --ref main --field tag_name=v0.1.0-rc.1" "$log_path"
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
  test_version_validation_accepts_v0_prerelease \
  test_version_validation_rejects_v1 \
  test_version_validation_requires_v_prefix \
  test_parse_args_accepts_dry_run_and_version \
  test_parse_args_allows_interactive_selection \
  test_candidates_default_to_first_rc \
  test_candidates_promote_current_rc \
  test_select_version_uses_menu_default \
  test_unknown_option_fails \
  test_available_version_rejects_existing_local_tag \
  test_available_version_rejects_existing_github_release \
  test_available_version_rejects_remote_lookup_error \
  test_dry_run_does_not_publish \
  test_publish_tags_pushes_and_dispatches \
  test_checksum_signature_policy; do
  run_test "$test_name" || ((failures += 1))
done

[[ "$failures" -eq 0 ]]
