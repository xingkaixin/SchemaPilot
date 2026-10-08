import { Loader } from "@cloudflare/kumo";
import {
  CheckCircleIcon,
  CircleDashedIcon,
  ProhibitIcon,
  StopCircleIcon,
  WarningIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import type { NodeState } from "../lib/model";

export function StatusIcon({ state }: { state: NodeState }) {
  switch (state) {
    case "running":
      return <Loader size="sm" className="shrink-0 text-kumo-brand" />;
    case "succeeded":
      return <CheckCircleIcon size={16} weight="fill" className="shrink-0 text-kumo-success" />;
    case "failed":
      return <XCircleIcon size={16} weight="fill" className="shrink-0 text-kumo-danger" />;
    case "cancelled":
      return <StopCircleIcon size={16} weight="fill" className="shrink-0 text-kumo-subtle" />;
    case "disabled":
      return <ProhibitIcon size={16} className="shrink-0 text-kumo-placeholder" />;
    case "missing":
      return <WarningIcon size={16} weight="fill" className="shrink-0 text-kumo-warning" />;
    default:
      return <CircleDashedIcon size={16} className="shrink-0 text-kumo-placeholder" />;
  }
}
