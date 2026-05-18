# browser-automation

浏览器自动化能力服务。

本仓库负责通用浏览器 session、task、profile、proxy 引用、租约和 artifact 生命周期。`gpt-register`、`outlook-register` 等业务仓通过公开 `contracts/browserautomation` 契约调用本服务。

## 当前实现

- Go module：`github.com/byte-v-forge/browser-automation`
- 公共契约 gRPC adapter：`internal/adapters/grpc`
- 核心应用服务：`internal/app`
- 领域模型和端口：`internal/core`
- 内存 store：`internal/app`
- PostgreSQL store：`internal/adapters/repository/postgres`
- Camoufox runtime adapter：`internal/adapters/runtime/camoufox`
- 数据迁移：`migrations/0001_browser_automation_store.sql`
- 内部契约：`proto/byte/v/forge/browserautomation/internal/v1/browser_automation_internal.proto`
- 同步命令执行：`ExecuteBrowserCommands`，支持 navigate、click、fill、press、wait、extract text、screenshot、upload file、evaluate 等 proto 化命令。

## 职责

- session/task/artifact/lease 契约定义在公开 `contracts/browserautomation`。
- 提供通用浏览器执行能力。
- 调用方执行 task 前先获取 session lease，并在 task input 中携带 `session_lease_token`。
- 通过内部 adapter 管理 Playwright、CDP、远程浏览器或 Camoufox sidecar runtime 细节。
- 根仓历史目录 `browser-reg` 中的可复用 runtime 能力进入本仓；站点执行逻辑回到对应业务仓。
- 使用 secret ref 或 artifact ref 表达 cookie、token、账号密码、验证码、storage state、代理凭据和可复用会话材料。

## 生成

```sh
sh scripts/generate-proto.sh
```

脚本默认从同级 `../contracts` 读取公开 proto，再生成本仓内部 proto。
如果单独检出本仓，可显式指定契约仓路径：

```sh
CONTRACTS_ROOT=/path/to/contracts sh scripts/generate-proto.sh
```

`gen/` 下的生成物是本地构建产物。

## 测试

```sh
GOPRIVATE=github.com/byte-v-forge/* GONOSUMDB=github.com/byte-v-forge/* go mod download
go test ./...
go vet ./...
```

`contracts-go` 提供公开 Go 契约 SDK；独立检出本仓时使用标准 Go module 解析即可。

## 数据库

```sh
psql "$BROWSER_AUTOMATION_POSTGRES_DSN" -f migrations/0001_browser_automation_store.sql
```

PostgreSQL store 会持久化 session、task 和 session lease。lease 的 acquire、renew、release 通过同一条 session 记录的事务更新完成。

## Camoufox Runtime

Camoufox adapter 通过 Python 官方生态启动 remote websocket server，并用常驻 worker 连接 Playwright Firefox endpoint 执行 proto 命令。

参考文档：[Camoufox Remote Server](https://camoufox.com/python/remote-server/)、[Camoufox Usage](https://camoufox.com/python/usage/)、[Playwright Python BrowserType.connect](https://playwright.dev/python/docs/api/class-browsertype#browser-type-connect)。

运行环境需要安装 Camoufox 和 Playwright Python 包：

```sh
python3 -m pip install -U camoufox playwright
```

Go 侧构造 runtime：

```go
runtime, err := camoufox.NewRuntime(camoufox.Config{
	PythonPath:   "python3",
	ArtifactsDir: "/tmp/browser-automation-artifacts",
	Headless:    true,
})
```

`BrowserProfile` 会映射到 Camoufox / Playwright 运行参数：`locale`、`timezone`、`user_agent`、`viewport`、`device_scale_factor`。`labels` 可设置 `camoufox.os`、`camoufox.geoip`、`camoufox.headless`、`camoufox.block_images`、`camoufox.block_webrtc`、`camoufox.block_webgl`、`camoufox.disable_coop`、`camoufox.main_world_eval`、`camoufox.enable_cache`、`camoufox.humanize`。

## 迁移范围

- 通用 session/task/profile/artifact 生命周期进入本仓。
- GPT、Outlook 等站点步骤、页面选择器、业务失败语义和账号注册编排留在各自业务仓。
- 本仓对外暴露浏览器能力服务。

## 后续建设

- artifact 对象存储 adapter。
- 进程入口、worker bootstrap 和生命周期事件发布。
