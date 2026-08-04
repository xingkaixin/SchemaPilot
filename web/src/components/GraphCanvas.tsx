import { useDroppable } from "@dnd-kit/core";
import {
  Background,
  BaseEdge,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  Handle,
  Position,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeProps,
  type EdgeProps,
  getSmoothStepPath,
} from "@xyflow/react";
import type { ELK } from "elkjs/lib/elk-api";
import { Check, Database, FileCode2, GitBranch, GripVertical, Layers3 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { graphDropTargetId, graphResourceForPath } from "../lib/graphResources";
import { useMigratorStore } from "../store";
import type {
  DatabaseProfile,
  MigrationGraph,
  MigrationNode,
  NodeStatus,
  ProjectFile,
} from "../types";
import { graphToFlow } from "../lib/graph";
import { ActionButton, StatusMark } from "./ui";

let elkPromise: Promise<ELK> | undefined;

function loadELK() {
  elkPromise ??= import("elkjs/lib/elk.bundled.js").then(
    ({ default: ELKConstructor }) => new ELKConstructor(),
  );
  return elkPromise;
}

const statusText = (status?: NodeStatus) => (status ? status.replaceAll("_", " ") : "pending");

function MigrationNodeCard({ data, selected }: NodeProps<Node<{ node: MigrationNode }>>) {
  const node = data.node;
  const status = node.status ?? "pending";
  return (
    <div
      className={`migration-node migration-node--${status} ${selected ? "migration-node--selected" : ""}`}
    >
      <Handle
        type="target"
        position={Position.Left}
        aria-label={`Dependencies into ${node.name}`}
      />
      <header className="migration-node-header">
        <span className="node-status-led" aria-hidden="true" />
        <code title={node.name}>{node.name}</code>
        <span className="spacer" />
        <StatusMark status={statusText(status)} />
      </header>
      <div className="migration-node-database">
        <Database size={13} />
        <code>{node.database || "unassigned"}</code>
      </div>
      <div className="migration-node-scripts">
        {node.scripts.length === 0 ? (
          <span className="node-empty">No scripts yet</span>
        ) : (
          node.scripts.map((script) => (
            <div className="migration-node-script" key={script.path}>
              <span
                className={`script-led script-led--${script.status ?? "pending"}`}
                aria-hidden="true"
              />
              <code title={script.path}>{script.path.split("/").pop()}</code>
              <span className="spacer" />
              <span className="script-check" aria-label={script.status ?? "pending"}>
                {script.status === "succeeded" || script.status === "already_applied"
                  ? "✓"
                  : script.status === "failed" || script.status === "cancelled"
                    ? "!"
                    : "·"}
              </span>
            </div>
          ))
        )}
      </div>
      <footer className="migration-node-footer">
        <Layers3 size={12} /> {node.scripts.length} scripts <span className="spacer" />{" "}
        {node.dependsOn.length} deps
      </footer>
      <Handle
        type="source"
        position={Position.Right}
        aria-label={`Dependencies from ${node.name}`}
      />
    </div>
  );
}

function DependencyEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  selected,
}: EdgeProps) {
  const [path] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 4,
  });
  return (
    <BaseEdge
      id={id}
      path={path}
      markerEnd={markerEnd}
      className={`dependency-edge ${selected ? "dependency-edge--selected" : ""}`}
    />
  );
}

const nodeTypes = { migration: MigrationNodeCard };
const edgeTypes = { dependency: DependencyEdge };

async function layoutNodes(nodes: Node[], edges: Edge[], direction: "RIGHT" | "DOWN") {
  const elk = await loadELK();
  const layout = await elk.layout({
    id: "migration-graph",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": direction,
      "elk.spacing.nodeNode": "44",
      "elk.layered.spacing.nodeNodeBetweenLayers": "110",
    },
    children: nodes.map((node) => ({ id: node.id, width: 250, height: 180 })),
    edges: edges.map((edge) => ({ id: edge.id, sources: [edge.source], targets: [edge.target] })),
  });
  const positions = new Map(
    (layout.children ?? []).map((child) => [child.id, { x: child.x ?? 0, y: child.y ?? 0 }]),
  );
  return nodes.map((node) => ({ ...node, position: positions.get(node.id) ?? node.position }));
}

