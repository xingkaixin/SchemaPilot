import Editor, { loader } from "@monaco-editor/react";
import * as monaco from "monaco-editor/editor/editor.api";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import "monaco-editor/languages/definitions/sql/register";

type MonacoScope = typeof globalThis & {
  MonacoEnvironment?: { getWorker: () => Worker };
};

(globalThis as MonacoScope).MonacoEnvironment = {
  getWorker: () => new EditorWorker(),
};
loader.config({ monaco });

export default Editor;
