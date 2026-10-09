import { describe, expect, it } from "vitest";
import type { FileRun } from "../api";
import { emptyConnection, type ConnectionState } from "./model";
import { buildReport, canExport, logfmtValue, toLogfmt } from "./report";

const at = (seconds: number) => new Date(Date.UTC(2026, 9, 9, 6, 0, seconds)).toISOString();

function result(path: string, status: FileRun["status"], from: number, to: number): FileRun {
  return {
    path,
    status,
    startedAt: at(from),
    finishedAt: at(to),
    statements: 2,
    executed: status === "succeeded" ? 2 : 1,
    rowsAffected: 3,
    log: [
      {
        at: at(from),
        kind: "statement",
        index: 1,
        text: "UPDATE t SET a = 'x y'",
        rows: 3,
        durationMs: 40,
      },
    ],
    error:
      status === "failed"
        ? {
            index: 2,
            startLine: 4,
            endLine: 4,
            line: 4,
            message: 'column "b" does not exist',
            code: "42703",
          }
        : undefined,
  };
}

const state: ConnectionState = {
  ...emptyConnection(),
  steps: [[["a.sql"]], [["b.sql"], ["c.sql"]], [["d.sql"]]],
  disabled: ["d.sql"],
  results: {
    "a.sql": result("a.sql", "succeeded", 0, 1),
    "b.sql": result("b.sql", "failed", 1, 3),
    "c.sql": { ...result("c.sql", "pending", 0, 0), startedAt: undefined, finishedAt: undefined },
  },
};

describe("report", () => {
  it("needs a started file and no running run", () => {
    expect(canExport(emptyConnection())).toBe(false);
    expect(canExport(state)).toBe(true);
    expect(canExport({ ...state, lastRun: { id: "r", status: "running", startedAt: at(0) } })).toBe(
      false,
    );
  });

  it("keeps arrangement order and classifies files that never ran", () => {
    const report = buildReport(state, "pg", "PostgreSQL", 0);
    expect(report.outcome).toBe("failed");
    expect(report.files.map((file) => [file.path, file.step, file.lane, file.outcome])).toEqual([
      ["a.sql", 1, 1, "succeeded"],
      ["b.sql", 2, 1, "failed"],
      ["c.sql", 2, 2, "notRun"],
      ["d.sql", 3, 1, "disabled"],
    ]);
    expect(report.finishedAt - report.startedAt).toBe(3000);
  });

  it("writes one logfmt event per line between run.start and run.end", () => {
    const lines = toLogfmt(buildReport(state, "pg", "PostgreSQL", 0))
      .trim()
      .split("\n");
    const events = lines.map((line) => line.split(" ").filter(Boolean)[2]);
    expect(events[0]).toBe("run.start");
    expect(events.at(-1)).toBe("run.end");
    expect(events).toContain("stmt.fail");
    const failure = lines.find((line) => line.includes(" stmt.fail "))!;
    expect(failure).toMatch(
      /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}[+-]\d\d:\d\d ERROR stmt\.fail /,
    );
    expect(failure).toContain('message="column \\"b\\" does not exist"');
    expect(failure).toContain("code=42703");
    expect(lines.find((line) => line.includes("file=d.sql"))).toContain("reason=disabled");
  });

  it("quotes values only when needed", () => {
    expect(logfmtValue("pg/a.sql")).toBe("pg/a.sql");
    expect(logfmtValue("a b")).toBe('"a b"');
    expect(logfmtValue('x="1"\nnext')).toBe('"x=\\"1\\"\\nnext"');
  });
});
