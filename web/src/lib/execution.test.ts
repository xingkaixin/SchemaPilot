import { describe, expect, it } from "vitest";
import { executionBlocker } from "./execution";

describe("executionBlocker", () => {
  it("explains why a dirty graph cannot run", () => {
    expect(executionBlocker({ ready: true, isDirty: true })).toBe(
      "Save graph changes before running.",
    );
  });

  it("uses the first workspace problem when the graph is not ready", () => {
    expect(
      executionBlocker({ ready: false, isDirty: false, problems: ["Import one SQL file."] }),
    ).toBe("Import one SQL file.");
  });
});
