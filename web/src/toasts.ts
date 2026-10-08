import { createKumoToastManager } from "@cloudflare/kumo";

export const toasts = createKumoToastManager();

export function notifyError(title: string, error: unknown) {
  toasts.add({
    title,
    description: error instanceof Error ? error.message : String(error),
    variant: "error",
  });
}
