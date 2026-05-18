# browser-automation

浏览器自动化能力服务。

本仓库负责通用浏览器 session、task、profile、proxy 引用和 artifact 生命周期。`gpt-register`、`outlook-register` 等业务仓通过公共契约调用本服务，不把通用浏览器 runtime 写进业务流程仓。

## 当前实现

- Go module：`github.com/byte-v-forge/browser-automation`
- 公共契约 adapter：`internal/adapters/grpc`
- 核心应用服务：`internal/app`
- 领域模型和端口：`internal/core`
- 内存 store：`internal/app`
- 内部契约：`proto/byte/v/forge/browserautomation/internal/v1/browser_automation_internal.proto`

## 契约边界

- 私有 session/task/artifact 契约定义在 `internal-contracts/browserautomation`。
- 本仓不承载 GPT、Outlook 或其他站点注册流程，只提供通用浏览器执行能力。
- Playwright、CDP 或远程浏览器 runtime 细节留在内部 adapter，不进入公共契约。
- cookie、token、账号密码、验证码、storage state、代理凭据和可复用会话材料不得明文进入公共契约或日志，只能使用 secret ref 或 artifact ref。

## 生成

```sh
sh scripts/generate-proto.sh
```

脚本会先生成 `../contracts` 的本地 Go 代码，再生成本仓内部 proto。
`gen/` 下的生成物是本地构建产物，不提交到仓库。

## 测试

```sh
go test ./...
```

## 尚未实现

- 真实 Playwright/CDP/remote-browser runtime adapter。
- artifact 对象存储 adapter。
- PostgreSQL 持久化 adapter 和迁移脚本。
- 进程入口、worker bootstrap 和生命周期事件发布。
