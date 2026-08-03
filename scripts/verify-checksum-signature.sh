#!/usr/bin/env sh
set -eu

asset_dir="${1:-dist}"
bundle="$asset_dir/SHA256SUMS.bundle"
checksum="$asset_dir/SHA256SUMS"
github_url="https://github.com"
repository="yowainwright/pk"
workflow_path="$github_url/$repository/.github/workflows/release.yml"
workflow_ref="refs/heads/main"
identity="$workflow_path@$workflow_ref"
oidc_host="token.actions.githubusercontent.com"
issuer="https://$oidc_host"

command -v cosign >/dev/null 2>&1 || {
  printf 'cosign is required\n' >&2
  exit 1
}

test -s "$bundle"
test -s "$checksum"
cosign verify-blob \
  --bundle "$bundle" \
  --certificate-identity "$identity" \
  --certificate-oidc-issuer "$issuer" \
  "$checksum"
