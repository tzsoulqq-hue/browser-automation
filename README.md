# browser-automation

浏览器自动化能力服务。

本仓库负责通用浏览器 session、task、profile、proxy 引用和 artifact 生命周期。`gpt-register`、`outlook-register` 等业务仓通过公共契约调用本服务。

## 当前实现

- Go module：`github.com/byte-v-forge/browser-automation`
- 共享私有契约 adapter：`internal/adapters/grpc`
- 核心应用服务：`internal/app`
- 领域模型和端口：`internal/core`
- 内存 store：`internal/app`
- 内部契约：`proto/byte/v/forge/browserautomation/internal/v1/browser_automation_internal.proto`

## 职责

- 私有 session/task/artifact 契约定义在 `internal-contracts/browserautomation`。
- 提供通用浏览器执行能力。
- 通过内部 adapter 管理 Playwright、CDP 或远程浏览器 runtime 细节。
- 根仓历史目录 `browser-reg` 中的可复用 runtime 能力进入本仓；站点执行逻辑回到对应业务仓。
- 使用 secret ref 或 artifact ref 表达 cookie、token、账号密码、验证码、storage state、代理凭据和可复用会话材料。

## 生成

```sh
sh scripts/generate-proto.sh
```

脚本默认从同级 `../internal-contracts` 读取共享私有 proto，再生成本仓内部 proto。
如果单独检出本仓，可显式指定契约仓路径：

```sh
INTERNAL_CONTRACTS_ROOT=/path/to/internal-contracts sh scripts/generate-proto.sh
```

`gen/` 下的生成物是本地构建产物。

## 测试

```sh
GOPRIVATE=github.com/byte-v-forge/* GONOSUMDB=github.com/byte-v-forge/* go mod download
go test ./...
go vet ./...
```

`internal-contracts-go` 是私有依赖；独立检出本仓时需要使用有权限的 Git 凭据，并让 Go 对 `github.com/byte-v-forge/*` 走私有模块解析。

## 迁移范围

- 通用 session/task/profile/artifact 生命周期进入本仓。
- GPT、Outlook 等站点步骤、页面选择器、业务失败语义和账号注册编排留在各自业务仓。
- 本仓对外暴露浏览器能力契约。

## 尚未实现

- 真实 Playwright/CDP/remote-browser runtime adapter。
- artifact 对象存储 adapter。
- PostgreSQL 持久化 adapter 和迁移脚本。
- 进程入口、worker bootstrap 和生命周期事件发布。
