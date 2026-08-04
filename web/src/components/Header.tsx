import { Play, Save } from "lucide-react";
import { useSaveGraph, useStartRun } from "../hooks";
import { executionBlocker } from "../lib/execution";
import { useMigratorStore } from "../store";
import type { MigrationGraph } from "../types";
import { ActionButton } from "./ui";

export function Header({
  graph,
  isDirty,
  ready,
  problems,
}: {
  graph: MigrationGraph;
  isDirty: boolean;
  ready: boolean;
  problems?: string[];
}) {
  const startRun = useStartRun();
  const saveGraph = useSaveGraph();
  const runBlocker = executionBlocker({ ready, isDirty, problems });
  return (
    <header className="app-header">
      <div className="brand-lockup">
        <span className="brand-mark" aria-hidden="true" />
        <span className="brand-word">SCHEMAPILOT</span>
      </div>
      <span className="header-rule" />
      <div className="project-title">
        <code>{graph.name || "migration"}.yaml</code>
        <span>
          {graph.nodes.length} nodes ·{" "}
          {graph.nodes.reduce((count, node) => count + node.scripts.length, 0)} scripts
        </span>
      </div>
      <div className="spacer" />
      {isDirty ? <span className="dirty-indicator">Unsaved changes</span> : null}
      <div className="header-actions">
        <ActionButton
          disabled={!isDirty || saveGraph.isPending}
          onClick={() => {
            const next = useMigratorStore.getState().graphDraft;
            if (next) saveGraph.mutate(next);
          }}
        >
          <Save size={14} /> Save
        </ActionButton>
        <ActionButton
          tone="purple"
          disabled={Boolean(runBlocker) || startRun.isPending}
          title={runBlocker || undefined}
          onClick={() => startRun.mutate(false)}
        >
          <Play size={14} /> {startRun.isPending ? "Starting…" : "Run graph"}
        </ActionButton>
      </div>
      {runBlocker ? <span className="run-blocker">{runBlocker}</span> : null}
      {saveGraph.isError ? (
        <span className="mutation-error" role="alert">
          {saveGraph.error instanceof Error ? saveGraph.error.message : "Save failed"}
        </span>
      ) : null}
      {startRun.isError ? (
        <span className="mutation-error" role="alert">
          {startRun.error instanceof Error ? startRun.error.message : "Run failed"}
        </span>
      ) : null}
    </header>
  );
}
