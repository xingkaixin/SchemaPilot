import { useState } from "react";
import { Tooltip } from "@cloudflare/kumo";
import { ArrowClockwiseIcon, FolderSimpleIcon, PackageIcon } from "@phosphor-icons/react";
import type { Workspace } from "../api";
import { Button } from "./Button";
import { PackageDialog } from "./PackageDialog";

export function Header({
  workspace,
  refreshing,
  onRescan,
}: {
  workspace: Workspace;
  refreshing: boolean;
  onRescan: () => void;
}) {
  const [packaging, setPackaging] = useState(false);
  return (
    <header className="flex min-h-[43px] flex-wrap items-center justify-between gap-x-4 gap-y-1.5 border-b border-kumo-line py-1.5 pr-4 pl-6">
      <div className="flex min-w-0 flex-[1_1_360px] items-center gap-2.5">
        <FolderSimpleIcon size={16} className="shrink-0 text-kumo-subtle" />
        <span className="min-w-0 truncate font-mono" title={workspace.root}>
          {workspace.root}
        </span>
        <span className="text-kumo-placeholder">/</span>
        {workspace.configExists ? (
          <span className="shrink-0 font-mono text-kumo-subtle">{workspace.configFile}</span>
        ) : (
          <span className="shrink-0 text-kumo-subtle">无配置文件</span>
        )}
        {workspace.configError ? (
          <Tooltip content={workspace.configError}>
            <span className="st st-sm st-red">解析失败</span>
          </Tooltip>
        ) : workspace.configExists ? (
          <span className="st st-sm st-green">已同步</span>
        ) : (
          <span className="st st-sm st-gray">添加连接后创建</span>
        )}
      </div>
      <div className="flex gap-1">
        <Button variant="ghost" size="sm" icon={PackageIcon} onClick={() => setPackaging(true)}>
          导出包
        </Button>
        <Button
          variant="ghost"
          size="sm"
          icon={ArrowClockwiseIcon}
          loading={refreshing}
          onClick={onRescan}
        >
          重新扫描目录
        </Button>
      </div>
      <PackageDialog workspace={workspace} open={packaging} onClose={() => setPackaging(false)} />
    </header>
  );
}
