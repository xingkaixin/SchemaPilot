# Architecture

```text
browser (React + Kumo)                       schemapilot (Go)
  results  ─────────────── localStorage
  polling ───────────────── HTTP API ── workspace:   scan dir, read/import .sql
                                     ── config:      schemapilot.toml
                                     ── arrangement: schemapilot.arrangement.json
                                     ── bundle:      export/import packages (.zip)
                                     ── runner:    steps → lanes → files → statements
                                                     └─ database: postgres / mysql / sqlserver / oracle / sqlite / opengauss / dm / xugu drivers
                                                     └─ sqlscript: split files into statements
```

## Ownership

- **The arrangement lives in the workspace.** The step/lane order, disabled files and files moved out of their connection directory are stored in `schemapilot.arrangement.json`, one step per line. `GET /api/workspace` returns it with a revision; the browser edits it in memory and writes it back with `PUT /api/arrangement`, which refuses a stale base revision so that a change made elsewhere is reloaded instead of overwritten. Each connection also records its driver, so a workspace opened elsewhere can spot a connection of another kind.
- **The browser owns the history.** The last result of every file lives in localStorage, keyed by the working directory.
- **The server owns the directory, the config file and the live run.** `GET /api/workspace` lists connections and files; files under `./<connection>/` carry that connection name so the browser can assign them. Saving a connection rewrites `schemapilot.toml`.
- **A run is a plan the browser sends.** `POST /api/runs` takes `steps[lane][file]` already filtered (disabled files removed; when continuing, succeeded files removed). The runner keeps only the latest run per connection in memory; the browser polls `GET /api/runs/{connection}` and merges each file result into its history.
- **Packages move arrangements between workspaces.** `GET /api/package` zips the chosen connections' arrangement, every file they arrange and a manifest of sha256 sums; connection details stay behind. `schemapilot pkg.zip` verifies the sums, refuses any file or arrangement that already exists with different content, then unpacks into the working directory, which from then on is an ordinary workspace.

## Runner

- Steps run in order. Each lane of a step runs in its own goroutine on one pinned database session, so temporary tables and session settings carry across the files of a lane.
- Files are read when they start, split by `internal/sqlscript`, and executed one statement at a time in autocommit mode. Each statement's rows, duration and server notices are logged (the log keeps the last 500 entries per file).
- A failure marks the run as halted: lanes finish their current file and start nothing new.
- Stopping first asks the server to cancel each active session's statement through a separate connection, then cancels the context. Closing a MySQL connection alone would leave the query running on the server. SQL Server, Oracle and SQLite drivers interrupt the statement themselves when the context is cancelled, so their cancel step does nothing.

## Drivers

`internal/database` hides the differences between PostgreSQL (pgx), MySQL, SQL Server (go-mssqldb), Oracle (go-ora), SQLite (modernc), openGauss (openGauss-connector-go-pq), DM (chunanyong/dm) and Xugu (go-xugu-driver): connection strings, session ids, cancellation, version queries, notice capture (PostgreSQL only) and error details (SQLSTATE, detail, hint, error position). Adding a driver means adding one implementation and listing it in `drivers`; the connection dialog reads the list from the API.

## Security

The server binds to 127.0.0.1 by default. It then rejects requests whose `Host` is not a loopback name (DNS rebinding) and any cross-origin non-GET request, because the API can run arbitrary SQL against configured databases.
