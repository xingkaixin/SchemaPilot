export type Driver = string;

export interface DriverInfo {
  id: Driver;
  label: string;
  defaultPort: number;
  file?: boolean;
  /** Set when the database speaks another driver's protocol. */
  protocol?: Driver;
}

export interface Connection {
  name: string;
  driver: Driver;
  host: string;
  port?: number;
  database: string;
  user: string;
  password: string;
  params?: Record<string, string>;
}

export interface WorkspaceFile {
  path: string;
  size: number;
  modTime: string;
  connection?: string;
  statements: number | null;
}

export interface Workspace {
  root: string;
  configFile: string;
  configExists: boolean;
  configError?: string;
  drivers: DriverInfo[];
  connections: Connection[];
  files: WorkspaceFile[];
}

export interface Statement {
  index: number;
  startLine: number;
  endLine: number;
}

export interface FileContent {
  path: string;
  content: string;
  truncated: boolean;
  statements: Statement[];
}

export type RunStatus = "pending" | "running" | "succeeded" | "failed" | "cancelled";

export interface StatementError {
  index: number;
  startLine: number;
  endLine: number;
  line?: number;
  message: string;
  detail?: string;
  hint?: string;
  code?: string;
}

export interface LogEntry {
  at: string;
  kind: "statement" | "notice" | "error";
  index?: number;
  text: string;
  rows?: number;
  durationMs?: number;
}

export interface FileRun {
  path: string;
  status: RunStatus;
  startedAt?: string;
  finishedAt?: string;
  statements: number;
  executed: number;
  current?: number;
  currentStartedAt?: string;
  currentText?: string;
  rowsAffected: number;
  session?: number;
  error?: StatementError;
  message?: string;
  log: LogEntry[];
  logDropped?: number;
}

export interface Run {
  id: string;
  connection: string;
  status: RunStatus;
  startedAt: string;
  finishedAt?: string;
  error?: string;
  files: FileRun[];
}

export interface Plan {
  connection: string;
  steps: string[][][];
}

export interface TestResult {
  version: string;
  latencyMs: number;
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new ApiError(body?.error ?? `请求失败（${response.status}）`, response.status);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function json(method: string, body: unknown): RequestInit {
  return { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}

export const api = {
  workspace: () => request<Workspace>("/api/workspace"),
  saveConnection: (connection: Connection, previousName?: string) =>
    request<Connection>("/api/connections", json("POST", { previousName, connection })),
  deleteConnection: (name: string) =>
    request<void>(`/api/connections/${encodeURIComponent(name)}`, { method: "DELETE" }),
  testConnection: (connection: Connection) =>
    request<TestResult>("/api/connections/test", json("POST", connection)),
  file: (path: string, driver?: Driver) =>
    request<FileContent>(
      `/api/file?${new URLSearchParams({ path, driver: driver ?? "" }).toString()}`,
    ),
  importFiles: (files: File[]) => {
    const form = new FormData();
    for (const file of files) form.append("file", file, file.name);
    return request<{ paths: string[] }>("/api/files", { method: "POST", body: form });
  },
  startRun: (plan: Plan) => request<Run>("/api/runs", json("POST", plan)),
  run: async (connection: string) => {
    try {
      return await request<Run>(`/api/runs/${encodeURIComponent(connection)}`);
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) return null;
      throw error;
    }
  },
  stopRun: (connection: string) =>
    request<void>(`/api/runs/${encodeURIComponent(connection)}/stop`, { method: "POST" }),
};
