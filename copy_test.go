package docker

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFromStagesRegularFiles(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		directory bool
		wantErr   error
		want      string
	}{
		{name: "incomplete archive", body: "ne", wantErr: io.ErrUnexpectedEOF, want: "old"},
		{name: "different destination name", body: "new", want: "new"},
		{name: "existing directory", body: "new", directory: true, want: "new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var content bytes.Buffer
			archive := tar.NewWriter(&content)
			if err := archive.WriteHeader(&tar.Header{Name: "source.txt", Mode: 0644, Size: 3}); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(archive, tc.body); err != nil {
				t.Fatal(err)
			}
			if tc.wantErr == nil {
				if err := archive.Close(); err != nil {
					t.Fatal(err)
				}
			}

			dir := t.TempDir()
			target := filepath.Join(dir, "destination.txt")
			dst := target
			if tc.directory {
				target = filepath.Join(dir, "source.txt")
				dst = dir
			}
			if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
				t.Fatal(err)
			}

			d := newTestDocker(t, "daemon.example:2375", func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1.47/containers/example/archive" {
					return nil, unexpectedRequest(r)
				}
				resp, _ := response(http.StatusOK, content.String())
				stat := base64.StdEncoding.EncodeToString([]byte(`{"name":"source.txt","size":3,"mode":420}`))
				resp.Header.Set("X-Docker-Container-Path-Stat", stat)
				return resp, nil
			})
			if err := d.CopyFrom(t.Context(), "example", "/source.txt", dst); !errors.Is(err, tc.wantErr) {
				t.Fatalf("CopyFrom error = %v, want %v", err, tc.wantErr)
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != tc.want {
				t.Fatalf("destination = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}
