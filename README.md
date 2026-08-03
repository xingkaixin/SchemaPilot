# SchemaPilot

SchemaPilot 是一个支持 DAG 编排的数据库迁移执行器。迁移拓扑写在 YAML 中，数据库连接写在 TOML 中；CLI 和 Web 控制台共享同一套执行引擎与 SQLite 运行日志。

它适合这些场景：

- 一个发布需要迁移多个数据库或业务模块；
- 独立迁移可以并行，报表等下游迁移必须等待依赖；
- 需要精确看到失败节点、SQL 文件和历史执行结果；
- 失败后需要在保留审计记录的前提下恢复执行；
- 已执行 SQL 被修改时，必须显式确认才能再次执行。

## 核心语义

```text
Migration Graph
  └─ Migration Node（节点之间按 DAG 调度）
       └─ SQL Script（节点内部严格串行）
```

- 所有依赖成功后节点才会进入执行队列。
- 无依赖节点最多按 `parallelism` 并行执行。
- 每个 SQL 文件在目标库事务中执行，并记录到 `_schemapilot_history`。
- 相同 checksum 的已执行文件会标记为 `already_applied`，不会重复执行。
- checksum 变化默认终止；只有 `--force` 或 Web 中明确选择强制恢复才会重跑。
- Resume 在原 Migration Run 内创建新的 Attempt，旧的节点、脚本和日志记录不会被覆盖。

## 快速开始

项目使用 [mise](https://mise.jdx.dev/) 固定 Go、Node.js 和 pnpm 版本。

```bash
mise install
pnpm --dir web install --frozen-lockfile
mise run check
mise run build
```

设置示例数据库连接后，可以验证并执行示例图：

```bash
export SHOP_PRIMARY_DSN='postgres://user:password@127.0.0.1:5432/shop?sslmode=disable'
export SHOP_REPORTING_DSN='postgres://user:password@127.0.0.1:5432/reporting?sslmode=disable'

./bin/schemapilot validate examples/shop/migration.yaml
./bin/schemapilot run examples/shop/migration.yaml
```

默认约定：

- 图文件：与命令所在路径相对的 `migration.yaml`；
- 数据库文件：图文件同目录的 `databases.toml`；
- 运行日志：图文件同目录的 `.schemapilot/runs.db`。

三个路径都可以通过 `--graph`、`--databases`、`--state` 显式指定。

## 配置

`migration.yaml` 只保存可共享的拓扑和 SQL 相对路径：

```yaml
version: 1
name: shop
parallelism: 2
on_error: halt
nodes:
  user-schema:
    database: primary
    scripts:
      - sql/user/001_create_users.sql
      - sql/user/002_add_user_email.sql
  order-schema:
    database: primary
    scripts:
      - sql/order/001_create_orders.sql
  report-schema:
    database: reporting
    depends_on:
      - user-schema
      - order-schema
    scripts:
      - sql/report/001_create_order_report.sql
```

`databases.toml` 保存环境相关连接信息，DSN 支持 `${ENV_NAME}` 展开：

```toml
version = 1

[databases.primary]
driver = "postgres"
dsn = "${SHOP_PRIMARY_DSN}"
max_open_connections = 4
connection_timeout = "5s"

[databases.reporting]
driver = "postgres"
dsn = "${SHOP_REPORTING_DSN}"
```

支持 `postgres`、`mysql`、`sqlserver`。配置字段、校验规则和 DSN 示例见 [配置参考](docs/configuration.md)。

## CLI

```text
schemapilot validate [migration.yaml]  校验配置、DAG、数据库引用和 SQL 文件
schemapilot run [migration.yaml]       创建并等待一次 Migration Run
schemapilot resume [run-id]            恢复指定或最近一次失败/取消的 Run
schemapilot status [run-id]            查看指定或最近一次 Run
schemapilot runs                       列出最近的 Run
schemapilot serve [migration.yaml]     启动 Web 控制台和 HTTP API
```

常用示例：

```bash
schemapilot run migration.yaml --databases databases.toml --state .schemapilot/runs.db
schemapilot resume --force
schemapilot status --json
schemapilot runs --limit 50 --json
schemapilot serve migration.yaml --listen 127.0.0.1:8080
```

`--force` 不是“忽略错误”，只允许 checksum 已变化的历史脚本再次执行。是否能够安全重跑仍由 SQL 作者负责。

## Web 控制台

```bash
schemapilot serve migration.yaml
```

打开 `http://127.0.0.1:8080` 后可以：

- 用 React Flow 查看、连线和自动布局 DAG；
- 从文件树拖拽 SQL 到画布，新建节点或追加脚本；
- 编辑节点数据库、依赖、脚本顺序和错误策略；
- 使用 Shiki 预览、Monaco Editor 编辑 SQL；
- 测试 TOML 中的数据库连接；
- 启动、观察、恢复 Migration Run，并查看节点/脚本日志。

未保存的图草稿会禁止执行；保存图和 SQL 都使用版本校验，避免静默覆盖并发修改。数据库 DSN 在 API 和界面中会脱敏。

## 数据库集成测试

Docker Compose 会启动临时数据库，测试完成后自动清理容器：

```bash
mise run test:integration
```

默认验证 PostgreSQL 和 MySQL。包含 SQL Server：

```bash
SCHEMAPILOT_TEST_SQLSERVER=1 mise run test:integration
```

SQL Server 镜像在 Apple Silicon 上通过 `linux/amd64` 运行，首次启动会明显更慢。

## 架构

领域和执行规则集中在 Go 内核，外层适配器不复制业务逻辑：

```text
YAML + TOML + SQL
        │
        ▼
  project loader
        │
        ▼
 execution engine ─────► SQLite run journal
        │
        ▼
 database adapters ────► PostgreSQL / MySQL / SQL Server
        ▲
        │
      CLI / HTTP + React
```

更详细的模块边界和执行不变量见 [架构说明](docs/architecture.md)。

## 已知边界

- SchemaPilot 不推断 SQL 是否幂等，也不会自动改写 SQL。
- MySQL 的部分 DDL 会隐式提交，不能获得与 PostgreSQL 相同的事务原子性；历史表仍只会在执行成功后更新。
- Resume 要求节点、依赖、数据库归属和脚本路径保持不变；脚本内容可以修复，数据库环境不能悄悄切换。
- 当前 Web 服务面向本机或受信网络，默认只监听 `127.0.0.1`，不包含身份认证。
