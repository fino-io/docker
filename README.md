# docker

独立的 Go Docker 客户端封装，沿用 Docker SDK。提供容器运行、执行命令、日志、状态查询、文件复制及镜像清理。

```go
import (
    "context"
    "fmt"

    "github.com/fino-io/docker"
)

func run(ctx context.Context) error {
    d, err := docker.NewDocker()
    if err != nil {
        return err
    }
    defer d.Close()

    _, code, logs, err := d.Run(ctx, "alpine:3", "", []string{"echo", "hello"}, nil)
    if err != nil {
        return err
    }
    if code != 0 {
        return fmt.Errorf("container exited with code %d: %s", code, logs)
    }
    fmt.Print(string(logs))
    return nil
}
```

`NewDocker()` 读取 Docker SDK 支持的环境变量（例如 `DOCKER_HOST`）。可传入 SDK 的 `client.WithHost(...)` 指定 daemon。构造时不连接 daemon；调用容器方法时才发起请求。运行示例需要可访问的 Docker daemon 和本地已有的 `alpine:3` 镜像。

`Run` 和 `Start` 接受由 `docker.Option*` 常量作为键的选项 map；绑定挂载按宿主机路径和容器路径成对传入。文件复制采用 `docker cp` 的目标路径规则。镜像清理由 `d.NewImageCleaner(duration).Clean(ctx)` 显式触发。
