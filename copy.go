package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/moby/go-archive"
	"github.com/moby/moby/client"
)

// CopyFrom copies a file or directory from a container to a local path using
// docker cp's destination path rules.
func (d *Docker) CopyFrom(ctx context.Context, id, src, dst string) error {
	if src == "" || dst == "" {
		return errors.New("source and destination paths are required")
	}

	result, err := d.cli.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: src})
	if err != nil {
		return fmt.Errorf("get %q from container %s: %w", src, id, err)
	}
	defer result.Content.Close()

	info := archive.CopyInfo{Path: src, Exists: true, IsDir: result.Stat.Mode.IsDir()}
	if err := archive.CopyTo(result.Content, info, dst); err != nil {
		return fmt.Errorf("copy %q to %q: %w", src, dst, err)
	}
	return nil
}
