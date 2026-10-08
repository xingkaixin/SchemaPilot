# SQL File Orchestration

SchemaPilot runs the SQL files of a working directory against named database connections, in an order the user arranges, statement by statement.

**Workspace**:
The directory SchemaPilot was started in. It holds `schemapilot.toml` and the SQL files.
_Avoid_: Project, repository

**Connection**:
A named database destination stored in `schemapilot.toml`. Its name doubles as the sub-directory whose files are assigned to it automatically.
_Avoid_: Database profile, datasource

**Unassigned file**:
A SQL file in the workspace that belongs to no connection's arrangement yet.

**Arrangement**:
The execution order of one connection's files: a list of steps.
_Avoid_: Graph, DAG, pipeline

**Step**:
One position in an arrangement. It holds one or more lanes; the next step starts only after every lane finishes.
_Avoid_: Stage, node

**Lane**:
A parallel branch inside a step; its files run one after another on the same database session.
_Avoid_: Branch, thread

**Disabled file**:
A file that stays in the arrangement but is skipped when running.

**Missing file**:
An arranged file that no longer exists on disk. It keeps its place and blocks running until it is restored or disabled.

**Run**:
One execution of a plan built from an arrangement: all enabled files, or, when continuing, the ones that have not succeeded.
_Avoid_: Job, attempt

**Statement**:
One executable unit split out of a file. Statements commit individually, so a failed file may have committed its earlier statements.
