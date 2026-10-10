import { useEffect, useMemo, useState, type ReactNode } from "react";
import { cn } from "@cloudflare/kumo";
import type { ThemedToken } from "shiki/core";
import type { FileRun, Statement } from "../api";
import { tokenize } from "../lib/highlight";
import { formatDuration } from "../lib/model";

const MAX_LINES = 3000;
const MAX_HIGHLIGHT_CHARS = 300_000;

type StatementStatus = "none" | "ok" | "failed" | "cancelled" | "running" | "wait";

function statusOf(statement: Statement, result?: FileRun): StatementStatus {
  if (!result || result.status === "pending") return "none";
  if (result.error?.index === statement.index)
    return result.status === "cancelled" ? "cancelled" : "failed";
  if (statement.index <= result.executed) return "ok";
  if (result.status === "running" && result.current === statement.index) return "running";
  return "wait";
}

function useTokens(content: string) {
  const [tokens, setTokens] = useState<{ content: string; lines: ThemedToken[][] } | null>(null);
  useEffect(() => {
    if (content.length > MAX_HIGHLIGHT_CHARS) return;
    let active = true;
    tokenize(content)
      .then((lines) => active && setTokens({ content, lines }))
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [content]);
  return tokens?.content === content ? tokens.lines : null;
}

function visibleLines(total: number, focus: Statement[]): Set<number> {
  const visible = new Set<number>();
  for (let line = 1; line <= Math.min(total, MAX_LINES); line++) visible.add(line);
  for (const statement of focus) {
    for (
      let line = Math.max(1, statement.startLine - 2);
      line <= Math.min(total, statement.endLine + 2);
      line++
    ) {
      visible.add(line);
    }
  }
  return visible;
}

export function SqlView({
  content,
  statements,
  truncated,
  result,
}: {
  content: string;
  statements: Statement[];
  truncated: boolean;
  result?: FileRun;
}) {
  const lines = useMemo(() => {
    const split = content.split("\n");
    if (split.length > 1 && split[split.length - 1] === "") split.pop();
    return split;
  }, [content]);
  const tokens = useTokens(content);

  const byStart = new Map(statements.map((statement) => [statement.startLine, statement]));
  const byEnd = new Map(statements.map((statement) => [statement.endLine, statement]));
  const owners: (Statement | undefined)[] = [];
  for (const statement of statements) {
    for (let line = statement.startLine; line <= statement.endLine; line++)
      owners[line] = statement;
  }
  const focus = statements.filter((statement) =>
    ["failed", "cancelled", "running"].includes(statusOf(statement, result)),
  );
  const visible = visibleLines(lines.length, focus);
  const logByIndex = new Map(
    (result?.log ?? [])
      .filter((entry) => entry.kind !== "notice")
      .map((entry) => [entry.index, entry]),
  );

  const rows: ReactNode[] = [];
  let skipped = 0;
  const flushSkipped = (key: string) => {
    if (skipped > 0) {
      rows.push(
        <div key={key} className="py-1 pl-14 font-sans text-xs text-kumo-subtle">
          … 省略 {skipped} 行 …
        </div>,
      );
      skipped = 0;
    }
  };

  for (let number = 1; number <= lines.length; number++) {
    if (!visible.has(number)) {
      skipped++;
      continue;
    }
    flushSkipped(`skip-${number}`);
    const statement = owners[number];
    const status = statement ? statusOf(statement, result) : "none";
    const starting = byStart.get(number);
    if (starting && result && result.status !== "pending") {
      rows.push(
        <StatementLabel
          key={`label-${number}`}
          statement={starting}
          status={statusOf(starting, result)}
          entry={logByIndex.get(starting.index)}
          result={result}
        />,
      );
    }
    rows.push(
      <div
        key={number}
        className={cn(
          "flex whitespace-pre",
          (status === "failed" || status === "cancelled") && "bg-kumo-danger-tint",
          status === "running" && "bg-kumo-info-tint",
          status === "wait" && "opacity-55",
        )}
      >
        <span
          className={cn(
            "mr-1.5 w-[3px] shrink-0",
            status === "ok" && "bg-kumo-success/60",
            (status === "failed" || status === "cancelled") && "bg-kumo-danger",
            status === "running" && "bg-kumo-info",
          )}
        />
        <span
          className={cn(
            "w-9 shrink-0 pr-3 text-right text-kumo-placeholder select-none",
            status === "failed" && "text-kumo-danger",
          )}
        >
          {number}
        </span>
        <span>
          {tokens?.[number - 1]
            ? tokens[number - 1].map((token, index) => (
                <span key={index} style={{ color: token.color }}>
                  {token.content}
                </span>
              ))
            : lines[number - 1] || " "}
        </span>
      </div>,
    );
    const ending = byEnd.get(number);
    if (
      ending &&
      result?.error &&
      result.error.index === ending.index &&
      result.status === "failed"
    ) {
      rows.push(<ErrorBox key={`error-${number}`} result={result} />);
    }
  }
  flushSkipped("skip-end");

  return (
    <div className="overflow-x-auto py-2 font-mono text-sm leading-5">
      {rows}
      {truncated && (
        <div className="px-4 py-2 font-sans text-xs text-kumo-subtle">
          文件较大，仅显示前 2 MB 内容。
        </div>
      )}
      {statements.length === 0 && (
        <div className="px-4 py-2 font-sans text-sm text-kumo-subtle">文件里没有可执行的语句。</div>
      )}
    </div>
  );
}

function StatementLabel({
  statement,
  status,
  entry,
  result,
}: {
  statement: Statement;
  status: StatementStatus;
  entry?: { rows?: number; durationMs?: number };
  result: FileRun;
}) {
  const range =
    statement.startLine === statement.endLine
      ? `第 ${statement.startLine} 行`
      : `第 ${statement.startLine}–${statement.endLine} 行`;
  const parts = [`语句 ${statement.index}`];
  let tone = "text-kumo-subtle";
  if (status === "ok") {
    parts.push("已提交");
    if (entry?.rows != null && entry.rows > 0) parts.push(`${entry.rows.toLocaleString()} 行`);
    if (entry?.durationMs != null) parts.push(formatDuration(entry.durationMs));
    tone = "text-kumo-success";
  } else if (status === "failed") {
    parts.push("失败", range);
    tone = "text-kumo-danger font-medium";
  } else if (status === "cancelled") {
    parts.push("已终止", range);
    tone = "text-kumo-danger font-medium";
  } else if (status === "running") {
    parts.push("执行中");
    tone = "text-kumo-info font-medium";
  } else if (result.status !== "running") {
    parts.push("未执行");
  } else {
    parts.push("等待");
  }
  return (
    <div className={cn("pt-1.5 pb-0.5 pl-12 font-sans text-xs", tone)}>{parts.join(" · ")}</div>
  );
}

function ErrorBox({ result }: { result: FileRun }) {
  const error = result.error!;
  return (
    <div className="bg-kumo-danger-tint pr-3 pb-2.5 pl-12">
      <div className="flex flex-col gap-1 rounded-(--r-lg) bg-kumo-base px-3 py-2.5 whitespace-normal ring ring-kumo-danger/30">
        <span className="text-[12.5px] leading-[18px] font-medium text-kumo-danger">
          {error.message}
        </span>
        {error.detail && <span className="text-xs text-kumo-subtle">DETAIL: {error.detail}</span>}
        {error.hint && <span className="text-xs text-kumo-subtle">HINT: {error.hint}</span>}
        <span className="font-sans text-xs text-kumo-subtle">
          {[
            error.code && `SQLSTATE ${error.code}`,
            error.line && `出错位置在第 ${error.line} 行`,
            "本条语句未生效",
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </div>
    </div>
  );
}
