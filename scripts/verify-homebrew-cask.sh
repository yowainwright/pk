#!/usr/bin/env sh
set -eu

dist_dir="${1:-dist}"
cask_file="$dist_dir/homebrew/Casks/pk.rb"
expected_platforms=4

test -s "$cask_file"
ruby -c "$cask_file"
checksum_count="$(grep -Ec 'sha256 "[0-9a-f]{64}"' "$cask_file")"
binary_count="$(grep -Ec 'binary "pk-[^"]+", target: "pk"' "$cask_file")"
test "$checksum_count" -eq "$expected_platforms"
test "$binary_count" -eq "$expected_platforms"
