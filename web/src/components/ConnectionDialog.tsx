import { useState } from "react";
import {
  Banner,
  Button,
  Collapsible,
  Dialog,
  Input,
  Select,
  SensitiveInput,
  cn,
} from "@cloudflare/kumo";
import { CheckCircleIcon, DatabaseIcon, XCircleIcon, XIcon } from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { api, type Connection, type DriverInfo, type TestResult, type Workspace } from "../api";
import { useStore } from "../store";
import { notifyError } from "../toasts";
import { useUi, type ConnectionPreset } from "../ui";
import { ConfirmDialog } from "./ConfirmDialog";
import { driverDatabaseLabel, driverParamsExample, driverTone } from "../lib/driver";

interface Draft {
  name: string;
  driver: string;
  host: string;
  port: string;
  database: string;
  user: string;
  password: string;
  params: string;
}

function toDraft(connection?: Connection, driver = "postgres"): Draft {
  return {
    name: connection?.name ?? "",
    driver: connection?.driver ?? driver,
    host: connection?.host ?? "127.0.0.1",
    port: connection?.port ? String(connection.port) : "",
    database: connection?.database ?? "",
    user: connection?.user ?? "",
    password: connection?.password ?? "",
    params: new URLSearchParams(connection?.params ?? {}).toString(),
  };
}

function toConnection(draft: Draft, file: boolean): Connection {
  const params = Object.fromEntries(new URLSearchParams(draft.params.trim()));
  return {
    name: draft.name.trim(),
    driver: draft.driver,
    host: file ? "" : draft.host.trim(),
    port: !file && draft.port ? Number(draft.port) : undefined,
    database: draft.database.trim(),
    user: file ? "" : draft.user.trim(),
    password: file ? "" : draft.password,
    params: Object.keys(params).length > 0 ? params : undefined,
  };
}

export function ConnectionDialog({ workspace }: { workspace: Workspace }) {
  const { open, editing, preset } = useUi((ui) => ui.connectionDialog);
  const close = useUi((ui) => ui.closeConnectionDialog);
  const existing = workspace.connections.find((connection) => connection.name === editing);
  // Remount the form whenever the dialog opens for a different connection.
  const formKey = `${open}-${editing ?? preset?.name ?? ""}`;
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && close()}>
      <Dialog size="lg" className="p-0">
        <ConnectionForm
          key={formKey}
          workspace={workspace}
          existing={existing}
          preset={preset}
          onDone={close}
        />
      </Dialog>
    </Dialog.Root>
  );
}

