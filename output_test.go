package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func TestExecCancelsDuringAttach(t *testing.T) {
	peers := make(chan net.Conn, 2)
	d, err := NewDocker(
		client.WithHost("tcp://engine.example:2375"),
		client.WithAPIVersion("1.47"),
		client.WithDialContext(func(context.Context, string, string) (net.Conn, error) {
			conn, peer := net.Pipe()
			peers <- peer
			return conn, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := d.Exec(ctx, "example", client.ExecCreateOptions{Cmd: []string{"true"}})
		done <- err
	}()

	receive := func(path string) net.Conn {
		t.Helper()
		select {
		case peer := <-peers:
			t.Cleanup(func() { _ = peer.Close() })
			_ = peer.SetDeadline(time.Now().Add(2 * time.Second))
			req, err := http.ReadRequest(bufio.NewReader(peer))
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				t.Fatal(err)
			}
			if req.URL.Path != path {
				t.Fatalf("request path = %q, want %q", req.URL.Path, path)
			}
			return peer
		case err := <-done:
			t.Fatalf("Exec returned before %s: %v", path, err)
		case <-time.After(2 * time.Second):
			t.Fatalf("no request for %s", path)
		}
		return nil
	}

	create := receive("/v1.47/containers/example/exec")
	body := `{"Id":"exec-id"}`
	if _, err := fmt.Fprintf(create, "HTTP/1.1 201 Created\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body); err != nil {
		t.Fatal(err)
	}
	_ = create.Close()

	// The daemon receives the attach request but never sends the upgrade response.
	receive("/v1.47/exec/exec-id/start")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Exec error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Exec blocked after cancellation during attach")
	}
}
