package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jeffrywainwright/pk/internal/audit"
)

func TestReportsTargetsComposeContainers(t *testing.T) {
	container := testContainer()

	reports := Reports([]Container{container})

	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}
	assertReason(t, reports[0], "compose-container")
}

func TestReportsSortsByContainerID(t *testing.T) {
	first := testContainer()
	first.ID = "b"
	second := testContainer()
	second.ID = "a"

	reports := Reports([]Container{first, second})

	if reports[0].Container.ID != "a" {
		t.Fatalf("expected sorted reports, got %#v", reports)
	}
}

func TestReportsTargetsDevContainers(t *testing.T) {
	container := testContainer()
	container.Labels = map[string]string{"devcontainer.local_folder": "/Users/me/app"}

	reports := Reports([]Container{container})

	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}
	assertReason(t, reports[0], "devcontainer")
	assertReason(t, reports[0], "local-workdir")
}

func TestReportsSkipsProtectedContainers(t *testing.T) {
	container := testContainer()
	container.Labels["pk.protected"] = "true"

	reports := Reports([]Container{container})

	if len(reports) != 0 {
		t.Fatalf("expected no reports, got %d", len(reports))
	}
}

func TestRunStopsTargetsWhenApplied(t *testing.T) {
	client := &fakeClient{available: true, containers: []Container{testContainer()}}
	recorder := &fakeRecorder{}
	apply := true

	results, err := Run(context.Background(), client, recorder, apply)
	if err != nil {
		t.Fatalf("run docker cleanup: %v", err)
	}
	if client.stoppedID != "abc123" {
		t.Fatalf("expected stopped container, got %q", client.stoppedID)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	assertContainerEvent(t, recorder.events[0])
}

func TestRunDryRunRecordsWithoutStopping(t *testing.T) {
	client := &fakeClient{available: true, containers: []Container{testContainer()}}
	recorder := &fakeRecorder{}
	apply := false

	results, err := Run(context.Background(), client, recorder, apply)
	if err != nil {
		t.Fatalf("run docker cleanup: %v", err)
	}
	if client.stoppedID != "" {
		t.Fatalf("expected no stopped container, got %q", client.stoppedID)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
}

func TestRunReturnsListErrors(t *testing.T) {
	client := &fakeClient{available: true, err: errors.New("denied")}
	apply := true

	_, err := Run(context.Background(), client, nil, apply)

	if err == nil {
		t.Fatal("expected list error")
	}
}

func TestRunReturnsRecorderErrors(t *testing.T) {
	client := &fakeClient{available: true, containers: []Container{testContainer()}}
	recorder := &fakeRecorder{err: errors.New("disk full")}
	apply := false

	_, err := Run(context.Background(), client, recorder, apply)

	if err == nil {
		t.Fatal("expected recorder error")
	}
}

func TestRunSkipsUnavailableDocker(t *testing.T) {
	client := &fakeClient{}
	apply := true

	results, err := Run(context.Background(), client, nil, apply)
	if err != nil {
		t.Fatalf("run docker cleanup: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func TestIsDaemonUnavailableMatchesDockerDaemonErrors(t *testing.T) {
	message := "Cannot connect to the Docker daemon. Is the docker daemon running?"
	err := errors.New(message)

	if !IsDaemonUnavailable(err) {
		t.Fatal("expected daemon unavailable error")
	}
}

func TestIsDaemonUnavailableMatchesDockerStderr(t *testing.T) {
	message := "Cannot connect to the Docker daemon. Is the docker daemon running?"
	err := stderrExitError(t, message)
	wrapped := fmt.Errorf("listing docker containers: %w", err)

	if !IsDaemonUnavailable(wrapped) {
		t.Fatal("expected daemon unavailable error")
	}
}

func TestIsDaemonUnavailableRejectsOtherErrors(t *testing.T) {
	message := "permission denied"
	err := errors.New(message)

	if IsDaemonUnavailable(err) {
		t.Fatal("expected non-daemon error")
	}
}

func TestCLIClientChecksAvailability(t *testing.T) {
	client := NewClientWithRunner(&fakeRunner{available: true})

	if !client.Available() {
		t.Fatal("expected docker available")
	}
}

func TestCLIClientReportsUnavailableDocker(t *testing.T) {
	client := NewClientWithRunner(&fakeRunner{})

	if client.Available() {
		t.Fatal("expected docker unavailable")
	}
}

func TestExecRunnerRunsCommands(t *testing.T) {
	runner := execRunner{}

	if _, err := runner.LookPath("true"); err != nil {
		t.Fatalf("look path: %v", err)
	}
	if _, err := runner.Output(context.Background(), "true"); err != nil {
		t.Fatalf("output true: %v", err)
	}
	if err := runner.Run(context.Background(), "true"); err != nil {
		t.Fatalf("run true: %v", err)
	}
}

func TestCLIClientWrapsListErrors(t *testing.T) {
	runner := &fakeRunner{available: true, err: errors.New("denied")}
	client := NewClientWithRunner(runner)

	_, err := client.List(context.Background())

	if err == nil {
		t.Fatal("expected list error")
	}
}

func TestCLIClientParsesDockerOutput(t *testing.T) {
	runner := &fakeRunner{available: true, output: dockerOutput()}
	client := NewClientWithRunner(runner)

	containers, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if containers[0].Labels["com.docker.compose.project"] != "app" {
		t.Fatalf("expected compose label, got %#v", containers[0].Labels)
	}
}

func TestParseContainersReturnsJSONErrors(t *testing.T) {
	_, err := parseContainers([]byte("not-json\n"))

	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestCLIClientWrapsStopErrors(t *testing.T) {
	runner := &fakeRunner{available: true, err: errors.New("denied")}
	client := NewClientWithRunner(runner)

	err := client.Stop(context.Background(), "abc123")

	if err == nil {
		t.Fatal("expected stop error")
	}
}

func TestWriteResultsIgnoresEmptyResults(t *testing.T) {
	var out bytes.Buffer

	if err := WriteResults(&out, nil); err != nil {
		t.Fatalf("write results: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("expected no output, got %q", out.String())
	}
}

func TestWriteResultsWritesContainerRows(t *testing.T) {
	var out bytes.Buffer
	result := Result{Report: Report{Container: testContainer()}, Applied: true}

	if err := WriteResults(&out, []Result{result}); err != nil {
		t.Fatalf("write results: %v", err)
	}
	if !strings.Contains(out.String(), "CONTAINER\tAPPLIED") {
		t.Fatalf("expected header, got %q", out.String())
	}
}

type fakeClient struct {
	available  bool
	containers []Container
	stoppedID  string
	err        error
}

func (c *fakeClient) Available() bool {
	return c.available
}

func (c *fakeClient) List(ctx context.Context) ([]Container, error) {
	return c.containers, c.err
}

func (c *fakeClient) Stop(ctx context.Context, id string) error {
	c.stoppedID = id
	return c.err
}

type fakeRecorder struct {
	events []audit.Event
	err    error
}

func (r *fakeRecorder) Record(event audit.Event) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

type fakeRunner struct {
	available bool
	output    []byte
	err       error
}

func (r *fakeRunner) LookPath(name string) (string, error) {
	if r.available {
		return "/usr/bin/docker", nil
	}
	return "", errors.New("missing")
}

func (r *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.output, r.err
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	return r.err
}

func stderrExitError(t *testing.T, message string) error {
	t.Helper()
	script := "printf '%s' \"$PK_DOCKER_ERR\" >&2; exit 1"
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "PK_DOCKER_ERR="+message)
	_, err := cmd.Output()
	if err == nil {
		t.Fatal("expected command error")
	}
	return err
}

func testContainer() Container {
	return Container{
		ID:      "abc123",
		Name:    "web",
		Image:   "node:20",
		Command: "node server.js",
		Labels: map[string]string{
			"com.docker.compose.project": "app",
		},
	}
}

func dockerOutput() []byte {
	return []byte(
		`{"ID":"abc123","Image":"node:20","Names":"web","Command":"node server.js","Labels":"com.docker.compose.project=app"}` + "\n",
	)
}

func assertReason(t *testing.T, report Report, reason string) {
	t.Helper()
	for _, current := range report.Reasons {
		if current == reason {
			return
		}
	}
	t.Fatalf("expected reason %q in %#v", reason, report.Reasons)
}

func assertContainerEvent(t *testing.T, event audit.Event) {
	t.Helper()
	if event.TargetType != "container" {
		t.Fatalf("expected container event, got %q", event.TargetType)
	}
	if event.ContainerID != "abc123" {
		t.Fatalf("expected container id, got %q", event.ContainerID)
	}
}
