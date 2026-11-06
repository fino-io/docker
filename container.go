package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
)

// Docker owns a client for one Docker daemon.
type Docker struct {
	cli *client.Client
}

// NewDocker uses the Docker environment by default. Pass client.WithHost
// to select a daemon explicitly. The caller should close the returned client.
func NewDocker(opts ...client.Opt) (*Docker, error) {
	cli, err := client.NewClientWithOpts(append([]client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	return &Docker{cli: cli}, nil
}

func (d *Docker) Close() error { return d.cli.Close() }

// Run block to run the container, and waiting for stop
func (d *Docker) Run(ctx context.Context, imageName, containerName string, cmd []string, options map[string]any, bindPaths ...string) (string, int64, []byte, error) {
	cfg, hostConfig := createConfig(imageName, cmd, options, bindPaths...)
	resp, err := d.cli.ContainerCreate(ctx, cfg, hostConfig, nil, nil, containerName)
	if err != nil {
		slog.Warn("failed to create the container", "containerName", containerName, "error", err)
		return "", 0, nil, err
	}

	start := time.Now()
	id := SimplifyId(resp.ID)
	slog.Info("run the docker", "containerId", id, "cmd", cmd)

	if err = d.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		slog.Warn("failed to start the container", "containerId", id, "error", err)
		return "", 0, nil, err
	}

	statusCh, errCh := d.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)

	var exitCode int64 = -1
	timeout := 0
	if timeoutOption, ok := options[OptionTimeout].(int); ok {
		timeout = timeoutOption
	}
	if timeout > 0 {
		timeoutTimer := time.NewTimer(time.Duration(timeout) * time.Second)
		select {
		case err = <-errCh:
			timeoutTimer.Stop()
			if err != nil {
				return "", 0, nil, err
			}
		case status := <-statusCh:
			exitCode = status.StatusCode
			timeoutTimer.Stop()
		case <-timeoutTimer.C:
		}
	} else {
		select {
		case err = <-errCh:
			if err != nil {
				return "", 0, nil, err
			}
		case status := <-statusCh:
			exitCode = status.StatusCode
		}
	}

	out, err := d.containerLogs(ctx, id, container.LogsOptions{ShowStdout: true})
	if err != nil {
		slog.Warn("failed to get the logs form container", "containerId", id, "error", err)
		return "", exitCode, nil, err
	}

	if err = d.cli.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
		slog.Warn("failed to stop the container", "containerId", id, "error", err)
		return "", exitCode, out, nil
	}

	if err = d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		slog.Warn("failed to remove the container", "containerId", id, "error", err)
	}

	slog.Info("run docker container successfully", "containerId", id, "imageName", imageName, "containerName", containerName, "exitCode", exitCode, "duration", time.Since(start).String())
	return id, exitCode, out, nil
}

func (d *Docker) Start(ctx context.Context, imageName, containerName string, cmd []string, options map[string]any, bindPaths ...string) (string, error) {
	cfg, hostConfig := createConfig(imageName, cmd, options, bindPaths...)
	resp, err := d.cli.ContainerCreate(ctx, cfg, hostConfig, nil, nil, containerName)
	if err != nil {
		slog.Warn("failed to create the container", "containerName", containerName, "error", err)
		return "", err
	}

	start := time.Now()
	id := SimplifyId(resp.ID)
	slog.Info("start the docker", "containerId", id, "cmd", cmd)

	if err = d.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		slog.Warn("failed to start the container", "containerId", id, "error", err)
		return "", err
	}

	go d.Wait(ctx, id, func(exitCode int64, err error, duration time.Duration) {
		var containerLog string
		kvs := []interface{}{"containerId", id, "imageName", imageName, "containerName", containerName, "cmd", cmd, "exitCode", exitCode, "duration", duration.String()}

		if exitCode > 0 {
			buffer, err := d.Logs(ctx, id, 10)
			if err == nil {
				containerLog = string(buffer)
			}
			kvs = append(kvs, "containerLog", containerLog, "error", err)
		}

		slog.Info("docker container stopped", kvs...)
	})

	slog.Info("start the docker container successfully", "containerId", id, "duration", time.Since(start).String())
	return id, nil
}

