export type NodeStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "completed_with_errors"
  | "failed"
  | "blocked"
  | "cancelled";

export type ScriptStatus =
  "pending" | "running" | "succeeded" | "already_applied" | "failed" | "blocked" | "cancelled";
export type ErrorPolicy = "halt" | "continue";
export type DatabaseDriver = "postgres" | "mysql" | "sqlserver";

export interface MigrationScript {
  path: string;
  checksum?: string;
  sql?: string;
  status?: ScriptStatus;
  startedAt?: string;
  finishedAt?: string;
  rowsAffected?: number;
  error?: string;
}

export interface MigrationNode {
  name: string;
  database: string;
  dependsOn: string[];
  scripts: MigrationScript[];
  onError?: ErrorPolicy;
  status?: NodeStatus;
  error?: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface MigrationGraph {
  version: number;
  name: string;
  parallelism: number;
  onError: ErrorPolicy;
  nodes: MigrationNode[];
}

export interface DatabaseProfile {
  name: string;
  driver: DatabaseDriver;
  dsn: string;
  maxOpenConnections?: number;
  connectionTimeout?: string;
  status?: "unknown" | "connected" | "failed";
  error?: string;
}

export interface ProjectFile {
  path: string;
  kind?: "file" | "directory";
  node?: string;
}

export interface ProjectPayload {
  graph: MigrationGraph;
  databases: DatabaseProfile[];
  files: ProjectFile[];
  ready: boolean;
  problems: string[];
  fingerprint?: string;
  databaseFingerprint?: string;
}

export interface RunSummary {
  id: string;
  graphName: string;
  status: string;
  attempt: number;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
  completedNodes: number;
  totalNodes: number;
}

export interface RunSnapshot {
  run: RunSummary;
  attempts: Array<{
    number: number;
    status: string;
    startedAt?: string;
    finishedAt?: string;
    error?: string;
  }>;
  nodes: Array<MigrationNode & { attempt?: number }>;
  logs: Array<{
    sequence?: number;
    at: string;
    level: "info" | "warn" | "error";
    node?: string;
    script?: string;
    message: string;
  }>;
}

export interface ScriptDocument {
  path: string;
  sql: string;
  checksum?: string;
}

export type GraphDirection = "right" | "down";
export type ActivePanel = "files" | "connections" | "graph" | "inspector";
