#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "$script_dir/../.." && pwd)"
temporary_root="${TMPDIR:-/tmp}"
suite_dir="$(mktemp -d "$temporary_root/pk-process-e2e.XXXXXX")"
container_image="alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"

cleanup() {
  case "$suite_dir" in
    "$temporary_root"/pk-process-e2e.*) rm -rf -- "$suite_dir" ;;
    *) echo "refusing to remove unexpected path: $suite_dir" >&2 ;;
  esac
}
trap cleanup EXIT INT TERM

architecture="$(go env GOARCH)"
case "$architecture" in
  amd64|arm64) ;;
  *) echo "unsupported Docker architecture: $architecture" >&2; exit 1 ;;
esac

cd "$repository_root"
env CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
  go build -ldflags="-s -w -X main.version=v9.8.7-e2e" -o "$suite_dir/pk" ./cmd/pk
env CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
  go build -tags=e2e -o "$suite_dir/process-reaper" ./tests/e2e/fixture
cp "$suite_dir/process-reaper" "$suite_dir/vite"

docker info >/dev/null
docker run --rm \
  --volume "$suite_dir:/fixture:ro" \
  --volume "$script_dir/container-process-cleanup.sh:/test.sh:ro" \
  "$container_image" /bin/sh /test.sh
