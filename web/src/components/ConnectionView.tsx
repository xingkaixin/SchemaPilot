import { useState } from "react";
import { Badge, Banner, Button, Loader, Meter } from "@cloudflare/kumo";
import {
  ArrowCounterClockwiseIcon,
  PlayIcon,
  StopIcon,
  WarningCircleIcon,
  WarningIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { api, type Connection, type Workspace } from "../api";
import { allPaths } from "../lib/arrangement";
import {
  buildSteps,
  emptyConnection,
  fileName,
  formatClock,
  nodeState,
  runOverview,
  type ConnectionState,
  type RunMode,
} from "../lib/model";
import { useNow } from "../lib/useNow";
import { useStore } from "../store";
import { notifyError } from "../toasts";
import { useUi } from "../ui";
import { ConfirmDialog } from "./ConfirmDialog";
import { DetailPanel } from "./DetailPanel";
import { Pipeline } from "./Pipeline";
import { driverBadge } from "../lib/driver";

type Confirm = "stop" | "all" | "clear" | null;

export function ConnectionView({
  workspace,
  connection,
}: {
  workspace: Workspace;
  connection: Connection;
}) {
  const name = connection.name;
  const state = useStore((store) => store.connections[name]) ?? emptyConnection();
  const selectedFile = useStore((store) => store.selectedFile);
  const { prepareRun, applyRun, setDisabled, clear } = useStore.getState();
  const openConnectionDialog = useUi((ui) => ui.openConnectionDialog);
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState<Confirm>(null);
  const [busy, setBusy] = useState(false);

  const existing = new Set(workspace.files.map((file) => file.path));
  const overview = runOverview(state, existing);
  const driver = workspace.drivers.find((item) => item.id === connection.driver);
  const address = driver?.file
    ? connection.database
    : `${connection.user ? `${connection.user}@` : ""}${connection.host}:${connection.port || driver?.defaultPort || ""}/${connection.database}`;
  const blocked = overview.missing.length > 0 || overview.enabled.length === 0;

  const start = async (mode: RunMode) => {
    const steps = buildSteps(state, mode);
    if (steps.length === 0) return;
    setBusy(true);
    try {
      prepareRun(name, mode, allPaths(steps));
      const run = await api.startRun({ connection: name, steps });
      applyRun(name, run);
      queryClient.setQueryData(["run", name], run);
    } catch (error) {
      notifyError("无法开始运行", error);
    } finally {
      setBusy(false);
    }
  };

  const stop = async () => {
    try {
      await api.stopRun(name);
      await queryClient.invalidateQueries({ queryKey: ["run", name] });
    } catch (error) {
      notifyError("停止失败", error);
    }
  };

  const currentFile = Object.values(state.results).find((result) => result.status === "running");
  const enabledInRun = Object.values(state.results).filter((result) =>
    overview.enabled.includes(result.path),
  );

  return (
    <div className="flex min-w-0 flex-col gap-4 px-6 pt-5 pb-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex items-center gap-2">
            <h1 className="m-0 text-xl font-semibold">{name}</h1>
            <Badge variant={driverBadge(connection.driver)}>
              {driver?.label ?? connection.driver}
            </Badge>
          </div>
          <span className="truncate font-mono text-sm text-kumo-subtle">{address}</span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button disabled={overview.running} onClick={() => openConnectionDialog(name)}>
            编辑连接
          </Button>
          {overview.running ? (
            <Button variant="destructive" icon={StopIcon} onClick={() => setConfirm("stop")}>
              停止
            </Button>
          ) : overview.anySucceeded && !overview.allSucceeded ? (
            <>
              <Button
                icon={ArrowCounterClockwiseIcon}
                disabled={blocked || busy}
                onClick={() => setConfirm("all")}
              >
                全部重跑
              </Button>
              <Button
                variant="primary"
                icon={PlayIcon}
                loading={busy}
                disabled={blocked}
                onClick={() => start("continue")}
              >
                {overview.interrupted ? "从失败处继续" : "继续执行"}
              </Button>
            </>
          ) : overview.allSucceeded ? (
            <Button
              variant="primary"
              icon={ArrowCounterClockwiseIcon}
              loading={busy}
              disabled={blocked}
              onClick={() => setConfirm("all")}
            >
              全部重跑
            </Button>
          ) : (
            <Button
              variant="primary"
              icon={PlayIcon}
              loading={busy}
              disabled={blocked}
              onClick={() => start("start")}
            >
              运行
            </Button>
          )}
        </div>
      </div>

      {overview.missing.length > 0 && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={`${overview.missing.length} 个文件已不存在，暂时无法运行`}
          description={
            <>
              <span className="font-mono text-[0.9em]">
                {overview.missing.map((path) => `./${path}`).join("、")}
              </span>{" "}
              已从目录中移除。放回文件后点“重新扫描目录”即可恢复；不需要的话就禁用它们。
            </>
          }
          action={
            <Button size="sm" onClick={() => setDisabled(name, overview.missing, true)}>
              禁用缺失文件
            </Button>
          }
        />
      )}

      {!overview.running && <OutcomeBanner state={state} />}

      {overview.running && (
        <RunProgress
          startedAt={state.lastRun?.startedAt}
          done={enabledInRun.filter((result) => result.status === "succeeded").length}
          total={enabledInRun.length}
          current={currentFile ? fileName(currentFile.path) : undefined}
        />
      )}

      <div className="flex flex-wrap items-start gap-4">
        <Pipeline
          name={name}
          state={state}
          workspace={workspace}
          onClear={() => setConfirm("clear")}
        />
        {selectedFile && (
          <DetailPanel
            key={selectedFile}
            path={selectedFile}
            file={workspace.files.find((file) => file.path === selectedFile)}
            connection={allPaths(state.steps).includes(selectedFile) ? connection : undefined}
            state={allPaths(state.steps).includes(selectedFile) ? state : undefined}
            nodeState={
              allPaths(state.steps).includes(selectedFile)
                ? nodeState(selectedFile, state, existing)
                : undefined
            }
          />
        )}
      </div>

      <ConfirmDialog
        open={confirm === "stop"}
        title="停止执行？"
        description={`会取消 ${name} 上正在运行的语句${currentFile ? `（${fileName(currentFile.path)} 第 ${currentFile.current ?? "?"} 条）` : ""}。已提交的语句不会回滚。`}
        cancelLabel="继续运行"
        confirmLabel="停止"
        destructive
        onConfirm={stop}
        onClose={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === "all"}
        title="从头重跑全部文件？"
        description={`${overview.enabled.length} 个启用的文件会全部重新执行，包括已经成功的文件。请确认这些 SQL 可以重复执行。`}
        confirmLabel="全部重跑"
        onConfirm={() => start("all")}
        onClose={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === "clear"}
        title={`清空 ${name} 的编排？`}
        description={`执行记录会被删除，手动分配的文件移回未分配，./${name}/ 下的文件按文件名重新排列。磁盘上的 SQL 文件和连接配置不受影响。`}
        confirmLabel="清空"
        destructive
        onConfirm={() => clear(name)}
        onClose={() => setConfirm(null)}
      />
    </div>
  );
}

