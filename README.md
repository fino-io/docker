# docker

数据结构定义在 `proto/docker/types.proto`。修改 Proto 后，在项目根目录运行：

```bash
fino build --targets=api
go test ./...
```

生成的 Go 类型从 `github.com/fino-io/docker/go/docker` 导入。手写库 API 可以放在根目录，命令入口可以放在 `cmd/`。

类型文档生成到 `docs/docker/types.md`。需要安装项目协作 skills 时，运行 `fino create skills`。
