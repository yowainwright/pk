# End-to-end tests

Run the full suite:

```sh
./tests/e2e/test.sh
```

`cli_test.go` builds and executes the public CLI against temporary homes,
audit logs, shell plugin installs, and fake service/Docker executables.

`test-process-cleanup.sh` cross-builds Linux binaries and runs real process
scanning, cleanup, monitoring, daemons, preferences, and audit history inside
Docker. Build artifacts stay under the checkout's `tmp/`. Containers receive
read-only fixture mounts, their own PID namespace, and no Docker socket.

The [live tests](live_test.go) exercise the public CLI through a fixture
[systemctl boundary](fixture/service.go). The fixture validates native command
arguments, starts/stops the real daemon, and gives it a different
`XDG_CONFIG_HOME` from the CLI.

| Behavior | Test |
| --- | --- |
| Saved ignores protect scan/manual cleanup; unignore restores cleanup | `TestLiveSavedIgnoresProtectManualCleanup` |
| Monitor reloads saved names; matching is case-sensitive | `TestLiveMonitorReloadsExactCaseIgnores` |
| Live daemon reload, multiple projects, persistence through restart | `TestLiveIgnoreReloadSurvivesDaemonRestartAcrossProjects` |
| Disable stops the process, removes integration, preserves data; re-enable ignores old sessions | `TestLiveDisablePreservesDataAndReenableOnlyTracksFreshSessions` |
| Corrupt preferences prevent kills; recovery retains queued events | `TestLiveCorruptPreferencesStopCleanupAndRecoveryKeepsQueuedEvents` |
| Replacing a stable binary symlink refreshes the daemon and retains preferences | `TestLiveUpgradeUsesStableEntrypointAndRetainsPreferences` |
| Concurrent CLI updates retain every saved name | `TestConcurrentCLIIgnoresPreserveEverySuccessfulUpdate` in [cli_test.go](cli_test.go) |

Actual OS login/reboot and a real Homebrew package upgrade remain outside this
suite. The binary-upgrade test swaps two compiled versions through a symlink;
it does not run Homebrew. Interactive zsh hook behavior is covered by the
[shell integration tests](../../internal/service/shell_test.go).

Run either layer independently:

```sh
go test -tags=e2e ./tests/e2e -v
./tests/e2e/test-process-cleanup.sh
```
