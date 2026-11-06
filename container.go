package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// RunResult contains the container ID, exit status, and combined stdout and stderr.
// ExitCode is -1 if the exit status could not be obtained.
type RunResult struct {
	ID       string
	ExitCode int64
	Output   []byte
}

// Run starts a container, waits for it to exit, and collects its logs.
// Cleanup is attempted even if waiting or reading the logs fails. Use a context
// deadline to limit the run. A nonzero exit code is part of the result.
func (d *Docker) Run(ctx context.Context, opts client.ContainerCreateOptions) (result RunResult, err error) {
	result.ExitCode = -1
	if opts.HostConfig != nil && opts.HostConfig.AutoRemove {
		return result, errors.New("run manages cleanup; HostConfig.AutoRemove must be false")
	}

	result.ID, err = d.Start(ctx, opts)
	if err != nil {
		return result, err
	}

	defer func() {
		if cleanupErr := d.removeContainer(ctx, result.ID); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	wait := d.cli.ContainerWait(ctx, result.ID, client.ContainerWaitOptions{})
	select {
	case err := <-wait.Error:
		return result, fmt.Errorf("wait for container %s: %w", result.ID, err)
	case status := <-wait.Result:
		if status.Error != nil {
			return result, fmt.Errorf("wait for container %s: %s", result.ID, status.Error.Message)
		}
		result.ExitCode = status.StatusCode
	}

	tty := opts.Config != nil && opts.Config.Tty
	result.Output, err = d.logs(ctx, result.ID, "", tty)
	return result, err
}

// Start creates and starts a container. The caller owns it after a successful
// start. On a start failure, the ID is returned along with the error and cleanup
// is attempted.
func (d *Docker) Start(ctx context.Context, opts client.ContainerCreateOptions) (string, error) {
	created, err := d.cli.ContainerCreate(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if _, err := d.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		startErr := fmt.Errorf("start container %s: %w", created.ID, err)
		if cleanupErr := d.removeContainer(ctx, created.ID); cleanupErr != nil {
			return created.ID, errors.Join(startErr, cleanupErr)
		}
		return created.ID, startErr
	}

	return created.ID, nil
}

func (d *Docker) removeContainer(ctx context.Context, id string) error {
	// Cleanup must still work when the caller's deadline has passed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	_, err := d.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("remove container %s: %w", id, err)
	}
	return nil
}
