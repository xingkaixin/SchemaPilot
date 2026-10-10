import { useState } from "react";
import { DropdownMenu, cn } from "@cloudflare/kumo";
import { useDraggable } from "@dnd-kit/core";
import {
  ArrowArcLeftIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ArrowsSplitIcon,
  DatabaseIcon,
  DotsSixVerticalIcon,
  DotsThreeIcon,
  EyeIcon,
  GitForkIcon,
  ProhibitIcon,
  SkipForwardIcon,
} from "@phosphor-icons/react";
import type { FileRun, WorkspaceFile } from "../api";
import {
  elapsed,
  fileName,
  formatClock,
  formatDuration,
  formatSize,
  type NodeState,
} from "../lib/model";
import { useNow } from "../lib/useNow";
import { Button } from "./Button";
import { StatusIcon } from "./StatusIcon";

export interface NodeActions {
  select: () => void;
  parallelWithPrevious?: () => void;
  extract?: () => void;
  moveUp?: () => void;
  moveDown?: () => void;
  toggleDisabled: () => void;
  disableAfter?: () => void;
  moveTo: { name: string; run: () => void }[];
  unassign: () => void;
}

export function FileNode({
  path,
  state,
  result,
  file,
  selected,
  locked,
  actions,
}: {
  path: string;
  state: NodeState;
  result?: FileRun;
  file?: WorkspaceFile;
  selected: boolean;
  locked: boolean;
  actions: NodeActions;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, isDragging } = useDraggable({
    id: path,
    disabled: locked,
  });
  const now = useNow(state === "running");
  const settled = useSettled(state);
  const statements = result?.statements || file?.statements;

  let meta = statements != null ? `${statements} 条语句` : "";
  if (file && state !== "missing") meta += `${meta ? " · " : ""}${formatSize(file.size)}`;
  let side = "待执行";
  let tone = "";
  switch (state) {
    case "missing":
      meta = `./${path}`;
      side = "文件不存在";
      tone = "text-kumo-warning font-medium";
      break;
    case "disabled":
      side = "已禁用，运行时跳过";
      break;
    case "waiting":
      side = "等待中";
      break;
    case "notRun":
      side = "未执行";
      break;
    case "running":
      meta = `语句 ${result?.current ?? 0} / ${result?.statements ?? "?"}`;
      side = result?.startedAt ? formatClock(now - Date.parse(result.startedAt)) : "";
      tone = "text-kumo-info font-mono";
      break;
    case "succeeded":
      if (result && result.rowsAffected > 0) meta = `${result.rowsAffected.toLocaleString()} 行`;
      side = formatDuration(elapsed(result?.startedAt, result?.finishedAt));
      break;
    case "failed":
      meta = result?.error
        ? `失败于语句 ${result.error.index} / ${result.statements}`
        : (result?.message ?? "执行失败");
      side = formatDuration(elapsed(result?.startedAt, result?.finishedAt));
      break;
    case "cancelled":
      meta = result?.error ? `停在语句 ${result.error.index} / ${result.statements}` : "已终止";
      side = "已终止";
      break;
  }

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "group flex h-14 min-w-0 items-center gap-1 rounded-(--r-xl) bg-kumo-base pr-1.5 pl-0.5 shadow-(--sh-card) ring ring-kumo-line",
        (state === "running" || settled) && "run-sweep",
        state === "running" && "run-sweep-running",
        settled === "succeeded" && "run-sweep-succeeded",
        settled === "failed" && "run-sweep-failed run-shake",
        selected && "ring-2 ring-kumo-brand",
        state === "running" && "ring-2 ring-kumo-info",
        state === "failed" && "ring-2 ring-kumo-danger",
        state === "missing" && "ring-[1.5px] ring-kumo-warning",
        state === "disabled" &&
          "bg-kumo-recessed text-kumo-subtle shadow-none ring-0 outline-1 -outline-offset-1 outline-kumo-interact outline-dashed",
        isDragging && "opacity-40",
      )}
    >
      <button
        type="button"
        ref={setActivatorNodeRef}
        {...listeners}
        {...attributes}
        aria-label={`拖动 ${fileName(path)}`}
        className={cn(
          "flex h-9 w-5.5 shrink-0 items-center justify-center rounded-md text-kumo-inactive",
          locked ? "cursor-not-allowed" : "cursor-grab hover:bg-kumo-tint hover:text-kumo-subtle",
        )}
      >
        <DotsSixVerticalIcon size={16} weight="bold" />
      </button>
      <button
        type="button"
        onClick={actions.select}
        className="flex h-full min-w-0 flex-1 cursor-pointer items-center gap-2.5 px-1 text-left"
      >
        <span key={state} className={cn("flex", settled && "run-pop")}>
          <StatusIcon state={state} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span
            className={cn("truncate font-mono font-medium", state === "disabled" && "line-through")}
          >
            {fileName(path)}
          </span>
          <span
            className={cn(
              "truncate text-xs",
              state === "failed"
                ? "text-kumo-danger"
                : state === "running"
                  ? "text-kumo-info"
                  : "text-kumo-subtle",
              state === "missing" && "font-mono",
            )}
          >
            {meta}
          </span>
        </span>
        <span
          className={cn(
            "shrink-0 text-xs whitespace-nowrap text-kumo-subtle",
            tone,
            state === "waiting" && "run-text-shimmer",
          )}
        >
          {side}
        </span>
      </button>
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="ghost"
              size="sm"
              icon={DotsThreeIcon}
              aria-label={`${fileName(path)} 的更多操作`}
            />
          }
        />
        <DropdownMenu.Content>
          <DropdownMenu.Item icon={EyeIcon} onClick={actions.select}>
            查看 SQL
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={GitForkIcon}
            disabled={locked || !actions.parallelWithPrevious}
            onClick={actions.parallelWithPrevious}
          >
            与上一步并行
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={ArrowsSplitIcon}
            disabled={locked || !actions.extract}
            onClick={actions.extract}
          >
            拆出为单独一步
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={ArrowUpIcon}
            disabled={locked || !actions.moveUp}
            onClick={actions.moveUp}
          >
            前移一步
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={ArrowDownIcon}
            disabled={locked || !actions.moveDown}
            onClick={actions.moveDown}
          >
            后移一步
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item icon={ProhibitIcon} disabled={locked} onClick={actions.toggleDisabled}>
            {state === "disabled" ? "启用" : "禁用"}
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={SkipForwardIcon}
            disabled={locked || !actions.disableAfter}
            onClick={actions.disableAfter}
          >
            禁用此文件之后的全部
          </DropdownMenu.Item>
          {actions.moveTo.length > 0 && (
            <DropdownMenu.Sub>
              <DropdownMenu.SubTrigger icon={DatabaseIcon} disabled={locked}>
                移到其他连接
              </DropdownMenu.SubTrigger>
              <DropdownMenu.SubContent>
                {actions.moveTo.map((target) => (
                  <DropdownMenu.Item key={target.name} onClick={target.run}>
                    {target.name}
                  </DropdownMenu.Item>
                ))}
              </DropdownMenu.SubContent>
            </DropdownMenu.Sub>
          )}
          <DropdownMenu.Item icon={ArrowArcLeftIcon} disabled={locked} onClick={actions.unassign}>
            移回未分配
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
    </div>
  );
}

/**
 * Returns the outcome a file just reached during a run, so the card can
 * play its one-off motion. Results restored on load are not "just reached".
 */
function useSettled(state: NodeState) {
  const [tracked, setTracked] = useState<{ state: NodeState; settled: NodeState | null }>({
    state,
    settled: null,
  });
  if (tracked.state !== state) {
    const finished =
      (tracked.state === "running" || tracked.state === "waiting") &&
      (state === "succeeded" || state === "failed");
    setTracked({ state, settled: finished ? state : null });
  }
  return tracked.state === state ? tracked.settled : null;
}

export function DragGhost({ path }: { path: string }) {
  return (
    <div className="flex h-11 w-72 rotate-[-2deg] items-center gap-2 rounded-(--r-xl) bg-kumo-base px-3 shadow-(--sh-pop) ring-[1.5px] ring-kumo-brand">
      <DotsSixVerticalIcon size={16} weight="bold" className="text-kumo-subtle" />
      <span className="truncate font-mono font-medium">{fileName(path)}</span>
    </div>
  );
}
