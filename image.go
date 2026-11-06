package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// PruneImages removes unused images with the given label that are older than
// olderThan. The label may be a key or key=value. Containers are left intact.
func (d *Docker) PruneImages(ctx context.Context, label string, olderThan time.Duration) (image.PruneReport, error) {
	label = strings.TrimSpace(label)
	key, _, _ := strings.Cut(label, "=")
	if strings.TrimSpace(key) == "" || olderThan <= 0 {
		return image.PruneReport{}, errors.New("a label and a positive age are required")
	}

	filters := make(client.Filters).
		Add("dangling", "false").
		Add("label", label).
		Add("until", olderThan.String())

	result, err := d.cli.ImagePrune(ctx, client.ImagePruneOptions{Filters: filters})
	if err != nil {
		return image.PruneReport{}, fmt.Errorf("prune images with label %q: %w", label, err)
	}
	return result.Report, nil
}
