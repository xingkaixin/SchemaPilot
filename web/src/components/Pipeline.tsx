import { Fragment, useState, type ReactNode } from "react";
import { Button, cn } from "@cloudflare/kumo";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  pointerWithin,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
} from "@dnd-kit/core";
import {
  ArrowDownIcon,
  FolderSimpleIcon,
  GitForkIcon,
  SortAscendingIcon,
} from "@phosphor-icons/react";
import type { Workspace } from "../api";
import {
  allPaths,
  extractToOwnStep,
  moveFile,
  parallelWithPrevious,
  pathsAfter,
  arrangeByName,
  shift,
  type DropTarget,
  type Step,
} from "../lib/arrangement";
import { nodeState, stepState, type ConnectionState, type NodeState } from "../lib/model";
import { useStore } from "../store";
import { DragGhost, FileNode, type NodeActions } from "./FileNode";

// Prefer the narrow insertion zones over the step container they sit in.
const collision: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  const precise = hits.filter((hit) => !String(hit.id).startsWith("step:"));
  return precise.length > 0 ? precise : hits;
};

function parseTarget(id: string): DropTarget {
  const [kind, ...numbers] = id.split(":");
  const [a, b, c] = numbers.map(Number);
  if (kind === "gap") return { kind, index: a };
  if (kind === "step") return { kind, step: a };
  return { kind: "lane", step: a, lane: b, position: c };
}

export function Pipeline({
  name,
  state,
  workspace,
  onClear,
}: {
  name: string;
  state: ConnectionState;
  workspace: Workspace;
  onClear: () => void;
}) {
  const arrange = useStore((store) => store.arrange);
  const [dragging, setDragging] = useState<string | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const locked = state.lastRun?.status === "running";
  const existing = new Set(workspace.files.map((file) => file.path));
  const steps = state.steps;
  const paths = allPaths(steps);
  const states = paths.map((path) => nodeState(path, state, existing));
  const hasDirectory = workspace.files.some((file) => file.connection === name);

  const tally = (value: NodeState) => states.filter((item) => item === value).length;
  const ran = states.some((item) =>
    ["succeeded", "failed", "cancelled", "running", "waiting", "notRun"].includes(item),
  );
  const summary = ran
    ? [
        [tally("succeeded"), "成功"],
        [tally("running"), "执行中"],
        [tally("failed"), "失败"],
        [tally("cancelled"), "已终止"],
        [tally("missing"), "缺失"],
        [tally("waiting"), "等待"],
        [tally("notRun") + tally("idle"), "未执行"],
        [tally("disabled"), "禁用"],
      ]
        .filter(([count]) => count)
        .map(([count, label]) => `${count} ${label}`)
        .join(" · ")
    : `${steps.length} 步 · ${paths.length} 个文件${tally("disabled") ? ` · ${tally("disabled")} 个禁用` : ""}`;

  return (
    <section className="flex min-w-0 flex-[999_1_560px] flex-col rounded-xl bg-kumo-base shadow-xs ring ring-kumo-line">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-kumo-hairline px-4 py-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex items-baseline gap-2">
            <h2 className="m-0 text-lg font-semibold">执行顺序</h2>
            <span className="text-sm text-kumo-subtle">
              {locked ? "运行中不能调整顺序" : summary}
            </span>
          </div>
          {hasDirectory && (
            <span className="flex items-center gap-1.5 text-xs text-kumo-subtle">
              <FolderSimpleIcon size={14} />
              自动包含 <span className="font-mono">./{name}/</span> 下的文件
            </span>
          )}
        </div>
        <div className="flex gap-1">
          <Button
            variant="ghost"
            icon={SortAscendingIcon}
            disabled={locked || paths.length < 2}
            onClick={() => arrange(name, (current) => arrangeByName(allPaths(current)))}
          >
            按文件名重排
          </Button>
          <Button variant="ghost" disabled={locked || paths.length === 0} onClick={onClear}>
            清空编排
          </Button>
        </div>
      </div>

      {steps.length === 0 ? (
        <div className="dot-grid flex flex-1 items-center justify-center px-6 py-12 text-center text-sm text-kumo-subtle">
          <p className="m-0 max-w-sm">
            还没有文件。在左侧勾选未分配的文件并分配到 {name}，或在当前目录下创建{" "}
            <span className="font-mono text-[0.9em] text-kumo-default">./{name}/</span>{" "}
            子目录后重新扫描。
          </p>
        </div>
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={collision}
          onDragStart={({ active }) => setDragging(String(active.id))}
          onDragCancel={() => setDragging(null)}
          onDragEnd={({ active, over }) => {
            setDragging(null);
            if (over)
              arrange(name, (current) =>
                moveFile(current, String(active.id), parseTarget(String(over.id))),
              );
          }}
        >
          <div className="dot-grid flex flex-1 flex-col px-5 pt-5 pb-6">
            {steps.map((step, index) => (
              <Fragment key={allPaths([step]).join("|")}>
                <Gap index={index} dragging={dragging} edge={index === 0} />
                <StepRow
                  index={index}
                  step={step}
                  steps={steps}
                  name={name}
                  state={state}
                  existing={existing}
                  workspace={workspace}
                  dragging={dragging}
                  locked={locked}
                />
              </Fragment>
            ))}
            <Gap index={steps.length} dragging={dragging} edge />
          </div>
          <DragOverlay dropAnimation={null}>
            {dragging && <DragGhost path={dragging} />}
          </DragOverlay>
        </DndContext>
      )}

      <div className="border-t border-kumo-hairline px-4 py-2.5 text-sm text-kumo-subtle">
        拖动左侧把手调整顺序：放在两步之间成为新的一步，放到某一步上与它并行执行。更多操作在每个文件的
        ⋯ 菜单里。
      </div>
    </section>
  );
}

