import { beforeEach, describe, expect, it } from "vitest";
import { useMigratorStore } from "./store";
import type { MigrationGraph } from "./types";

const graph: MigrationGraph = {
  version: 1,
  name: "release",
  parallelism: 2,
  onError: "halt",
  nodes: [
    {
      name: "users",
      database: "main",
      dependsOn: [],
      scripts: [{ path: "users/001.sql" }, { path: "users/002.sql" }],
    },
    {
      name: "reports",
      database: "reporting",
      dependsOn: ["users"],
      scripts: [{ path: "reports/001.sql" }],
    },
  ],
};

describe("migration draft store", () => {
  beforeEach(() => useMigratorStore.getState().setGraphDraft(graph));

  it("moves and removes scripts without mutating the source graph", () => {
    const original = useMigratorStore.getState().graphDraft;
    useMigratorStore.getState().moveScript("users", 0, 1);
    useMigratorStore.getState().removeScript("users", "users/002.sql");
    expect(
      useMigratorStore.getState().graphDraft?.nodes[0]?.scripts.map((script) => script.path),
    ).toEqual(["users/001.sql"]);
    expect(original?.nodes[0]?.scripts.map((script) => script.path)).toEqual([
      "users/001.sql",
      "users/002.sql",
    ]);
  });

  it("does not remove the last script from a node", () => {
    useMigratorStore.getState().setGraphDraft({
      ...graph,
      nodes: [{ ...graph.nodes[0], scripts: [{ path: "users/001.sql" }] }],
    });
    useMigratorStore.getState().removeScript("users", "users/001.sql");
    expect(useMigratorStore.getState().graphDraft?.nodes[0]?.scripts).toHaveLength(1);
  });

  it("removes a node, its dependency references, and its selection", () => {
    useMigratorStore.getState().selectNode("users");
    useMigratorStore.getState().removeNode("users");
    const draft = useMigratorStore.getState().graphDraft;
    expect(draft?.nodes.map((node) => node.name)).toEqual(["reports"]);
    expect(draft?.nodes[0]?.dependsOn).toEqual([]);
    expect(useMigratorStore.getState().selectedNodeId).toBeNull();
  });

  it("keeps an unrelated selection when removing another node", () => {
    useMigratorStore.getState().selectNode("reports");
    useMigratorStore.getState().removeNode("users");
    expect(useMigratorStore.getState().selectedNodeId).toBe("reports");
  });

  it("renames a node and updates dependency references", () => {
    useMigratorStore.getState().selectNode("users");
    useMigratorStore.getState().updateNode("users", { name: "accounts" });
    expect(useMigratorStore.getState().selectedNodeId).toBe("accounts");
    expect(useMigratorStore.getState().graphDraft?.nodes[1]?.dependsOn).toEqual(["accounts"]);
  });
});
