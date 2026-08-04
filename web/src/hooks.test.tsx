import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useProjectQuery } from "./hooks";
import { useMigratorStore } from "./store";
import type { MigrationGraph, ProjectPayload } from "./types";

const graph: MigrationGraph = {
  version: 1,
  name: "shop",
  parallelism: 2,
  onError: "halt",
  nodes: [
    {
      name: "users",
      database: "primary",
      dependsOn: [],
      scripts: [{ path: "users/001.sql" }],
    },
  ],
};

function Harness() {
  const project = useProjectQuery();
  const draft = useMigratorStore((state) => state.graphDraft);
  const updateGraph = useMigratorStore((state) => state.updateGraph);
  return (
    <>
      <output aria-label="Server parallelism">{project.data?.graph.parallelism}</output>
      <button onClick={() => updateGraph({ name: "shop-draft" })}>{draft?.name}</button>
    </>
  );
}

describe("project graph synchronization", () => {
  beforeEach(() => {
    useMigratorStore.setState({
      graphDraft: null,
      graphBaseline: null,
      graphFingerprint: undefined,
      databaseFingerprint: undefined,
    });
  });

  it("preserves an unsaved graph draft when the project query refreshes", async () => {
    const project: ProjectPayload = {
      graph,
      databases: [],
      files: [],
      ready: true,
      problems: [],
      fingerprint: "v1",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(project), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    );

    const draftButton = await screen.findByRole("button", { name: "shop" });
    await userEvent.click(draftButton);
    expect(screen.getByRole("button", { name: "shop-draft" })).toBeInTheDocument();

    await act(async () => {
      client.setQueryData(["project"], {
        ...project,
        graph: { ...graph, parallelism: 3 },
        fingerprint: "v2",
      });
      await Promise.resolve();
    });
    expect(await screen.findByText("3")).toBeInTheDocument();

    expect(screen.getByRole("button", { name: "shop-draft" })).toBeInTheDocument();
    expect(useMigratorStore.getState().graphFingerprint).toBe("v1");
  });
});
