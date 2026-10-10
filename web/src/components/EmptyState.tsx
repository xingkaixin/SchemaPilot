import { DatabaseIcon, PlusIcon } from "@phosphor-icons/react";
import type { Workspace } from "../api";
import { useUi } from "../ui";
import { Button } from "./Button";

export function EmptyState({ workspace }: { workspace: Workspace }) {
  const openConnectionDialog = useUi((ui) => ui.openConnectionDialog);
  const steps = [
    <>
      添加 PostgreSQL 或 MySQL 连接。如果当前目录有同名子目录（如{" "}
      <span className="font-mono text-[0.9em]">./pg-main/</span>），里面的 SQL 会自动归属到该连接
    </>,
    "其余文件在左侧勾选后分配到连接；默认按文件名串行",
    "拖动文件调整顺序或设为并行，然后运行",
  ];
  return (
    <div className="flex flex-1 items-center justify-center px-6 py-16">
      <div className="flex max-w-[460px] flex-col items-center gap-5 text-center">
        <span className="flex size-12 items-center justify-center rounded-(--r-xl) border border-kumo-line bg-kumo-base text-(--fg-2) shadow-(--sh-card)">
          <DatabaseIcon size={24} />
        </span>
        <div className="flex flex-col gap-1.5">
          <h1 className="m-0 text-[28px] leading-[34px] font-light tracking-[-0.01em]">
            添加数据库连接，开始编排
          </h1>
          <p className="m-0 text-kumo-subtle">
            {workspace.configExists ? (
              "配置文件里还没有连接。"
            ) : (
              <>
                当前目录没有{" "}
                <span className="font-mono text-[0.9em] text-kumo-default">
                  {workspace.configFile}
                </span>
                。添加第一个连接时会自动创建，之后页面上的修改都会写回这个文件。
              </>
            )}
          </p>
        </div>
        <Button variant="primary" icon={PlusIcon} onClick={() => openConnectionDialog()}>
          添加连接
        </Button>
        <ol className="m-0 mt-2 flex w-full flex-col gap-2.5 rounded-(--r-xl) border border-kumo-line bg-kumo-base p-4 text-left shadow-(--sh-card)">
          {steps.map((step, index) => (
            <li key={index} className="flex list-none gap-2.5">
              <span className="font-mono text-kumo-placeholder">{index + 1}</span>
              <span>{step}</span>
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}
