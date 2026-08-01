#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "$script_dir/../.." && pwd)"

cd "$repository_root"
go test -tags=e2e ./tests/e2e -v
"$script_dir/test-process-cleanup.sh"