function FlowSurface({
  graph,
  databases,
  files,
}: {
  graph: MigrationGraph;
  databases: DatabaseProfile[];
  files: ProjectFile[];
}) {
  const selectedNodeId = useMigratorStore((state) => state.selectedNodeId);
  const selectNode = useMigratorStore((state) => state.selectNode);
  const addDependency = useMigratorStore((state) => state.addDependency);
  const removeDependency = useMigratorStore((state) => state.removeDependency);
  const addResource = useMigratorStore((state) => state.addResource);
  const graphDirection = useMigratorStore((state) => state.graphDirection);
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [layouting, setLayouting] = useState(false);
  const { fitView } = useReactFlow();
  const { isOver, setNodeRef } = useDroppable({ id: graphDropTargetId });

  useEffect(() => {
    const next = graphToFlow(graph);
    setNodes((current) =>
      next.nodes.map((node) => ({
        ...node,
        position: current.find((existing) => existing.id === node.id)?.position ?? node.position,
      })),
    );
    setEdges(next.edges);
  }, [graph, setEdges, setNodes]);

  const runLayout = useCallback(async () => {
    setLayouting(true);
    const next = await layoutNodes(nodes, edges, graphDirection === "right" ? "RIGHT" : "DOWN");
    setNodes(next);
    setLayouting(false);
    window.requestAnimationFrame(() => fitView({ padding: 0.18, duration: 180 }));
  }, [edges, fitView, graphDirection, nodes, setNodes]);

  useEffect(() => {
    if (nodes.length > 0) void runLayout();
    // A new graph needs one automatic layout; manual dragging remains local afterwards.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph.name, graphDirection, nodes.length]);

  useEffect(() => {
    const onLayout = () => void runLayout();
    const onFit = () => fitView({ padding: 0.18, duration: 180 });
    window.addEventListener("migrator:layout", onLayout);
    window.addEventListener("migrator:fit", onFit);
    return () => {
      window.removeEventListener("migrator:layout", onLayout);
      window.removeEventListener("migrator:fit", onFit);
    };
  }, [fitView, runLayout]);

  const handleConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return;
      addDependency(connection.source, connection.target);
    },
    [addDependency],
  );

  const handleEdgesDelete = useCallback(
    (deleted: Edge[]) => {
      deleted.forEach((edge) => removeDependency(edge.source, edge.target));
    },
    [removeDependency],
  );

  const handleDrop = useCallback(
    (event: React.DragEvent<HTMLDivElement>) => {
      event.preventDefault();
      const path =
        event.dataTransfer.getData("text/migrator-resource") ||
        event.dataTransfer.getData("text/plain");
      const resource = graphResourceForPath(files, path);
      if (resource) addResource(resource, databases[0]?.name ?? "");
    },
    [addResource, databases, files],
  );

  const handleDragOver = useCallback((event: React.DragEvent<HTMLDivElement>) => {
    if (
      event.dataTransfer.types.includes("text/migrator-resource") ||
      event.dataTransfer.types.includes("text/plain")
    ) {
      event.preventDefault();
    }
  }, []);

  return (
    <div
      ref={setNodeRef}
      className={`graph-surface ${isOver ? "graph-surface--drag-over" : ""}`}
      onDrop={handleDrop}
      onDragOver={handleDragOver}
      data-testid="graph-canvas"
    >
      {layouting ? (
        <div className="layout-indicator" role="status" aria-live="polite">
          Arranging graph…
        </div>
      ) : null}
      {graph.nodes.length === 0 ? (
        <EmptyGraphOnboarding databases={databases} files={files} />
      ) : null}
      <ReactFlow
        nodes={nodes.map((node) => ({ ...node, selected: node.id === selectedNodeId }))}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={handleConnect}
        onEdgesDelete={handleEdgesDelete}
        onNodeClick={(_, node) => selectNode(node.id)}
        onPaneClick={() => selectNode(null)}
        fitView
        minZoom={0.35}
        maxZoom={1.6}
        proOptions={{ hideAttribution: true }}
        className="migration-flow"
      >
        <Background color="#E7E2D6" gap={18} size={1.4} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeColor={(node) => (node.id === selectedNodeId ? "#9B5CFF" : "#1A1A1A")}
          maskColor="rgba(255,252,242,.75)"
        />
      </ReactFlow>
    </div>
  );
}

