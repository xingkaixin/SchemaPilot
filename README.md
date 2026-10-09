# SchemaPilot

SchemaPilot 是一个本地命令行工具：在某个目录启动后，会打开一个 Web 控制台，用来把目录里的 SQL 文件分配到数据库连接、编排执行顺序（串行 + 并行），然后逐条语句执行并查看结果。

```bash
cd ~/work/shop-migration
schemapilot            # 控制台默认在 http://127.0.0.1:8080，端口被占用时自动顺延
```

## 目录约定

| 内容 | 位置 | 说明 |
| --- | --- | --- |
| 数据库连接 | `./schemapilot.toml` | 没有也能启动；在页面上添加连接时自动创建，之后页面上的修改都会写回这个文件 |
| 未分配的 SQL | `./*.sql` | 启动目录下直接放的 `.sql` 文件，在页面左侧勾选后分配到连接 |
| 自动归属的 SQL | `./<连接名>/**/*.sql` | 与连接同名的子目录会被递归扫描，里面的文件自动归属到该连接 |
| 拖入的文件 | 复制到 `./` | 从其他目录拖进窗口的 `.sql` 文件会复制到启动目录；重名时自动加 `-1` 后缀，不会覆盖 |

其他子目录不会被扫描。

```toml
[connections.pg-main]
driver = 'postgres'
host = '127.0.0.1'
port = 5432
database = 'shop'
user = 'app'
password = '${PG_PASSWORD}'   # ${NAME} 在连接时从环境变量读取

[connections.pg-main.params]  # 原样追加到连接串
sslmode = 'disable'

[connections.mysql-report]
driver = 'mysql'
host = '127.0.0.1'
database = 'report'
user = 'app'
password = '${MYSQL_PASSWORD}'
```

连接名只能包含字母、数字、`.`、`_`、`-`，因为它同时是子目录名。

支持的数据库（均使用纯 Go 驱动，二进制无需额外依赖）：

| `driver` | 数据库 | 说明 |
| --- | --- | --- |
| `postgres` | PostgreSQL | |
| `mysql` | MySQL | |
| `sqlserver` | SQL Server | `database` 填库名 |
| `oracle` | Oracle | `database` 填服务名，例如 `FREEPDB1` |
| `sqlite` | SQLite | 只需 `database`：数据库文件路径，相对路径从启动目录算起 |
| `opengauss` | openGauss | 支持 SHA256、SM3 密码认证 |
| `dm` | 达梦 DM8 | `database` 填默认模式（schema），可留空 |
| `xugu` | 虚谷 XuguDB | `database` 填库名，例如 `SYSTEM` |

兼容 MySQL 协议的数据库：`mariadb`、`tidb`、`oceanbase`（MySQL 模式）、`goldendb`、`tdsql-mysql`、`polardb-mysql`（含 PolarDB-X）、`greatsql`、`gbase8a`。

兼容 PostgreSQL 协议的数据库：`polardb-pg`、`tdsql-pg`、`opentenbase`、`kwdb`。

基于 openGauss 内核的数据库：`vastbase`、`gbase8c`、`gaussdb`。

这些类型使用对应协议的驱动和脚本拆分规则，单独列出是为了在页面上直接选到。

## 编排与执行

