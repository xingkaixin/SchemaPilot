import type { FileRun, RunStatus } from "../api";
import { allPaths, type Step } from "./arrangement";

export interface RunSummary {
  id: string;
  status: RunStatus;
  startedAt: string;
  finishedAt?: string;
  error?: string;
}

export interface ConnectionState {
  steps: Step[];
  disabled: string[];
  results: Record<string, FileRun>;
  lastRun?: RunSummary;
  /** Server runs that started before this moment belong to a cleared history. */
  resetAt?: string;
}

export const emptyConnection = (): ConnectionState => ({ steps: [], disabled: [], results: {} });

export type NodeState =
  | "missing"
  | "disabled"
  | "idle"
  | "waiting"
  | "notRun"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelled";

export function isRunning(state: ConnectionState) {
  return state.lastRun?.status === "running";
}

export function nodeState(path: string, state: ConnectionState, existing: Set<string>): NodeState {
  if (state.disabled.includes(path)) return "disabled";
  if (!existing.has(path)) return "missing";
  const result = state.results[path];
  if (!result) return "idle";
  if (result.status === "pending") return isRunning(state) ? "waiting" : "notRun";
  return result.status;
}

export function enabledPaths(state: ConnectionState) {
  return allPaths(state.steps).filter((path) => !state.disabled.includes(path));
}

export type RunMode = "start" | "continue" | "all";

/** Keeps the arranged shape but drops disabled files and, when continuing, files that already succeeded. */
export function buildSteps(state: ConnectionState, mode: RunMode): string[][][] {
  const skip = (path: string) =>
    state.disabled.includes(path) ||
    (mode === "continue" && state.results[path]?.status === "succeeded");
  return state.steps
    .map((step) =>
      step.map((lane) => lane.filter((path) => !skip(path))).filter((lane) => lane.length > 0),
    )
    .filter((step) => step.length > 0);
}

export function runOverview(state: ConnectionState, existing: Set<string>) {
  const enabled = enabledPaths(state);
  const succeeded = enabled.filter((path) => state.results[path]?.status === "succeeded");
  return {
    enabled,
    missing: enabled.filter((path) => !existing.has(path)),
    running: isRunning(state),
    anySucceeded: succeeded.length > 0,
    allSucceeded: enabled.length > 0 && succeeded.length === enabled.length,
    interrupted: state.lastRun?.status === "failed" || state.lastRun?.status === "cancelled",
  };
}

export function stepState(step: Step, state: ConnectionState, existing: Set<string>): NodeState {
  const states = allPaths([step]).map((path) => nodeState(path, state, existing));
  if (states.includes("failed") || states.includes("cancelled")) return "failed";
  if (states.includes("running")) return "running";
  if (states.includes("missing")) return "missing";
  if (states.every((value) => value === "disabled")) return "disabled";
  if (states.every((value) => value === "succeeded" || value === "disabled")) return "succeeded";
  return "idle";
}

export function formatDuration(ms: number) {
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.round((ms % 60_000) / 1000);
  return `${minutes} 分 ${seconds} 秒`;
}

export function formatClock(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${pad(Math.floor(total / 3600))}:${pad(Math.floor((total % 3600) / 60))}:${pad(total % 60)}`;
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

export function elapsed(from?: string, to?: string) {
  if (!from) return 0;
  return (to ? Date.parse(to) : Date.now()) - Date.parse(from);
}

export function fileName(path: string) {
  return path.slice(path.lastIndexOf("/") + 1);
}
