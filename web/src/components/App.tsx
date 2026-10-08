import { useEffect, useState } from "react";
import { Banner, Button, Empty, Loader } from "@cloudflare/kumo";
import { UploadSimpleIcon, WarningCircleIcon, XCircleIcon } from "@phosphor-icons/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useStore } from "../store";
import { notifyError, toasts } from "../toasts";
import { ConnectionDialog } from "./ConnectionDialog";
import { ConnectionView } from "./ConnectionView";
import { EmptyState } from "./EmptyState";
import { Header } from "./Header";
import { RunSync } from "./RunSync";
import { Sidebar } from "./Sidebar";

export function App() {
  const query = useQuery({ queryKey: ["workspace"], queryFn: api.workspace });
  const sync = useStore((store) => store.sync);
  const workspace = useStore((store) => store.workspace);
  const selected = useStore((store) => store.selectedConnection);
  const dropping = useFileDrop();

  useEffect(() => {
    if (query.data) sync(query.data);
  }, [query.data, sync]);

  if (!workspace) {
    return (
      <div className="flex min-h-dvh items-center justify-center">
        {query.isError ? (
          <Empty
            icon={<WarningCircleIcon size={40} />}
            title="无法读取工作目录"
            description={query.error.message}
            contents={<Button onClick={() => query.refetch()}>重试</Button>}
          />
        ) : (
          <Loader size="lg" />
        )}
      </div>
    );
  }

  const connection = workspace.connections.find((item) => item.name === selected);
  return (
    <div className="flex min-h-dvh flex-col">
      <Header
        workspace={workspace}
        refreshing={query.isFetching}
        onRescan={() => query.refetch()}
      />
      <div className="flex flex-1 flex-wrap items-stretch">
        <Sidebar workspace={workspace} />
        <main className="flex min-w-0 flex-[999_1_560px] flex-col">
          {workspace.configError && (
            <div className="px-6 pt-5">
              <Banner
                variant="error"
                icon={<XCircleIcon weight="fill" />}
                title={`${workspace.configFile} 无法解析`}
                description={`${workspace.configError}。修正文件后点“重新扫描目录”。`}
              />
            </div>
          )}
          {connection ? (
            <ConnectionView key={connection.name} workspace={workspace} connection={connection} />
          ) : (
            <EmptyState workspace={workspace} />
          )}
        </main>
      </div>
      {workspace.connections.map((item) => (
        <RunSync key={item.name} name={item.name} />
      ))}
      <ConnectionDialog workspace={workspace} />
      {dropping && (
        <div className="pointer-events-none fixed inset-3 z-50 flex items-center justify-center rounded-2xl bg-kumo-base/85 outline-2 outline-kumo-brand outline-dashed">
          <div className="flex flex-col items-center gap-2 text-kumo-link">
            <UploadSimpleIcon size={32} />
            <span className="text-lg font-semibold">松开以复制到当前目录</span>
            <span className="text-sm text-kumo-subtle">
              只会导入 .sql 文件，同名文件会自动加后缀
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

function useFileDrop() {
  const queryClient = useQueryClient();
  const [dropping, setDropping] = useState(false);

  useEffect(() => {
    let depth = 0;
    const carriesFiles = (event: DragEvent) => event.dataTransfer?.types.includes("Files") ?? false;
    const enter = (event: DragEvent) => {
      if (!carriesFiles(event)) return;
      depth++;
      setDropping(true);
    };
    const leave = (event: DragEvent) => {
      if (!carriesFiles(event)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDropping(false);
    };
    const over = (event: DragEvent) => {
      if (carriesFiles(event)) event.preventDefault();
    };
    const drop = async (event: DragEvent) => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      depth = 0;
      setDropping(false);
      const files = [...(event.dataTransfer?.files ?? [])].filter((file) =>
        /\.sql$/i.test(file.name),
      );
      if (files.length === 0) {
        toasts.add({ title: "没有可导入的 .sql 文件", variant: "warning" });
        return;
      }
      try {
        const { paths } = await api.importFiles(files);
        toasts.add({
          title: `已复制 ${paths.length} 个文件到当前目录`,
          description: paths.join("、"),
          variant: "success",
        });
        await queryClient.invalidateQueries({ queryKey: ["workspace"] });
      } catch (error) {
        notifyError("导入失败", error);
      }
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragleave", leave);
    window.addEventListener("dragover", over);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("dragover", over);
      window.removeEventListener("drop", drop);
    };
  }, [queryClient]);

  return dropping;
}
