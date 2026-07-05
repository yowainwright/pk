---
name: pk
description: >
  Use when setting up, running, or maintaining pk local process cleanup,
  background cleanup services, agent-session cleanup, Docker cleanup, or audit
  history.
---

# pk

Use `pk scan` to preview restartable local process targets.
Use `pk cleanup` for a dry-run cleanup record.
Use `pk cleanup --apply` to kill high-confidence process trees and stop matching local containers.
Use `pk cleanup --apply --watch` to run cleanup continuously.
Use `pk install` to install background cleanup for the current user.
Use `pk status` to check the background service.
Use `pk uninstall` to remove the background service.
Use `pk history` to inspect audit events.

## Agent Setup

Install this skill locally:

```bash
pk skills install
```

Then install background cleanup:

```bash
pk install
```

## Agent Loop

1. Run `pk scan`.
2. Run `pk cleanup`.
3. If the targets are expected, run `pk cleanup --apply`.
4. Run `pk history` after cleanup to inspect actions.

Cleanup is local-only. Keep protected long-running tools alive by adding
`-protected` process names or Docker label `pk.protected=true`.
