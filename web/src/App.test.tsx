import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "./App";

describe("App loading contract", () => {
  it("shows an explicit API error instead of inventing project data", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("API offline")));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/Could not load this surface/)).toBeInTheDocument();
    expect(screen.getByText("API offline")).toBeInTheDocument();
  });

  it("opens an empty workspace with setup guidance", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/api/v1/project"))
          return Promise.resolve(
            new Response(
              JSON.stringify({
                graph: {
                  version: 1,
                  name: "workspace",
                  parallelism: 4,
                  on_error: "halt",
                  nodes: [],
                },
                databases: [],
                files: [],
                ready: false,
                problems: ["database profiles have not been configured"],
              }),
              { status: 200, headers: { "Content-Type": "application/json" } },
            ),
          );
        return Promise.resolve(
          new Response(JSON.stringify({ runs: [] }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Turn files into a runnable graph.")).toBeInTheDocument();
    expect(screen.getByText("Configure a connection")).toBeInTheDocument();
    expect(screen.getByText("Import SQL files")).toBeInTheDocument();
    expect(screen.getByText("Build and save the graph")).toBeInTheDocument();
  });
});
