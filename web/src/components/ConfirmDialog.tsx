import type { ReactNode } from "react";
import { Button, Dialog } from "@cloudflare/kumo";

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
      <Dialog size="base" className="flex flex-col gap-4 p-5">
        <div className="flex flex-col gap-1.5">
          <Dialog.Title className="m-0 text-lg font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="m-0 text-kumo-subtle">{description}</Dialog.Description>
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {cancelLabel}
          </Button>
          <Button
            variant={destructive ? "destructive" : "primary"}
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
