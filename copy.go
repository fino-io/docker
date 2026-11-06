package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/moby/go-archive"
	"github.com/moby/moby/client"
)

// CopyFrom copies a file or directory from a container to a local path using
// docker cp's destination path rules. Regular files are staged before replacing
// the destination. A failed directory copy may leave partial results.
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
	if result.Stat.Mode.IsRegular() {
		err = copyRegularFile(ctx, result.Content, info, dst)
	} else {
		err = archive.CopyTo(result.Content, info, dst)
	}
	if err != nil {
		return fmt.Errorf("copy %q to %q: %w", src, dst, err)
	}
	return nil
}

func copyRegularFile(ctx context.Context, content io.Reader, src archive.CopyInfo, dst string) error {
	abs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	info, err := archive.CopyInfoDestinationPath(archive.PreserveTrailingDotOrSeparator(abs, dst))
	if err != nil {
		return err
	}
	target := filepath.Clean(info.Path)
	if info.IsDir {
		_, base := archive.SplitPathDirEntry(src.Path)
		target = filepath.Join(target, base)
	} else if !info.Exists && archive.PreserveTrailingDotOrSeparator(target, info.Path) != target {
		return archive.ErrDirNotExists
	}

	stage, err := os.MkdirTemp(filepath.Dir(target), ".docker-copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	staged := filepath.Join(stage, filepath.Base(target))
	if err := archive.CopyTo(content, src, staged); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(staged, target)
}
