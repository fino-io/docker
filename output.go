package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

const outputLimit = 1 << 20 // 1 MiB

// Output contains the first 1 MiB of combined stdout and stderr. Truncated
// reports whether more output was received than fits in Bytes.
type Output struct {
	Bytes     []byte
	Truncated bool
}

// ExecResult contains the command exit status and output.
// ExitCode is -1 if the exit status could not be obtained.
type ExecResult struct {
	ExitCode int
	Output   Output
}

// Logs captures combined stdout and stderr in Output. A tail of zero or less
// requests all available lines.
func (d *Docker) Logs(ctx context.Context, id string, tail int) (Output, error) {
	info, err := d.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Output{}, fmt.Errorf("inspect container %s: %w", id, err)
	}
	if info.Container.Config == nil {
		return Output{}, fmt.Errorf("inspect container %s: missing config", id)
	}

	var lines string
	if tail > 0 {
		lines = strconv.Itoa(tail)
	}

	return d.logs(ctx, id, lines, info.Container.Config.Tty)
}

// Exec runs a command without stdin and captures stdout and stderr in Output.
// A nonzero exit code is part of the result.
func (d *Docker) Exec(ctx context.Context, id string, opts client.ExecCreateOptions) (ExecResult, error) {
	result := ExecResult{ExitCode: -1}
	opts.AttachStdin = false
	opts.AttachStdout = true
	opts.AttachStderr = true

	exec, err := d.cli.ExecCreate(ctx, id, opts)
	if err != nil {
		return result, fmt.Errorf("create exec in container %s: %w", id, err)
	}

	stream, err := d.cli.ExecAttach(ctx, exec.ID, client.ExecAttachOptions{
		TTY:         opts.TTY,
		ConsoleSize: opts.ConsoleSize,
	})
	if err != nil {
		return result, fmt.Errorf("attach to exec %s: %w", exec.ID, err)
	}
	defer stream.Close()
	stop := context.AfterFunc(ctx, stream.Close)
	defer stop()

	result.Output, err = readOutput(stream.Reader, opts.TTY)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("read output of exec %s: %w", exec.ID, err)
	}

	info, err := d.cli.ExecInspect(ctx, exec.ID, client.ExecInspectOptions{})
	if err != nil {
		return result, fmt.Errorf("inspect exec %s: %w", exec.ID, err)
	}
	if info.Running {
		return result, fmt.Errorf("exec %s is still running after output closed", exec.ID)
	}

	result.ExitCode = info.ExitCode
	return result, nil
}

func (d *Docker) logs(ctx context.Context, id, tail string, tty bool) (Output, error) {
	stream, err := d.cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		return Output{}, fmt.Errorf("get logs for container %s: %w", id, err)
	}
	defer stream.Close()

	output, err := readOutput(stream, tty)
	if err != nil {
		return output, fmt.Errorf("read logs for container %s: %w", id, err)
	}
	return output, nil
}

func readOutput(stream io.Reader, tty bool) (Output, error) {
	var capture outputCapture
	var err error
	if tty {
		_, err = io.Copy(&capture, stream)
	} else {
		_, err = stdcopy.StdCopy(&capture, &capture, stream)
	}
	return Output{Bytes: capture.buf.Bytes(), Truncated: capture.truncated}, err
}

type outputCapture struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *outputCapture) Write(p []byte) (int, error) {
	n := min(len(p), outputLimit-c.buf.Len())
	_, _ = c.buf.Write(p[:n])
	c.truncated = c.truncated || n < len(p)
	// Keep draining the stream so the command can finish and we can read its exit status.
	return len(p), nil
}
