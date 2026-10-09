import { describe, expect, it } from "vitest";
import {
  extractToOwnStep,
  moveFile,
  parallelWithPrevious,
  pathsAfter,
  arrangeByName,
  shift,
  type Step,
} from "./arrangement";

const base: Step[] = [[["a"]], [["b"]], [["c"], ["d", "e"]], [["f"]]];

describe("moveFile", () => {
  it("inserts a new step between steps", () => {
    expect(moveFile(base, "f", { kind: "gap", index: 1 })).toEqual([
      [["a"]],
      [["f"]],
      [["b"]],
      [["c"], ["d", "e"]],
    ]);
  });

  it("keeps the position when the dragged step is the anchor", () => {
    expect(moveFile(base, "b", { kind: "gap", index: 1 })).toEqual(base);
    expect(moveFile(base, "b", { kind: "gap", index: 2 })).toEqual(base);
  });

  it("adds a parallel lane to a step", () => {
    expect(moveFile(base, "a", { kind: "step", step: 1 })).toEqual([
      [["b"], ["a"]],
      [["c"], ["d", "e"]],
      [["f"]],
    ]);
  });

  it("inserts inside a lane and drops emptied steps", () => {
    expect(moveFile(base, "b", { kind: "lane", step: 2, lane: 1, position: 1 })).toEqual([
      [["a"]],
      [["c"], ["d", "b", "e"]],
      [["f"]],
    ]);
  });

  it("reorders within the same lane", () => {
    expect(moveFile(base, "e", { kind: "lane", step: 2, lane: 1, position: 0 })).toEqual([
      [["a"]],
      [["b"]],
      [["c"], ["e", "d"]],
      [["f"]],
    ]);
  });
});

describe("menu operations", () => {
  it("runs a file in parallel with the previous step", () => {
    expect(parallelWithPrevious(base, "f")).toEqual([[["a"]], [["b"]], [["c"], ["d", "e"], ["f"]]]);
    expect(parallelWithPrevious(base, "a")).toBe(base);
  });

  it("extracts a lane file into its own following step", () => {
    expect(extractToOwnStep(base, "c")).toEqual([[["a"]], [["b"]], [["d", "e"]], [["c"]], [["f"]]]);
  });

  it("swaps single-file steps and splits parallel ones", () => {
    expect(shift(base, "b", -1)).toEqual([[["b"]], [["a"]], [["c"], ["d", "e"]], [["f"]]]);
    expect(shift(base, "d", -1)).toEqual([[["a"]], [["b"]], [["d"]], [["c"], ["e"]], [["f"]]]);
    expect(shift(base, "f", 1)).toBe(base);
  });

  it("lists files after a given file", () => {
    expect(pathsAfter(base, "d")).toEqual(["e", "f"]);
  });

  it("builds a serial order by natural path order", () => {
    expect(arrangeByName(["10_x.sql", "2_y.sql", "1_z.sql"])).toEqual([
      [["1_z.sql"]],
      [["2_y.sql"]],
      [["10_x.sql"]],
    ]);
  });

  it("groups {step}_{branch}_ files into parallel lanes", () => {
    expect(
      arrangeByName([
        "pg/030_2_products.sql",
        "pg/030_1_orders.sql",
        "pg/030_customers.sql",
        "pg/030_1_items.sql",
        "pg/040_done.sql",
        "pg/10_1_a.sql",
        "pg/010_2_b.sql",
        "other/030_1_x.sql",
      ]),
    ).toEqual([
      [["other/030_1_x.sql"]],
      [["pg/10_1_a.sql"], ["pg/010_2_b.sql"]],
      [["pg/030_1_items.sql", "pg/030_1_orders.sql"], ["pg/030_2_products.sql"]],
      [["pg/030_customers.sql"]],
      [["pg/040_done.sql"]],
    ]);
  });
});
