import { Dialog, Select, Tooltip } from "@base-ui/react";
import type { ReactNode } from "react";
import { Check, ChevronDown, X } from "lucide-react";

export function ActionButton({
  children,
  tone = "neutral",
  className = "",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  tone?: "neutral" | "purple" | "yellow" | "danger";
}) {
  return (
    <button className={`action-button action-button--${tone} ${className}`} {...props}>
      {children}
    </button>
  );
}

export function IconButton({
  label,
  children,
  className = "",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return (
    <Tooltip.Root>
      <Tooltip.Trigger
        render={<button className={`icon-button ${className}`} aria-label={label} {...props} />}
      >
        {children}
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Positioner sideOffset={6} className="tooltip-positioner">
          <Tooltip.Popup className="tooltip-popup">{label}</Tooltip.Popup>
        </Tooltip.Positioner>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}

export function FieldLabel({ children, htmlFor }: { children: ReactNode; htmlFor?: string }) {
  return (
    <label className="field-label" htmlFor={htmlFor}>
      {children}
    </label>
  );
}

export function BaseSelect({
  value,
  onValueChange,
  options,
  label,
}: {
  value: string;
  onValueChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
  label?: string;
}) {
  return (
    <Select.Root value={value} onValueChange={(next) => next !== null && onValueChange(next)}>
      {label ? <Select.Label className="field-label">{label}</Select.Label> : null}
      <Select.Trigger className="base-select-trigger">
        <Select.Value />
        <Select.Icon>
          <ChevronDown size={14} />
        </Select.Icon>
      </Select.Trigger>
      <Select.Portal>
        <Select.Positioner className="base-select-positioner" sideOffset={4}>
          <Select.Popup className="base-select-popup">
            <Select.List>
              {options.map((option) => (
                <Select.Item className="base-select-item" key={option.value} value={option.value}>
                  <Select.ItemText>{option.label}</Select.ItemText>
                  <Select.ItemIndicator>
                    <Check size={13} />
                  </Select.ItemIndicator>
                </Select.Item>
              ))}
            </Select.List>
          </Select.Popup>
        </Select.Positioner>
      </Select.Portal>
    </Select.Root>
  );
}

export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  className = "",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="dialog-backdrop" />
        <Dialog.Viewport className="dialog-viewport">
          <Dialog.Popup className={`dialog-popup ${className}`}>
            <div className="dialog-header">
              <div>
                <Dialog.Title className="dialog-title">{title}</Dialog.Title>
                {description ? (
                  <Dialog.Description className="dialog-description">
                    {description}
                  </Dialog.Description>
                ) : null}
              </div>
              <Dialog.Close className="icon-button" aria-label="Close">
                <X size={17} />
              </Dialog.Close>
            </div>
            {children}
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function StatusMark({ status }: { status?: string }) {
  const normalized = status?.toLowerCase().replaceAll(" ", "_") ?? "pending";
  const label = normalized.replaceAll("_", " ");
  return (
    <span className={`status-mark status-mark--${normalized}`}>
      <span className="status-dot" aria-hidden="true" />
      <span>{label}</span>
    </span>
  );
}

export function LoadingState({ label = "Loading project…" }: { label?: string }) {
  return (
    <div className="state-message" role="status" aria-live="polite">
      <span className="loading-spinner" aria-hidden="true" />
      {label}
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="state-message state-message--error">
      <strong>Could not load this surface.</strong>
      <span>{message}</span>
      {onRetry ? <ActionButton onClick={onRetry}>Try again</ActionButton> : null}
    </div>
  );
}

export function EmptyState({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="state-message">
      <strong>{title}</strong>
      <span>{detail}</span>
    </div>
  );
}
