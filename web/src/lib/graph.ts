import type { Edge, Node } from "@xyflow/react";
import type { MigrationGraph, MigrationNode } from "../types";

export const graphToFlow = (graph: MigrationGraph): { nodes: Node[]; edges: Edge[] } => {
  const nodes: Node[] = graph.nodes.map((node, index) => ({
    id: node.name,
    type: "migration",
    position: { x: 70 + (index % 3) * 300, y: 80 + Math.floor(index / 3) * 240 },
    data: { node },
  }));
  const edges: Edge[] = graph.nodes.flatMap((node) =>
    node.dependsOn.map((source) => ({
      id: `${source}->${node.name}`,
      source,
      target: node.name,
      type: "dependency",
      markerEnd: { type: "arrowclosed" as const },
    })),
  );
  return { nodes, edges };
};

export const addEdgeToGraph = (
  graph: MigrationGraph,
  source: string,
  target: string,
): MigrationGraph => ({
  ...graph,
  nodes: graph.nodes.map((node) =>
    node.name === target && source !== target && !node.dependsOn.includes(source)
      ? { ...node, dependsOn: [...node.dependsOn, source] }
      : node,
  ),
});

export const removeEdgeFromGraph = (
  graph: MigrationGraph,
  source: string,
  target: string,
): MigrationGraph => ({
  ...graph,
  nodes: graph.nodes.map((node) =>
    node.name === target
      ? { ...node, dependsOn: node.dependsOn.filter((name) => name !== source) }
      : node,
  ),
});

export const nodeById = (
  graph: MigrationGraph | null,
  id: string | null,
): MigrationNode | undefined => graph?.nodes.find((node) => node.name === id);
