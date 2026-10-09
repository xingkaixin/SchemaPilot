import { useState } from "react";
import { Banner, Button, Checkbox, Dialog } from "@cloudflare/kumo";
import { PackageIcon, XCircleIcon } from "@phosphor-icons/react";
import { api, type Workspace } from "../api";
import { allPaths } from "../lib/arrangement";
import { useStore } from "../store";

export function PackageDialog({
  workspace,
  open,
  onClose,
}: {
  workspace: Workspace;
  open: boolean;
  onClose: () => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
      <Dialog size="base" className="flex flex-col gap-4 p-5">
        {/* Remount so each opening starts from all connections. */}
        {open && <PackageForm workspace={workspace} onClose={onClose} />}
      </Dialog>
    </Dialog.Root>
  );
}

function PackageForm({ workspace, onClose }: { workspace: Workspace; onClose: () => void }) {
  const connections = useStore((state) => state.connections);
  const existing = new Set(workspace.files.map((file) => file.path));
  const candidates = Object.entries(connections)
    .map(([name, state]) => {
      const paths = allPaths(state.steps);
      return { name, files: paths.length, missing: paths.filter((path) => !existing.has(path)) };
    })
    .filter((candidate) => candidate.files > 0)
    .sort((a, b) => a.name.localeCompare(b.name));
  const [chosen, setChosen] = useState(() => candidates.map((candidate) => candidate.name));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const blocked = candidates.some(
    (candidate) => chosen.includes(candidate.name) && candidate.missing.length > 0,
  );

  const download = async () => {
    setBusy(true);
    setError(null);
    try {
      const { blob, name } = await api.exportPackage(chosen);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = name;
      link.click();
      URL.revokeObjectURL(url);
      onClose();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="flex flex-col gap-1.5">
        <Dialog.Title className="m-0 text-lg font-semibold">导出包</Dialog.Title>
        <Dialog.Description className="m-0 text-kumo-subtle">
          包含所选连接的编排和 SQL 文件，不含连接地址和密码。在另一个目录运行{" "}
          <span className="font-mono text-[0.9em] text-kumo-default">schemapilot 包名.zip</span>{" "}
          即可还原，连接按名称匹配那里的配置。
        </Dialog.Description>
      </div>
      {candidates.length === 0 ? (
        <p className="m-0 text-sm text-kumo-subtle">还没有编排过文件的连接。</p>
      ) : (
        <div className="flex flex-col gap-1">
          {candidates.map((candidate) => (
            <label
              key={candidate.name}
              className="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2 hover:bg-kumo-tint"
            >
              <Checkbox
                checked={chosen.includes(candidate.name)}
                onCheckedChange={(value) =>
                  setChosen((current) =>
                    value === true
                      ? [...current, candidate.name]
                      : current.filter((name) => name !== candidate.name),
                  )
                }
              />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="font-medium">{candidate.name}</span>
                <span
                  className={
                    candidate.missing.length > 0
                      ? "text-xs text-kumo-warning"
                      : "text-xs text-kumo-subtle"
                  }
                >
                  {candidate.files} 个文件
                  {candidate.missing.length > 0 && ` · ${candidate.missing.length} 个文件不存在`}
                </span>
              </span>
            </label>
          ))}
        </div>
      )}
      {blocked && (
        <p className="m-0 text-sm text-kumo-warning">
          有文件已不存在。放回文件或从编排中移除后才能打包。
        </p>
      )}
      {error && (
        <Banner
          size="sm"
          variant="error"
          icon={<XCircleIcon weight="fill" />}
          title="导出失败"
          description={error}
        />
      )}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          取消
        </Button>
        <Button
          variant="primary"
          icon={PackageIcon}
          loading={busy}
          disabled={chosen.length === 0 || blocked}
          onClick={download}
        >
          下载 .zip
        </Button>
      </div>
    </>
  );
}
