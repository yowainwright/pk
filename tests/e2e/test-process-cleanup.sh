#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "$script_dir/../.." && pwd)"
temporary_root="$repository_root/tmp"
suite_dir=
container_image="alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"

cleanup() {
  case "$suite_dir" in
    "$temporary_root"/pk-process-e2e.*) rm -rf -- "$suite_dir" ;;
    *) echo "refusing to remove unexpected path: $suite_dir" >&2 ;;
  esac
}

build_fixtures() {
  local architecture
  architecture="$(go env GOARCH)"
  case "$architecture" in
    amd64|arm64) ;;
    *) echo "unsupported Docker architecture: $architecture" >&2; exit 1 ;;
  esac
  env CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
    go build -ldflags="-s -w -X main.version=v9.8.7-e2e" -o "$suite_dir/pk" ./cmd/pk
  env CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
    go build -tags=e2e -o "$suite_dir/process-reaper" ./tests/e2e/fixture
  env CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
    go build -ldflags="-s -w -X main.version=v9.8.8-e2e" -o "$suite_dir/pk-next" ./cmd/pk
  env CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
    go test -c -tags=e2e -o "$suite_dir/live-tests" ./tests/e2e
  cp "$suite_dir/process-reaper" "$suite_dir/vite"
  cp "$suite_dir/process-reaper" "$suite_dir/Vite"
}

run_process_tests() {
  docker run --rm \
    --volume "$suite_dir:/fixture:ro" \
    --volume "$script_dir/container-process-cleanup.sh:/test.sh:ro" \
    "$container_image" /bin/sh /test.sh
  docker run --rm --init \
    --volume "$suite_dir:/fixture:ro" \
    --env PK_E2E_ISOLATED=1 --env PK_E2E_BINARY=/fixture/pk \
    "$container_image" /fixture/live-tests -test.v -test.run '^TestLive' -test.timeout 3m
}

main() {
  mkdir -p "$temporary_root"
  suite_dir="$(mktemp -d "$temporary_root/pk-process-e2e.XXXXXX")"
  trap cleanup EXIT INT TERM
  cd "$repository_root"
  docker info >/dev/null
  build_fixtures
  run_process_tests
}

main "$@"
