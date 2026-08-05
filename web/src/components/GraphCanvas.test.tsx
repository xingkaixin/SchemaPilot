import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

function renderCanvas(files: Array<{ path: string; kind: "file" }>) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <GraphCanvas
        graph={emptyGraph}
        databases={[{ name: "primary", driver: "postgres", dsn: "postgres://primary" }]}
        files={files}
      />
    </QueryClientProvider>,
  );
}

describe("GraphCanvas SQL drop", () => {
  beforeEach(() => {
    useMigratorStore.setState({
      graphDraft: structuredClone(emptyGraph),
      graphBaseline: structuredClone(emptyGraph),
      selectedNodeId: null,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("creates the first node from an imported SQL file", () => {
    renderCanvas([{ path: "users/001_create_users.sql", kind: "file" }]);

    fireEvent.drop(screen.getByTestId("graph-canvas"), {
      dataTransfer: {
        types: [],
        getData: (type: string) =>
          type === "text/migrator-resource" ? "users/001_create_users.sql" : "",
      },
    });

    expect(useMigratorStore.getState().graphDraft?.nodes).toEqual([
      {
        name: "users-001_create_users",
        database: "primary",
        dependsOn: [],
        scripts: [{ path: "users/001_create_users.sql" }],
      },
    ]);
    expect(useMigratorStore.getState().selectedNodeId).toBe("users-001_create_users");
  });

  it("creates one node with every SQL file dropped as a directory", () => {
    renderCanvas([
      { path: "users/002_add_email.sql", kind: "file" },
      { path: "users/001_create_users.sql", kind: "file" },
    ]);

    fireEvent.drop(screen.getByTestId("graph-canvas"), {
      dataTransfer: {
        types: [],
        getData: (type: string) => (type === "text/plain" ? "users/" : ""),
      },
    });

    expect(useMigratorStore.getState().graphDraft?.nodes).toEqual([
      {
        name: "users",
        database: "primary",
        dependsOn: [],
        scripts: [{ path: "users/001_create_users.sql" }, { path: "users/002_add_email.sql" }],
      },
    ]);
  });

  it("imports an OS file dropped directly on the canvas and adds it as a node", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ path: "orders.sql", content: "select 1;" }), {
          status: 201,
        }),
      ),
    );
    renderCanvas([]);

    fireEvent.drop(screen.getByTestId("graph-canvas"), {
      dataTransfer: {
        types: ["Files"],
        items: [],
        files: [
          Object.assign(new File(["select 1;"], "orders.sql"), {
            text: () => Promise.resolve("select 1;"),
          }),
        ],
        getData: () => "",
      },
    });

    await waitFor(() => {
      expect(useMigratorStore.getState().graphDraft?.nodes).toEqual([
        {
          name: "orders",
          database: "primary",
          dependsOn: [],
          scripts: [{ path: "orders.sql" }],
        },
      ]);
    });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/scripts",
      expect.objectContaining({ method: "POST" }),
    );
  });
});
