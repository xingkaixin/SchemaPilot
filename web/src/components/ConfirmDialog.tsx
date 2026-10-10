import type { ReactNode } from "react";
import { Dialog } from "@cloudflare/kumo";
import { Button } from "./Button";

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel = "取消",
  destructive,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  destructive?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
      <Dialog size="base" className={dialogClass}>
        <div className="flex flex-col gap-1 px-6 pt-5 pb-4">
          <Dialog.Title className="m-0 text-base font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="m-0 text-sm text-kumo-subtle">
            {description}
          </Dialog.Description>
        </div>
        <div className={dialogFooterClass}>
          <Button variant="ghost" onClick={onClose}>
            {cancelLabel}
          </Button>
          <Button
            variant={destructive ? "danger-solid" : "primary"}
            onClick={() => {
              onConfirm();
              onClose();
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

export const dialogClass = "flex flex-col rounded-(--r-dialog) p-0 shadow-(--sh-dialog)";

export const dialogFooterClass =
  "flex flex-wrap items-center justify-end gap-2 rounded-b-(--r-dialog) border-t border-kumo-line bg-(--panel) px-6 py-3.5";
