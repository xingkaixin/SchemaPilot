import { describe, expect, it } from "vitest";
import { droppedResources } from "./osDrop";

function fileEntry(fullPath: string): FileSystemFileEntry {
  return {
    isFile: true,
    isDirectory: false,
    fullPath,
    file: (resolve: (file: File) => void) =>
      resolve(new File(["select 1;"], fullPath.split("/").pop() ?? "")),
  } as unknown as FileSystemFileEntry;
}

function directoryEntry(fullPath: string, children: FileSystemEntry[]): FileSystemDirectoryEntry {
  return {
    isFile: false,
    isDirectory: true,
    fullPath,
    createReader: () => {
      let drained = false;
      return {
        readEntries: (resolve: (entries: FileSystemEntry[]) => void) => {
          resolve(drained ? [] : children);
          drained = true;
        },
      };
    },
  } as unknown as FileSystemDirectoryEntry;
}

function dataTransfer(entries: FileSystemEntry[], files: File[] = []): DataTransfer {
  return {
    items: entries.map((entry) => ({ kind: "file", webkitGetAsEntry: () => entry })),
    files,
  } as unknown as DataTransfer;
}

describe("droppedResources", () => {
  it("recursively collects files from a dropped directory", async () => {
    const drop = dataTransfer([
      directoryEntry("/user", [
        fileEntry("/user/002.sql"),
        directoryEntry("/user/nested", [fileEntry("/user/nested/003.sql")]),
        fileEntry("/user/001.sql"),
      ]),
    ]);
    const resources = await droppedResources(drop);
    expect(resources).toHaveLength(1);
    expect(resources[0]?.kind).toBe("directory");
    expect(resources[0]?.path).toBe("user");
    expect(resources[0]?.files.map((file) => file.path)).toEqual([
      "user/001.sql",
      "user/002.sql",
      "user/nested/003.sql",
    ]);
  });

  it("keeps top-level files as separate resources", async () => {
    const drop = dataTransfer([fileEntry("/001.sql"), fileEntry("/002.sql")]);
    const resources = await droppedResources(drop);
    expect(resources.map((resource) => [resource.kind, resource.path])).toEqual([
      ["file", "001.sql"],
      ["file", "002.sql"],
    ]);
  });

  it("falls back to the flat file list without entry support", async () => {
    const drop = {
      items: [],
      files: [new File(["select 1;"], "standalone.sql")],
    } as unknown as DataTransfer;
    const resources = await droppedResources(drop);
    expect(resources).toEqual([
      {
        kind: "file",
        path: "standalone.sql",
        files: [{ path: "standalone.sql", file: drop.files[0] }],
      },
    ]);
  });
});
