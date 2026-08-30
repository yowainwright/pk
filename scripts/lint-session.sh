#!/usr/bin/env sh
set -eu

main() {
  repository_root="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
  cd "$repository_root"
  [ -f go.mod ] || exit 0
  [ -f scripts/lint.sh ] || exit 0
  ./scripts/lint.sh --agent "$@"
  printf '{}\n'
}

main "$@"
