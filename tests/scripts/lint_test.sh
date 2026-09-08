#!/usr/bin/env bash
set -euo pipefail

TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly TEST_ROOT

write_fake_git() {
  cat > "$fixture/bin/git" << 'EOF'
#!/usr/bin/env sh
case "$*" in
  'rev-parse --show-toplevel') pwd ;;
  'rev-parse --verify '*) exit 0 ;;
  'diff --quiet '*) exit "${SHELL_CHANGED:-1}" ;;
  'diff --name-only '*) printf 'sample.go\n' ;;
  'ls-files --cached '*) printf 'sample.sh\n' ;;
esac
EOF
}

write_fake_go() {
  cat > "$fixture/bin/go" << 'EOF'
#!/usr/bin/env sh
case "$*" in
  'version -m '*) printf 'linter: go1.26.5\n' ;;
  'env GOVERSION') printf 'go1.26.5\n' ;;
  'vet ./...') exit "${VET_STATUS:-0}" ;;
esac
EOF
}

write_fake_shell_linters() {
  cat > "$fixture/bin/shellcheck" << 'EOF'
#!/usr/bin/env sh
exit "${SHELLCHECK_STATUS:-0}"
EOF
  cat > "$fixture/bin/shellcheck-legibility" << 'EOF'
#!/usr/bin/env sh
printf 'shell readability: %s\n' "$*" >&2
case "$*" in
  *--exit-zero*) exit 0 ;;
esac
exit "${SHELL_STYLE_STATUS:-0}"
EOF
}

write_fake_custom_linter() {
  cat > "$fixture/bin/legibility-golangci-lint" << 'EOF'
#!/usr/bin/env sh
case "$*" in
  *--disable=legibility*) exit "${GO_LINT_STATUS:-0}" ;;
  *--enable-only=legibility*)
    printf 'Go readability: %s\n' "$*" >&2
    case "$*" in *--issues-exit-code=0*) exit 0 ;; esac
    exit "${GO_STYLE_STATUS:-0}"
    ;;
esac
EOF
}

write_fake_mise() {
  cat > "$fixture/bin/mise" << 'EOF'
#!/usr/bin/env sh
./scripts/lint.sh --agent
EOF
}

prepare_fixture() {
  mkdir -p "$fixture/scripts/lib" "$fixture/bin/tools/golangci-lint-v2.12.2"
  cp "$TEST_ROOT/scripts/lint.sh" "$TEST_ROOT/scripts/lint-session.sh" "$fixture/scripts/"
  cp "$TEST_ROOT/scripts/.shellcheck-legibility.toml" "$fixture/scripts/"
  cp "$TEST_ROOT/scripts/lib/go-tool.sh" "$fixture/scripts/lib/"
  cp "$TEST_ROOT/.custom-gcl.yml" "$fixture/"
  cd "$fixture"
  cksum .custom-gcl.yml > bin/legibility-golangci-lint.config.cksum
  printf '#!/bin/sh\n' > sample.sh
  touch go.mod
  write_fake_git
  write_fake_go
  write_fake_shell_linters
  write_fake_custom_linter
  write_fake_mise
  cp bin/legibility-golangci-lint bin/tools/golangci-lint-v2.12.2/golangci-lint
  chmod +x bin/* bin/tools/golangci-lint-v2.12.2/golangci-lint
  export PATH="$fixture/bin:$PATH"
}

assert_lint_status() {
  expected=${1:?expected status is required}
  shift
  actual=0
  ./scripts/lint.sh "$@" > "$fixture/stdout" 2> "$fixture/stderr" || actual=$?
  assert_exit_status
}

assert_hook_status() {
  expected=${1:?expected status is required}
  actual=0
  ./scripts/lint-session.sh > "$fixture/stdout" 2> "$fixture/stderr" || actual=$?
  assert_exit_status
}

assert_exit_status() {
  [ "$actual" -eq "$expected" ] && return 0
  cat "$fixture/stderr" >&2
  printf 'Expected status %s, got %s\n' "$expected" "$actual" >&2
  return 1
}

test_general_readability_is_advisory() {
  reset_statuses
  export SHELL_STYLE_STATUS=1 GO_STYLE_STATUS=1
  assert_lint_status 0 --all
  grep -Fq 'shell readability:' "$fixture/stderr"
  grep -Fq 'Go readability:' "$fixture/stderr"
}

test_agent_rejects_shell_readability() {
  reset_statuses
  export SHELL_STYLE_STATUS=1
  assert_lint_status 1 --agent
}

test_agent_rejects_go_readability() {
  reset_statuses
  export GO_STYLE_STATUS=1
  assert_lint_status 1 --agent
}

test_unchanged_shell_readability_is_skipped() {
  reset_statuses
  export SHELL_STYLE_STATUS=1 SHELL_CHANGED=0
  assert_lint_status 0 --agent
  assert_lint_status 1 --agent --all
}

test_correctness_always_fails() {
  reset_statuses
  export SHELLCHECK_STATUS=1
  assert_lint_status 1
  export SHELLCHECK_STATUS=0 VET_STATUS=1
  assert_lint_status 1
  export VET_STATUS=0 GO_LINT_STATUS=1
  assert_lint_status 1
}

test_hook_blocks_and_preserves_diagnostics() {
  reset_statuses
  export GO_STYLE_STATUS=1
  assert_hook_status 2
  grep -Fq 'Go readability:' "$fixture/stderr"
  grep -Fq 'Fix the reported issues' "$fixture/stderr"
  [ ! -s "$fixture/stdout" ]
}

test_hook_success_returns_json() {
  reset_statuses
  assert_hook_status 0
  grep -Fxq '{}' "$fixture/stdout"
}

reset_statuses() {
  unset SHELL_STYLE_STATUS GO_STYLE_STATUS SHELL_CHANGED SHELLCHECK_STATUS VET_STATUS GO_LINT_STATUS
}

run_policy_tests() {
  test_general_readability_is_advisory
  test_agent_rejects_shell_readability
  test_agent_rejects_go_readability
  test_unchanged_shell_readability_is_skipped
  test_correctness_always_fails
  test_hook_blocks_and_preserves_diagnostics
  test_hook_success_returns_json
  printf '%s\n' 'ok - 7 lint policy checks'
}

main() {
  mkdir -p "$TEST_ROOT/tmp/lint-tests"
  fixture="$(mktemp -d "$TEST_ROOT/tmp/lint-tests/case.XXXXXX")"
  trap 'rm -rf -- "$fixture"' EXIT
  prepare_fixture
  run_policy_tests
}

main "$@"
