import type {
  DatabaseProfile,
  MigrationGraph,
  MigrationNode,
  MigrationScript,
  ProjectFile,
  ProjectPayload,
  RunSnapshot,
  RunSummary,
  ScriptDocument,
} from "../types";

type AnyRecord = Record<string, unknown>;

const value = (record: AnyRecord, ...keys: string[]) =>
  keys.map((key) => record[key]).find((entry) => entry !== undefined);

const asString = (input: unknown, fallback = "") =>
  typeof input === "string" ? input : input == null ? fallback : String(input);

const asNumber = (input: unknown, fallback = 0) =>
  typeof input === "number" && Number.isFinite(input) ? input : Number(input) || fallback;

const asArray = (input: unknown): unknown[] => (Array.isArray(input) ? input : []);

const asRecord = (input: unknown): AnyRecord =>
  typeof input === "object" && input !== null ? (input as AnyRecord) : {};

export function normalizeScript(input: unknown): MigrationScript {
  const record = asRecord(input);
  return {
    path: asString(value(record, "path", "Path", "filename", "Filename")),
    checksum: asString(value(record, "checksum", "Checksum")) || undefined,
    sql: asString(value(record, "sql", "SQL")) || undefined,
    status: asString(value(record, "status", "Status")) as MigrationScript["status"],
    startedAt: asString(value(record, "startedAt", "StartedAt", "started_at")) || undefined,
    finishedAt: asString(value(record, "finishedAt", "FinishedAt", "finished_at")) || undefined,
    rowsAffected: value(record, "rowsAffected", "RowsAffected", "rows_affected") as
      number | undefined,
    error: asString(value(record, "error", "Error")) || undefined,
  };
}

export function normalizeNode(input: unknown): MigrationNode {
  const record = asRecord(input);
  return {
    name: asString(value(record, "name", "Name")),
    database: asString(value(record, "database", "Database", "connection")),
    dependsOn: asArray(value(record, "dependsOn", "DependsOn", "depends_on")).map((item) =>
      asString(item),
    ),
    scripts: asArray(value(record, "scripts", "Scripts")).map(normalizeScript),
    onError: (asString(value(record, "onError", "OnError", "on_error")) || undefined) as
      MigrationNode["onError"] | undefined,
    status: asString(value(record, "status", "Status")) as MigrationNode["status"],
    error: asString(value(record, "error", "Error")) || undefined,
    startedAt: asString(value(record, "startedAt", "StartedAt", "started_at")) || undefined,
    finishedAt: asString(value(record, "finishedAt", "FinishedAt", "finished_at")) || undefined,
  };
}

export function normalizeGraph(input: unknown): MigrationGraph {
  const record = asRecord(input);
  const nodesInput = value(record, "nodes", "Nodes");
  const nodes = Array.isArray(nodesInput)
    ? nodesInput.map(normalizeNode)
    : Object.entries(asRecord(nodesInput)).map(([name, node]) => ({
        ...normalizeNode(node),
        name: normalizeNode(node).name || name,
      }));
  return {
    version: asNumber(value(record, "version", "Version"), 1),
    name: asString(value(record, "name", "Name"), "migration"),
    parallelism: asNumber(value(record, "parallelism", "Parallelism"), 4),
    onError: (asString(value(record, "onError", "OnError", "on_error"), "halt") ||
      "halt") as MigrationGraph["onError"],
    nodes,
  };
}

export function normalizeDatabase(input: unknown, fallbackName?: string): DatabaseProfile {
  const record = asRecord(input);
  return {
    name: asString(value(record, "name", "Name"), fallbackName),
    driver: asString(value(record, "driver", "Driver"), "postgres") as DatabaseProfile["driver"],
    dsn: asString(value(record, "dsn", "DSN")),
    maxOpenConnections: value(
      record,
      "maxOpenConnections",
      "MaxOpenConnections",
      "max_open_connections",
    ) as number | undefined,
    connectionTimeout:
      asString(value(record, "connectionTimeout", "ConnectionTimeout", "connection_timeout")) ||
      undefined,
    status: asString(value(record, "status", "Status")) as DatabaseProfile["status"],
    error: asString(value(record, "error", "Error")) || undefined,
  };
}

