export function executionBlocker({
  ready,
  isDirty,
  problems = [],
}: {
  ready: boolean;
  isDirty: boolean;
  problems?: string[];
}) {
  if (isDirty) return "Save graph changes before running.";
  if (ready) return "";
  return problems[0] || "Complete the workspace setup before running.";
}
