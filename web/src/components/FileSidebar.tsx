import { FileTree, useFileTree } from "@pierre/trees/react";
import { Database, FileCode2, GripVertical, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { DatabaseProfile, ProjectFile } from "../types";
import { useTestDatabase } from "../hooks";
import { useMigratorStore } from "../store";
import { ActionButton, EmptyState, StatusMark } from "./ui";

function treePaths(files: ProjectFile[]) {
  const paths = new Set<string>();
  for (const file of files) {
    const segments = file.path.split("/").filter(Boolean);
    for (let index = 1; index < segments.length; index += 1)
      paths.add(`${segments.slice(0, index).join("/")}/`);
    paths.add(file.path);
  }
  return [...paths].sort();
}

export function FileSidebar({
  files,
  databases,
}: {
  files: ProjectFile[];
  databases: DatabaseProfile[];
}) {
  const activePanel = useMigratorStore((state) => state.activePanel);
  const setActivePanel = useMigratorStore((state) => state.setActivePanel);
  const selectScript = useMigratorStore((state) => state.selectScript);
  const [search, setSearch] = useState("");
  const paths = useMemo(() => treePaths(files), [files]);
  const { model } = useFileTree({ paths, initialExpansion: "open", search: true });
  useEffect(() => {
    model.resetPaths(paths);
  }, [model, paths]);
  const visibleFiles = files.filter(
    (file) => file.kind !== "directory" && file.path.endsWith(".sql"),
  );
  const filteredFiles = visibleFiles.filter((file) =>
    file.path.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <aside className="sidebar left-sidebar" aria-label="Project files and database profiles">
      <div className="panel-tabs" role="tablist" aria-label="Project resources">
        <button
          className={activePanel !== "connections" ? "panel-tab panel-tab--active" : "panel-tab"}
          onClick={() => setActivePanel("files")}
          role="tab"
          aria-selected={activePanel !== "connections"}
        >
          Files
        </button>
        <button
          className={activePanel === "connections" ? "panel-tab panel-tab--active" : "panel-tab"}
          onClick={() => setActivePanel("connections")}
          role="tab"
          aria-selected={activePanel === "connections"}
        >
          Profiles
        </button>
      </div>
      {activePanel !== "connections" ? (
        <div className="sidebar-content">
          <div className="drop-hint">
            <GripVertical size={17} />
            <span>Drag a SQL file onto the graph to create a node.</span>
          </div>
          <div className="tree-heading">
            <span>Repository</span>
            <code>{visibleFiles.length} sql</code>
          </div>
          <input
            className="tree-search"
            aria-label="Filter SQL files"
            name="sql-file-filter"
            autoComplete="off"
            spellCheck={false}
            placeholder="Filter paths…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <div className="pierre-tree-wrap" aria-label="Migration files">
            <FileTree model={model} style={{ height: "100%" }} />
          </div>
          <div className="drag-files" aria-label="Draggable SQL files">
            <div className="section-caption">Drop targets</div>
            {filteredFiles.length === 0 ? (
              <EmptyState title="No SQL files" detail="The API returned an empty project tree." />
            ) : null}
            {filteredFiles.map((file) => (
              <button
                type="button"
                className="drag-file-row"
                draggable
                key={file.path}
                onDragStart={(event) => {
                  event.dataTransfer.effectAllowed = "copy";
                  event.dataTransfer.setData("text/migrator-script", file.path);
                  event.dataTransfer.setData("text/plain", file.path);
                }}
                onClick={() => selectScript(file.path)}
                title="Drag onto graph or select to preview"
              >
                <FileCode2 size={14} />
                <code>{file.path}</code>
              </button>
            ))}
          </div>
        </div>
      ) : (
        <ConnectionsPanel databases={databases} />
      )}
    </aside>
  );
}

function ConnectionsPanel({ databases }: { databases: DatabaseProfile[] }) {
  const testDatabase = useTestDatabase();

  if (databases.length === 0)
    return (
      <div className="sidebar-content">
        <EmptyState
          title="No database profiles"
          detail="Add profiles to databases.toml to test a destination."
        />
      </div>
    );
  return (
    <div className="sidebar-content connections-list">
      <div className="connection-toolbar">
        <span className="section-caption">Destinations</span>
        <span className="muted-copy">from databases.toml</span>
      </div>
      {databases.map((database) => {
        const isTesting = testDatabase.isPending && testDatabase.variables === database.name;
        const result =
          testDatabase.data && testDatabase.variables === database.name ? testDatabase.data : null;
        return (
          <article className="connection-card" key={database.name}>
            <header>
              <span className={`database-led database-led--${database.driver}`}>
                <Database size={14} />
              </span>
              <code>{database.name}</code>
              <span className="driver-badge">{database.driver}</span>
            </header>
            <div className="connection-dsn">{database.dsn || "DSN supplied by environment"}</div>
            <div className="connection-footer">
              <StatusMark status={result ? "succeeded" : database.status} />
              <div className="spacer" />
              <ActionButton onClick={() => testDatabase.mutate(database.name)} disabled={isTesting}>
                {isTesting ? <RefreshCw className="spin" size={13} /> : "Test"}
              </ActionButton>
            </div>
            {result ? (
              <div className="connection-result" role="status" aria-live="polite">
                Reachable in {result.latency_ms} ms
              </div>
            ) : null}
            {testDatabase.isError && testDatabase.variables === database.name ? (
              <div className="connection-result connection-result--error" role="alert">
                Test failed:{" "}
                {testDatabase.error instanceof Error ? testDatabase.error.message : "Unknown error"}
              </div>
            ) : null}
          </article>
        );
      })}
      <div className="credentials-note">
        Keep secrets in environment variables. Commit only TOML profiles that reference those
        variables.
      </div>
    </div>
  );
}