func createConfig(imageName string, cmd []string, options map[string]any, bindPaths ...string) (*container.Config, *container.HostConfig) {
	var env []string
	if envOption, ok := options[OptionEnv].([]string); ok {
		env = envOption
	}
	cpuset := ""
	if cpusetOption, ok := options[OptionCpuSet].(string); ok {
		cpuset = cpusetOption
	}

	var ports []string
	if portsOption, ok := options[OptionPorts].([]string); ok {
		ports = portsOption
	}
	workingDir := ""
	if workingDirOption, ok := options[OptionWorkingDir].(string); ok {
		workingDir = workingDirOption
	}

	cfg := &container.Config{
		Image:      imageName,
		Cmd:        cmd,
		Env:        env,
		Tty:        true,
		WorkingDir: workingDir,
	}

	var addHost []string
	if addHostOption, ok := options[OptionAddHost].(string); ok && len(addHostOption) > 0 {
		hs := strings.Split(addHostOption, ",")
		for _, h := range hs {
			addHost = append(addHost, h)
		}
	}

	var networkMode string
	if nw, ok := options[OptionNetwork].(string); ok && len(nw) > 0 {
		networkMode = nw
	}

	var mounts []mount.Mount
	for i := 0; i < len(bindPaths)-1; i += 2 {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: bindPaths[i],
			Target: bindPaths[i+1],
		})
	}

	hostConfig := &container.HostConfig{
		Mounts:      mounts,
		ExtraHosts:  addHost,
		NetworkMode: container.NetworkMode(networkMode),
		Resources: container.Resources{
			CpusetCpus: cpuset,
		},
	}

	if memoryLimit, ok := options[OptionMemoryLimit].(string); ok {
		if ml, err := units.FromHumanSize(memoryLimit); err == nil {
			hostConfig.Resources.Memory = ml
			hostConfig.Resources.MemorySwap = -1 // enable unlimited swap
		}
	}

	pset, pbindings, _ := nat.ParsePortSpecs(ports)
	if len(pbindings) > 0 {
		cfg.ExposedPorts = pset
		hostConfig.PortBindings = pbindings
	}

	return cfg, hostConfig
}

func (d *Docker) Exec(ctx context.Context, id string, cmd []string, env []string, workingDir string) (string, error) {
	resp, err := d.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStderr: true,
		AttachStdout: true,
		Env:          env,
		WorkingDir:   workingDir,
	})
	if err != nil {
		return "", fmt.Errorf("docker exec create %q", err)
	}

	hresp, err := d.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{
		Detach: false,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("docker exec attach %q", err)
	}

	defer hresp.Close()

	var buf bytes.Buffer
	if _, err = buf.ReadFrom(hresp.Reader); err != nil {
		return "", fmt.Errorf("docker exec attach read buffer %q", err)
	}

	return buf.String(), nil
}

// Wait waits until the specified container is not running
func (d *Docker) Wait(ctx context.Context, id string, callback func(exitCode int64, err error, duration time.Duration)) {
	if callback == nil {
		return
	}
	start := time.Now()
	replyC, errC := d.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)

	var response container.WaitResponse
	var err error
	select {
	case response = <-replyC:
		if response.Error != nil {
			err = errors.New(response.Error.Message)
		}
	case err = <-errC:
		slog.Warn("container wait has something wrong", "error", err)
	}

	callback(response.StatusCode, err, time.Since(start))
}

func (d *Docker) Stop(ctx context.Context, id string, timeout int, remove bool) error {
	if id == "" {
		slog.Warn("failed to stop container because of id is empty")
		return nil
	}

	start := time.Now()
	if err := d.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		slog.Warn("failed to stop the container", "containerId", id, "timeout", timeout, "error", err, "remove", remove)
		return err
	}

	slog.Info("stop the container successfully", "containerId", id, "duration", time.Since(start).String())

	if remove {
		start = time.Now()
		if err := d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
			slog.Warn("failed to remove the container", "containerId", id, "error", err)
			return err
		}

		slog.Info("remove the container successfully", "containerId", id, "duration", time.Since(start).String())
	}

	return nil
}

func (d *Docker) Remove(ctx context.Context, id string) error {
	if len(id) == 0 {
		slog.Warn("failed to remove container because of id is empty")
		return nil
	}

	start := time.Now()
	if err := d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		slog.Warn("failed to remove the container", "containerId", id, "error", err)
		return err
	}

	slog.Info("remove the container successfully", "containerId", id, "duration", time.Since(start).String())

	return nil
}

