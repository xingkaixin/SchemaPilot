# Architecture

SchemaPilot is a database migration graph runner with two adapters at its outermost seam: a CLI and an HTTP/Web application. Both invoke the same execution module and observe the same persisted run state.

```text
YAML graph + TOML profiles
           |
           v
   Project loader module
           |
           v
   Migration execution module
      |                |
      v                v
Database adapter   Run store adapter
      |                |
      v                v
target databases   local SQLite state
           ^
           |
       CLI / HTTP
```

## Module seams

- The project module exposes a tolerant workspace inspection path for Web authoring and a strict project load path for execution. It hides parsing, environment expansion, safe path resolution, script loading, checksums, and DAG validation. Missing files become authoring problems, while run and resume still fail strict validation.
- The execution module has two commands: start a Migration Run and resume one. It hides dependency scheduling, bounded concurrency, sequential script execution, error policy, state transitions, and applied-script checks.
- The database seam is real because PostgreSQL, MySQL, SQL Server, and an in-memory test adapter differ in connection and history-table behavior.
- The run-store seam is real because production uses SQLite while execution tests use an in-memory adapter.
- CLI and HTTP are adapters. They contain presentation and transport behavior only; orchestration rules do not live there.

## Configuration

The YAML file owns topology and repository-relative SQL paths. The TOML file owns Database Profiles and may reference environment variables. This keeps credentials out of the shareable graph and lets the same graph run against different environments.

## Execution invariants

- A graph must be acyclic and every dependency and Database Profile reference must resolve before a run is created.
- Nodes may run concurrently only after all dependencies have an outcome that permits downstream execution.
- Scripts within one node run exactly in listed order.
- A previously Applied Script with the same checksum is not executed again.
- A previously Applied Script with a different checksum stops execution unless the caller explicitly forces it.
- A failed dependency produces a Blocked Node; it is never reported as pending or successful.
- Resume advances the same Migration Run through a new Run Attempt and never overwrites prior execution evidence.
- Resume accepts repaired script contents but rejects graph-structure or database-environment changes.
- History-table initialization is serialized per driver and DSN so parallel nodes targeting the same new database cannot race during bootstrap.
- Graph, Database Profile, and SQL writes are serialized, use atomic file replacement where mutation is allowed, and are guarded by resource fingerprints; stale Web edits fail instead of overwriting newer content.

## State ownership

The target database history and the local run journal answer different questions:

- `_schemapilot_history` answers whether a specific graph/node/script identity has already been applied to that database and with which checksum.
- SQLite answers what happened during each operator Run and Attempt, including failures, blocked work, durations, rows affected, and logs.

Neither can be derived from the other, so both are necessary. Database credentials are never copied into SQLite; only an environment fingerprint is stored for resume safety.
