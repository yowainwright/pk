#!/usr/bin/env sh
set -eu

asset_dir="${1:-dist}"
expected_platforms=4

asset_path() {
  name=${1:?asset name is required}
  snapshot=false
  [ -f "$asset_dir/artifacts.json" ] && snapshot=true
  case "$snapshot" in
  true)
    jq -er --arg name "$name" \
      '[.[] | select(.name == $name)] | if length == 1 then .[0].path else error("expected one artifact") end' \
      "$asset_dir/artifacts.json"
    ;;
  false) printf '%s/%s\n' "$asset_dir" "$name" ;;
  esac
}

verify_asset() {
  name=${1:?asset name is required}
  path="$(asset_path "$name")"
  test -s "$path"
  expected="$(grep -E "^[0-9a-f]{64}  $name$" "$asset_dir/SHA256SUMS" | cut -d ' ' -f 1)"
  actual="$(shasum -a 256 "$path" | cut -d ' ' -f 1)"
  [ "$actual" = "$expected" ] && return
  printf 'checksum mismatch: %s\n' "$name" >&2
  return 1
}

main() {
  test -s "$asset_dir/SHA256SUMS"
  test "$(wc -l < "$asset_dir/SHA256SUMS")" -eq "$expected_platforms"
  for platform in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
    verify_asset "pk-$platform"
  done
  printf 'verified %s release binaries\n' "$expected_platforms"
}

main
