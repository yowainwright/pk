package docker

import (
	"context"
	"os/exec"
)

func (r execRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (r execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (r execRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}
