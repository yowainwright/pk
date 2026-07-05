package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/jeffrywainwright/pk/internal/audit"
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
}

type execRunner struct{}

func NewClient() *CLIClient {
	return &CLIClient{runner: execRunner{}}
}

func NewClientWithRunner(runner CommandRunner) *CLIClient {
	return &CLIClient{runner: runner}
}

func (c *CLIClient) Available() bool {
	_, err := c.runner.LookPath("docker")
	return err == nil
}

func (c *CLIClient) List(ctx context.Context) ([]Container, error) {
	output, err := c.runner.Output(ctx, "docker", "container", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("listing docker containers: %w", err)
	}
	return parseContainers(output)
}

func (c *CLIClient) Stop(ctx context.Context, id string) error {
	if err := c.runner.Run(ctx, "docker", "container", "stop", id); err != nil {
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
	message := strings.ToLower(err.Error())
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
