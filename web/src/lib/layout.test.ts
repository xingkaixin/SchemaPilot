import { beforeEach, describe, expect, it } from "vitest";
import { loadNodeLayout, saveNodeLayout } from "./layout";

describe("node layout storage", () => {
  beforeEach(() => window.localStorage.clear());

  it("round-trips node positions per graph", () => {
    saveNodeLayout("shop", { users: { x: 10, y: 20 } });
    saveNodeLayout("other", { users: { x: 99, y: 99 } });
    expect(loadNodeLayout("shop")).toEqual({ users: { x: 10, y: 20 } });
  });

  it("ignores malformed stored entries", () => {
    window.localStorage.setItem(
      "schemapilot:layout:shop",
      JSON.stringify({ users: { x: "10", y: 20 }, orders: { x: 1, y: 2 } }),
    );
    expect(loadNodeLayout("shop")).toEqual({ orders: { x: 1, y: 2 } });
  });

  it("returns an empty layout for unknown graphs or invalid JSON", () => {
    expect(loadNodeLayout("missing")).toEqual({});
    window.localStorage.setItem("schemapilot:layout:bad", "{not json");
    expect(loadNodeLayout("bad")).toEqual({});
  });
});
