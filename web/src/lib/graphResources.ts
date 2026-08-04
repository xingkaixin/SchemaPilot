import type { MigrationGraph, ProjectFile } from "../types";

export const graphDropTargetId = "migration-graph-drop-target";

export interface GraphResource {
  kind: "directory" | "file";
  path: string;
  scripts: string[];
}

export function graphResources(files: ProjectFile[]) {
  const scripts = sqlPaths(files);
  const directories = new Set<string>();
  for (const script of scripts) {
    const segments = script.split("/");
    for (let index = 1; index < segments.length; index += 1) {
      directories.add(segments.slice(0, index).join("/"));
    }
  }

  return [
    ...[...directories].map((path): GraphResource => ({
      kind: "directory",
      path,
      scripts: scripts.filter((script) => script.startsWith(`${path}/`)),
    })),
    ...scripts.map((path): GraphResource => ({ kind: "file", path, scripts: [path] })),
  ].sort(
    (left, right) => left.path.localeCompare(right.path) || left.kind.localeCompare(right.kind),
  );
}

export function graphResourceForPath(files: ProjectFile[], path: string) {
  const normalizedPath = normalizePath(path);
  if (!normalizedPath) return null;

  const scripts = sqlPaths(files);
  if (scripts.includes(normalizedPath)) {
    return {
      kind: "file",
      path: normalizedPath,
      scripts: [normalizedPath],
    } satisfies GraphResource;
  }

  const directoryScripts = scripts.filter((script) => script.startsWith(`${normalizedPath}/`));
  if (directoryScripts.length === 0) return null;
  return {
    kind: "directory",
    path: normalizedPath,
    scripts: directoryScripts,
  } satisfies GraphResource;
}

export function addGraphResource(graph: MigrationGraph, resource: GraphResource, database: string) {
  if (resource.scripts.length === 0) return null;

  const nodeName = nodeNameForResource(resource);
  const existingNode = graph.nodes.find((node) => node.name === nodeName);
  if (existingNode) {
    const existingScripts = new Set(existingNode.scripts.map((script) => script.path));
    const addedScripts = resource.scripts
      .filter((path) => !existingScripts.has(path))
      .map((path) => ({ path }));
    if (addedScripts.length === 0) return { graph, nodeName };
    return {
      graph: {
        ...graph,
        nodes: graph.nodes.map((node) =>
          node.name === nodeName ? { ...node, scripts: [...node.scripts, ...addedScripts] } : node,
        ),
      },
      nodeName,
    };
  }

  return {
    graph: {
      ...graph,
      nodes: [
        ...graph.nodes,
        {
          name: nodeName,
          database,
          dependsOn: [],
          scripts: resource.scripts.map((path) => ({ path })),
        },
      ],
    },
    nodeName,
  };
}

export function isGraphResource(value: unknown): value is GraphResource {
  if (!value || typeof value !== "object") return false;
  const resource = value as Partial<GraphResource>;
  return (
    (resource.kind === "directory" || resource.kind === "file") &&
    typeof resource.path === "string" &&
    Array.isArray(resource.scripts) &&
    resource.scripts.every((script) => typeof script === "string")
  );
}

function sqlPaths(files: ProjectFile[]) {
  return files
    .filter((file) => file.kind !== "directory" && file.path.toLowerCase().endsWith(".sql"))
    .map((file) => normalizePath(file.path))
    .filter(Boolean)
    .sort((left, right) => left.localeCompare(right));
}

function normalizePath(path: string) {
  return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
}

function nodeNameForResource(resource: GraphResource) {
  const segments = resource.path.split("/").filter(Boolean);
  const raw =
    resource.kind === "directory"
      ? segments.at(-1)
      : (segments.at(-2) ?? segments.at(-1)?.replace(/\.sql$/i, ""));
  const normalized = (raw ?? "migration").replace(/[^A-Za-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "");
  if (!normalized) return "migration";
  return /^[A-Za-z]/.test(normalized) ? normalized : `migration-${normalized}`;
}
