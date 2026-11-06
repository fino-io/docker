package docker

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

var demoImageName = regexp.MustCompile(`demo-([\w\-]+)?\.([a-z\d]+)?`)

type ImageCleaner struct {
	cli                  *client.Client
	InvalidDuration      time.Duration
	ForceRemoveContainer bool
}

func (d *Docker) NewImageCleaner(invalidDuration time.Duration) *ImageCleaner {
	return &ImageCleaner{
		cli:                  d.cli,
		InvalidDuration:      invalidDuration,
		ForceRemoveContainer: true,
	}
}

func (i *ImageCleaner) Clean(ctx context.Context) error {
	images, err := i.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list the docker images, %w", err)
	}

	for _, img := range images {
		imageCreated := time.Unix(img.Created, 0)
		if len(img.RepoTags) == 1 && demoImageName.MatchString(img.RepoTags[0]) && i.imageNeedRemove(ctx, img.ID, imageCreated) {
			resItem, err := i.cli.ImageRemove(ctx, img.ID, image.RemoveOptions{Force: true, PruneChildren: true})
			if err != nil {
				slog.Warn("failed to remove the image", "id", img.ID, "name", img.RepoTags[0], "error", err)
			} else {
				slog.Info(" removed the image", "id", img.ID, "name", img.RepoTags[0], "created", imageCreated)
				for i, value := range resItem {
					slog.Info(" removed the image response item", "no", i, "deleted", value.Deleted, "untagged", value.Untagged)
				}
			}
		}
	}
	return nil
}

func (i *ImageCleaner) imageNeedRemove(ctx context.Context, imageId string, created time.Time) bool {
	duration := time.Since(created)
	if duration < i.InvalidDuration {
		return false
	}

	return !i.hasValidContainers(ctx, imageId)
}

func (i *ImageCleaner) hasValidContainers(ctx context.Context, imageId string) bool {
	containers, err := i.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		slog.Warn("failed to list the docker containers", "imageId", imageId, "error", err)
		return true
	}

	var removingContainers []string
	for _, c := range containers {
		if c.ImageID != imageId {
			continue
		}

		if c.State == "running" {
			return true
		}

		created := time.Unix(c.Created, 0)
		if time.Since(created) > i.InvalidDuration {
			removingContainers = append(removingContainers, c.ID)
		} else {
			return true
		}
	}

	if i.ForceRemoveContainer {
		allRemoved := true
		for _, id := range removingContainers {
			if err = i.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
				allRemoved = false
				slog.Warn("failed to remove the container", "id", id, "error", err)
			}
		}
		if !allRemoved {
			return true
		}
	}

	return false
}
