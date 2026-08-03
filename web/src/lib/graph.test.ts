import { describe, expect, it } from "vitest";
import { addEdgeToGraph, graphToFlow, removeEdgeFromGraph } from "./graph";
import type { MigrationGraph } from "../types";

const graph: MigrationGraph = {
  version: 1,
  name: "test",
  parallelism: 1,
  onError: "halt",
  nodes: [
    { name: "a", database: "main", dependsOn: [], scripts: [{ path: "a.sql" }] },
    { name: "b", database: "main", dependsOn: ["a"], scripts: [{ path: "b.sql" }] },
  ],
};

describe("graph flow adapter", () => {
  it("creates nodes and directed dependency edges", () => {
    const flow = graphToFlow(graph);
    expect(flow.nodes).toHaveLength(2);
    expect(flow.edges).toEqual(
      expect.arrayContaining([expect.objectContaining({ source: "a", target: "b" })]),
    );
  });

  it("adds and removes dependency edges immutably", () => {
    const withEdge = addEdgeToGraph(graph, "b", "a");
    expect(withEdge.nodes[0]?.dependsOn).toEqual(["b"]);
    expect(removeEdgeFromGraph(withEdge, "b", "a").nodes[0]?.dependsOn).toEqual([]);
    expect(graph.nodes[0]?.dependsOn).toEqual([]);
  });
});
