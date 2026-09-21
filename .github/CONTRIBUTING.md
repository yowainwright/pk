# Contributing

## Setup

Install mise, jq, and zsh; on macOS, also install Homebrew.
Docker is needed for process tests and release previews.

<!-- setup commands derived from .mise.toml and scripts/setup.sh -->

```sh
mise run setup
mise run build
```

Setup installs tools and hooks for Git and agents. Restart your agent session afterward.

## Changes

Branch from `main`, keep changes focused, test changed behavior, and update relevant docs.
Before opening a pull request, run:

```sh
mise run check
```

Readability checks warn during normal use and fail for agent edits.
Correctness and formatting errors always fail. See [`.mise.toml`](../.mise.toml) for all tasks.

## Dependency updates

<!-- dependency automation derived from workflows/update.yml and ../.codependencerc -->

[Codependence](workflows/update.yml) opens dependency PRs weekly or on manual dispatch.
Policy lives in [`.codependencerc`](../.codependencerc). Run `codependence` for a local check.
The workflow requires `PR_CREATE_TOKEN` with Contents, Pull requests, and Workflows write access.
The digest-pinned Docker image in the process test script still needs manual updates;
Codependence only scans Dockerfiles here.

## Release

<!-- release commands derived from .mise.toml and scripts/release.sh -->

From a clean `main` synchronized with GitHub:

```sh
mise run release-preview
mise run release
```

The preview does not publish. The release command asks before tagging, pushing, and publishing.

<!-- Homebrew release flow derived from .github/workflows/release.yml and update-homebrew.yml -->

Stable releases open a formula PR in [homebrew-tap](https://github.com/yowainwright/homebrew-tap).
The workflow verifies the signed binaries, generates the formula with the tap's scripts,
and installs and tests it before opening the PR. The PR merges after required checks pass.
Prereleases do not update Homebrew.

`HOMEBREW_TAP_TOKEN` needs Contents and Pull requests write access to the tap.
If the tap update fails, the release stays published. Rerun **Update Homebrew Formula**
with the published tag after fixing the failure.

Report vulnerabilities privately using the [security policy](SECURITY.md).
