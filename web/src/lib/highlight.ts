import type { HighlighterCore, ThemedToken } from "shiki/core";

let highlighter: Promise<HighlighterCore> | undefined;

function load() {
  highlighter ??= (async () => {
    const [{ createHighlighterCore }, { createJavaScriptRegexEngine }] = await Promise.all([
      import("shiki/core"),
      import("shiki/engine/javascript"),
    ]);
    return createHighlighterCore({
      themes: [import("shiki/themes/github-light.mjs")],
      langs: [import("shiki/langs/sql.mjs")],
      engine: createJavaScriptRegexEngine(),
    });
  })();
  return highlighter;
}

export async function tokenize(code: string): Promise<ThemedToken[][]> {
  const instance = await load();
  return instance.codeToTokensBase(code, { lang: "sql", theme: "github-light" });
}
