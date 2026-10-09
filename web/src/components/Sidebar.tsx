import { Button, Checkbox, DropdownMenu, Loader, cn } from "@cloudflare/kumo";
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
import { driverTone } from "../lib/driver";

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
    <aside className="flex flex-[1_1_272px] flex-col gap-5 border-r border-kumo-hairline bg-kumo-base px-3 py-4">
      <section className="flex flex-col gap-1">
        <div className="flex h-7 items-center justify-between px-2.5 text-xs font-medium text-kumo-subtle">
          <span>数据库连接</span>
          <Button
            variant="ghost"
            size="xs"
            shape="square"
            icon={PlusIcon}
            aria-label="添加连接"
            onClick={() => openConnectionDialog()}
          />
        </div>
        {workspace.connections.length === 0 && unconfigured.length === 0 ? (
          <div className="flex flex-col gap-2.5 rounded-lg bg-kumo-recessed p-3 text-sm text-kumo-subtle">
            <span>还没有连接。SQL 文件分配到连接后才能执行。</span>
            <Button className="self-start" icon={PlusIcon} onClick={() => openConnectionDialog()}>
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
              className="flex w-full cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-left outline-1 -outline-offset-1 outline-kumo-interact outline-dashed hover:bg-kumo-tint"
            >
              <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-kumo-warning-tint text-kumo-warning">
                <WarningIcon size={16} weight="fill" />
              </span>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate font-medium">{item.name}</span>
                <span className="truncate text-xs text-kumo-warning">
                  未配置{label ? ` · ${label}` : ""} · {item.files} 个文件 · 点击配置
                </span>
              </span>
            </button>
          );
        })}
      </section>

      <section className="flex flex-col gap-1">
        <div className="flex h-7 items-center justify-between px-2.5 text-xs font-medium text-kumo-subtle">
          <span>未分配文件 · {unassigned.length}</span>
          {unassigned.length > 0 && (
            <button
              type="button"
              className="cursor-pointer text-kumo-link hover:underline"
              onClick={() => setChecked(allChecked ? [] : unassigned.map((file) => file.path))}
            >
              {allChecked ? "取消全选" : "全选"}
            </button>
          )}
        </div>
        {unassigned.length === 0 && (
          <p className="m-0 px-2.5 py-1 text-sm text-kumo-subtle">没有未分配的文件</p>
        )}
        {unassigned.map((file) => (
          <div
            key={file.path}
            className={cn(
              "flex items-start gap-2.5 rounded-lg px-2.5 py-2 hover:bg-kumo-tint",
              checked.includes(file.path) && "bg-kumo-info-tint hover:bg-kumo-info-tint",
              selectedFile === file.path && "ring-1 ring-kumo-line",
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
              <span className="truncate font-mono text-sm font-medium">{fileName(file.path)}</span>
              <span className="truncate font-mono text-xs text-kumo-subtle">./{file.path}</span>
            </button>
          </div>
        ))}
        {checked.length > 0 && (
          <div className="mt-1.5 flex items-center justify-between gap-2 rounded-xl bg-kumo-contrast py-2 pr-2 pl-3 text-kumo-inverse">
            <span className="text-sm">已选 {checked.length} 个</span>
            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <Button size="sm" variant="secondary">
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

      <div className="flex flex-col gap-2 rounded-xl bg-kumo-recessed p-3 text-sm text-kumo-subtle">
        <span className="font-medium text-kumo-default">自动归属</span>
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
  let indicator = <span className="size-2 rounded-full bg-kumo-interact" />;
  if (status === "running") {
    subtitle = "运行中";
    indicator = <Loader size="sm" className="text-kumo-brand" />;
  } else if (status === "failed" || status === "cancelled") {
    subtitle = state?.lastRun?.error
      ? "连接失败"
      : status === "failed"
        ? "已暂停 · 有文件失败"
        : "已停止";
    indicator = <XCircleIcon size={16} weight="fill" className="text-kumo-danger" />;
  } else if (status === "succeeded") {
    indicator = <CheckCircleIcon size={16} weight="fill" className="text-kumo-success" />;
  }

  return (
    <button
      type="button"
      onClick={() => select(connection.name)}
      className={cn(
        "flex w-full cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-left hover:bg-kumo-tint",
        selected && "bg-kumo-tint ring-1 ring-kumo-line",
      )}
    >
      <span
        className={cn(
          "flex size-7 shrink-0 items-center justify-center rounded-md",
          driverTone(driver),
        )}
      >
        <DatabaseIcon size={16} />
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate font-medium">{connection.name}</span>
        <span
          className={cn(
            "truncate text-xs",
            status === "running"
              ? "text-kumo-link"
              : status === "failed" || status === "cancelled"
                ? "text-kumo-danger"
                : "text-kumo-subtle",
          )}
        >
          {subtitle}
        </span>
      </span>
      {indicator}
    </button>
  );
}
