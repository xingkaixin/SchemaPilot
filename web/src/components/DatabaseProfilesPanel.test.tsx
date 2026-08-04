import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DatabaseProfilesPanel } from "./DatabaseProfilesPanel";

afterEach(() => vi.unstubAllGlobals());

describe("DatabaseProfilesPanel", () => {
  it("does not overwrite an existing profile from the add flow", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <DatabaseProfilesPanel
          databases={[
            {
              name: "primary",
              driver: "postgres",
              dsn: "postgres://primary",
              maxOpenConnections: 4,
              connectionTimeout: "5s",
            },
          ]}
        />
      </QueryClientProvider>,
    );

    await userEvent.click(screen.getByRole("button", { name: "Add profile" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "primary");
    await userEvent.type(screen.getByRole("textbox", { name: "DSN" }), "postgres://replacement");
    await userEvent.click(screen.getByRole("button", { name: "Save profile" }));

    expect(
      await screen.findByText("Profile primary already exists. Use Edit to change it."),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