func (d *Docker) Logs(ctx context.Context, id string, tail int) ([]byte, error) {
	options := container.LogsOptions{ShowStdout: true}
	if tail > 0 {
		options.Tail = strconv.Itoa(tail)
	}

	return d.containerLogs(ctx, id, options)
}

func (d *Docker) Inspect(ctx context.Context, id string) (string, error) {
	state, err := d.InspectContainer(ctx, id)
	if err != nil {
		return "", err
	}

	if state.OOMKilled {
		return "", ErrOOMKilled
	}

	return state.Status, err
}

func (d *Docker) Top(ctx context.Context, id string) ([]string, error) {
	result, err := d.cli.ContainerTop(ctx, id, nil)
	if err != nil {
		slog.Warn("failed to inspect container", "containerId", id, "error", err)
		return nil, err
	}

	// Titles: [“PID”, “USER”, “TIME”, “COMMAND”]
	// Processes: [
	//  ["1", "root", "0:01", "/bin/bash"],
	//  ["50", "user", "0:00", "/usr/bin/python"]
	// ]
	commands := make([]string, 0, len(result.Processes))
	for _, ps := range result.Processes {
		if len(ps) > 0 {
			commands = append(commands, strings.Join(ps, " "))
		}
	}

	return commands, nil
}

func (d *Docker) IsExited(ctx context.Context, id string) (bool, error) {
	state, err := d.InspectContainer(ctx, id)
	if err != nil {
		return false, nil
	}

	if state.Status != "exited" {
		return false, nil
	}

	if state.OOMKilled {
		return true, ErrOOMKilled
	}

	if state.ExitCode == 0 {
		return true, nil
	}

	containerLog, _ := d.Logs(ctx, id, 20)
	return true, fmt.Errorf("container stopped unexpectedly, log: %s, exit code: %d", containerLog, state.ExitCode)
}

func (d *Docker) InspectContainer(ctx context.Context, id string) (*types.ContainerState, error) {
	result, err := d.cli.ContainerInspect(ctx, id)
	if err != nil {
		slog.Warn("failed to inspect container", "containerId", id, "error", err)
		return nil, err
	}

	return result.State, nil
}

func (d *Docker) containerLogs(ctx context.Context, id string, options container.LogsOptions) ([]byte, error) {
	out, err := d.cli.ContainerLogs(ctx, id, options)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = out.Close()
	}()

	byt, err := io.ReadAll(out)
	if err != nil {
		return nil, err
	}

	return []byte(stripansi.Strip(string(byt))), nil
}

func (d *Docker) Commit(ctx context.Context, id string, image string) (string, error) {
	if len(id) == 0 || len(image) == 0 {
		slog.Warn("failed to remove container because of id or image is empty")
		return "", nil
	}

	resp, err := d.cli.ContainerCommit(ctx, id, container.CommitOptions{Reference: image})
	if err != nil {
		slog.Warn("failed to commit the container", "containerId", id, "image", image, "error", err)
		return "", err
	}

	return SimplifyId(resp.ID), err
}

// CopyFromContainer copy file or dir from container to the host
func (d *Docker) CopyFromContainer(ctx context.Context, id string, src, dst string) error {
	if id == "" || src == "" || dst == "" {
		return errors.New("container ID, source, and destination are required")
	}
	reader, stat, err := d.cli.CopyFromContainer(ctx, id, src)
	if err != nil {
		return err
	}
	defer reader.Close()
	return archive.CopyTo(reader, archive.CopyInfo{Path: src, Exists: true, IsDir: stat.Mode.IsDir()}, dst)
}

func (d *Docker) Pause(ctx context.Context, id string) error {
	if err := d.cli.ContainerPause(ctx, id); err != nil {
		slog.Warn("failed to pause container", "containerId", id, "error", err)
		return err
	}

	return nil
}

func (d *Docker) Resume(ctx context.Context, id string) error {
	if err := d.cli.ContainerUnpause(ctx, id); err != nil {
		slog.Warn("failed to resume container", "containerId", id, "error", err)
		return err
	}

	return nil
}
