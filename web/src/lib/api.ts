import {
  normalizeProject,
  normalizeRunSummary,
  normalizeScriptDocument,
  normalizeSnapshot,
} from "./normalize";
import type { GraphDraft } from "../store";
import type { DatabaseDriver, MigrationGraph } from "../types";

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";

class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: { "Content-Type": "application/json", ...options?.headers },
  });
  if (!response.ok) {
    throw new ApiError(await responseError(response), response.status);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

async function responseError(response: Response) {
  const fallback = `${response.status} ${response.statusText}`;
  const body = await response.text();
  if (!body) return fallback;
  try {
    const payload = JSON.parse(body) as { error?: unknown };
    return typeof payload.error === "string" ? payload.error : body;
  } catch {
    return body;
  }
}

export async function fetchProject(signal?: AbortSignal) {
  return normalizeProject(await request<unknown>("/api/v1/project", { signal }));
}

export async function saveGraph(graph: GraphDraft, fingerprint?: string) {
  return normalizeProject(
    await request<unknown>("/api/v1/graph", {
      method: "PUT",
      body: JSON.stringify(graphToApi(graph)),
      headers: fingerprint ? { "If-Match": quoteETag(fingerprint) } : undefined,
    }),
  );
}

export async function fetchScript(path: string) {
  const query = new URLSearchParams({ path });
  return normalizeScriptDocument(await request<unknown>(`/api/v1/scripts?${query}`), path);
}

export async function saveScript(path: string, sql: string, checksum?: string) {
  const query = new URLSearchParams({ path });
  return normalizeScriptDocument(
    await request<unknown>(`/api/v1/scripts?${query}`, {
      method: "PUT",
      body: JSON.stringify({ content: sql }),
      headers: checksum ? { "If-Match": quoteETag(checksum) } : undefined,
    }),
    path,
  );
}

export async function importScript(path: string, content: string) {
  return normalizeScriptDocument(
    await request<unknown>("/api/v1/scripts", {
      method: "POST",
      body: JSON.stringify({ path, content }),
    }),
    path,
  );
}

export interface DatabaseProfileInput {
  name: string;
  driver: DatabaseDriver;
  dsn?: string;
  maxOpenConnections?: number;
  connectionTimeout?: string;
}

export async function saveDatabase(profile: DatabaseProfileInput, fingerprint?: string) {
  const body: Record<string, unknown> = {
    driver: profile.driver,
    max_open_connections: profile.maxOpenConnections ?? 4,
    connection_timeout: profile.connectionTimeout ?? "5s",
  };
  if (profile.dsn?.trim()) body.dsn = profile.dsn.trim();
  return normalizeProject(
    await request<unknown>(`/api/v1/databases/${encodeURIComponent(profile.name)}`, {
      method: "PUT",
      body: JSON.stringify(body),
      headers: fingerprint ? { "If-Match": quoteETag(fingerprint) } : undefined,
    }),
  );
}

export async function deleteDatabase(name: string, fingerprint?: string) {
  return normalizeProject(
    await request<unknown>(`/api/v1/databases/${encodeURIComponent(name)}`, {
      method: "DELETE",
      headers: fingerprint ? { "If-Match": quoteETag(fingerprint) } : undefined,
    }),
  );
}

export async function testDatabase(name: string) {
  return request<{ name: string; latency_ms: number }>(
    `/api/v1/databases/${encodeURIComponent(name)}/test`,
    {
      method: "POST",
    },
  );
}

export async function fetchRuns() {
  const payload = await request<{ runs?: unknown[] } | unknown[]>("/api/v1/runs");
  const runs = Array.isArray(payload) ? payload : (payload.runs ?? []);
  return runs.map(normalizeRunSummary);
}

export async function startRun(force: boolean, nodes?: string[]) {
  const body: Record<string, unknown> = { force };
  if (nodes?.length) body.nodes = nodes;
  return normalizeRunSummary(
    await request<unknown>("/api/v1/runs", { method: "POST", body: JSON.stringify(body) }),
  );
}

export async function fetchRun(id: string) {
  return normalizeSnapshot(await request<unknown>(`/api/v1/runs/${encodeURIComponent(id)}`));
}

export async function resumeRun(id: string, force: boolean) {
  return normalizeRunSummary(
    await request<unknown>(`/api/v1/runs/${encodeURIComponent(id)}/resume`, {
      method: "POST",
      body: JSON.stringify({ force }),
    }),
  );
}

export type { ApiError };

function graphToApi(graph: MigrationGraph) {
  return {
    version: graph.version,
    name: graph.name,
    parallelism: graph.parallelism,
    on_error: graph.onError,
    nodes: graph.nodes.map((node) => ({
      name: node.name,
      database: node.database,
      depends_on: node.dependsOn,
      on_error: node.onError,
      scripts: node.scripts.map((script) => ({ path: script.path })),
    })),
  };
}

function quoteETag(value: string) {
  return `"${value}"`;
}
