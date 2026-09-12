#!/usr/bin/env sh
set -eu

formula=${1:?formula path is required}
tag=${2:?release tag is required}
asset_dir=${3:?verified release asset directory is required}

main() {
  test -s "$formula"
  ruby -c "$formula"
  for platform in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
    name="pk-$platform"
    checksum="$(grep -E "^[0-9a-f]{64}  $name$" "$asset_dir/SHA256SUMS" | cut -d ' ' -f 1)"
    test -n "$checksum"
    url="https://github.com/yowainwright/pk/releases/download/$tag/$name"
    grep -F -A 1 "url \"$url\"" "$formula" | grep -Fq "sha256 \"$checksum\""
  done
}

main
