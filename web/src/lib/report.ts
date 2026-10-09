import type { LogEntry, StatementError } from "../api";
import type { ConnectionState } from "./model";

/** What a report says about one file: its latest result, or why it has none. */
export type FileOutcome = "succeeded" | "failed" | "cancelled" | "notRun" | "disabled";

export type RunOutcome = "succeeded" | "failed" | "cancelled" | "partial";

export interface Excerpt {
  firstLine: number;
  lines: string[];
  highlight: [number, number];
}

export interface ReportFile {
  path: string;
  /** 1-based position in the arrangement. */
  step: number;
  lane: number;
  outcome: FileOutcome;
  startedAt?: number;
  finishedAt?: number;
  statements: number;
  executed: number;
  rows: number;
  error?: StatementError;
  message?: string;
  log: LogEntry[];
  logDropped: number;
  excerpt?: Excerpt;
}

export interface Report {
  connection: string;
  driver: string;
  version?: string;
  generatedAt: number;
  outcome: RunOutcome;
  startedAt: number;
  finishedAt: number;
  steps: number;
  files: ReportFile[];
}

/** A report needs at least one file that started, and no run in progress. */
export function canExport(state: ConnectionState) {
  if (state.lastRun?.status === "running") return false;
  return Object.values(state.results).some((result) => result.startedAt);
}

export function buildReport(
  state: ConnectionState,
  connection: string,
  driver: string,
  generatedAt: number,
): Report {
  const files: ReportFile[] = [];
  state.steps.forEach((step, stepIndex) =>
    step.forEach((lane, laneIndex) =>
      lane.forEach((path) => {
        const result = state.results[path];
        const started = result?.startedAt && result.status !== "pending";
        const outcome: FileOutcome = started
          ? (result.status as FileOutcome)
          : state.disabled.includes(path)
            ? "disabled"
            : "notRun";
        files.push({
          path,
          step: stepIndex + 1,
          lane: laneIndex + 1,
          outcome,
          startedAt: started ? Date.parse(result.startedAt!) : undefined,
          finishedAt: started && result.finishedAt ? Date.parse(result.finishedAt) : undefined,
          statements: result?.statements ?? 0,
          executed: started ? result.executed : 0,
          rows: started ? result.rowsAffected : 0,
          error: started ? result.error : undefined,
          message: started ? result.message : undefined,
          log: started ? result.log : [],
          logDropped: started ? (result.logDropped ?? 0) : 0,
        });
      }),
    ),
  );

  const started = files.filter((file) => file.startedAt !== undefined);
  const startedAt = Math.min(...started.map((file) => file.startedAt!));
  const finishedAt = Math.max(...started.map((file) => file.finishedAt ?? file.startedAt!));
  const outcomes = files.map((file) => file.outcome);
  const outcome: RunOutcome = outcomes.includes("failed")
    ? "failed"
    : outcomes.includes("cancelled")
      ? "cancelled"
      : outcomes.every((value) => value === "succeeded" || value === "disabled")
        ? "succeeded"
        : "partial";

  return {
    connection,
    driver,
    version: state.lastRun?.version,
    generatedAt,
    outcome,
    startedAt,
    finishedAt,
    steps: state.steps.length,
    files,
  };
}

export function countOutcomes(report: Report) {
  const counts: Record<FileOutcome, number> = {
    succeeded: 0,
    failed: 0,
    cancelled: 0,
    notRun: 0,
    disabled: 0,
  };
  for (const file of report.files) counts[file.outcome]++;
  return counts;
}

/** The lines of a failed statement plus a little context around it. */
export function excerptFor(content: string, error: StatementError, context = 3): Excerpt {
  const lines = content.split("\n");
  const first = Math.max(1, error.startLine - context);
  const last = Math.min(lines.length, error.endLine + context);
  return {
    firstLine: first,
    lines: lines.slice(first - 1, last),
    highlight: error.line ? [error.line, error.line] : [error.startLine, error.endLine],
  };
}

const levels = { INFO: "INFO ", WARN: "WARN ", ERROR: "ERROR" };

interface Event {
  at: number;
  order: number;
  level: keyof typeof levels;
  name: string;
  fields: [string, string | number | undefined][];
}

/**
 * Renders the report as logfmt: one event per line, a local ISO 8601
 * timestamp, a level, an event name, then key=value fields.
 */
