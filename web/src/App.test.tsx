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
});
