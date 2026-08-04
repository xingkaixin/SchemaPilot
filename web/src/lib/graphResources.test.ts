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
