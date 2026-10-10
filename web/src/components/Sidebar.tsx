import { Checkbox, DropdownMenu, cn } from "@cloudflare/kumo";
import {
  CaretDownIcon,
  CheckCircleIcon,
  DatabaseIcon,
  PlusIcon,
  UploadSimpleIcon,
  WarningIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import type { Connection, DriverInfo, Workspace } from "../api";
import { allPaths } from "../lib/arrangement";
import { fileName, type ConnectionState } from "../lib/model";
import { useStore } from "../store";
import { useUi } from "../ui";
import { Button } from "./Button";
import { Logo } from "./Logo";

export function Sidebar({ workspace }: { workspace: Workspace }) {
  const connections = useStore((state) => state.connections);
  const checked = useStore((state) => state.checked);
  const setChecked = useStore((state) => state.setChecked);
  const selectFile = useStore((state) => state.selectFile);
  const selectedFile = useStore((state) => state.selectedFile);
  const assign = useStore((state) => state.assign);
  const openConnectionDialog = useUi((state) => state.openConnectionDialog);

  const arranged = new Set(Object.values(connections).flatMap((state) => allPaths(state.steps)));
  // Arranged here (e.g. unpacked from a package) but missing from this directory's config.
  const unconfigured = Object.entries(connections)
    .filter(([, state]) => state.steps.length > 0)
    .filter(([name]) => !workspace.connections.some((connection) => connection.name === name))
    .map(([name, state]) => ({
      name,
      files: allPaths(state.steps).length,
      driver: workspace.arrangement?.connections[name]?.driver,
    }));
  const unassigned = workspace.files.filter((file) => !arranged.has(file.path));
  const allChecked =
    unassigned.length > 0 && unassigned.every((file) => checked.includes(file.path));

  const toggle = (path: string, value: boolean) =>
    setChecked(value ? [...checked, path] : checked.filter((item) => item !== path));

  return (
    <aside className="flex min-w-0 flex-[1_1_240px] flex-col gap-1.5 pb-2">
      <div className="flex h-11 items-center gap-2 px-3">
        <Logo size={22} />
        <span className="text-[15px] font-bold tracking-[-0.01em]">SchemaPilot</span>
      </div>

      <section className="flex flex-col gap-0.5">
        <div className={labelClass}>
          <span>数据库连接</span>
          <Button
            variant="ghost"
            size="xs"
            icon={PlusIcon}
            aria-label="添加连接"
            onClick={() => openConnectionDialog()}
          />
        </div>
        {workspace.connections.length === 0 && unconfigured.length === 0 ? (
          <div className={cn(cardClass, "flex flex-col gap-2.5 p-3 text-kumo-subtle")}>
            <span>还没有连接。SQL 文件分配到连接后才能执行。</span>
            <Button
              size="sm"
              className="self-start"
              icon={PlusIcon}
              onClick={() => openConnectionDialog()}
            >
              添加连接
            </Button>
          </div>
        ) : (
          workspace.connections.map((connection) => (
            <ConnectionItem
              key={connection.name}
              connection={connection}
              state={connections[connection.name]}
              driver={workspace.drivers.find((driver) => driver.id === connection.driver)}
            />
          ))
        )}
        {unconfigured.map((item) => {
          const label = workspace.drivers.find((driver) => driver.id === item.driver)?.label;
          return (
            <button
              key={item.name}
              type="button"
              onClick={() =>
                openConnectionDialog(undefined, { name: item.name, driver: item.driver })
              }
              className={cn(
                navClass,
                "outline-1 -outline-offset-1 outline-kumo-interact outline-dashed",
              )}
            >
              <span className="flex size-7 shrink-0 items-center justify-center rounded-(--r-lg) bg-kumo-warning-tint text-kumo-warning">
                <WarningIcon size={16} />
              </span>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate">{item.name}</span>
                <span className="truncate text-xs text-kumo-warning">
                  未配置{label ? ` · ${label}` : ""} · {item.files} 个文件 · 点击配置
                </span>
              </span>
            </button>
          );
        })}
      </section>

      <section className="flex flex-col gap-0.5">
        <div className={labelClass}>
          <span>未分配文件 · {unassigned.length}</span>
          {unassigned.length > 0 && (
            <button
              type="button"
              className="cursor-pointer font-medium text-kumo-link hover:underline"
              onClick={() => setChecked(allChecked ? [] : unassigned.map((file) => file.path))}
            >
              {allChecked ? "取消全选" : "全选"}
            </button>
          )}
        </div>
        {unassigned.length === 0 && (
          <p className="m-0 px-3 py-1 text-(--side-muted)">没有未分配的文件</p>
        )}
        {unassigned.length > 0 && (
          <div className={cn(cardClass, "flex flex-col gap-0.5 p-1")}>
            {unassigned.map((file) => (
              <div
                key={file.path}
                className={cn(
                  "flex items-start gap-2.5 rounded-(--r-lg) px-2 py-2 hover:bg-kumo-tint",
                  checked.includes(file.path) && "bg-(--primary-tint) hover:bg-(--primary-tint)",
                  selectedFile === file.path && "ring-[1.5px] ring-kumo-brand",
                )}
              >
                <Checkbox
                  className="mt-0.5"
                  aria-label={`选择 ${file.path}`}
                  checked={checked.includes(file.path)}
                  onCheckedChange={(value) => toggle(file.path, value === true)}
                />
                <button
                  type="button"
                  className="flex min-w-0 cursor-pointer flex-col text-left"
                  onClick={() => selectFile(file.path)}
                >
                  <span className="truncate font-mono font-medium">{fileName(file.path)}</span>
                  <span className="truncate font-mono text-[11px] text-kumo-subtle">
                    ./{file.path}
                  </span>
                </button>
              </div>
            ))}
          </div>
        )}
        {checked.length > 0 && (
          <div className="mt-1.5 flex items-center justify-between gap-2 rounded-(--r-xl) bg-(--fg) py-1.5 pr-1.5 pl-3 text-white">
            <span>已选 {checked.length} 个</span>
            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <Button size="sm">
                    分配到…
                    <CaretDownIcon size={14} />
                  </Button>
                }
              />
              <DropdownMenu.Content>
                <DropdownMenu.Group>
                  <DropdownMenu.Label>分配 {checked.length} 个文件到</DropdownMenu.Label>
                  {workspace.connections.map((connection) => (
                    <DropdownMenu.Item
                      key={connection.name}
                      icon={DatabaseIcon}
                      onClick={() => assign(checked, connection.name)}
                    >
                      {connection.name}
                    </DropdownMenu.Item>
                  ))}
                </DropdownMenu.Group>
                {workspace.connections.length > 0 && <DropdownMenu.Separator />}
                <DropdownMenu.Item icon={PlusIcon} onClick={() => openConnectionDialog()}>
                  新建连接…
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu>
          </div>
        )}
      </section>

      <div className="min-h-4 flex-1" />
      <div className={cn(cardClass, "flex flex-col gap-1.5 p-3 text-xs text-kumo-subtle")}>
        <span className="text-[13px] font-medium text-kumo-default">自动归属</span>
        <span>
          与连接同名的子目录会被递归扫描，例如{" "}
          <span className="font-mono text-[0.9em] text-kumo-default">./pg-main/</span> 下的 SQL
          自动归属到 pg-main。
        </span>
        <span className="flex items-start gap-1.5">
          <span className="flex h-[1lh] items-center">
            <UploadSimpleIcon size={14} />
          </span>
          把 .sql 文件拖进窗口，会复制到当前目录。
        </span>
      </div>
    </aside>
  );
}

