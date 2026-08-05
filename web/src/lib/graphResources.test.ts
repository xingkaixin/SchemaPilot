import { describe, expect, it } from "vitest";
import type { MigrationGraph, ProjectFile } from "../types";
import { addGraphResource, graphResourceForPath, graphResources } from "./graphResources";

const files: ProjectFile[] = [
  { path: "sql/user/002_add_email.sql", kind: "file" },
  { path: "sql/order/001_create_orders.sql", kind: "file" },
  { path: "sql/user/001_create_users.sql", kind: "file" },
  { path: "README.md", kind: "file" },
];

const emptyGraph: MigrationGraph = {
  version: 1,
  name: "workspace",
  parallelism: 4,
  onError: "halt",
  nodes: [],
};

describe("graph resources", () => {
  it("derives draggable SQL files and their directories", () => {
    expect(graphResources(files)).toEqual([
      {
        kind: "directory",
        path: "sql",
        scripts: [
          "sql/order/001_create_orders.sql",
          "sql/user/001_create_users.sql",
          "sql/user/002_add_email.sql",
        ],
      },
      {
        kind: "directory",
        path: "sql/order",
        scripts: ["sql/order/001_create_orders.sql"],
      },
      {
        kind: "file",
        path: "sql/order/001_create_orders.sql",
        scripts: ["sql/order/001_create_orders.sql"],
      },
      {
        kind: "directory",
        path: "sql/user",
        scripts: ["sql/user/001_create_users.sql", "sql/user/002_add_email.sql"],
      },
      {
        kind: "file",
        path: "sql/user/001_create_users.sql",
        scripts: ["sql/user/001_create_users.sql"],
      },
      {
        kind: "file",
        path: "sql/user/002_add_email.sql",
        scripts: ["sql/user/002_add_email.sql"],
      },
    ]);
  });

  it("resolves a tree directory path and creates one ordered node", () => {
    const resource = graphResourceForPath(files, "sql/user/");
    expect(resource).toEqual({
      kind: "directory",
      path: "sql/user",
      scripts: ["sql/user/001_create_users.sql", "sql/user/002_add_email.sql"],
    });

    const result = addGraphResource(emptyGraph, resource!, "primary");
    expect(result).toEqual({
      graph: {
        ...emptyGraph,
        nodes: [
          {
            name: "user",
            database: "primary",
            dependsOn: [],
            scripts: [
              { path: "sql/user/001_create_users.sql" },
              { path: "sql/user/002_add_email.sql" },
            ],
          },
        ],
      },
      nodeName: "user",
    });
  });

  it("creates a separate node for each file dropped from the same directory", () => {
    const first = addGraphResource(
      emptyGraph,
      graphResourceForPath(files, "sql/user/001_create_users.sql")!,
      "primary",
    )!;
    const second = addGraphResource(
      first.graph,
      graphResourceForPath(files, "sql/user/002_add_email.sql")!,
      "primary",
    )!;

    expect(second.graph.nodes.map((node) => node.name)).toEqual([
      "user-001_create_users",
      "user-002_add_email",
    ]);
    expect(second.graph.nodes[1]?.scripts).toEqual([{ path: "sql/user/002_add_email.sql" }]);
  });

  it("selects the owning node instead of duplicating an assigned script", () => {
    const first = addGraphResource(
      emptyGraph,
      graphResourceForPath(files, "sql/user/001_create_users.sql")!,
      "primary",
    )!;
    const again = addGraphResource(
      first.graph,
      graphResourceForPath(files, "sql/user/001_create_users.sql")!,
      "primary",
    )!;

    expect(again.graph.nodes).toHaveLength(1);
    expect(again.nodeName).toBe("user-001_create_users");
  });

  it("suffixes the node name when it is already taken", () => {
    const graph: MigrationGraph = {
      ...emptyGraph,
      nodes: [
        {
          name: "user-002_add_email",
          database: "primary",
          dependsOn: [],
          scripts: [{ path: "elsewhere.sql" }],
        },
      ],
    };
    const result = addGraphResource(
      graph,
      graphResourceForPath(files, "sql/user/002_add_email.sql")!,
      "primary",
    )!;

    expect(result.nodeName).toBe("user-002_add_email-2");
  });

  it("adds only missing scripts when the target node already exists", () => {
    const graph: MigrationGraph = {
      ...emptyGraph,
      nodes: [
        {
          name: "user",
          database: "primary",
          dependsOn: [],
          scripts: [{ path: "sql/user/001_create_users.sql" }],
        },
      ],
    };
    const resource = graphResourceForPath(files, "sql/user")!;

    expect(addGraphResource(graph, resource, "secondary")?.graph.nodes[0]).toEqual({
      name: "user",
      database: "primary",
      dependsOn: [],
      scripts: [{ path: "sql/user/001_create_users.sql" }, { path: "sql/user/002_add_email.sql" }],
    });
  });
});
