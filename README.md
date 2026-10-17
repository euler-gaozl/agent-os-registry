# Agent OS Registry

管理 Agent、提示词、知识库配置等资源的固定版本，以及加密保存的供应商凭证。独立 Go 服务只拥有 `public.resources`、`public.secrets`，不读取 Runtime、ToolHub 或 Knowledge 的表。

2026-10-17 修复：交付工具的 MCP 命名空间使用绑定资源 ID，与 Runtime 快照一致；中文或不同的显示名只用于展示。真实 PostgreSQL 发布/版本回读和固定 ToolHub HTTP 两项定向测试通过，错误显示名别名会被拒绝，旧无策略 Agent 保持兼容。安装事实由集成仓的受管 catalog 记录。

## 确定性交付策略候选（2026-10-17）

固定 SDK 升至 rc.4。创建 Agent 版本会保留可选 `final_output`，校验 `receipt-field-v1`、原生模型能力、候选工具与绑定的精确 Skill/MCP 资源版本。Skill/MCP 通过 ToolHub 的固定版本 HTTP 接口解析，不读 ToolHub 数据库；无法取得版本或工具不匹配时不发布。原无策略 Agent 不增加字段或额外目录依赖。

`/v1/models/capabilities` 与 `--model-management-profile` 另声明 `final_output: receipt-field-v1`，供管理与发布兼容检查使用。这不是 Runtime/Worker 已启用 v5 的证明；该增量尚未安装。完整数据库与 HTTP 检查共 39 个测试事件通过、零测试跳过，覆盖发布/重读不丢策略、旧绑定不随新资源版本漂移、失败不产生版本、ToolHub HTTP 失败关闭及旧 Agent 兼容；模型能力发现为合成供应商夹具。

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

## 模型原生长度候选

`feat/model-native-limits` 使用固定 SDK `v0.1.0-rc.3`。创建 Agent 版本时，
`model.limits` 非空表示采用官方模型能力；Registry 丢弃客户端提供的能力值，使用已保存的
命名凭证查询官方模型列表，将模型、端点、协议、上下文窗口、最大输出、查询时间和元数据
摘要一起冻结进该版本。旧版本查询和运行恢复不重新发现，不静默改变历史任务。

当前解析适配器支持 `https://api.deepseek.com` 和其 `/v1` 别名；其他供应商明确报未支持，
不猜测长度。请求拒绝重定向，响应最多 1 MiB，15 秒超时；供应商错误正文及凭证不进入返回值。
缺失、异常、重复模型或查询失败不会发布新版本。`POST /v1/models/resolve` 仅为中台提供只读预览，
预览不会创建资源，正式保存仍独立查询。它不是凭证明文解析接口。

`GET /v1/models/capabilities` 向已认证的中台声明 `agent-model-native-v1` 配置协议；不读数据库或调用供应商。独立程序的 `--model-management-profile` 在配置和数据库初始化前输出同一声明，供发布器核对实际二进制。已保存原生能力的任一历史 Agent 版本都需要匹配的管理服务；发布器必须在回退前检查全部历史，并在 Registry 停止后等待仍在提交的数据库会话结束再复查。

原生模式取消旧的整任务 token 预算，保留调用次数策略（未配置时为 36 次，显式配置保留）；Runtime/Worker 的执行支持和部署门禁
必须一起完成才能启用。此分支尚未安装，不能把 Registry 发现成功写成完整 Agent 已支持原生长度。

本机该候选完成 35 条检查、零测试跳过及 vet/build，包括实际隔离 PostgreSQL 的加密凭证、
新版本发布、旧版本不变和失败不发布；供应商响应使用合成夹具，没有付费模型调用。
独立只读评审发现的重复成员和大小写别名解析问题已修复并复验。
