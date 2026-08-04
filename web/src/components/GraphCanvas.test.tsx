import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useMigratorStore } from "../store";
import type { MigrationGraph } from "../types";
import { GraphCanvas } from "./GraphCanvas";

const emptyGraph: MigrationGraph = {
  version: 1,
  name: "workspace",
  parallelism: 4,
  onError: "halt",
  nodes: [],
};

describe("GraphCanvas SQL drop", () => {
  beforeEach(() => {
    useMigratorStore.setState({
      graphDraft: structuredClone(emptyGraph),
      graphBaseline: structuredClone(emptyGraph),
      selectedNodeId: null,
    });
  });

  it("creates the first node from an imported SQL file", () => {
    render(
      <GraphCanvas
        graph={emptyGraph}
        databases={[{ name: "primary", driver: "postgres", dsn: "postgres://primary" }]}
        files={[{ path: "users/001_create_users.sql", kind: "file" }]}
      />,
    );

    fireEvent.drop(screen.getByTestId("graph-canvas"), {
      dataTransfer: {
        getData: (type: string) =>
          type === "text/migrator-script" ? "users/001_create_users.sql" : "",
      },
    });

    expect(useMigratorStore.getState().graphDraft?.nodes).toEqual([
      {
        name: "users",
        database: "primary",
        dependsOn: [],
        scripts: [{ path: "users/001_create_users.sql" }],
      },
    ]);
    expect(useMigratorStore.getState().selectedNodeId).toBe("users");
  });
});
