export interface DroppedFile {
  path: string;
  file: File;
}

export interface DroppedResource {
  kind: "file" | "directory";
  path: string;
  files: DroppedFile[];
}

export function isOsFileDrag(dataTransfer: DataTransfer) {
  return dataTransfer.types.includes("Files");
}

// webkitGetAsEntry only works synchronously inside the drop event, so entries
// are captured before the first await.
export async function droppedResources(dataTransfer: DataTransfer): Promise<DroppedResource[]> {
  const entries = [...dataTransfer.items]
    .filter((item) => item.kind === "file")
    .map((item) => item.webkitGetAsEntry?.() ?? null)
    .filter((entry): entry is FileSystemEntry => entry !== null);
  if (entries.length > 0) {
    const resources = await Promise.all(entries.map(entryResource));
    return resources.filter((resource) => resource.files.length > 0);
  }
  return [...dataTransfer.files].map((file) => ({
    kind: "file",
    path: file.name,
    files: [{ path: file.name, file }],
  }));
}

async function entryResource(entry: FileSystemEntry): Promise<DroppedResource> {
  const files: DroppedFile[] = [];
  await collectEntry(entry, files);
  files.sort((left, right) => left.path.localeCompare(right.path));
  return {
    kind: entry.isDirectory ? "directory" : "file",
    path: entryPath(entry),
    files,
  };
}

async function collectEntry(entry: FileSystemEntry, out: DroppedFile[]): Promise<void> {
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) =>
      (entry as FileSystemFileEntry).file(resolve, reject),
    );
    out.push({ path: entryPath(entry), file });
    return;
  }
  if (!entry.isDirectory) return;
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  // readEntries returns results in batches and must be called until empty.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) =>
      reader.readEntries(resolve, reject),
    );
    if (batch.length === 0) return;
    for (const child of batch) await collectEntry(child, out);
  }
}

function entryPath(entry: FileSystemEntry) {
  return entry.fullPath.replace(/^\/+/, "");
}
