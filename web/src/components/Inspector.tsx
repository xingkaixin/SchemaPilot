import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Code2, GripVertical, Play, Save, Trash2 } from "lucide-react";
import { Suspense, lazy, useEffect, useMemo, useState } from "react";
import { createHighlighterCore, type HighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
import sqlLanguage from "shiki/langs/sql.mjs";
import githubLightTheme from "shiki/themes/github-light.mjs";
import { useScriptQuery, useSaveGraph, useSaveScript, useStartRun } from "../hooks";
import { executionBlocker } from "../lib/execution";
import { useMigratorStore } from "../store";
import { nodeById } from "../lib/graph";
import type {
  DatabaseProfile,
  ErrorPolicy,
  MigrationGraph,
  MigrationNode,
  MigrationScript,
} from "../types";
import { ActionButton, BaseSelect, EmptyState, FieldLabel, Modal, StatusMark } from "./ui";

const MonacoEditor = lazy(() => import("../lib/monaco"));
const graphNamePattern = /^[A-Za-z][A-Za-z0-9_-]*$/;

export function Inspector({
  graph,
  databases = [],
  isDirty,
  ready = false,
  problems = [],
}: {
  graph: MigrationGraph;
  databases?: DatabaseProfile[];
  isDirty: boolean;
  ready?: boolean;
  problems?: string[];
}) {
  const selectedNodeId = useMigratorStore((state) => state.selectedNodeId);
  const selectedScriptPath = useMigratorStore((state) => state.selectedScriptPath);
  const node = nodeById(graph, selectedNodeId);
  if (!node)
    return <GraphInspector graph={graph} isDirty={isDirty} ready={ready} problems={problems} />;
  return (
    <NodeInspector
      graph={graph}
      node={node}
      selectedScriptPath={selectedScriptPath}
      databases={databases}
      isDirty={isDirty}
      ready={ready}
      problems={problems}
    />
  );
}

function GraphInspector({
  graph,
  isDirty,
  ready,
  problems,
}: {
  graph: MigrationGraph;
  isDirty: boolean;
  ready: boolean;
  problems: string[];
}) {
  const updateGraph = useMigratorStore((state) => state.updateGraph);
  const saveGraph = useSaveGraph();
  const runBlocker = executionBlocker({ ready, isDirty, problems });
  return (
    <aside className="inspector" aria-label="Migration graph settings">
      <div className="inspector-header">
        <div>
          <div className="eyebrow">Graph</div>
          <h2>Graph settings</h2>
        </div>
        <StatusMark status={runBlocker ? "blocked" : "ready"} />
      </div>
      <div className="inspector-body">
        {runBlocker ? (
          <div className="callout callout--yellow" role="status">
            <span className="callout-mark">!</span>
            <span>{runBlocker}</span>
          </div>
        ) : null}
        <FieldLabel htmlFor="graph-name">Name</FieldLabel>
        <input
          id="graph-name"
          className="field-input"
          name="graph-name"
          autoComplete="off"
          spellCheck={false}
          value={graph.name}
          pattern={graphNamePattern.source}
          onChange={(event) => {
            if (graphNamePattern.test(event.target.value))
              updateGraph({ name: event.target.value });
          }}
        />
        <BaseSelect
          label="Default on error"
          value={graph.onError}
          onValueChange={(value) => updateGraph({ onError: value as ErrorPolicy })}
          options={[
            { value: "halt", label: "Halt on error" },
            { value: "continue", label: "Continue after error" },
          ]}
        />
        <div className="callout callout--yellow">
          <span className="callout-mark">!</span>
          <span>
            Every script is checksum-guarded. Re-running changed SQL requires an explicit force.
          </span>
        </div>
        <div className="inspector-divider" />
        <div className="inspector-copy">
          Select a migration node to edit its database, dependency edges, script order, and error
          policy.
        </div>
      </div>
      <div className="inspector-footer">
        {saveGraph.isError ? (
          <span className="mutation-error" role="alert">
            {saveGraph.error instanceof Error ? saveGraph.error.message : "Save failed"}
          </span>
        ) : null}
        <ActionButton
          tone="purple"
          disabled={!isDirty || saveGraph.isPending}
          onClick={() => {
            const next = useMigratorStore.getState().graphDraft;
            if (next) saveGraph.mutate(next);
          }}
        >
          <Save size={14} /> {saveGraph.isPending ? "Saving…" : "Save graph"}
        </ActionButton>
      </div>
    </aside>
  );
}

function NodeInspector({
  graph,
  node,
  selectedScriptPath,
  databases,
  isDirty,
  ready,
  problems,
}: {
  graph: MigrationGraph;
  node: MigrationNode;
  selectedScriptPath: string | null;
  databases: DatabaseProfile[];
  isDirty: boolean;
  ready: boolean;
  problems: string[];
}) {
  const updateNode = useMigratorStore((state) => state.updateNode);
  const removeNode = useMigratorStore((state) => state.removeNode);
  const moveScript = useMigratorStore((state) => state.moveScript);
  const removeScript = useMigratorStore((state) => state.removeScript);
  const selectScript = useMigratorStore((state) => state.selectScript);
  const saveGraph = useSaveGraph();
  const startRun = useStartRun();
  const runBlocker = executionBlocker({ ready, isDirty, problems });
  const [editorPath, setEditorPath] = useState<string | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const onDragEnd = (event: DragEndEvent) => {
    if (!event.over || event.active.id === event.over.id) return;
    const from = node.scripts.findIndex((script) => script.path === event.active.id);
    const to = node.scripts.findIndex((script) => script.path === event.over?.id);
    if (from >= 0 && to >= 0) moveScript(node.name, from, to);
  };
  const selectedScript =
    node.scripts.find((script) => script.path === selectedScriptPath) ?? node.scripts[0];
  return (
    <aside className="inspector" aria-label={`Inspector for ${node.name}`}>
      <div className="inspector-header">
        <div>
          <div className="eyebrow">Migration node</div>
          <h2>{node.name}</h2>
        </div>
        <StatusMark status={node.status ?? "pending"} />
      </div>
      <div className="inspector-body inspector-scroll">
        {runBlocker ? (
          <div className="callout callout--yellow" role="status">
            <span className="callout-mark">!</span>
            <span>{runBlocker}</span>
          </div>
        ) : null}
        {node.error ? (
          <div className="failure-card">
            <strong>Failed</strong>
            <code>{node.error}</code>
            <span>Correct the failure, then resume the attempt from the run log.</span>
          </div>
        ) : null}
        <FieldLabel htmlFor="node-name">Name</FieldLabel>
        <input
          id="node-name"
          className="field-input"
          name="node-name"
          autoComplete="off"
          spellCheck={false}
          value={node.name}
          pattern={graphNamePattern.source}
          onChange={(event) => {
            if (graphNamePattern.test(event.target.value)) {
              updateNode(node.name, { name: event.target.value });
            }
          }}
        />
        <BaseSelect
          label="Database profile"
          value={node.database}
          onValueChange={(database) => updateNode(node.name, { database })}
          options={
            databases.length
              ? databases.map((profile) => ({
                  value: profile.name,
                  label: `${profile.name} · ${profile.driver}`,
                }))
              : [{ value: node.database, label: node.database || "No database profiles" }]
          }
        />
        <BaseSelect
          label="On error"
          value={node.onError ?? graph.onError}
          onValueChange={(value) => updateNode(node.name, { onError: value as ErrorPolicy })}
          options={[
            { value: "halt", label: "Halt node" },
            { value: "continue", label: "Continue node" },
          ]}
        />
        <DependencyEditor graph={graph} node={node} />
        <div className="scripts-heading">
          <div>
            <FieldLabel>Scripts</FieldLabel>
            <span className="muted-copy">{node.scripts.length} · drag to reorder</span>
          </div>
        </div>
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext
            items={node.scripts.map((script) => script.path)}
            strategy={verticalListSortingStrategy}
          >
            <div className="sortable-scripts">
              {node.scripts.map((script) => (
                <SortableScript
                  key={script.path}
                  script={script}
                  onEdit={() => {
                    selectScript(null);
                    setEditorPath(script.path);
                  }}
                  onRemove={() => removeScript(node.name, script.path)}
                  removable={node.scripts.length > 1}
                />
              ))}
            </div>
          </SortableContext>
        </DndContext>
        {node.scripts.length === 0 ? (
          <EmptyState
            title="No scripts assigned"
            detail="Drag a SQL file from the tree onto the graph, then add it here."
          />
        ) : null}
        {selectedScript ? (
          <button
            className="preview-link"
            onClick={() => {
              selectScript(null);
              setEditorPath(selectedScript.path);
            }}
          >
            <Code2 size={14} /> Preview {selectedScript.path}
          </button>
        ) : null}
      </div>
      <div className="inspector-footer">
        {saveGraph.isError || startRun.isError ? (
          <span className="mutation-error" role="alert">
            {saveGraph.isError && saveGraph.error instanceof Error
              ? saveGraph.error.message
              : startRun.isError && startRun.error instanceof Error
                ? startRun.error.message
                : "Operation failed"}
          </span>
        ) : null}
        <ActionButton onClick={() => removeNode(node.name)}>
          <Trash2 size={14} /> Delete node
        </ActionButton>
        <ActionButton
          disabled={!isDirty || saveGraph.isPending}
          onClick={() => {
            const next = useMigratorStore.getState().graphDraft;
            if (next) saveGraph.mutate(next);
          }}
        >
          <Save size={14} /> Save node
        </ActionButton>
        <ActionButton
          tone="purple"
          disabled={Boolean(runBlocker) || startRun.isPending}
          title={runBlocker || "Run this node and its upstream dependencies"}
          onClick={() => startRun.mutate({ force: false, nodes: [node.name] })}
        >
          <Play size={14} /> Run node
        </ActionButton>
      </div>
      {editorPath ? (
        <SqlEditor path={editorPath} open onOpenChange={(open) => !open && setEditorPath(null)} />
      ) : null}
    </aside>
  );
}

function DependencyEditor({ graph, node }: { graph: MigrationGraph; node: MigrationNode }) {
  const removeDependency = useMigratorStore((state) => state.removeDependency);
  const options = graph.nodes.filter(
    (candidate) => candidate.name !== node.name && !node.dependsOn.includes(candidate.name),
  );
  const addDependency = (dependency: string) => {
    if (dependency) useMigratorStore.getState().addDependency(dependency, node.name);
  };
  return (
    <div className="dependency-editor">
      <FieldLabel>Depends on</FieldLabel>
      <div className="dependency-list">
        {node.dependsOn.length === 0 ? (
          <span className="muted-copy">No upstream nodes</span>
        ) : (
          node.dependsOn.map((dependency) => (
            <span className="dependency-chip" key={dependency}>
              <GitBranchIcon />
              {dependency}
              <button
                aria-label={`Remove dependency ${dependency}`}
                onClick={() => removeDependency(dependency, node.name)}
              >
                ×
              </button>
            </span>
          ))
        )}
      </div>
      {options.length ? (
        <BaseSelect
          label="Add upstream dependency"
          value=""
          onValueChange={addDependency}
          options={[
            { value: "", label: "Add upstream node" },
            ...options.map((option) => ({ value: option.name, label: option.name })),
          ]}
        />
      ) : null}
    </div>
  );
}

function GitBranchIcon() {
  return (
    <span className="dependency-chip-mark" aria-hidden="true">
      ↗
    </span>
  );
}

function SortableScript({
  script,
  onEdit,
  onRemove,
  removable,
}: {
  script: MigrationScript;
  onEdit: () => void;
  onRemove: () => void;
  removable: boolean;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: script.path,
  });
  return (
    <article
      ref={setNodeRef}
      className={`sortable-script ${isDragging ? "sortable-script--dragging" : ""}`}
      style={{ transform: CSS.Transform.toString(transform), transition }}
    >
      <button
        className="drag-handle"
        aria-label={`Reorder ${script.path}`}
        {...attributes}
        {...listeners}
      >
        <GripVertical size={15} />
      </button>
      <span className={`script-led script-led--${script.status ?? "pending"}`} aria-hidden="true" />
      <code title={script.path}>{script.path.split("/").pop()}</code>
      <span className="spacer" />
      <StatusMark status={script.status ?? "pending"} />
      <button className="script-edit-button" aria-label={`Edit ${script.path}`} onClick={onEdit}>
        <Code2 size={14} />
      </button>
      <button
        className="script-remove-button"
        aria-label={`Remove ${script.path}`}
        disabled={!removable}
        title={removable ? "Remove script from this node" : "A node must contain one script"}
        onClick={onRemove}
      >
        <Trash2 size={14} />
      </button>
    </article>
  );
}

