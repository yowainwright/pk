#!/usr/bin/env sh
set -eu

agent_lint_failed() {
  printf '%s\n' 'Agent lint failed. Fix the reported issues before finishing.' >&2
  exit 2
}

main() {
  repository_root="$(git rev-parse --show-toplevel 2> /dev/null)" || exit 0
  cd "$repository_root"
  [ -f go.mod ] || exit 0
  [ -f scripts/lint.sh ] || exit 0
  mise exec -- sh scripts/lint.sh --agent "$@" >&2 || agent_lint_failed
  printf '{}\n'
}

main "$@"
