import { useState } from "react";
import { Loader, cn } from "@cloudflare/kumo";
import { XIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { api, type Connection, type FileRun, type WorkspaceFile } from "../api";
import { allPaths } from "../lib/arrangement";
import {
  elapsed,
  fileName,
  formatClock,
  formatDuration,
  formatSize,
  type ConnectionState,
  type NodeState,
} from "../lib/model";
import { useNow } from "../lib/useNow";
import { useStore } from "../store";
import { Button } from "./Button";
import { SqlView } from "./SqlView";

const badges: Partial<Record<NodeState, [string, "green" | "red" | "blue" | "orange" | "gray"]>> = {
  idle: ["待执行", "gray"],
  waiting: ["等待中", "gray"],
  notRun: ["未执行", "gray"],
  running: ["执行中", "blue"],
  succeeded: ["成功", "green"],
  failed: ["失败", "red"],
  cancelled: ["已终止", "red"],
  disabled: ["已禁用", "gray"],
  missing: ["文件不存在", "orange"],
};

const tabs = [
  { value: "sql", label: "SQL 内容" },
  { value: "output", label: "执行输出" },
];

export function DetailPanel({
  path,
  file,
  connection,
  state,
  nodeState,
}: {
  path: string;
  file?: WorkspaceFile;
  connection?: Connection;
  state?: ConnectionState;
  nodeState?: NodeState;
}) {
  const selectFile = useStore((store) => store.selectFile);
  const setDisabled = useStore((store) => store.setDisabled);
  const result = state?.results[path];
  const [tab, setTab] = useState(() => (result?.status === "running" ? "output" : "sql"));
  const content = useQuery({
    queryKey: ["file", path, connection?.driver, file?.modTime],
    queryFn: () => api.file(path, connection?.driver),
    enabled: Boolean(file),
  });
  const stepIndex = state?.steps.findIndex((step) => allPaths([step]).includes(path)) ?? -1;
  const badge = nodeState ? badges[nodeState] : undefined;
  const arranged = Boolean(connection && stepIndex >= 0);
  const disabled = Boolean(state?.disabled.includes(path));

  return (
    <section className="flex min-w-0 flex-[1_1_420px] flex-col self-start rounded-xl bg-kumo-base shadow-xs ring ring-kumo-line">
      <div className="flex flex-col gap-2.5 border-b border-kumo-hairline px-4 py-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate font-mono text-base font-medium">{fileName(path)}</span>
            {stepIndex >= 0 && <span className="tag">第 {stepIndex + 1} 步</span>}
            {badge && <span className={`st st-sm st-${badge[1]}`}>{badge[0]}</span>}
          </div>
          <Button
            variant="ghost"
            size="sm"
            icon={XIcon}
            aria-label="关闭详情"
            onClick={() => selectFile(undefined)}
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div role="tablist" className="-mb-3 flex">
            {tabs.map((item) => (
              <button
                key={item.value}
                type="button"
                role="tab"
                className="tab"
                aria-selected={tab === item.value}
                onClick={() => setTab(item.value)}
              >
                {item.label}
              </button>
            ))}
          </div>
          {arranged && connection && (
            <Button size="sm" onClick={() => setDisabled(connection.name, [path], !disabled)}>
              {disabled ? "启用" : "禁用"}
            </Button>
          )}
        </div>
      </div>

      <div className="max-h-[640px] min-h-40 flex-1 overflow-y-auto">
        {tab === "sql" ? (
          !file ? (
            <p className="m-0 px-4 py-6 text-sm text-kumo-subtle">
              文件已不在目录中，无法读取内容。
            </p>
          ) : content.isPending ? (
            <div className="flex justify-center py-8">
              <Loader />
            </div>
          ) : content.isError ? (
            <p className="m-0 px-4 py-6 text-sm text-kumo-danger">{content.error.message}</p>
          ) : (
            <SqlView
              content={content.data.content}
              statements={content.data.statements}
              truncated={content.data.truncated}
              result={result}
            />
          )
        ) : (
          <OutputLog result={result} />
        )}
      </div>

      {result && result.status === "failed" && result.executed > 0 && (
        <div className="mx-4 mb-3 flex flex-col gap-1 rounded-lg bg-kumo-recessed px-3 py-2.5 text-sm">
          <span className="font-medium">继续前确认</span>
          <span className="text-kumo-subtle">
            前 {result.executed} 条语句已提交，继续时这个文件会从第 1
            条重新执行。请确认它们可以重复执行，或先在文件里调整。
          </span>
        </div>
      )}

      <dl className="m-0 grid grid-cols-[64px_minmax(0,1fr)] gap-x-3 gap-y-2 rounded-b-xl border-t border-kumo-hairline bg-kumo-elevated px-4 py-3.5 text-sm">
        <dt className="text-kumo-subtle">路径</dt>
        <dd className="m-0 font-mono text-xs [overflow-wrap:anywhere]">./{path}</dd>
        {file && (
          <>
            <dt className="text-kumo-subtle">文件</dt>
            <dd className="m-0">
              {file.statements != null ? `${file.statements} 条语句 · ` : ""}
              {formatSize(file.size)} · 修改于 {new Date(file.modTime).toLocaleString()}
            </dd>
          </>
        )}
        {result?.startedAt && (
          <>
            <dt className="text-kumo-subtle">上次执行</dt>
            <dd className="m-0">
              {new Date(result.startedAt).toLocaleString()}
              {result.finishedAt &&
                ` · 耗时 ${formatDuration(elapsed(result.startedAt, result.finishedAt))}`}
            </dd>
          </>
        )}
        {result?.status === "running" && result.session != null && (
          <>
            <dt className="text-kumo-subtle">数据库会话</dt>
            <dd className="m-0 font-mono">{result.session}</dd>
          </>
        )}
      </dl>
    </section>
  );
}

function OutputLog({ result }: { result?: FileRun }) {
  const now = useNow(result?.status === "running");
  if (!result || result.status === "pending") {
    return <p className="m-0 px-4 py-6 text-sm text-kumo-subtle">还没有执行记录。</p>;
  }
  const remaining = result.status === "running" ? result.statements - (result.current ?? 0) : 0;
  return (
    <div className="py-1">
      {result.logDropped ? (
        <div className="px-4 py-2 text-xs text-kumo-subtle">
          已省略更早的 {result.logDropped} 条记录
        </div>
      ) : null}
      {result.message && <div className="px-4 py-2 text-sm text-kumo-danger">{result.message}</div>}
      {result.log.map((entry, index) => (
        <div
          key={index}
          className="flex flex-col gap-0.5 border-b border-kumo-hairline/60 px-4 py-2 last:border-b-0"
        >
          <div className="flex min-w-0 items-baseline gap-2">
            <span
              className={cn(
                "w-14 shrink-0 font-mono text-xs",
                entry.kind === "notice"
                  ? "text-kumo-warning"
                  : entry.kind === "error"
                    ? "text-kumo-danger"
                    : "text-kumo-subtle",
              )}
            >
              {entry.kind === "notice" ? "NOTICE" : `${entry.index}/${result.statements}`}
            </span>
            <span
              className={cn(
                "min-w-0 truncate font-mono text-[12.5px]",
                entry.kind === "notice" && "text-kumo-subtle",
              )}
            >
              {entry.text}
            </span>
          </div>
          {entry.kind !== "notice" && (
            <span
              className={cn(
                "pl-16 text-xs",
                entry.kind === "error" ? "text-kumo-danger" : "text-kumo-subtle",
              )}
            >
              {new Date(entry.at).toLocaleTimeString()} · {entry.kind === "error" ? "失败" : "完成"}
              {entry.rows != null && entry.rows > 0 ? ` · ${entry.rows.toLocaleString()} 行` : ""}
              {entry.durationMs != null ? ` · ${formatDuration(entry.durationMs)}` : ""}
            </span>
          )}
        </div>
      ))}
      {result.status === "running" && result.current != null && result.current > 0 && (
        <div className="flex flex-col gap-0.5 bg-kumo-info-tint px-4 py-2">
          <div className="flex items-baseline gap-2">
            <span className="w-14 shrink-0 font-mono text-xs text-kumo-link">
              {result.current}/{result.statements}
            </span>
            <span className="min-w-0 truncate font-mono text-[12.5px]">{result.currentText}</span>
          </div>
          <span className="pl-16 text-xs text-kumo-link">
            执行中{" "}
            {result.currentStartedAt ? formatClock(now - Date.parse(result.currentStartedAt)) : ""}
          </span>
        </div>
      )}
      {remaining > 1 && (
        <div className="px-4 py-2 text-sm text-kumo-placeholder">
          还有 {remaining - 1} 条语句等待执行
        </div>
      )}
    </div>
  );
}
