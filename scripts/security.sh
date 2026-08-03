#!/usr/bin/env sh
set -eu

. ./scripts/lib/go-tool.sh

govulncheck_version="${GOVULNCHECK_VERSION:-v1.2.0}"
gosec_version="${GOSEC_VERSION:-v2.25.0}"
gosec_exclude="${GOSEC_EXCLUDE:-G204}"
govulncheck="golang.org/x/vuln/cmd/govulncheck@$govulncheck_version"
gosec="github.com/securego/gosec/v2/cmd/gosec@$gosec_version"
govulncheck_dir="$PWD/bin/security/govulncheck-$govulncheck_version"
gosec_dir="$PWD/bin/security/gosec-$gosec_version"
govulncheck_path="$govulncheck_dir/govulncheck"
gosec_path="$gosec_dir/gosec"

install_go_tool "$govulncheck_path" "$govulncheck"
install_go_tool "$gosec_path" "$gosec"
"$govulncheck_path" ./...
"$gosec_path" -quiet -exclude="$gosec_exclude" ./...