export function toLogfmt(report: Report): string {
  const events: Event[] = [];
  const push = (at: number, level: Event["level"], name: string, fields: Event["fields"]) =>
    events.push({ at, order: events.length, level, name, fields });
  const counts = countOutcomes(report);

  push(report.startedAt, "INFO", "run.start", [
    ["connection", report.connection],
    ["driver", report.driver],
    ["version", report.version],
    ["files", report.files.length],
    ["steps", report.steps],
  ]);
  for (const file of report.files) {
    if (file.startedAt === undefined) {
      push(report.finishedAt, "INFO", "file.skip", [
        ["file", file.path],
        ["step", file.step],
        ["lane", file.lane],
        ["reason", file.outcome === "disabled" ? "disabled" : "not_run"],
      ]);
      continue;
    }
    push(file.startedAt, "INFO", "file.start", [
      ["file", file.path],
      ["step", file.step],
      ["lane", file.lane],
      ["statements", file.statements],
    ]);
    if (file.logDropped > 0) {
      push(file.startedAt, "WARN", "log.dropped", [
        ["file", file.path],
        ["count", file.logDropped],
      ]);
    }
    for (const entry of file.log) {
      const at = Date.parse(entry.at);
      if (entry.kind === "notice") {
        push(at, "INFO", "stmt.notice", [
          ["file", file.path],
          ["index", entry.index],
          ["message", entry.text],
        ]);
      } else if (entry.kind === "error") {
        const error = file.error;
        push(at, "ERROR", "stmt.fail", [
          ["file", file.path],
          ["index", entry.index],
          ["line", error?.line || error?.startLine],
          ["duration_ms", entry.durationMs],
          ["code", error?.code],
          ["message", error?.message],
          ["detail", error?.detail],
          ["hint", error?.hint],
          ["sql", entry.text],
        ]);
      } else {
        push(at, "INFO", "stmt.ok", [
          ["file", file.path],
          ["index", entry.index],
          ["rows", entry.rows],
          ["duration_ms", entry.durationMs],
          ["sql", entry.text],
        ]);
      }
    }
    const error = file.error;
    if (file.outcome === "failed" && error && !file.log.some((entry) => entry.kind === "error")) {
      push(file.finishedAt ?? file.startedAt, "ERROR", "stmt.fail", [
        ["file", file.path],
        ["index", error.index],
        ["line", error.line || error.startLine],
        ["code", error.code],
        ["message", error.message],
        ["detail", error.detail],
        ["hint", error.hint],
      ]);
    }
    const level =
      file.outcome === "failed" ? "ERROR" : file.outcome === "cancelled" ? "WARN" : "INFO";
    push(file.finishedAt ?? file.startedAt, level, "file.end", [
      ["file", file.path],
      ["status", file.outcome],
      ["executed", `${file.executed}/${file.statements}`],
      ["rows", file.rows],
      ["duration_ms", (file.finishedAt ?? file.startedAt) - file.startedAt],
      ["stopped_at", file.outcome === "cancelled" ? file.error?.index : undefined],
      ["message", file.outcome === "failed" && !file.error ? file.message : undefined],
    ]);
  }
  push(report.finishedAt, report.outcome === "succeeded" ? "INFO" : "ERROR", "run.end", [
    ["status", report.outcome],
    ["succeeded", counts.succeeded],
    ["failed", counts.failed],
    ["cancelled", counts.cancelled],
    ["not_run", counts.notRun],
    ["disabled", counts.disabled],
    ["duration_ms", report.finishedAt - report.startedAt],
  ]);

  // run.start and run.end bracket the file events regardless of clock order.
  const [first, ...rest] = events;
  const last = rest.pop()!;
  rest.sort((a, b) => a.at - b.at || a.order - b.order);
  return [first, ...rest, last].map(formatEvent).join("\n") + "\n";
}

function formatEvent(event: Event) {
  const fields = event.fields
    .filter(([, value]) => value !== undefined && value !== "")
    .map(([key, value]) => `${key}=${logfmtValue(String(value))}`);
  return [isoLocal(event.at), levels[event.level], event.name, ...fields].join(" ");
}

export function logfmtValue(value: string) {
  if (/^[^\s"=\\]+$/.test(value)) return value;
  const escaped = value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r")
    .replace(/\t/g, "\\t");
  return `"${escaped}"`;
}

/** ISO 8601 with milliseconds and the local UTC offset, e.g. 2026-10-09T14:25:07.120+08:00. */
export function isoLocal(ms: number) {
  const date = new Date(ms);
  const pad = (value: number, size = 2) => String(Math.abs(value)).padStart(size, "0");
  const offset = -date.getTimezoneOffset();
  const sign = offset >= 0 ? "+" : "-";
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}` +
    `${sign}${pad(Math.floor(Math.abs(offset) / 60))}:${pad(Math.abs(offset) % 60)}`
  );
}

/** A filesystem-friendly local timestamp, e.g. 20261009-142507. */
export function fileStamp(ms: number) {
  return isoLocal(ms).slice(0, 19).replace(/[-:]/g, "").replace("T", "-");
}
