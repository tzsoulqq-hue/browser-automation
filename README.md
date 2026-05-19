# browser-automation

浏览器自动化能力服务。

本仓库负责通用浏览器 session、task、profile、proxy 引用和 artifact 生命周期。GPT、Outlook 等业务服务通过本仓公开浏览器自动化契约调用本服务。

## 当前实现

- Go module：`github.com/byte-v-forge/browser-automation`
- 公共契约 gRPC adapter：`internal/adapters/grpc`
- 服务入口：`cmd/browser-automation-service`
- 核心应用服务：`internal/app`
- 领域模型和端口：`internal/core`
- 内存 store：`internal/app`
- PostgreSQL store：`internal/adapters/repository/postgres`
- Camoufox runtime adapter：`internal/adapters/runtime/camoufox`
- 数据迁移：`migrations/0001_browser_automation_store.sql`
- 镜像入口：`Dockerfile`
- 内部契约：`proto/byte/v/forge/browserautomation/internal/v1/browser_automation_internal.proto`
- 同步命令执行：`ExecuteBrowserCommands`，支持页面导航、加载等待、选择器等待、键盘输入、鼠标操作、表单交互、元素读取、截图、文件上传和脚本执行等 proto 化命令。
- 命令 runtime 内置点击和输入 fallback，处理页面遮挡、动态重绘和输入框事件派发等通用浏览器细节。
- 命令 runtime 提供 cookie 和 storage state 读取命令，用于账号注册、登录和会话材料捕获等业务流程。

## 职责

- session/task/artifact 契约定义在本仓 `proto/byte/v/forge/contracts/browserautomation/v1/`。
- 提供通用浏览器执行能力。
- 通过内部 adapter 管理 Playwright、CDP、远程浏览器或 Camoufox sidecar runtime 细节。
- 根仓历史目录 `browser-reg` 中的可复用 runtime 能力进入本仓；站点执行逻辑回到对应业务仓。
- 使用 secret ref 或 artifact ref 表达 cookie、token、账号密码、验证码、storage state、代理凭据和可复用会话材料。

## 生成

```sh
sh scripts/generate-proto.sh
```

脚本读取本仓 `proto/` 下的公开契约和内部契约，并生成到 `gen/`。
`gen/` 下的生成物随契约一起提交。

## 检查

```sh
GOPRIVATE=github.com/byte-v-forge/* GONOSUMDB=github.com/byte-v-forge/* go mod download
go build ./...
go vet ./...
```

公开 Go 契约类型来自本仓 `gen/`。

## 运行

```sh
BROWSER_AUTOMATION_POSTGRES_DSN='host=postgres user=byte_v_forge password=byte_v_forge dbname=byte_v_forge port=5432 sslmode=disable' \
BROWSER_AUTOMATION_APPLY_MIGRATIONS=true \
browser-automation-service
```

常用配置：

- `BROWSER_AUTOMATION_LISTEN_ADDR`：gRPC 监听地址，默认 `:50051`。
- `BROWSER_AUTOMATION_POSTGRES_DSN`：session/task 持久化数据库。
- `BROWSER_AUTOMATION_APPLY_MIGRATIONS`：启动时应用本仓迁移。
- `BROWSER_AUTOMATION_MIGRATIONS_DIR`：迁移目录，默认 `migrations`。
- `BROWSER_AUTOMATION_RUNTIME`：runtime 类型，当前值为 `camoufox`。
- `BROWSER_AUTOMATION_ARTIFACTS_DIR`：截图等 artifact 输出目录。
- `BROWSER_AUTOMATION_CAMOUFOX_HEADLESS`：Camoufox headless 模式。
- `BROWSER_AUTOMATION_CAMOUFOX_TASK_TIMEOUT_SECONDS`：单个命令任务默认超时。
- `BROWSER_AUTOMATION_PROXY_REFS_JSON`：服务端代理引用映射，JSON object，例如 `{"register":"socks5://proxy.internal:10813"}`。

## 数据库

```sh
psql "$BROWSER_AUTOMATION_POSTGRES_DSN" -f migrations/0001_browser_automation_store.sql
```

PostgreSQL store 会持久化 session 和 task。

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

`BrowserProfile` 会映射到 Camoufox / Playwright 运行参数：`locale`、`timezone`、`user_agent`、`viewport`、`device_scale_factor`、`extra_http_headers`、`init_scripts`。`proxy_ref` 通过服务端 `BROWSER_AUTOMATION_PROXY_REFS_JSON` 解析为 Camoufox proxy 配置。`labels` 可设置 `camoufox.os`、`camoufox.geoip`、`camoufox.headless`、`camoufox.block_images`、`camoufox.block_webrtc`、`camoufox.block_webgl`、`camoufox.disable_coop`、`camoufox.main_world_eval`、`camoufox.enable_cache`、`camoufox.humanize`。

## 后续建设

- artifact 对象存储 adapter。
- 生命周期事件发布。
