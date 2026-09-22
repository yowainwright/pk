package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yowainwright/pk/internal/audit"
)

type Container struct {
	ID      string
	Name    string
	Image   string
	Command string
	Labels  map[string]string
}

type Report struct {
	Container  Container
	Action     string
	Confidence string
	Reasons    []string
}

type Result struct {
	Report  Report
	Applied bool
	Error   string
}

type Client interface {
	Available() bool
	List(context.Context) ([]Container, error)
	Stop(context.Context, string) error
}

type Recorder interface {
	Record(audit.Event) error
}

type CommandRunner interface {
	LookPath(string) (string, error)
	Output(context.Context, string, ...string) ([]byte, error)
	Run(context.Context, string, ...string) error
}

type CLIClient struct {
	runner CommandRunner
	host   string
}

type execRunner struct{}

func NewClient() *CLIClient {
	return &CLIClient{runner: execRunner{}}
}

func (c *CLIClient) Available() bool {
	_, err := c.runner.LookPath("docker")
	return err == nil
}

func (c *CLIClient) List(ctx context.Context) ([]Container, error) {
	args, err := c.localArgs(ctx, "container", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	output, err := c.runner.Output(ctx, "docker", args...)
	if err != nil {
		return nil, fmt.Errorf("listing docker containers: %w", err)
	}
	return parseContainers(output)
}

func (c *CLIClient) Stop(ctx context.Context, id string) error {
	args, err := c.localArgs(ctx, "container", "stop", id)
	if err != nil {
		return err
	}
	if err := c.runner.Run(ctx, "docker", args...); err != nil {
		return fmt.Errorf("stopping docker container %s: %w", id, err)
	}
	return nil
}

func Reports(containers []Container) []Report {
	reports := make([]Report, 0, len(containers))
	for _, container := range containers {
		report, ok := reportForContainer(container)
		if ok {
			reports = append(reports, report)
		}
	}
	sortReports(reports)
	return reports
}

func Run(ctx context.Context, client Client, recorder Recorder, apply bool) ([]Result, error) {
	if !client.Available() {
		return nil, nil
	}
	containers, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	return runReports(ctx, Reports(containers), client, recorder, apply)
}

func IsDaemonUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := dockerErrorMessage(err)
	return hasDaemonUnavailableMessage(message)
}

func dockerErrorMessage(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr := strings.TrimSpace(string(exitErr.Stderr))
		if stderr != "" {
			return stderr
		}
	}
	return err.Error()
}

func hasDaemonUnavailableMessage(message string) bool {
	message = strings.ToLower(message)
	cannotConnect := strings.Contains(message, "cannot connect to the docker daemon")
	daemonPrompt := strings.Contains(message, "is the docker daemon running")
	daemonStopped := strings.Contains(message, "docker daemon is not running")
	if cannotConnect {
		return true
	}
	if daemonPrompt {
		return true
	}
	return daemonStopped
}

func (c *CLIClient) localArgs(ctx context.Context, args ...string) ([]string, error) {
	if c.host == "" {
		host, err := c.localEndpoint(ctx)
		if err != nil {
			return nil, err
		}
		c.host = host
	}
	return append([]string{"--host", c.host}, args...), nil
}

func (c *CLIClient) localEndpoint(ctx context.Context) (string, error) {
	format := "{{json .Endpoints.docker.Host}}"
	output, err := c.runner.Output(ctx, "docker", "context", "inspect", "--format", format)
	if err != nil {
		return "", fmt.Errorf("inspecting Docker endpoint: %w", err)
	}
	var host string
	if err := json.Unmarshal(output, &host); err != nil {
		return "", fmt.Errorf("decoding Docker endpoint: %w", err)
	}
	if !isLocalEndpoint(host) {
		return "", fmt.Errorf("docker cleanup requires a local Unix socket endpoint")
	}
	return host, nil
}

func isLocalEndpoint(host string) bool {
	endpoint, err := url.Parse(host)
	if err != nil {
		return false
	}
	localSocket := endpoint.Scheme == "unix" && endpoint.Host == ""
	plainPath := endpoint.RawQuery == "" && endpoint.Fragment == ""
	noCredentials := endpoint.User == nil
	valid := localSocket && plainPath && noCredentials && filepath.IsAbs(endpoint.Path)
	return valid
}

