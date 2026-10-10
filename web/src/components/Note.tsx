import type { ReactNode } from "react";
import { cn } from "@cloudflare/kumo";
import { CheckCircleIcon, InfoIcon, WarningIcon, XCircleIcon } from "@phosphor-icons/react";

const icons = {
  danger: XCircleIcon,
  warn: WarningIcon,
  ok: CheckCircleIcon,
  info: InfoIcon,
};

export function Note({
  tone,
  title,
  description,
  action,
  className,
}: {
  tone: keyof typeof icons;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  const Icon = icons[tone];
  return (
    <div className={cn("note", `note-${tone}`, "flex-wrap", className)}>
      <Icon size={16} className="mt-0.5 shrink-0" />
      <div className="flex min-w-0 flex-[1_1_240px] flex-col gap-0.5">
        <span className="text-[13px] font-medium">{title}</span>
        {description && <span className="[overflow-wrap:anywhere]">{description}</span>}
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}
