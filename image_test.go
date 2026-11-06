package docker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func TestPruneImagesRequiresLabelAndAge(t *testing.T) {
	requests := 0
	d := newTestDocker(t, "daemon.example:2375", func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1.47/images/prune" {
			return nil, unexpectedRequest(r)
		}
		var filters client.Filters
		if err := json.Unmarshal([]byte(r.URL.Query().Get("filters")), &filters); err != nil {
			return nil, err
		}
		if !filters["label"]["io.fino.demo=true"] || !filters["until"]["24h0m0s"] || !filters["dangling"]["false"] {
			return nil, fmt.Errorf("unexpected prune filters: %v", filters)
		}
		return response(http.StatusOK, `{"SpaceReclaimed":1024}`)
	})

	if _, err := d.PruneImages(t.Context(), "", 24*time.Hour); err == nil || requests != 0 {
		t.Fatalf("unlabelled prune: error = %v, requests = %d", err, requests)
	}
	if _, err := d.PruneImages(t.Context(), "io.fino.demo=true", 0); err == nil || requests != 0 {
		t.Fatalf("prune without age: error = %v, requests = %d", err, requests)
	}

	result, err := d.PruneImages(t.Context(), "io.fino.demo=true", 24*time.Hour)
	if err != nil || result.SpaceReclaimed != 1024 || requests != 1 {
		t.Fatalf("PruneImages() = %+v, %v, requests = %d", result, err, requests)
	}
}
