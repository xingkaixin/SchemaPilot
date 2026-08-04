import { FileTree, useFileTree } from "@pierre/trees/react";
import { FileCode2, GripVertical, Upload } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useImportScript } from "../hooks";
import { useMigratorStore } from "../store";
import type { DatabaseProfile, ProjectFile } from "../types";
import { DatabaseProfilesPanel } from "./DatabaseProfilesPanel";
import { ActionButton, EmptyState } from "./ui";

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
  const importScript = useImportScript();
  const fileInput = useRef<HTMLInputElement>(null);
  const [isImporting, setIsImporting] = useState(false);
  const [isFileDragOver, setIsFileDragOver] = useState(false);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);
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
  const importFiles = async (incoming: File[]) => {
    const results: ImportResult["items"] = [];
    setIsImporting(true);
    setImportResult(null);
    for (const file of incoming) {
      const path = filePath(file);
      if (!path.endsWith(".sql")) {
        results.push({ path, error: "Only .sql files can be imported." });
        continue;
      }
      try {
        await importScript.mutateAsync({ path, content: await file.text() });
        results.push({ path });
      } catch (error) {
        results.push({
          path,
          error: error instanceof Error ? error.message : "Import failed.",
        });
      }
    }
    setIsImporting(false);
    setImportResult({ items: results });
  };
  const handleFileDrop = (event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setIsFileDragOver(false);
    if (event.dataTransfer.files.length > 0) void importFiles([...event.dataTransfer.files]);
  };

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
          <div
            className={`import-dropzone ${isFileDragOver ? "import-dropzone--active" : ""}`}
            onDragEnter={(event) => {
              if (event.dataTransfer.types.includes("Files")) setIsFileDragOver(true);
            }}
            onDragOver={(event) => {
              if (event.dataTransfer.types.includes("Files")) event.preventDefault();
            }}
            onDragLeave={() => setIsFileDragOver(false)}
            onDrop={handleFileDrop}
          >
            <Upload size={16} />
            <span>
              <strong>Import SQL</strong>
              <small>Drop files here or choose from disk.</small>
            </span>
            <ActionButton
              type="button"
              onClick={() => fileInput.current?.click()}
              disabled={isImporting}
            >
              {isImporting ? "Importing…" : "Choose"}
            </ActionButton>
            <input
              ref={fileInput}
              className="visually-hidden"
              type="file"
              accept=".sql,text/plain"
              multiple
              onChange={(event) => {
                if (event.target.files) void importFiles([...event.target.files]);
                event.target.value = "";
              }}
            />
          </div>
          <div className="drop-hint">
            <GripVertical size={17} />
            <span>Drag an imported SQL file onto the graph to create a node.</span>
          </div>
          {importResult ? <ImportResultNotice result={importResult} /> : null}
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
              <EmptyState title="No SQL files" detail="Import a .sql file to begin authoring." />
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
        <DatabaseProfilesPanel databases={databases} />
      )}
    </aside>
  );
}

interface ImportResult {
  items: Array<{ path: string; error?: string }>;
}

function ImportResultNotice({ result }: { result: ImportResult }) {
  const failures = result.items.filter((item) => item.error);
  return (
    <div className={`import-result ${failures.length ? "import-result--error" : ""}`} role="status">
      <strong>
        {failures.length
          ? `${result.items.length - failures.length} imported · ${failures.length} skipped`
          : `${result.items.length} SQL file${result.items.length === 1 ? "" : "s"} imported`}
      </strong>
      {failures.length ? (
        <ul>
          {failures.map((item) => (
            <li key={`${item.path}-${item.error}`}>
              <code>{item.path}</code>: {item.error}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function filePath(file: File) {
  return (
    (file as File & { webkitRelativePath?: string }).webkitRelativePath?.replace(/^\/+/, "") ||
    file.name
  );
}