export function normalizeFile(input: unknown): ProjectFile {
  if (typeof input === "string") return { path: input, kind: "file" };
  const record = asRecord(input);
  return {
    path: asString(value(record, "path", "Path", "name", "Name")),
    kind: (asString(value(record, "kind", "Kind", "type", "Type")) ||
      "file") as ProjectFile["kind"],
    node: asString(value(record, "node", "Node")) || undefined,
  };
}

export function normalizeProject(input: unknown): ProjectPayload {
  const record = asRecord(input);
  const databasesInput = value(record, "databases", "Databases");
  const databases = Array.isArray(databasesInput)
    ? databasesInput.map((item) => normalizeDatabase(item))
    : Object.entries(asRecord(databasesInput)).map(([name, item]) => normalizeDatabase(item, name));
  return {
    graph: normalizeGraph(value(record, "graph", "Graph") ?? record),
    databases,
    files: asArray(value(record, "files", "Files")).map(normalizeFile),
    fingerprint: asString(value(record, "fingerprint", "Fingerprint")) || undefined,
  };
}

export function normalizeRunSummary(input: unknown): RunSummary {
  const record = asRecord(input);
  const run = asRecord(value(record, "run", "Run") ?? record);
  return {
    id: asString(value(run, "id", "ID")),
    graphName: asString(value(run, "graphName", "GraphName", "graph_name")),
    status: asString(value(run, "status", "Status")),
    attempt: asNumber(value(run, "attempt", "Attempt")),
    startedAt: asString(value(run, "startedAt", "StartedAt", "started_at")) || undefined,
    finishedAt: asString(value(run, "finishedAt", "FinishedAt", "finished_at")) || undefined,
    error: asString(value(run, "error", "Error")) || undefined,
    completedNodes: asNumber(value(record, "completedNodes", "CompletedNodes", "completed_nodes")),
    totalNodes: asNumber(value(record, "totalNodes", "TotalNodes", "total_nodes")),
  };
}

export function normalizeSnapshot(input: unknown): RunSnapshot {
  const record = asRecord(input);
  return {
    run: normalizeRunSummary(value(record, "run", "Run") ?? record),
    attempts: asArray(value(record, "attempts", "Attempts")).map((item) => {
      const attempt = asRecord(item);
      return {
        number: asNumber(value(attempt, "number", "Number")),
        status: asString(value(attempt, "status", "Status")),
        startedAt: asString(value(attempt, "startedAt", "StartedAt", "started_at")) || undefined,
        finishedAt:
          asString(value(attempt, "finishedAt", "FinishedAt", "finished_at")) || undefined,
        error: asString(value(attempt, "error", "Error")) || undefined,
      };
    }),
    nodes: asArray(value(record, "nodes", "Nodes")).map(normalizeNode) as RunSnapshot["nodes"],
    logs: asArray(value(record, "logs", "Logs")).map((item) => {
      const log = asRecord(item);
      return {
        sequence: value(log, "sequence", "Sequence") as number | undefined,
        at: asString(value(log, "at", "At")),
        level: asString(value(log, "level", "Level"), "info") as "info" | "warn" | "error",
        node: asString(value(log, "node", "Node")) || undefined,
        script: asString(value(log, "script", "Script")) || undefined,
        message: asString(value(log, "message", "Message")),
      };
    }),
  };
}

export function normalizeScriptDocument(input: unknown, path: string): ScriptDocument {
  const record = asRecord(input);
  return {
    path: asString(value(record, "path", "Path"), path),
    sql: asString(value(record, "sql", "SQL", "content", "Content") ?? input),
    checksum: asString(value(record, "checksum", "Checksum")) || undefined,
  };
}
