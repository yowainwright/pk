# End-to-end tests

Run the full suite:

```sh
./tests/e2e/test.sh
```

`cli_test.go` builds and executes the public CLI against temporary homes,
audit logs, skills directories, and fake service/Docker executables.

`test-process-cleanup.sh` cross-builds Linux binaries and runs the real process
scanner, monitor preview, process-tree killer, and audit history inside Docker.
It does not mount the host PID namespace or Docker socket. The only terminated
process is the fixture inside the disposable container.

Run either layer independently:

```sh
go test -tags=e2e ./tests/e2e -v
./tests/e2e/test-process-cleanup.sh
```
