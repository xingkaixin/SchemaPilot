# Database Migration Orchestration

This context describes a database migration as a dependency graph whose nodes apply ordered SQL scripts to named databases, with every execution recorded for inspection and recovery.

## Authoring

**Migration Workspace**:
A local authoring area that may begin empty and holds the Database Profiles, Migration Scripts, and Migration Graph needed to become runnable.
_Avoid_: Project, workflow directory, repository

## Definition

**Migration Graph**:
The declarative, acyclic set of Migration Nodes and their dependencies.
_Avoid_: Workflow, job, migration

**Migration Node**:
An ordered group of Migration Scripts that targets one Database Profile and becomes eligible only after all of its dependencies succeed.
_Avoid_: Task, step, stage

**Migration Script**:
A versioned SQL file applied as one tracked change inside a Migration Node.
_Avoid_: File, command

**Database Profile**:
A named database destination referenced by Migration Nodes without embedding connection details in the Migration Graph.
_Avoid_: Connection, datasource, database config

## Execution

**Migration Run**:
One logical execution of a Migration Graph, including any recovery attempts made after failure.
_Avoid_: Workflow run, task, execution job

**Run Attempt**:
One initial or resumed effort to advance a Migration Run toward completion.
_Avoid_: Retry, rerun

**Node Execution**:
The recorded outcome of a Migration Node during a Run Attempt.
_Avoid_: Node run, task execution

**Script Execution**:
The recorded outcome of one Migration Script during a Run Attempt.
_Avoid_: File execution, statement execution

**Applied Script**:
A Migration Script whose checksum is recorded in its target database, whether it was applied in the current Migration Run or an earlier one.
_Avoid_: Completed file, installed migration

**Blocked Node**:
A Migration Node that cannot become eligible because at least one dependency failed.
_Avoid_: Skipped node, pending node

**Completed with Errors**:
The outcome of a Migration Node whose configured policy allowed later scripts and downstream nodes to proceed after one or more Script Executions failed.
_Avoid_: Ignored, successful