export function SqlPreview({
  path,
  open,
  onOpenChange,
}: {
  path: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [editing, setEditing] = useState(false);
  const query = useScriptQuery(path);
  const [highlighter, setHighlighter] = useState<HighlighterCore | null>(null);
  useEffect(() => {
    let cancelled = false;
    void sqlHighlighter().then((next) => {
      if (!cancelled) setHighlighter(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);
  const highlighted = useMemo(
    () =>
      highlighter && query.data
        ? highlighter.codeToHtml(query.data.sql, { lang: "sql", theme: "github-light" })
        : "",
    [highlighter, query.data],
  );
  if (editing)
    return (
      <SqlEditor
        path={path}
        open
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setEditing(false);
            onOpenChange(false);
          }
        }}
      />
    );
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={path.split("/").pop() ?? path}
      description={path}
      className="sql-preview-modal"
    >
      {query.isLoading ? (
        <div className="editor-loading">Loading SQL…</div>
      ) : query.isError ? (
        <div className="editor-error" role="alert">
          Could not load this script.{" "}
          {query.error instanceof Error ? query.error.message : "Unknown error"}
        </div>
      ) : (
        <div
          className="shiki-preview shiki-preview--full"
          aria-label="Read-only SQL preview"
          dangerouslySetInnerHTML={{
            __html: highlighted || `<pre>${escapeHtml(query.data?.sql ?? "")}</pre>`,
          }}
        />
      )}
      <div className="dialog-footer">
        <span className="muted-copy">
          Read-only preview · checksum {query.data?.checksum?.slice(0, 12) ?? "pending"}
        </span>
        <div className="spacer" />
        <ActionButton onClick={() => setEditing(true)} disabled={query.isLoading || query.isError}>
          <Code2 size={14} /> Edit SQL
        </ActionButton>
      </div>
    </Modal>
  );
}

function SqlEditor({
  path,
  open,
  onOpenChange,
}: {
  path: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useScriptQuery(path);
  const saveScript = useSaveScript();
  const [sql, setSql] = useState("");
  const [highlighter, setHighlighter] = useState<HighlighterCore | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEffect(() => {
    if (query.data) setSql(query.data.sql);
  }, [query.data]);
  useEffect(() => {
    let cancelled = false;
    void sqlHighlighter().then((next) => {
      if (!cancelled) setHighlighter(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);
  const highlighted = useMemo(
    () => (highlighter ? highlighter.codeToHtml(sql, { lang: "sql", theme: "github-light" }) : ""),
    [highlighter, sql],
  );
  const handleSave = async () => {
    setSaveError(null);
    try {
      await saveScript.mutateAsync({ path, sql, checksum: query.data?.checksum });
      onOpenChange(false);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Could not save this SQL file.");
    }
  };
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={path.split("/").pop() ?? path}
      description={path}
      className="sql-editor-modal"
    >
      <div className="editor-tabs">
        <span className="editor-tab editor-tab--active">SQL</span>
        <span className="editor-checksum">
          {query.data?.checksum ? `sha256 ${query.data.checksum.slice(0, 10)}…` : "unsaved"}
        </span>
      </div>
      {query.isLoading ? (
        <div className="editor-loading">Loading SQL…</div>
      ) : query.isError ? (
        <div className="editor-error" role="alert">
          Could not load this script.{" "}
          {query.error instanceof Error ? query.error.message : "Unknown error"}
        </div>
      ) : (
        <div className="editor-grid">
          <div
            className="shiki-preview"
            aria-label="SQL syntax preview"
            dangerouslySetInnerHTML={{ __html: highlighted || `<pre>${escapeHtml(sql)}</pre>` }}
          />
          <div className="monaco-wrap">
            <Suspense fallback={<div className="editor-loading">Loading editor…</div>}>
              <MonacoEditor
                height="390px"
                language="sql"
                theme="vs"
                value={sql}
                onChange={(value) => setSql(value ?? "")}
                options={{
                  minimap: { enabled: false },
                  fontFamily: "Space Mono",
                  fontSize: 13,
                  lineNumbers: "on",
                  wordWrap: "on",
                  padding: { top: 12 },
                }}
              />
            </Suspense>
          </div>
        </div>
      )}
      {saveError ? (
        <div className="editor-error" role="alert">
          {saveError}
        </div>
      ) : null}
      <div className="dialog-footer">
        <span className="muted-copy">
          Edit writes back to the SQL file; checksum changes are guarded on run.
        </span>
        <div className="spacer" />
        <ActionButton onClick={() => onOpenChange(false)}>Cancel</ActionButton>
        <ActionButton
          tone="purple"
          disabled={saveScript.isPending || query.isLoading}
          onClick={() => void handleSave()}
        >
          <Save size={14} /> {saveScript.isPending ? "Saving…" : "Save SQL"}
        </ActionButton>
      </div>
    </Modal>
  );
}

let highlighterPromise: Promise<HighlighterCore> | undefined;

function sqlHighlighter() {
  highlighterPromise ??= createHighlighterCore({
    themes: [githubLightTheme],
    langs: [sqlLanguage],
    engine: createJavaScriptRegexEngine(),
  });
  return highlighterPromise;
}

function escapeHtml(value: string) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}
