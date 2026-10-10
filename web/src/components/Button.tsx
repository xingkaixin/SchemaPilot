import type { ComponentProps } from "react";
import { cn } from "@cloudflare/kumo";
import type { Icon } from "@phosphor-icons/react";

export function Button({
  variant = "outline",
  size,
  icon: IconComponent,
  loading,
  disabled,
  type = "button",
  className,
  children,
  ...props
}: ComponentProps<"button"> & {
  variant?: "primary" | "outline" | "ghost" | "danger" | "danger-solid";
  size?: "sm" | "xs";
  icon?: Icon;
  loading?: boolean;
}) {
  return (
    <button
      type={type}
      disabled={disabled || loading}
      className={cn(
        "btn",
        `btn-${variant}`,
        size && `btn-${size}`,
        children == null && "btn-icon",
        className,
      )}
      {...props}
    >
      {loading ? (
        <span className="spin" />
      ) : (
        IconComponent && <IconComponent size={size ? 14 : 16} />
      )}
      {children}
    </button>
  );
}
