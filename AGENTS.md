# AGENTS.md

- 本仓库只承载通用浏览器自动化能力、内部契约、runtime adapter 边界和服务实现。
- 公共浏览器自动化契约来自 `contracts/browserautomation`；runtime 私有配置、provider raw metadata 和内部执行状态留在本仓内部 proto。
- 内部业务命令、状态、runtime 配置、artifact 引用、事件和 raw metadata 也优先使用本仓内部 proto 建模；不要只用 Go struct 作为私有契约源头。
- 本仓不得沉淀 GPT、Outlook 或其他站点注册业务流程；业务流程留在对应业务仓，通过公共契约调用本仓能力。
- 后端优先使用 Go，按 Clean Code、DI 和面向抽象设计组织代码。
- 引入 Playwright、CDP、浏览器驱动或其他外部 SDK 时必须按官方文档和稳定版本规范开发；无法用 Go 官方稳定 SDK 覆盖的 runtime 可通过明确 adapter/worker 边界隔离。
- 生成物不提交到仓库；提交前确认 `gen/` 和其他可再生成产物没有进入暂存区。
- proto 变更后必须运行生成命令、格式化和 Go 测试。