- 每个连接有一条独立的执行顺序。新文件默认按路径的自然顺序串行追加到末尾。
- 可选的命名约定 `{序号}_{分支号}_名称.sql` 用来自动编排并行：同一目录下序号相同、带分支号的文件合成一步，分支号相同的文件组成一路、按文件名依次执行，不同分支号的各路并行。例如 `040_1_seed_customers.sql`、`040_1_seed_orders.sql`、`040_2_seed_products.sql` 编排为两路并行，第一路先执行 customers 再执行 orders。不带分支号的文件仍各占一步。新文件加入时和点“按文件名重排”时都按这个规则编排。
- 一步可以是单个文件，也可以是几路并行的分支；分支内的文件依次执行。所有分支完成后才进入下一步。
- 在页面上拖动文件调整顺序：放在两步之间成为新的一步，放到某一步上与它并行，放到分支内则在该分支里串行。每个文件的 ⋯ 菜单里也有前移、后移、并行、禁用、移到其他连接等操作。
- 文件被拆成单条语句执行，每条语句自动提交。拆分规则与各数据库自带的命令行工具一致：
  - PostgreSQL 支持 `$$` 函数体。
  - MySQL 支持 `DELIMITER` 命令，可用来定义存储过程。
  - SQL Server 支持单独一行的 `GO` 结束批次；`CREATE PROCEDURE/FUNCTION/TRIGGER` 一直到 `GO` 或文件末尾为一条；`BEGIN … END` 块内的分号不拆分。
  - Oracle、达梦、虚谷的 PL/SQL 块（`DECLARE`/`BEGIN`、`CREATE PROCEDURE/FUNCTION/PACKAGE/TRIGGER/TYPE`）以单独一行的 `/` 结束；普通 SQL 末尾的分号会去掉。
  - openGauss 在 PostgreSQL 规则之外，支持 gsql 写法：`AS`/`IS` 后直接跟 PL/SQL 的存储过程、函数，以及 `DECLARE`/`BEGIN` 匿名块，以单独一行的 `/` 结束。
  - SQLite 的 `CREATE TRIGGER … BEGIN … END` 作为一条执行。
- 任何文件失败后，整条执行顺序暂停：正在执行的其他分支跑完当前文件后停止，后续文件不再开始。页面会标出失败的语句、行号和数据库返回的错误。
- 暂停后可以：
  - **从失败处继续**：已成功的文件跳过；失败的文件从第 1 条语句重新执行。如果它前面的语句已经提交，需要先确认它们可以重复执行，或修改文件。
  - **禁用文件**：被禁用的文件在运行时跳过。⋯ 菜单里可以一次禁用某个文件之后的全部文件。
  - **全部重跑**：所有启用的文件从头执行。
- **停止**会在数据库端取消正在运行的语句（PostgreSQL、openGauss 用 `pg_cancel_backend`，MySQL 用 `KILL QUERY`，达梦用 `SP_CANCEL_SESSION_OPERATION`，虚谷用 `DBMS_DBA.KILL_SESSION_TRANS`，SQL Server、Oracle、SQLite 由驱动发送中断），已提交的语句不会回滚。
- 不支持 psql 元命令、`COPY ... FROM stdin` 以及 PostgreSQL 的 `BEGIN ATOMIC` 函数体。

## 数据保存在哪里

- 连接信息：`schemapilot.toml`。
- 执行顺序、禁用状态和执行结果：浏览器的 localStorage，按启动目录区分。刷新页面不会丢失；换浏览器就看不到了。
- 服务端只在内存里保留每个连接最近一次运行，供页面刷新后继续跟踪。
- 文件被删除或移走后，执行顺序里会保留它并标为“文件不存在”；文件放回原处、重新扫描后即恢复。有启用的缺失文件时不能运行。

## 开发

工具链由 [mise](https://mise.jdx.dev/) 固定（Go、Bun）。

```bash
mise install
bun install --cwd web
mise run check   # Go 测试 + 前端格式、lint、类型检查、单元测试、构建
mise run build   # 构建前端并编译 bin/schemapilot（前端产物内嵌进二进制）
mise run dev     # examples/shop 上的 API（:8080）+ Vite 开发服务器（:5173）
```

集成测试需要真实的 PostgreSQL 和 MySQL。设置下面两个变量指向已有的数据库；不设置时，脚本会用 `compose.yaml` 在本机起临时容器：

```bash
export SCHEMAPILOT_TEST_POSTGRES='postgres://user:pass@host:5432/db?sslmode=disable'
export SCHEMAPILOT_TEST_MYSQL='mysql://user:pass@host:3306/db'
mise run test:integration
```

代码结构见 [docs/architecture.md](docs/architecture.md)。
