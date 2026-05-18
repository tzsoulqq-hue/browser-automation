# 贡献指南

## 边界

本仓只接收通用浏览器自动化能力。

以下内容不进入本仓：

- GPT、Outlook 或其他站点的注册/登录流程；
- 业务编排、业务状态机或账号生命周期逻辑；
- 业务仓私有 proto、provider 私有 shape 或业务数据库模型；
- 检查报告、临时二进制和其他构建产物。

## 验证

```sh
sh scripts/generate-proto.sh
GOPRIVATE=github.com/byte-v-forge/* GONOSUMDB=github.com/byte-v-forge/* go mod download
go vet ./...
```
