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

- The project loader has one interface: load and validate a Migration Graph plus its Database Profiles. It hides parsing, environment expansion, safe path resolution, script loading, checksums, and DAG validation.
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
