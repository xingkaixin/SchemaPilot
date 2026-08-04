import { describe, expect, it } from "vitest";
import { normalizeProject } from "./normalize";

describe("normalizeProject workspace state", () => {
  it("preserves an empty workspace and its run blockers", () => {
    const project = normalizeProject({
      graph: { version: 1, name: "workspace", parallelism: 4, on_error: "halt", nodes: [] },
      databases: [],
      files: [],
      ready: false,
      problems: ["add a database profile"],
      fingerprint: "empty",
      database_fingerprint: "databases-empty",
    });

    expect(project).toMatchObject({
      ready: false,
      problems: ["add a database profile"],
      fingerprint: "empty",
      databaseFingerprint: "databases-empty",
    });
    expect(project.graph.nodes).toEqual([]);
  });
});