function Gap({
  index,
  dragging,
  edge,
}: {
  index: number;
  dragging: string | null;
  edge?: boolean;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `gap:${index}`, disabled: !dragging });
  return (
    <div
      ref={setNodeRef}
      className={cn("flex items-center gap-2 pl-14", dragging ? "h-6" : edge ? "h-0" : "h-3")}
    >
      {isOver && (
        <>
          <span className="h-0.5 flex-1 rounded-full bg-kumo-brand" />
          <span className="rounded-full bg-kumo-brand px-2 text-xs leading-[22px] font-medium text-white">
            新的第 {index + 1} 步
          </span>
        </>
      )}
    </div>
  );
}

const circleTone: Record<string, string> = {
  succeeded: "bg-kumo-success text-white ring-0",
  failed: "bg-kumo-danger text-white ring-0",
  running: "bg-kumo-info-tint text-kumo-link ring-2 ring-kumo-brand",
  missing: "bg-kumo-warning text-white ring-0",
  disabled:
    "bg-kumo-recessed text-kumo-subtle ring-0 outline-[1.5px] -outline-offset-[1.5px] outline-kumo-interact outline-dashed",
};

function StepRow({
  index,
  step,
  steps,
  name,
  state,
  existing,
  workspace,
  dragging,
  locked,
}: {
  index: number;
  step: Step;
  steps: Step[];
  name: string;
  state: ConnectionState;
  existing: Set<string>;
  workspace: Workspace;
  dragging: string | null;
  locked: boolean;
}) {
  const parallel = step.length > 1;
  const ownSingle = !parallel && step[0][0] === dragging;
  const { setNodeRef, isOver } = useDroppable({
    id: `step:${index}`,
    disabled: !dragging || ownSingle,
  });
  const last = index === steps.length - 1;
  const tone =
    circleTone[stepState(step, state, existing)] ??
    "bg-kumo-base text-kumo-subtle ring-[1.5px] ring-kumo-interact";

  const render = (path: string) => (
    <NodeFor
      key={path}
      path={path}
      index={index}
      steps={steps}
      name={name}
      state={state}
      existing={existing}
      workspace={workspace}
      locked={locked}
    />
  );

  let content: ReactNode;
  if (!parallel) {
    content = render(step[0][0]);
  } else {
    content = (
      <div className="flex flex-col gap-2 rounded-[18px] bg-kumo-recessed p-2">
        <div className="flex h-6 items-center gap-1.5 px-1.5 text-xs text-kumo-subtle">
          <GitForkIcon size={14} />
          并行 {step.length} 路 · 全部完成后{last ? "结束" : `进入第 ${index + 2} 步`}
        </div>
        {/* The padding keeps the 2px rings of running or failed cards inside the scroll box. */}
        <div className="-m-1 flex gap-2 overflow-x-auto p-1">
          {step.map((lane, laneIndex) => (
            <div key={lane.join("|")} className="flex min-w-[220px] flex-1 flex-col">
              {dragging && <LaneGap id={`lane:${index}:${laneIndex}:0`} dragging />}
              {lane.map((path, position) => (
                <Fragment key={path}>
                  {position > 0 &&
                    (dragging ? (
                      <LaneGap id={`lane:${index}:${laneIndex}:${position}`} dragging />
                    ) : (
                      <span className="flex h-[18px] items-center justify-center text-kumo-inactive">
                        <ArrowDownIcon size={12} weight="bold" />
                      </span>
                    ))}
                  {render(path)}
                </Fragment>
              ))}
              {dragging && <LaneGap id={`lane:${index}:${laneIndex}:${lane.length}`} dragging />}
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[56px_minmax(0,1fr)] items-start">
      <div className="relative self-stretch">
        <span
          className={cn(
            "relative z-10 ml-4 flex size-6 items-center justify-center rounded-full text-xs font-semibold",
            parallel ? "mt-2" : "mt-4",
            tone,
          )}
        >
          {index + 1}
        </span>
        {!last && (
          <span
            className={cn(
              "absolute -bottom-10 left-[27px] w-[1.5px] bg-kumo-fill",
              parallel ? "top-5" : "top-7",
            )}
          />
        )}
      </div>
      <div
        ref={setNodeRef}
        className={cn(
          "min-w-0 rounded-[20px]",
          isOver && "bg-kumo-info-tint outline-2 outline-kumo-brand outline-dashed",
        )}
      >
        {content}
        {isOver && (
          <div className="m-1.5 flex h-11 items-center justify-center rounded-[10px] bg-kumo-brand/10 text-sm font-medium text-kumo-link">
            松开：加入第 {index + 1} 步并行执行
          </div>
        )}
      </div>
    </div>
  );
}

function LaneGap({ id, dragging }: { id: string; dragging: boolean }) {
  const { setNodeRef, isOver } = useDroppable({ id, disabled: !dragging });
  return (
    <div ref={setNodeRef} className="flex h-[18px] items-center">
      {isOver && <span className="h-0.5 flex-1 rounded-full bg-kumo-brand" />}
    </div>
  );
}

function NodeFor({
  path,
  index,
  steps,
  name,
  state,
  existing,
  workspace,
  locked,
}: {
  path: string;
  index: number;
  steps: Step[];
  name: string;
  state: ConnectionState;
  existing: Set<string>;
  workspace: Workspace;
  locked: boolean;
}) {
  const selected = useStore((current) => current.selectedFile === path);
  const store = useStore.getState();
  const alone = allPaths([steps[index]]).length === 1;
  const after = pathsAfter(steps, path);
  const disabled = state.disabled.includes(path);
  const arrange = (change: (steps: Step[]) => Step[]) => store.arrange(name, change);

  const actions: NodeActions = {
    select: () => store.selectFile(path),
    parallelWithPrevious:
      index > 0 ? () => arrange((s) => parallelWithPrevious(s, path)) : undefined,
    extract: alone ? undefined : () => arrange((s) => extractToOwnStep(s, path)),
    moveUp: index > 0 || !alone ? () => arrange((s) => shift(s, path, -1)) : undefined,
    moveDown:
      index < steps.length - 1 || !alone ? () => arrange((s) => shift(s, path, 1)) : undefined,
    toggleDisabled: () => store.setDisabled(name, [path], !disabled),
    disableAfter: after.length > 0 ? () => store.setDisabled(name, after, true) : undefined,
    moveTo: workspace.connections
      .filter((connection) => connection.name !== name)
      .map((connection) => ({
        name: connection.name,
        run: () => store.assign([path], connection.name),
      })),
    unassign: () => store.unassign(name, path),
  };

  return (
    <FileNode
      path={path}
      state={nodeState(path, state, existing)}
      result={state.results[path]}
      file={workspace.files.find((file) => file.path === path)}
      selected={selected}
      locked={locked}
      actions={actions}
    />
  );
}
