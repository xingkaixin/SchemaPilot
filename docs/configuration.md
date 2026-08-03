# Configuration reference

SchemaPilot deliberately separates shareable orchestration from environment credentials:

- `migration.yaml` owns graph topology, policies, and repository-relative SQL paths.
- `databases.toml` owns database drivers, DSNs, pool limits, and connection timeouts.

Both documents use `version = 1` / `version: 1`. Unknown fields are rejected so misspellings cannot silently change execution behavior.

## Migration graph YAML

```yaml
version: 1
name: release-2026-08
parallelism: 4
on_error: halt
nodes:
  accounts:
    database: primary
    scripts:
      - sql/accounts/001_create_accounts.sql
      - sql/accounts/002_add_email.sql
  reporting:
    database: analytics
    depends_on:
      - accounts
    on_error: continue
    scripts:
      - sql/reporting/001_rebuild_views.sql
```

### Graph fields

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | Configuration schema version; currently `1`. |
| `name` | yes | Stable graph identity. Starts with a letter; letters, digits, `_`, and `-` are allowed. |
| `parallelism` | no | Maximum concurrently running nodes. Defaults to `4`, minimum `1`. |
| `on_error` | no | Default script error policy: `halt` or `continue`. Defaults to `halt`. |
| `nodes` | yes | Map keyed by unique Migration Node name. |

### Node fields

| Field | Required | Meaning |
| --- | --- | --- |
| `database` | yes | Name of a profile in `databases.toml`. |
| `depends_on` | no | Nodes that must allow downstream execution before this node starts. |
| `scripts` | yes | Non-empty, ordered list of `.sql` paths relative to the graph directory. |
| `on_error` | no | Node override for the graph error policy. |

The graph must be acyclic. A SQL path can belong to only one node, cannot escape the graph directory, and cannot escape through a symbolic link.

`continue` means later scripts in the same node continue after an ordinary SQL error. The node finishes as `completed_with_errors` and allows downstream nodes to run. A checksum mismatch always halts the node because it requires an explicit force decision.

## Database profiles TOML

```toml
version = 1

[databases.primary]
driver = "postgres"
dsn = "${PRIMARY_DATABASE_URL}"
max_open_connections = 8
connection_timeout = "10s"
```

| Field | Required | Meaning |
| --- | --- | --- |
| `driver` | yes | `postgres`, `mysql`, or `sqlserver`. |
| `dsn` | yes | Driver connection string. Exact `${ENV_NAME}` references are expanded at load time. |
| `max_open_connections` | no | Per-node pool limit. Defaults to `4`, minimum `1`. |
| `connection_timeout` | no | Go duration used for initial connection, for example `5s` or `1m`. Defaults to `5s`. |

Missing environment variables are errors. Commit a template containing environment references, not a file containing production secrets.

### DSN examples

PostgreSQL:

```text
postgres://user:password@db.example.com:5432/app?sslmode=require
```

MySQL:

```text
user:password@tcp(db.example.com:3306)/app?parseTime=true&multiStatements=true
```

Use `multiStatements=true` when a MySQL script contains more than one statement.

SQL Server:

```text
sqlserver://user:password@db.example.com:1433?database=app&encrypt=true
```

## Local state

The SQLite journal is operational state, not migration source. It stores runs, attempts, node executions, script executions, and logs. The default path is `.schemapilot/runs.db` beside the graph.

Each target database also receives `_schemapilot_history`. Its key includes graph name, node name, and complete script path. This protects independent graphs and nodes from colliding even when filenames repeat.

## Resume compatibility

Resume uses two independent guards:

- The structure fingerprint prevents changes to graph identity, node names, database assignment, dependencies, script paths, and script order.
- The environment fingerprint prevents a failed run from being resumed against different drivers or DSNs.

Script content and checksum may change so a failed script can be fixed before resume. Changing only `parallelism` or an error policy is also allowed. A changed checksum still requires `--force` when that script was previously applied successfully.
