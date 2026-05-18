# AGENTS.md

- 本仓库按公开仓维护，承载通用浏览器自动化能力、内部契约、runtime adapter 边界和服务实现。
- 公开浏览器自动化契约来自本仓 `proto/byte/v/forge/contracts/browserautomation/v1/`；runtime 私有配置、provider raw metadata 和内部执行状态留在本仓内部 proto。
- session/task/artifact 等对外模型以公开 proto 为源头，应用层直接使用生成类型，避免手写重复模型和大面积字段映射。
- 内部业务命令、状态、runtime 配置、artifact 引用、事件和 raw metadata 也优先使用本仓内部 proto 建模；不要只用 Go struct 作为私有契约源头。
- 浏览器 session 必须持久化；调用方通过 session TTL 管理注册流程生命周期，避免长时间占用。
- 本仓不得沉淀 GPT、Outlook 或其他站点注册业务流程；业务流程留在对应业务仓，通过公共契约调用本仓能力。
- 后端优先使用 Go，按 Clean Code、DI 和面向抽象设计组织代码。
- 引入 Playwright、CDP、浏览器驱动或其他外部 SDK 时必须按官方文档和稳定版本规范开发；无法用 Go 官方稳定 SDK 覆盖的 runtime 可通过明确 adapter/worker 边界隔离。
- `gen/` 承载本仓 proto 生成物，随契约一起提交。
- proto 变更后必须运行生成命令、格式化和 Go 检查。
