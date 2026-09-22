package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
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
