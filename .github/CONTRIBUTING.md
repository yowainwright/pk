# Contributing

## Setup

Requirements:

- Go and mise versions declared in `.mise.toml`
- Docker for isolated process tests
- GoReleaser and `svu` for release-sensitive changes

```sh
git clone https://github.com/yowainwright/pk.git
cd pk
mise install
mise run build
```

## Workflow

1. Branch from `main`.
2. Keep the change focused.
3. Add tests for behavior and regressions.
4. Update documentation for user-facing changes.
5. Run `mise run check`.
6. Open a pull request.

## Validation

```sh
mise run fmt-check
mise run lint
mise run test
mise run test-e2e
mise run test-process-e2e
mise run security
mise run release-preview
```

Run the release preview for workflow, packaging, versioning, or Homebrew changes.

## Project Constraints

- Preserve preview-first behavior for destructive actions.
- Require explicit `--apply` authorization before changing processes or system services.
- Keep terminal output clear and restrained.
- Do not add runtime dependencies without a concrete portability or correctness need.
- Keep functions small, focused, and covered by tests.

## Security

Do not report vulnerabilities in public issues. Follow the [security policy].

[security policy]: SECURITY.md
