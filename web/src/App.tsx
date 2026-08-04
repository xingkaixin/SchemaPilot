import { useEffect, useMemo } from "react";
import { useProjectQuery, useRunQuery } from "./hooks";
import { graphsEqual, useMigratorStore } from "./store";
import { ErrorState, LoadingState } from "./components/ui";
import { Header } from "./components/Header";
import { FileSidebar } from "./components/FileSidebar";
import { GraphCanvas } from "./components/GraphCanvas";
import { GraphResourceDnd } from "./components/GraphResourceDnd";
import { Inspector, SqlPreview } from "./components/Inspector";
import { RunLog } from "./components/RunLog";
import type { MigrationGraph, RunSnapshot } from "./types";

export function App() {
  const project = useProjectQuery();
  const activePanel = useMigratorStore((state) => state.activePanel);
  const setActivePanel = useMigratorStore((state) => state.setActivePanel);
  const graph = useMigratorStore((state) => state.graphDraft);
  const graphBaseline = useMigratorStore((state) => state.graphBaseline);
  const selectedScriptPath = useMigratorStore((state) => state.selectedScriptPath);
  const selectScript = useMigratorStore((state) => state.selectScript);
  const activeRunId = useMigratorStore((state) => state.activeRunId);
  const run = useRunQuery(activeRunId);
  const projectData = project.data;
  const databaseList = useMemo(() => projectData?.databases ?? [], [projectData?.databases]);
  const isDirty = Boolean(graph && graphBaseline && !graphsEqual(graph, graphBaseline));
  useEffect(() => {
    if (!isDirty) return;
    const preventUnsavedNavigation = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", preventUnsavedNavigation);
    return () => window.removeEventListener("beforeunload", preventUnsavedNavigation);
  }, [isDirty]);

  if (project.isLoading)
    return (
      <main className="app-state">
        <LoadingState label="Loading migration project…" />
      </main>
    );
  if (project.isError)
    return (
      <main className="app-state">
        <ErrorState
          message={
            project.error instanceof Error
              ? project.error.message
              : "The project API did not respond."
          }
          onRetry={() => void project.refetch()}
        />
      </main>
    );
  if (!projectData || !graph)
    return (
      <main className="app-state">
        <ErrorState
          message="The project response did not contain a graph."
          onRetry={() => void project.refetch()}
        />
      </main>
    );
  const observedGraph = overlayRunState(graph, run.data?.nodes);

  return (
    <main className="app-shell">
      <a className="skip-link" href="#migration-graph">
        Skip to migration graph
      </a>
      <h1 className="visually-hidden">SchemaPilot migration console</h1>
      <Header
        graph={observedGraph}
        isDirty={isDirty}
        ready={projectData.ready}
        problems={projectData.problems}
      />
      <nav className="mobile-panel-nav" aria-label="Switch workspace panel">
        {(["files", "connections", "graph", "inspector"] as const).map((panel) => (
          <button
            key={panel}
            className={
              activePanel === panel
                ? "mobile-panel-button mobile-panel-button--active"
                : "mobile-panel-button"
            }
            onClick={() => setActivePanel(panel)}
            aria-pressed={activePanel === panel}
          >
            {panel}
          </button>
        ))}
      </nav>
      <GraphResourceDnd database={databaseList[0]?.name ?? ""}>
        <div className="workspace-grid">
          <div
            className={`workspace-sidebar ${activePanel === "files" || activePanel === "connections" ? "workspace-panel--active" : ""}`}
          >
            <FileSidebar files={projectData.files} databases={databaseList} />
          </div>
          <div
            className={`workspace-center ${activePanel === "graph" ? "workspace-panel--active" : ""}`}
          >
            <GraphCanvas graph={observedGraph} databases={databaseList} files={projectData.files} />
            <RunLog isDirty={isDirty} ready={projectData.ready} problems={projectData.problems} />
          </div>
          <div
            className={`workspace-inspector ${activePanel === "inspector" ? "workspace-panel--active" : ""}`}
          >
            <Inspector
              graph={observedGraph}
              databases={databaseList}
              isDirty={isDirty}
              ready={projectData.ready}
              problems={projectData.problems}
            />
          </div>
        </div>
      </GraphResourceDnd>
      {selectedScriptPath ? (
        <SqlPreview
          path={selectedScriptPath}
          open
          onOpenChange={(open) => {
            if (!open) selectScript(null);
          }}
        />
      ) : null}
    </main>
  );
}

function overlayRunState(graph: MigrationGraph, runNodes: RunSnapshot["nodes"] | undefined) {
  if (!runNodes || runNodes.length === 0) return graph;
  const states = new Map(runNodes.map((node) => [node.name, node]));
  return {
    ...graph,
    nodes: graph.nodes.map((node) => {
      const state = states.get(node.name);
      return state
        ? {
            ...node,
            status: state.status,
            error: state.error,
            startedAt: state.startedAt,
            finishedAt: state.finishedAt,
            scripts: node.scripts.map((script) => {
              const scriptState = state.scripts.find((item) => item.path === script.path);
              return scriptState ? { ...script, ...scriptState } : script;
            }),
          }
        : node;
    }),
  };
}
