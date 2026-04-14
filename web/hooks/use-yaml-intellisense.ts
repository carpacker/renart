"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useEffect, useRef, type MutableRefObject } from "react";
import type * as MonacoNS from "monaco-editor";

import { getIngestrSuggestions } from "@/lib/api";
import {
  connectionSuggestionsAtom,
  getIngestrTableSuggestionsFromCatalog,
  registerConnectionTablesAtom,
  RegisterConnectionTablesPayload,
  SuggestionCatalogState,
  suggestionCatalogAtom,
} from "@/lib/atoms/domains/suggestions";
import { selectedEnvironmentAtom } from "@/lib/atoms/domains/workspace";
import { WebAsset } from "@/lib/types";

type YamlFieldContext = {
  key: string;
  inParameters: boolean;
  normalizedValue: string;
  quoted: boolean;
  range: MonacoNS.IRange;
};

type ParsedIngestrYaml = {
  topLevel: Record<string, string>;
  parameters: Record<string, string>;
};

type ConnectionEntry = {
  name: string;
  type: string;
  databaseName?: string | null;
};

const SUPPORTED_DESTINATIONS = ["postgres", "duckdb", "s3"];

export function useYAMLIntellisense(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  asset: WebAsset | null
) {
  const catalog = useAtomValue(suggestionCatalogAtom);
  const connections = useAtomValue(connectionSuggestionsAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const registerConnectionTables = useSetAtom(registerConnectionTablesAtom);
  const cacheRef = useRef(new Map<string, Promise<Array<{ value: string; detail?: string; kind?: string }>>>());

  useEffect(() => {
    if (!monaco) {
      return;
    }

    const disposable = monaco.languages.registerCompletionItemProvider("yaml", {
      triggerCharacters: [":", "/", ".", "_"],
      provideCompletionItems: async (
        model: MonacoNS.editor.ITextModel,
        position: MonacoNS.Position
      ) => {
        if (!asset || !isYamlPath(asset.path)) {
          return { suggestions: [] };
        }

        const content = model.getValue();
        if (!isIngestrYaml(content)) {
          return { suggestions: [] };
        }

        const fieldContext = getYamlFieldContext(monaco, model, position);
        if (!fieldContext) {
          return { suggestions: [] };
        }

        const parsed = parseIngestrYaml(content);
        const suggestions = await buildSuggestions({
          catalog,
          cacheRef,
          connections,
          fieldContext,
          monaco,
          onRegisterConnectionTables: registerConnectionTables,
          parsed,
          selectedEnvironment,
        });

        return { suggestions };
      },
    });

    return () => {
      disposable.dispose();
    };
  }, [asset, catalog, connections, monaco, registerConnectionTables, selectedEnvironment]);

  useEffect(() => {
    if (!monaco || !editor || !asset || !isYamlPath(asset.path)) {
      return;
    }

    const disposable = editor.onDidChangeModelContent(
      (event: MonacoNS.editor.IModelContentChangedEvent) => {
        if (!event.changes.some((change: { text: string }) => change.text.includes("/"))) {
          return;
        }

        const model = editor.getModel();
        const position = editor.getPosition();
        if (!model || !position) {
          return;
        }

        const content = model.getValue();
        if (!isIngestrYaml(content)) {
          return;
        }

        const fieldContext = getYamlFieldContext(monaco, model, position);
        if (!fieldContext) {
          return;
        }

        if (!(fieldContext.inParameters && fieldContext.key === "source_table")) {
          return;
        }

        queueMicrotask(() => {
          void editor.getAction("editor.action.triggerSuggest")?.run();
        });
      }
    );

    return () => {
      disposable.dispose();
    };
  }, [asset, editor, monaco]);
}

async function buildSuggestions(args: {
  catalog: SuggestionCatalogState;
  cacheRef: MutableRefObject<
    Map<string, Promise<Array<{ value: string; detail?: string; kind?: string }>>>
  >;
  connections: ConnectionEntry[];
  fieldContext: YamlFieldContext;
  monaco: typeof MonacoNS;
  onRegisterConnectionTables: (payload: RegisterConnectionTablesPayload) => void;
  parsed: ParsedIngestrYaml;
  selectedEnvironment?: string;
}) {
  const {
    catalog,
    cacheRef,
    connections,
    fieldContext,
    monaco,
    onRegisterConnectionTables,
    parsed,
    selectedEnvironment,
  } = args;

  if (!fieldContext.inParameters && fieldContext.key === "connection") {
    const destination = parsed.parameters.destination.toLowerCase();
    const matchingConnections = destination
      ? connections.filter((entry) => entry.type === destination)
      : connections.filter((entry) => SUPPORTED_DESTINATIONS.includes(entry.type));

    return matchingConnections.map((entry) =>
      toCompletionItem(monaco, {
        detail: entry.type,
        kind: monaco.languages.CompletionItemKind.Reference,
        label: entry.name,
        range: fieldContext.range,
      })
    );
  }

  if (fieldContext.inParameters && fieldContext.key === "destination") {
    const values = Array.from(
      new Set([
        ...SUPPORTED_DESTINATIONS,
        ...connections
          .map((entry) => entry.type)
          .filter((type) => SUPPORTED_DESTINATIONS.includes(type)),
      ])
    ).sort();

    return values.map((value) =>
      toCompletionItem(monaco, {
        detail: "ingestr destination",
        kind: monaco.languages.CompletionItemKind.EnumMember,
        label: value,
        range: fieldContext.range,
      })
    );
  }

  if (fieldContext.inParameters && fieldContext.key === "destination_connection") {
    const destination = parsed.parameters.destination.toLowerCase();
    const matchingConnections = destination
      ? connections.filter((entry) => entry.type === destination)
      : connections;

    return matchingConnections.map((entry) =>
      toCompletionItem(monaco, {
        detail: entry.type,
        kind: monaco.languages.CompletionItemKind.Reference,
        label: entry.name,
        range: fieldContext.range,
      })
    );
  }

  if (fieldContext.inParameters && fieldContext.key === "source_connection") {
    return connections.map((entry) =>
      toCompletionItem(monaco, {
        detail: entry.type,
        kind: monaco.languages.CompletionItemKind.Reference,
        label: entry.name,
        range: fieldContext.range,
      })
    );
  }

  if (fieldContext.inParameters && fieldContext.key === "source_table") {
    const sourceConnectionName = parsed.parameters.source_connection;
    if (!sourceConnectionName) {
      return [];
    }

    const cachedSuggestions = getIngestrTableSuggestionsFromCatalog(catalog, {
      connectionName: sourceConnectionName,
      environment: selectedEnvironment,
      prefix: fieldContext.normalizedValue,
    });
    if (cachedSuggestions.length > 0) {
      return cachedSuggestions.map((item) =>
        toCompletionItem(monaco, {
          detail: item.detail,
          insertText: quoteValueIfNeeded(item.value, fieldContext.quoted),
          kind: mapSuggestionKind(monaco, item.kind),
          label: item.value,
          range: fieldContext.range,
        })
      );
    }

    const cacheKey = [
      sourceConnectionName,
      fieldContext.normalizedValue,
      selectedEnvironment ?? "",
    ].join("::");
    const existing = cacheRef.current.get(cacheKey);
    const pending =
      existing ??
      getIngestrSuggestions({
        connection: sourceConnectionName,
        environment: selectedEnvironment,
        prefix: fieldContext.normalizedValue,
      })
        .then((response) => {
          onRegisterConnectionTables({
            connectionName: sourceConnectionName,
            connectionType: response.connection_type,
            environment: selectedEnvironment,
            prefix: fieldContext.normalizedValue,
            tables: response.suggestions.map((suggestion) => ({
              name: suggestion.value,
              kind: suggestion.kind,
              detail: suggestion.detail,
            })),
          });

          return response.suggestions;
        })
        .catch(() => []);

    if (!existing) {
      cacheRef.current.set(cacheKey, pending);
    }

    const values = await pending;
    return values.map((item) =>
      toCompletionItem(monaco, {
        detail: item.detail,
        insertText: quoteValueIfNeeded(item.value, fieldContext.quoted),
        kind: mapSuggestionKind(monaco, item.kind),
        label: item.value,
        range: fieldContext.range,
      })
    );
  }

  return [];
}

function toCompletionItem(
  monaco: typeof MonacoNS,
  item: {
    label: string;
    range: MonacoNS.IRange;
    kind: MonacoNS.languages.CompletionItemKind;
    detail?: string;
    insertText?: string;
  }
): MonacoNS.languages.CompletionItem {
  return {
    detail: item.detail,
    insertText: item.insertText ?? item.label,
    kind: item.kind,
    label: item.label,
    range: item.range,
  };
}

function mapSuggestionKind(
  monaco: typeof MonacoNS,
  kind?: string
): MonacoNS.languages.CompletionItemKind {
  switch (kind) {
    case "bucket":
    case "prefix":
      return monaco.languages.CompletionItemKind.Folder;
    case "file":
      return monaco.languages.CompletionItemKind.File;
    case "table":
      return monaco.languages.CompletionItemKind.Struct;
    default:
      return monaco.languages.CompletionItemKind.Value;
  }
}

function getYamlFieldContext(
  monaco: typeof MonacoNS,
  model: MonacoNS.editor.ITextModel,
  position: MonacoNS.Position
): YamlFieldContext | null {
  const lineText = model.getLineContent(position.lineNumber);
  const match = lineText.match(/^(\s*)([A-Za-z_][\w-]*):(\s*)(.*)$/);
  if (!match) {
    return null;
  }

  const colonIndex = lineText.indexOf(":");
  if (position.column <= colonIndex + 1) {
    return null;
  }

  const rawValue = match[4] ?? "";
  const valueStartOffset = colonIndex + 1 + match[3].length;

  return {
    inParameters: isInsideParameters(model, position.lineNumber),
    key: match[2],
    normalizedValue: normalizeYamlValue(rawValue),
    quoted: startsWithQuote(rawValue),
    range: new monaco.Range(
      position.lineNumber,
      valueStartOffset + 1,
      position.lineNumber,
      lineText.length + 1
    ),
  };
}

function isInsideParameters(
  model: MonacoNS.editor.ITextModel,
  lineNumber: number
): boolean {
  let parametersIndent: number | null = null;

  for (let currentLine = 1; currentLine <= lineNumber; currentLine += 1) {
    const text = model.getLineContent(currentLine);
    const trimmed = text.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    const match = text.match(/^(\s*)([A-Za-z_][\w-]*):(\s*)(.*)$/);
    if (!match) {
      continue;
    }

    const indent = match[1].length;
    const key = match[2];
    const value = (match[4] ?? "").trim();

    if (key === "parameters" && value === "") {
      parametersIndent = indent;
      continue;
    }

    if (parametersIndent !== null && indent <= parametersIndent) {
      parametersIndent = null;
    }
  }

  return parametersIndent !== null;
}

function parseIngestrYaml(content: string): ParsedIngestrYaml {
  const topLevel: Record<string, string> = {};
  const parameters: Record<string, string> = {};
  let parametersIndent: number | null = null;

  for (const line of content.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    const match = line.match(/^(\s*)([A-Za-z_][\w-]*):(\s*)(.*)$/);
    if (!match) {
      continue;
    }

    const indent = match[1].length;
    const key = match[2];
    const value = normalizeYamlValue(match[4] ?? "");

    if (key === "parameters" && value === "") {
      parametersIndent = indent;
      continue;
    }

    if (parametersIndent !== null && indent > parametersIndent) {
      parameters[key] = value;
      continue;
    }

    parametersIndent = null;
    topLevel[key] = value;
  }

  return { topLevel, parameters };
}

function normalizeYamlValue(value: string): string {
  const withoutComment = value.replace(/\s+#.*$/, "").trim();
  if (
    (withoutComment.startsWith("\"") && withoutComment.endsWith("\"")) ||
    (withoutComment.startsWith("'") && withoutComment.endsWith("'"))
  ) {
    return withoutComment.slice(1, -1);
  }
  return withoutComment;
}

function quoteValueIfNeeded(value: string, quoted: boolean) {
  return quoted ? `'${value}'` : value;
}

function startsWithQuote(value: string) {
  const trimmed = value.trim();
  return trimmed.startsWith("\"") || trimmed.startsWith("'");
}

function isIngestrYaml(content: string) {
  return /^\s*type:\s*ingestr\s*$/m.test(content);
}

function isYamlPath(path: string) {
  const lower = path.toLowerCase();
  return lower.endsWith(".yml") || lower.endsWith(".yaml");
}