function OutcomeBanner({ state }: { state: ConnectionState }) {
  const run = state.lastRun;
  if (!run || (run.status !== "failed" && run.status !== "cancelled")) return null;
  if (run.error) {
    return (
      <Banner
        variant="error"
        icon={<XCircleIcon weight="fill" />}
        title="无法连接数据库"
        description={run.error}
      />
    );
  }
  const broken = Object.values(state.results).find(
    (result) =>
      (result.status === "failed" || result.status === "cancelled") &&
      state.steps.flat(2).includes(result.path),
  );
  if (!broken) return null;
  const stepNumber = state.steps.findIndex((step) => allPaths([step]).includes(broken.path)) + 1;
  const where = broken.error ? ` 的第 ${broken.error.index} 条语句` : "";
  if (broken.status === "cancelled") {
    return (
      <Banner
        variant="secondary"
        icon={<WarningCircleIcon weight="fill" />}
        title={`执行已停止：第 ${stepNumber} 步 ${fileName(broken.path)}${where}被终止`}
        description="点“从失败处继续”会从这个文件的第 1 条语句重新开始，已成功的文件不会再执行。"
      />
    );
  }
  return (
    <Banner
      variant="error"
      icon={<XCircleIcon weight="fill" />}
      title={`执行已暂停：第 ${stepNumber} 步 ${fileName(broken.path)}${where}失败`}
      description={
        broken.executed > 0
          ? `该文件前 ${broken.executed} 条语句已提交，后续步骤未执行。修复文件后点“从失败处继续”，会从这个文件的第 1 条语句重新执行；不需要它就先禁用再继续。`
          : (broken.message ??
            "后续步骤未执行。修复文件后点“从失败处继续”，会从这个文件重新执行；不需要它就先禁用再继续。")
      }
    />
  );
}

function RunProgress({
  startedAt,
  done,
  total,
  current,
}: {
  startedAt?: string;
  done: number;
  total: number;
  current?: string;
}) {
  const now = useNow(true);
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-3 rounded-xl bg-kumo-base px-4 py-3.5 shadow-xs ring ring-kumo-line">
      <div className="flex items-center gap-2">
        <Loader size="sm" className="text-kumo-brand" />
        <span className="font-medium">运行中</span>
      </div>
      <Meter
        className="min-w-0 flex-[1_1_280px]"
        label={`已完成 ${done} / ${total} 个文件`}
        customValue={current ? `正在执行 ${current}` : ""}
        value={total ? Math.round((done / total) * 100) : 0}
      />
      <dl className="m-0 flex gap-6 text-sm">
        <div>
          <dt className="text-kumo-subtle">开始于</dt>
          <dd className="m-0 font-medium">
            {startedAt ? new Date(startedAt).toLocaleTimeString() : "—"}
          </dd>
        </div>
        <div>
          <dt className="text-kumo-subtle">已用时</dt>
          <dd className="m-0 font-mono font-medium">
            {startedAt ? formatClock(now - Date.parse(startedAt)) : "—"}
          </dd>
        </div>
      </dl>
    </div>
  );
}