export function GraphCanvas({
  graph,
  databases,
  files = [],
}: {
  graph: MigrationGraph;
  databases: DatabaseProfile[];
  files?: ProjectFile[];
}) {
  const graphDirection = useMigratorStore((state) => state.graphDirection);
  const setGraphDirection = useMigratorStore((state) => state.setGraphDirection);
  const toggleConsole = useMigratorStore((state) => state.toggleConsole);
  const consoleOpen = useMigratorStore((state) => state.consoleOpen);
  return (
    <section id="migration-graph" className="graph-panel" aria-label="Migration graph">
      <div className="graph-toolbar">
        <div className="segmented-control" role="group" aria-label="Graph view">
          <button
            className={graphDirection === "right" ? "segment segment--active" : "segment"}
            onClick={() => setGraphDirection("right")}
            aria-pressed={graphDirection === "right"}
          >
            Left to right
          </button>
          <button
            className={graphDirection === "down" ? "segment segment--active" : "segment"}
            onClick={() => setGraphDirection("down")}
            aria-pressed={graphDirection === "down"}
          >
            Top to bottom
          </button>
        </div>
        <ActionButton onClick={() => window.dispatchEvent(new CustomEvent("migrator:layout"))}>
          <GitBranch size={14} /> Auto layout
        </ActionButton>
        <ActionButton onClick={() => window.dispatchEvent(new CustomEvent("migrator:fit"))}>
          Fit
        </ActionButton>
        <span className="toolbar-meta">
          <GripVertical size={13} /> Connect nodes to declare dependencies
        </span>
        <div className="spacer" />
        <button className="console-toggle" aria-expanded={consoleOpen} onClick={toggleConsole}>
          {consoleOpen ? "Hide run log" : "Show run log"}
        </button>
      </div>
      <ReactFlowProvider>
        <FlowSurface graph={graph} databases={databases} files={files} />
      </ReactFlowProvider>
    </section>
  );
}

function EmptyGraphOnboarding({
  databases,
  files,
}: {
  databases: DatabaseProfile[];
  files: ProjectFile[];
}) {
  const setActivePanel = useMigratorStore((state) => state.setActivePanel);
  const sqlCount = files.filter(
    (file) => file.kind !== "directory" && file.path.endsWith(".sql"),
  ).length;
  const isEmpty = databases.length === 0 && sqlCount === 0;
  const steps = [
    {
      number: "01",
      title: "Configure a connection",
      detail: databases.length
        ? `${databases.length} profile${databases.length === 1 ? "" : "s"} ready`
        : "Add a database profile first",
      complete: databases.length > 0,
      panel: "connections" as const,
      icon: Database,
    },
    {
      number: "02",
      title: "Import SQL files",
      detail: sqlCount
        ? `${sqlCount} SQL file${sqlCount === 1 ? "" : "s"} available`
        : "Drop or choose .sql files",
      complete: sqlCount > 0,
      panel: "files" as const,
      icon: FileCode2,
    },
    {
      number: "03",
      title: "Build and save the graph",
      detail: "Drag a SQL file or directory here to create the first node",
      complete: false,
      panel: "graph" as const,
      icon: GitBranch,
    },
  ];
  return (
    <div className="graph-onboarding" aria-label="Migration workspace setup">
      <div className="graph-onboarding-kicker">
        {isEmpty ? "Empty migration workspace" : "Migration workspace setup"}
      </div>
      <h2>Turn files into a runnable graph.</h2>
      <p>Set the destination, bring in SQL, then connect the steps you want to execute.</p>
      <div className="graph-onboarding-steps">
        {steps.map((step) => {
          const Icon = step.icon;
          return (
            <button
              type="button"
              className={`graph-onboarding-step ${step.complete ? "graph-onboarding-step--complete" : ""}`}
              key={step.number}
              onClick={() => setActivePanel(step.panel)}
            >
              <span className="graph-onboarding-step-number">{step.number}</span>
              <span className="graph-onboarding-step-icon">
                {step.complete ? <Check size={16} /> : <Icon size={16} />}
              </span>
              <span className="graph-onboarding-step-copy">
                <strong>{step.title}</strong>
                <span>{step.detail}</span>
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
