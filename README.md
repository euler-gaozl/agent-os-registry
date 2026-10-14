# Agent OS Registry

管理 Agent、提示词、知识库配置等资源的固定版本，以及加密保存的供应商凭证。独立 Go 服务只拥有 `public.resources`、`public.secrets`，不读取 Runtime、ToolHub 或 Knowledge 的表。

## 职责与权限

目录查询返回有界摘要；Skill/MCP 的写入转发给 ToolHub，启动必须提供 `TOOLHUB_URL`。中台可管理资源与凭证，但不能解析明文凭证；执行和检索服务按名称解析所需供应商凭证。Futures 只读取状态和自身发布列表。这是服务间信任边界，尚非租户 RBAC。

`MASTER_KEY` 必须在升级和恢复时保留，替换它会使已存凭证不可读。内部运行配置不能作为业务凭证解析。只有历史兼容的 `DEEPSEEK_API_KEY`、`LANGFUSE_PUBLIC_KEY`、`LANGFUSE_SECRET_KEY` 支持环境回退，其他命名凭证应通过加密凭证接口保存。

## 构建与运行

需要 Go 1.27.2、PostgreSQL 18；构建辅助脚本需要 PowerShell 7。

```powershell
$env:GOWORK='off'; $env:GOPROXY='off'
./scripts/Build.ps1
./bin/registry.exe --config <仓库外的受保护配置>
```

也可 `go build -mod=vendor ./cmd/registry`。SDK 和依赖固定于 [版本记录](source-provenance.json) 与 `vendor`；构建不依赖相邻仓库、Node 或模型 Key。

[配置示例](deploy/service.example.json) 仅有占位符。每个服务使用独立随机令牌，接收方保存对端 SHA-256，`SERVICE_PEERS` 包含自身哈希；环境变量优先于 JSON。容器部署显式设置服务地址、只读根目录和 CPU/内存/PID 限制，内部端口由私网/TLS 保护。

## 迁移与发布

应用角色须具备 schema USAGE，但不能是 schema 所有者或拥有 CREATE。单独以 `MIGRATION_DATABASE_URL` 向 `bin/migrate --role <应用账号>` 提供 DDL 权限，事务只修改两张本域表及授权，有锁和语句超时。正常启动不做 DDL，不兼容数据库会使就绪检查失败。

保留旧程序与配置，先执行兼容扩展迁移，再替换 Registry，检查需鉴权的 `/readyz`。按 [发布契约](deploy/release-contract.json) 回滚程序，不做破坏性反向迁移。`/healthz` 只检查进程；`/readyz` 检查本域结构/权限，不检查 ToolHub 健康或模型余额。

## 验证状态

运行 `go test -mod=vendor -count=1 ./...`、`go vet -mod=vendor ./...`。`TEST_DATABASE_URL` 用于创建隔离测试库和 NOLOGIN 角色，不迁移指定的原库；缺少时数据库测试明确跳过。测试不使用真实模型 Key。源代码拆分不等于全平台容错、容量或生产验收。

Actions 日常关闭；[历史细节](docs/history/service-notes-20261014.en.md) 和 [集成计划](https://github.com/euler-gaozl/agent-os/blob/main/docs/windows-single-machine-plan.md) 分别记录各项证据。
