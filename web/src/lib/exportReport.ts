import { api, type Connection } from "../api";
import type { ConnectionState } from "./model";
import { buildReport, excerptFor, fileStamp, toLogfmt } from "./report";
import { toHtml } from "./reportHtml";

export type ExportFormat = "log" | "html";

export async function exportReport(
  format: ExportFormat,
  state: ConnectionState,
  connection: Connection,
  driverLabel: string,
) {
  const report = buildReport(state, connection.name, driverLabel, Date.now());
  if (format === "html") {
    await Promise.all(
      report.files
        .filter((file) => file.error && file.outcome !== "succeeded")
        .map(async (file) => {
          const content = await api.file(file.path, connection.driver).catch(() => null);
          if (content) file.excerpt = excerptFor(content.content, file.error!);
        }),
    );
  }
  const name = `${connection.name}-${fileStamp(report.generatedAt)}.${format}`;
  if (format === "log") download(name, toLogfmt(report), "text/plain");
  else download(name, toHtml(report), "text/html");
}

function download(name: string, content: string, type: string) {
  const url = URL.createObjectURL(new Blob([content], { type: `${type};charset=utf-8` }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  URL.revokeObjectURL(url);
}
