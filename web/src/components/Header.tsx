import { Badge, Button, Tooltip } from "@cloudflare/kumo";
import { ArrowClockwiseIcon, FileTextIcon, FolderSimpleIcon } from "@phosphor-icons/react";
import type { Workspace } from "../api";
import { Logo } from "./Logo";

export function Header({
  workspace,
  refreshing,
  onRescan,
}: {
  workspace: Workspace;
  refreshing: boolean;
  onRescan: () => void;
}) {
  return (
    <header className="sticky top-0 z-20 flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-kumo-hairline bg-kumo-base px-5 py-2.5">
      <div className="flex items-center gap-2.5">
        <span className="text-kumo-default">
          <Logo size={28} />
        </span>
        <span className="text-[15px] font-semibold">SchemaPilot</span>
      </div>
      <span className="h-5 w-px bg-kumo-hairline" />
      <div className="flex min-w-0 flex-[1_1_360px] flex-wrap items-center gap-x-4 gap-y-1 text-sm text-kumo-subtle">
        <span className="flex min-w-0 items-center gap-1.5">
          <FolderSimpleIcon size={16} className="shrink-0" />
          <span className="truncate font-mono text-kumo-default" title={workspace.root}>
            {workspace.root}
          </span>
        </span>
        <span className="flex items-center gap-1.5">
          <FileTextIcon size={16} className="shrink-0" />
          {workspace.configExists ? (
            <span className="font-mono text-kumo-default">{workspace.configFile}</span>
          ) : (
            <span>无配置文件</span>
          )}
          {workspace.configError ? (
            <Tooltip content={workspace.configError}>
              <Badge variant="error">解析失败</Badge>
            </Tooltip>
          ) : workspace.configExists ? (
            <Badge variant="success">已同步</Badge>
          ) : (
            <Badge variant="neutral">添加连接后创建</Badge>
          )}
        </span>
      </div>
      <Button icon={ArrowClockwiseIcon} loading={refreshing} onClick={onRescan}>
        重新扫描目录
      </Button>
    </header>
  );
}
