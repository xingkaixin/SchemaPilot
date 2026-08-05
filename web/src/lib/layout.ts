import type { XYPosition } from "@xyflow/react";

const layoutKey = (graphName: string) => `schemapilot:layout:${graphName}`;

export function loadNodeLayout(graphName: string): Record<string, XYPosition> {
  try {
    const raw = window.localStorage.getItem(layoutKey(graphName));
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const layout: Record<string, XYPosition> = {};
    for (const [id, position] of Object.entries(parsed)) {
      if (
        position &&
        typeof position === "object" &&
        typeof (position as XYPosition).x === "number" &&
        typeof (position as XYPosition).y === "number"
      ) {
        layout[id] = { x: (position as XYPosition).x, y: (position as XYPosition).y };
      }
    }
    return layout;
  } catch {
    return {};
  }
}

export function saveNodeLayout(graphName: string, layout: Record<string, XYPosition>) {
  try {
    window.localStorage.setItem(layoutKey(graphName), JSON.stringify(layout));
  } catch {
    // Storage may be unavailable (private mode, quota); layout stays session-local.
  }
}
