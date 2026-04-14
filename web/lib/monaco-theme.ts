import type { Monaco } from "@monaco-editor/react";

let themesRegistered = false;

export function defineBruinMonacoThemes(monaco: Monaco) {
  if (themesRegistered) {
    return;
  }

  monaco.editor.defineTheme("bruin-vs", {
    base: "vs",
    inherit: true,
    semanticHighlighting: true,
    rules: [
      { token: "schema", foreground: "7c5a2a" },
      { token: "table", foreground: "0f766e" },
      { token: "column", foreground: "1d4ed8" },
      { token: "alias", foreground: "7c3aed" },
    ],
    colors: {},
    semanticTokenColors: {
      schema: "#7c5a2a",
      table: "#0f766e",
      column: "#1d4ed8",
      alias: "#7c3aed",
    },
  });

  monaco.editor.defineTheme("bruin-vs-dark", {
    base: "vs-dark",
    inherit: true,
    semanticHighlighting: true,
    rules: [
      { token: "schema", foreground: "d6b36d" },
      { token: "table", foreground: "74cfc5" },
      { token: "column", foreground: "93c5fd" },
      { token: "alias", foreground: "c4b5fd" },
    ],
    colors: {},
    semanticTokenColors: {
      schema: "#d6b36d",
      table: "#74cfc5",
      column: "#93c5fd",
      alias: "#c4b5fd",
    },
  });

  themesRegistered = true;
}
