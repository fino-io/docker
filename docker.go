package docker

import (
	"fmt"

	"github.com/moby/moby/client"
)

// Docker adds common workflows to a client for one Docker daemon.
type Docker struct {
	cli *client.Client
}

// NewDocker reads the Docker environment. Options can override these defaults.
// The caller should close the client when it is no longer needed.
func NewDocker(opts ...client.Opt) (*Docker, error) {
	cli, err := client.New(append([]client.Opt{client.FromEnv}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	return &Docker{cli: cli}, nil
}

// Client returns the underlying Docker SDK client for direct API calls.
func (d *Docker) Client() *client.Client { return d.cli }

func (d *Docker) Close() error { return d.cli.Close() }
