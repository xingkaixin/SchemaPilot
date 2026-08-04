import { afterEach, describe, expect, it, vi } from "vitest";
import { importScript, saveDatabase } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("workspace authoring API", () => {
  it("posts imported SQL content", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ path: "users/001.sql", content: "select 1;" }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await importScript("users/001.sql", "select 1;");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/scripts",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ path: "users/001.sql", content: "select 1;" }),
      }),
    );
  });

  it("leaves DSN out of an edit request when it is blank", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          graph: { version: 1, name: "workspace", parallelism: 4, on_error: "halt", nodes: [] },
          databases: [],
          files: [],
          ready: false,
          problems: [],
          database_fingerprint: "next",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await saveDatabase(
      {
        name: "primary",
        driver: "postgres",
        dsn: "",
        maxOpenConnections: 4,
        connectionTimeout: "5s",
      },
      "current",
    );

    const [, options] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(String(options.body))).not.toHaveProperty("dsn");
    expect(options.headers).toEqual(
      expect.objectContaining({
        "If-Match": '"current"',
      }),
    );
  });
});