func (r execRunner) LookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (r execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (r execRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

type containerRow struct {
	ID      string
	Image   string
	Names   string
	Command string
	Labels  string
}

func parseContainers(output []byte) ([]Container, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	containers := make([]Container, 0)
	for scanner.Scan() {
		container, ok, err := parseContainerLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		if ok {
			containers = append(containers, container)
		}
	}
	return containers, scanner.Err()
}

func parseContainerLine(line []byte) (Container, bool, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return Container{}, false, nil
	}
	var row containerRow
	if err := json.Unmarshal(line, &row); err != nil {
		return Container{}, false, err
	}
	return row.container(), true, nil
}

func (r containerRow) container() Container {
	return Container{
		ID:      r.ID,
		Name:    r.Names,
		Image:   r.Image,
		Command: r.Command,
		Labels:  parseLabels(r.Labels),
	}
}

func parseLabels(value string) map[string]string {
	labels := make(map[string]string)
	for _, part := range strings.Split(value, ",") {
		key, labelValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		hasLabel := ok && key != ""
		if hasLabel {
			labels[key] = labelValue
		}
	}
	return labels
}

const (
	ActionStop     = "stop"
	ConfidenceHigh = "high"
)

func reportForContainer(container Container) (Report, bool) {
	if protected(container) {
		return Report{}, false
	}
	reasons := reasonsForContainer(container)
	if len(reasons) == 0 {
		return Report{}, false
	}
	return Report{
		Container:  container,
		Action:     ActionStop,
		Confidence: ConfidenceHigh,
		Reasons:    reasons,
	}, true
}

func reasonsForContainer(container Container) []string {
	reasons := make([]string, 0, 2)
	if hasComposeLabels(container.Labels) {
		reasons = append(reasons, "compose-container")
	}
	if hasDevContainerLabels(container.Labels) {
		reasons = append(reasons, "devcontainer")
	}
	if hasLocalWorkdir(container.Labels) {
		reasons = append(reasons, "local-workdir")
	}
	return reasons
}

func protected(container Container) bool {
	return container.Labels["pk.protected"] == "true"
}

func hasComposeLabels(labels map[string]string) bool {
	_, hasProject := labels["com.docker.compose.project"]
	_, hasWorkingDir := labels["com.docker.compose.project.working_dir"]
	return hasProject || hasWorkingDir
}

func hasDevContainerLabels(labels map[string]string) bool {
	for key := range labels {
		if strings.Contains(strings.ToLower(key), "devcontainer") {
			return true
		}
	}
	return false
}

func hasLocalWorkdir(labels map[string]string) bool {
	workdir := labels["com.docker.compose.project.working_dir"]
	if workdir == "" {
		workdir = labels["devcontainer.local_folder"]
	}
	return isLocalPath(workdir)
}

func isLocalPath(path string) bool {
	return strings.HasPrefix(path, "/Users/") || strings.HasPrefix(path, "/home/")
}

func sortReports(reports []Report) {
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Container.ID < reports[j].Container.ID
	})
}

func runReports(
	ctx context.Context,
	reports []Report,
	client Client,
	recorder Recorder,
	apply bool,
) ([]Result, error) {
	results := make([]Result, 0, len(reports))
	for _, report := range reports {
		result := runReport(ctx, report, client, apply)
		if err := recordResult(recorder, result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func runReport(ctx context.Context, report Report, client Client, apply bool) Result {
	result := Result{Report: report, Applied: apply}
	if !apply {
		return result
	}
	if err := client.Stop(ctx, report.Container.ID); err != nil {
		result.Error = err.Error()
	}
	return result
}

func recordResult(recorder Recorder, result Result) error {
	if recorder == nil {
		return nil
	}
	event := eventForResult(result)
	if err := recorder.Record(event); err != nil {
		return fmt.Errorf("recording docker cleanup event: %w", err)
	}
	return nil
}

func eventForResult(result Result) audit.Event {
	container := result.Report.Container
	return audit.Event{
		Command:     "cleanup",
		Action:      result.Report.Action,
		TargetType:  "container",
		Applied:     result.Applied,
		Name:        container.Name,
		ContainerID: container.ID,
		Image:       container.Image,
		CommandLine: container.Command,
		Reasons:     result.Report.Reasons,
		Error:       result.Error,
	}
}

func WriteResults(w io.Writer, results []Result) error {
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "CONTAINER\tAPPLIED\tNAME\tIMAGE\tERROR\tREASONS"); err != nil {
		return err
	}
	return writeRows(w, results)
}

func writeRows(w io.Writer, results []Result) error {
	for _, result := range results {
		if err := writeRow(w, result); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, result Result) error {
	container := result.Report.Container
	_, err := fmt.Fprintf(w, "%s\t%t\t%s\t%s\t%s\t%s\n",
		container.ID,
		result.Applied,
		container.Name,
		container.Image,
		result.Error,
		strings.Join(result.Report.Reasons, ","),
	)
	return err
}
