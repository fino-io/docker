package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestDocker(t *testing.T, host string, handler roundTripFunc) *Docker {
	t.Helper()
	d, err := NewDocker(
		client.WithHost("tcp://"+host),
		client.WithHTTPClient(&http.Client{Transport: handler}),
		client.WithAPIVersion("1.47"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func response(code int, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func unexpectedRequest(r *http.Request) error {
	return fmt.Errorf("unexpected request: %s %s", r.Method, r.URL)
}

func TestDockerClientsKeepTheirOwnDaemon(t *testing.T) {
	newClient := func(host, status string) *Docker {
		return newTestDocker(t, host, func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet || r.URL.Host != host || r.URL.Path != "/v1.47/containers/example/json" {
				return nil, unexpectedRequest(r)
			}
			return response(http.StatusOK, fmt.Sprintf(`{"State":{"Status":%q}}`, status))
		})
	}

	first := newClient("first.example:2375", "running")
	second := newClient("second.example:2375", "exited")

	for _, tc := range []struct {
		d    *Docker
		want string
	}{{first, "running"}, {second, "exited"}} {
		info, err := tc.d.Client().ContainerInspect(t.Context(), "example", client.ContainerInspectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if info.Container.State == nil || string(info.Container.State.Status) != tc.want {
			t.Fatalf("state = %+v; want %q", info.Container.State, tc.want)
		}
	}
}

func TestRunLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tty     bool
		wantOut string
	}{
		{"multiplexed", false, "stdout\nstderr\n"},
		{"tty", true, "\x1b[31moutput\x1b[0m\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "1234567890123456789012345678901234567890123456789012345678901234"
			base := "/v1.47/containers/" + id

			// Docker prefixes each seven-byte payload with its stream and size.
			logStream := "\x01\x00\x00\x00\x00\x00\x00\x07stdout\n" +
				"\x02\x00\x00\x00\x00\x00\x00\x07stderr\n"
			if tc.tty {
				logStream = tc.wantOut
			}

			logsRead, removed := false, false
			d := newTestDocker(t, "daemon.example:2375", func(r *http.Request) (*http.Response, error) {
				switch r.Method + " " + r.URL.Path {
				case "POST /v1.47/containers/create":
					return response(http.StatusCreated, fmt.Sprintf(`{"Id":%q}`, id))
				case "POST " + base + "/start":
					return response(http.StatusNoContent, "")
				case "POST " + base + "/wait":
					return response(http.StatusOK, `{"StatusCode":7}`)
				case "GET " + base + "/logs":
					if q := r.URL.Query(); q.Get("stdout") != "1" || q.Get("stderr") != "1" {
						return nil, fmt.Errorf("expected both log streams: %s", r.URL)
					}
					logsRead = true
					return response(http.StatusOK, logStream)
				case "DELETE " + base:
					if !logsRead {
						return nil, errors.New("container removed before collecting logs")
					}
					removed = true
					return response(http.StatusNoContent, "")
				default:
					return nil, unexpectedRequest(r)
				}
			})

			result, err := d.Run(t.Context(), client.ContainerCreateOptions{
				Image:  "alpine:3",
				Config: &container.Config{Tty: tc.tty},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.ID != id || result.ExitCode != 7 || string(result.Output) != tc.wantOut || !removed {
				t.Fatalf("Run() = %+v, output %q, removed %v", result, result.Output, removed)
			}
		})
	}
}

func TestRunCleansUpAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	removed := false
	d := newTestDocker(t, "daemon.example:2375", func(r *http.Request) (*http.Response, error) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1.47/containers/create":
			return response(http.StatusCreated, `{"Id":"example"}`)
		case "POST /v1.47/containers/example/start":
			return response(http.StatusNoContent, "")
		case "POST /v1.47/containers/example/wait":
			cancel()
			return nil, r.Context().Err()
		case "DELETE /v1.47/containers/example":
			if err := r.Context().Err(); err != nil {
				return nil, err
			}
			if r.URL.Query().Get("force") != "1" {
				return nil, errors.New("cleanup must be able to remove a running container")
			}
			removed = true
			return response(http.StatusNoContent, "")
		default:
			return nil, unexpectedRequest(r)
		}
	})

	result, err := d.Run(ctx, client.ContainerCreateOptions{Image: "alpine:3"})
	if !errors.Is(err, context.Canceled) || result.ID != "example" || result.ExitCode != -1 || !removed {
		t.Fatalf("Run() = %+v, %v, removed %v", result, err, removed)
	}
}