function ConnectionItem({
  connection,
  state,
  driver,
}: {
  connection: Connection;
  state?: ConnectionState;
  driver?: DriverInfo;
}) {
  const selected = useStore((store) => store.selectedConnection === connection.name);
  const select = useStore((store) => store.select);
  const count = state ? allPaths(state.steps).length : 0;
  const status = state?.lastRun?.status;

  let subtitle = `${driver?.label ?? connection.driver} · ${count} 个文件`;
  let indicator = <span className="mx-[5px] size-1.5 rounded-full bg-kumo-interact" />;
  if (status === "running") {
    subtitle = "运行中";
    indicator = <span className="spin size-3 text-kumo-info" />;
  } else if (status === "failed" || status === "cancelled") {
    subtitle = state?.lastRun?.error
      ? "连接失败"
      : status === "failed"
        ? "已暂停 · 有文件失败"
        : "已停止";
    indicator = <XCircleIcon size={16} className="text-kumo-danger" />;
  } else if (status === "succeeded") {
    indicator = <CheckCircleIcon size={16} className="text-kumo-success" />;
  }

  return (
    <button
      type="button"
      onClick={() => select(connection.name)}
      aria-current={selected ? "page" : undefined}
      className={cn(navClass, selected && "bg-(--sidebar-accent) hover:bg-(--sidebar-accent)")}
    >
      <span className="flex size-7 shrink-0 items-center justify-center rounded-(--r-lg) bg-kumo-base text-(--fg-2) shadow-(--sh-card)">
        <DatabaseIcon size={16} />
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={cn("truncate", selected && "font-medium")}>{connection.name}</span>
        <span
          className={cn(
            "truncate text-xs",
            status === "running"
              ? "text-kumo-info"
              : status === "failed" || status === "cancelled"
                ? "text-kumo-danger"
                : "text-(--side-muted)",
          )}
        >
          {subtitle}
        </span>
      </span>
      {indicator}
    </button>
  );
}

const labelClass =
  "flex items-center justify-between px-3 pt-2.5 pb-1 text-[11px] text-kumo-subtle";

const navClass =
  "flex w-full cursor-pointer items-center gap-2.5 rounded-(--r-xl) px-2 py-1.5 text-left transition-colors hover:bg-(--sidebar-accent)/66";

const cardClass = "rounded-(--r-xl) border border-(--border-soft) bg-kumo-base shadow-(--sh-card)";
