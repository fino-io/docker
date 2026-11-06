package docker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDockerClientsKeepTheirOwnDaemon(t *testing.T) {
	newClient := func(host, status string) *Docker {
		t.Helper()
		transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet || r.URL.Host != host || r.URL.Path != "/v1.47/containers/example/json" {
				return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"State":{"Status":%q}}`, status))),
			}, nil
		})
		d, err := NewDocker(
			client.WithHost("tcp://"+host),
			client.WithHTTPClient(&http.Client{Transport: transport}),
			client.WithVersion("1.47"),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.Close() })
		return d
	}

	first := newClient("first.example:2375", "running")
	second := newClient("second.example:2375", "exited")
	for _, tc := range []struct {
		d    *Docker
		want string
	}{{first, "running"}, {second, "exited"}} {
		got, err := tc.d.Inspect(context.Background(), "example")
		if err != nil || got != tc.want {
			t.Fatalf("Inspect() = %q, %v; want %q", got, err, tc.want)
		}
	}
}
