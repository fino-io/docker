# docker

用于 Go 的 Docker Engine 封装。基于官方 Moby SDK，保留容器运行、命令执行、日志读取、文件复制和镜像清理等常用操作。

容器状态、停止、暂停等单步操作可直接调用 `d.Client()` 返回的 SDK 客户端。

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/fino-io/docker"
    "github.com/moby/moby/api/types/container"
    "github.com/moby/moby/client"
)

func run(ctx context.Context) error {
    d, err := docker.NewDocker()
    if err != nil {
        return err
    }
    defer d.Close()

    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    result, err := d.Run(ctx, client.ContainerCreateOptions{
        Image:  "alpine:3",
        Config: &container.Config{Cmd: []string{"echo", "hello"}},
    })
    if err != nil {
        return err
    }

    if result.ExitCode != 0 {
        return fmt.Errorf("container exited with code %d: %s", result.ExitCode, result.Output)
    }

    fmt.Print(string(result.Output))
    return nil
}
```

`NewDocker` 读取 SDK 支持的环境变量（如 `DOCKER_HOST`），也可传入 `client.WithHost(...)`。首次请求会协商 API 版本。

示例需要可访问的 Docker daemon 和本地已有的 `alpine:3` 镜像。

- `Run` 等待容器退出，收集标准输出和标准错误，然后删除容器及其匿名卷。取消或超时后也会尝试清理。容器的非零退出码由调用方处理。
- `Start` 启动后返回完整容器 ID，由调用方管理后续生命周期。环境变量、挂载和资源限制等通过 SDK 的 `Config` 和 `HostConfig` 设置。
- `Exec` 返回命令退出码和合并后的输出；`Logs` 读取已有日志。两者都保留原始输出内容。
- `CopyFrom` 将容器内的文件或目录复制到本地路径，沿用 `docker cp` 的目标路径规则。
- `PruneImages(ctx, "io.fino.demo=true", 24*time.Hour)` 让 daemon 清理超过 24 小时且带指定标签的未使用镜像。只要还有容器引用镜像（包括已停止的容器），就会保留镜像。

旧版的选项 map、状态和生命周期转发方法、按镜像名强制清理，以及业务专用的 Dockerfile 标记和镜像命名工具已移除。

`go test ./...` 可在没有 daemon 的环境中运行。