function ConnectionForm({
  workspace,
  existing,
  preset,
  onDone,
}: {
  workspace: Workspace;
  existing?: Connection;
  preset?: ConnectionPreset;
  onDone: () => void;
}) {
  const queryClient = useQueryClient();
  const { renameConnection, removeConnection, select } = useStore.getState();
  const [draft, setDraft] = useState(() => {
    const draft = toDraft(existing, preset?.driver ?? workspace.drivers[0]?.id);
    return preset && !existing ? { ...draft, name: preset.name } : draft;
  });
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [test, setTest] = useState<
    { ok: true; result: TestResult } | { ok: false; message: string } | null
  >(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const driver = workspace.drivers.find((item) => item.id === draft.driver);
  const file = driver?.file ?? false;
  const change = (patch: Partial<Draft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    setTest(null);
  };
  const dirName = draft.name.trim() || "连接名称";

  const runTest = async () => {
    setTesting(true);
    try {
      setTest({ ok: true, result: await api.testConnection(toConnection(draft, file)) });
    } catch (failure) {
      setTest({ ok: false, message: failure instanceof Error ? failure.message : String(failure) });
    } finally {
      setTesting(false);
    }
  };

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const saved = await api.saveConnection(toConnection(draft, file), existing?.name);
      if (existing && existing.name !== saved.name) renameConnection(existing.name, saved.name);
      await queryClient.invalidateQueries({ queryKey: ["workspace"] });
      select(saved.name);
      onDone();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!existing) return;
    try {
      await api.deleteConnection(existing.name);
      removeConnection(existing.name);
      await queryClient.invalidateQueries({ queryKey: ["workspace"] });
      onDone();
    } catch (failure) {
      notifyError("删除连接失败", failure);
    }
  };

  return (
    <form
      className="flex flex-col"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <div className="flex items-start justify-between gap-4 px-6 pt-5 pb-4">
        <div className="flex flex-col gap-1">
          <Dialog.Title className="m-0 text-lg font-semibold">
            {existing ? "编辑连接" : "添加数据库连接"}
          </Dialog.Title>
          <Dialog.Description className="m-0 text-sm text-kumo-subtle">
            保存后写入 <span className="font-mono text-[0.9em]">./{workspace.configFile}</span>
          </Dialog.Description>
        </div>
        <Dialog.Close
          render={(props) => (
            <Button {...props} variant="ghost" shape="square" icon={XIcon} aria-label="关闭" />
          )}
        />
      </div>

      <div className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-6 pt-1 pb-5">
        <Input
          label="名称"
          className="w-full min-w-0 font-mono"
          required
          value={draft.name}
          onChange={(event) => change({ name: event.target.value })}
          description={
            <>
              当前目录下的 <span className="w-full min-w-0 font-mono">./{dirName}/</span>{" "}
              会被递归扫描，里面的 SQL 自动归属到这个连接
            </>
          }
        />
        <Select
          label="数据库类型"
          className="w-full"
          value={draft.driver}
          onValueChange={(value) =>
            change({ driver: String(value), port: "", host: draft.host || "127.0.0.1" })
          }
          renderValue={(value) => (
            <DriverOption
              driver={workspace.drivers.find((item) => item.id === value)}
              drivers={workspace.drivers}
            />
          )}
        >
          {workspace.drivers.map((item) => (
            <Select.Option key={item.id} value={item.id}>
              <DriverOption driver={item} drivers={workspace.drivers} />
            </Select.Option>
          ))}
        </Select>
        {file ? (
          <Input
            label="数据库文件"
            className="w-full min-w-0 font-mono"
            required
            placeholder="data/app.db"
            value={draft.database}
            onChange={(event) => change({ database: event.target.value })}
            description="相对路径从当前目录算起；文件不存在时会新建"
          />
        ) : (
          <>
            <div className="grid grid-cols-[minmax(0,1fr)_112px] gap-3">
              <Input
                label="主机"
                className="w-full min-w-0 font-mono"
                required
                value={draft.host}
                onChange={(event) => change({ host: event.target.value })}
              />
              <Input
                label="端口"
                className="w-full min-w-0 font-mono"
                inputMode="numeric"
                placeholder={driver ? String(driver.defaultPort) : ""}
                value={draft.port}
                onChange={(event) => change({ port: event.target.value.replace(/\D/g, "") })}
              />
            </div>
            <Input
              label={driverDatabaseLabel(driver)}
              className="w-full min-w-0 font-mono"
              value={draft.database}
              onChange={(event) => change({ database: event.target.value })}
            />
            <div className="grid grid-cols-2 gap-3">
              <Input
                label="用户名"
                className="w-full min-w-0 font-mono"
                autoComplete="off"
                value={draft.user}
                onChange={(event) => change({ user: event.target.value })}
              />
              <SensitiveInput
                label="密码"
                autoComplete="new-password"
                value={draft.password}
                onValueChange={(value: string) => change({ password: value })}
              />
            </div>
            <p className="m-0 -mt-2 text-sm text-kumo-subtle">
              密码会以明文写入配置文件；可以填{" "}
              <span className="font-mono text-[0.9em] text-kumo-default">{"${PG_PASSWORD}"}</span>{" "}
              改为启动时读取环境变量。
            </p>
          </>
        )}
        <Collapsible.Root defaultOpen={draft.params !== ""}>
          <Collapsible.DefaultTrigger>高级选项</Collapsible.DefaultTrigger>
          <Collapsible.DefaultPanel>
            <Input
              label="连接参数"
              className="w-full min-w-0 font-mono"
              placeholder={driverParamsExample(driver)}
              value={draft.params}
              onChange={(event) => change({ params: event.target.value })}
              description="以 key=value 形式追加到连接串，多个参数用 & 连接"
            />
          </Collapsible.DefaultPanel>
        </Collapsible.Root>

        {test && (
          <Banner
            size="sm"
            variant={test.ok ? "default" : "error"}
            icon={test.ok ? <CheckCircleIcon weight="fill" /> : <XCircleIcon weight="fill" />}
            title={test.ok ? "连接成功" : "连接失败"}
            description={
              test.ok ? `${test.result.version} · ${test.result.latencyMs} ms` : test.message
            }
          />
        )}
        {error && (
          <Banner
            size="sm"
            variant="error"
            icon={<XCircleIcon weight="fill" />}
            title="保存失败"
            description={error}
          />
        )}
      </div>

      <div className="flex items-center justify-between gap-2 border-t border-kumo-hairline px-6 py-3.5">
        <div className="flex gap-2">
          <Button
            loading={testing}
            disabled={file ? !draft.database.trim() : !draft.host.trim()}
            onClick={runTest}
          >
            测试连接
          </Button>
          {existing && (
            <Button variant="secondary-destructive" onClick={() => setConfirmDelete(true)}>
              删除连接
            </Button>
          )}
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={onDone}>
            取消
          </Button>
          <Button variant="primary" type="submit" loading={saving}>
            保存
          </Button>
        </div>
      </div>

      <ConfirmDialog
        open={confirmDelete}
        title={`删除连接 ${existing?.name ?? ""}？`}
        description="连接会从配置文件中移除，它的编排和执行记录一并删除。磁盘上的 SQL 文件不受影响。"
        confirmLabel="删除"
        destructive
        onConfirm={remove}
        onClose={() => setConfirmDelete(false)}
      />
    </form>
  );
}

function DriverOption({ driver, drivers }: { driver?: DriverInfo; drivers: DriverInfo[] }) {
  const protocol = drivers.find((item) => item.id === driver?.protocol);
  return (
    <span className="flex items-center gap-2.5">
      <span
        className={cn("flex size-5 items-center justify-center rounded-[5px]", driverTone(driver))}
      >
        <DatabaseIcon size={12} weight="bold" />
      </span>
      {driver?.label}
      {protocol && <span className="text-xs text-kumo-subtle">兼容 {protocol.label}</span>}
    </span>
  );
}
