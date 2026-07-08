"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useEffect, useRef, type MutableRefObject } from "react";
import type * as MonacoNS from "monaco-editor";

import { getIngestrSuggestions, getOpenAPISuggestions } from "@/lib/api";
import type { OpenAPISuggestionsResult } from "@/lib/generated/api-types";
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
  path: string[];
  quoted: boolean;
  range: MonacoNS.IRange;
};

type YamlKeyContext = {
  path: string[];
  prefix: string;
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

type APIKeySuggestion = {
  label: string;
  insertText: string;
  detail?: string;
};

type APIValueSuggestion = {
  value: string;
  detail?: string;
};

const SUPPORTED_DESTINATIONS = ["postgres", "duckdb", "s3"];
const API_EXAMPLE_OPENAPI_URL = "https://petstore3.swagger.io/api/v3/openapi.json";
const API_EXAMPLE_REQUEST_URL = "https://petstore3.swagger.io/api/v3/pet/findByStatus?status=available";

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
  // OpenAPI spec fetches are relatively expensive and stable per (spec URL,
  // request URL, method); cache the in-flight/settled result so keystroke-driven
  // completion requests don't refetch.
  const openapiCacheRef = useRef(new Map<string, Promise<OpenAPISuggestionsResult | null>>());

  // The completion provider reads its live inputs through this ref so an SSE
  // update (which changes catalog/connections/asset) does not re-register it.
  // Re-registering fires the shared completion registry's onDidChange and resets
  // any open suggestion widget — including the SQL editor's, since the registry
  // spans all languages.
  const yamlStateRef = useRef({ asset, catalog, connections, selectedEnvironment, registerConnectionTables });
  yamlStateRef.current = { asset, catalog, connections, selectedEnvironment, registerConnectionTables };

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
        const { asset, catalog, connections, selectedEnvironment, registerConnectionTables } =
          yamlStateRef.current;
        if (!asset || !isYamlPath(asset.path)) {
          return { suggestions: [] };
        }

        const content = model.getValue();
        const isIngestr = isIngestrYaml(content);
        // The API asset editor scopes the buffer to the `parameters:` block, so
        // `type: api` isn't in view — fall back to the asset's own type.
        const isAPI = isAPIYaml(content) || asset.type.toLowerCase() === "api";
        if (!isIngestr && !isAPI) {
          return { suggestions: [] };
        }

        const fieldContext = getYamlFieldContext(monaco, model, position);
        if (fieldContext) {
          if (isAPI) {
            const openapiDriven = await buildAPIOpenAPIValueSuggestions({
              cacheRef: openapiCacheRef,
              content,
              fieldContext,
              monaco,
            });
            if (openapiDriven) {
              return { suggestions: openapiDriven };
            }
            return { suggestions: buildAPIValueSuggestions({ fieldContext, monaco }) };
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
        }

        if (!isAPI) {
          return { suggestions: [] };
        }

        const keyContext = getYamlKeyContext(monaco, model, position);
        if (!keyContext) {
          return { suggestions: [] };
        }
        return { suggestions: buildAPIKeySuggestions({ keyContext, monaco }) };
      },
    });

    return () => {
      disposable.dispose();
    };
  }, [monaco]);

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

function buildAPIKeySuggestions({
  keyContext,
  monaco,
}: {
  keyContext: YamlKeyContext;
  monaco: typeof MonacoNS;
}) {
  const snippetsForPath = apiKeySnippetsForPath(keyContext.path);
  return snippetsForPath
    .filter((item) => item.label.startsWith(keyContext.prefix))
    .map((item) =>
      toCompletionItem(monaco, {
        detail: item.detail,
        insertText: item.insertText,
        insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
        kind: monaco.languages.CompletionItemKind.Property,
        label: item.label,
        range: keyContext.range,
      })
    );
}

function buildAPIValueSuggestions({
  fieldContext,
  monaco,
}: {
  fieldContext: YamlFieldContext;
  monaco: typeof MonacoNS;
}) {
  const values = apiValueSuggestionsForField(fieldContext);
  return values.map((item) =>
    toCompletionItem(monaco, {
      detail: item.detail,
      insertText: quoteValueIfNeeded(item.value, fieldContext.quoted),
      kind: monaco.languages.CompletionItemKind.Value,
      label: item.value,
      range: fieldContext.range,
    })
  );
}

// buildAPIOpenAPIValueSuggestions resolves live completions for the two
// spec-driven fields — `request.url` (the OpenAPI paths) and
// `response.records_path` (record locations in the selected endpoint's response
// schema). Returns null when the field isn't spec-driven or no OpenAPI URL is
// set yet, so the caller falls back to the static sample suggestions.
async function buildAPIOpenAPIValueSuggestions({
  cacheRef,
  content,
  fieldContext,
  monaco,
}: {
  cacheRef: MutableRefObject<Map<string, Promise<OpenAPISuggestionsResult | null>>>;
  content: string;
  fieldContext: YamlFieldContext;
  monaco: typeof MonacoNS;
}): Promise<MonacoNS.languages.CompletionItem[] | null> {
  const isRequestURL =
    pathEndsWith(fieldContext.path, ["parameters", "request"]) && fieldContext.key === "url";
  const isRecordsPath =
    pathEndsWith(fieldContext.path, ["parameters", "response"]) && fieldContext.key === "records_path";
  if (!isRequestURL && !isRecordsPath) {
    return null;
  }

  const { openapiUrl, requestUrl, method } = parseAPIYaml(content);
  if (!openapiUrl) {
    return null;
  }

  const result = await fetchOpenAPISuggestionsCached(cacheRef, { openapiUrl, requestUrl, method });
  if (!result) {
    return null;
  }

  if (isRequestURL) {
    if (result.request_urls.length === 0) {
      return null;
    }
    return result.request_urls.map((item) =>
      toCompletionItem(monaco, {
        detail: [item.method, item.summary].filter(Boolean).join(" · ") || undefined,
        insertText: quoteValueIfNeeded(item.url, fieldContext.quoted),
        kind: monaco.languages.CompletionItemKind.Value,
        label: item.url,
        range: fieldContext.range,
      })
    );
  }

  if (result.records_paths.length === 0) {
    return null;
  }
  return result.records_paths.map((item) => {
    const isRoot = item.path === "";
    return toCompletionItem(monaco, {
      detail: item.detail,
      insertText: isRoot ? '""' : quoteValueIfNeeded(item.path, fieldContext.quoted),
      kind: isRoot
        ? monaco.languages.CompletionItemKind.Value
        : monaco.languages.CompletionItemKind.Field,
      label: isRoot ? '""' : item.path,
      range: fieldContext.range,
    });
  });
}

function fetchOpenAPISuggestionsCached(
  cacheRef: MutableRefObject<Map<string, Promise<OpenAPISuggestionsResult | null>>>,
  args: { openapiUrl: string; requestUrl: string; method: string }
): Promise<OpenAPISuggestionsResult | null> {
  const key = [args.openapiUrl, args.requestUrl, args.method].join("::");
  const existing = cacheRef.current.get(key);
  if (existing) {
    return existing;
  }
  const pending = getOpenAPISuggestions({
    openapiUrl: args.openapiUrl,
    requestUrl: args.requestUrl || undefined,
    method: args.method || undefined,
  }).catch(() => null);
  cacheRef.current.set(key, pending);
  return pending;
}

// parseAPIYaml pulls the three spec-driven values out of an API asset by
// tracking indentation into a key path — enough to reach parameters.openapi.url,
// parameters.request.url, and parameters.request.method without a full YAML
// parse (the buffer may be mid-edit / invalid).
function parseAPIYaml(content: string): { openapiUrl: string; requestUrl: string; method: string } {
  const stack: Array<{ indent: number; key: string }> = [];
  let openapiUrl = "";
  let requestUrl = "";
  let method = "";

  for (const line of content.split(/\r?\n/)) {
    const match = line.match(/^(\s*)([A-Za-z_][\w-]*):(\s*)(.*)$/);
    if (!match) {
      continue;
    }
    const indent = match[1].length;
    const key = match[2];
    const value = normalizeYamlValue(match[4] ?? "");

    while (stack.length > 0 && stack[stack.length - 1].indent >= indent) {
      stack.pop();
    }
    const joined = [...stack.map((entry) => entry.key), key].join(".");

    if (value === "") {
      stack.push({ indent, key });
      continue;
    }
    if (joined === "parameters.openapi.url") {
      openapiUrl = value;
    } else if (joined === "parameters.request.url") {
      requestUrl = value;
    } else if (joined === "parameters.request.method") {
      method = value;
    }
  }

  return { openapiUrl, requestUrl, method };
}

function apiKeySnippetsForPath(path: string[]): APIKeySuggestion[] {
  if (path.length === 0) {
    return [
      { label: "name", insertText: "name: ${1:schema.asset}" },
      { label: "type", insertText: "type: api" },
      { label: "connection", insertText: "connection: ${1:duckdb-default}" },
      { label: "parameters", insertText: "parameters:\n  openapi:\n    url: ${1:" + API_EXAMPLE_OPENAPI_URL + "}\n  request:\n    url: ${2:" + API_EXAMPLE_REQUEST_URL + "}\n    method: GET\n  response:\n    records_path: ${3:\"\"}" },
      { label: "columns", insertText: "columns:\n  - name: ${1:column_name}\n    type: ${2:string}" },
    ];
  }

  if (pathEndsWith(path, ["parameters"])) {
    return [
      { label: "openapi", detail: "OpenAPI spec metadata", insertText: "openapi:\n  url: ${1:" + API_EXAMPLE_OPENAPI_URL + "}" },
      { label: "request", detail: "HTTP request", insertText: "request:\n  url: ${1:" + API_EXAMPLE_REQUEST_URL + "}\n  method: GET\n  headers:\n    Accept: application/json" },
      { label: "response", detail: "Response shape", insertText: "response:\n  records_path: ${1:\"\"}" },
      { label: "iterate", detail: "Repeat request over values", insertText: "iterate:\n  as: ${1:item}\n  over:\n    - ${2:value}" },
      { label: "load", detail: "Load override", insertText: "load:\n  target: ${1:duckdb-default}\n  object: ${2:schema.table}" },
    ];
  }

  if (pathEndsWith(path, ["parameters", "openapi"])) {
    return [
      { label: "url", detail: "OpenAPI JSON/YAML URL", insertText: "url: ${1:" + API_EXAMPLE_OPENAPI_URL + "}" },
      { label: "path", detail: "OpenAPI path override", insertText: "path: ${1:/pet/findByStatus}" },
      { label: "method", detail: "OpenAPI method override", insertText: "method: ${1:GET}" },
      { label: "operation_id", detail: "OpenAPI operationId override", insertText: "operation_id: ${1:findPetsByStatus}" },
      { label: "response_status", detail: "Response status to infer", insertText: "response_status: ${1:200}" },
    ];
  }

  if (pathEndsWith(path, ["parameters", "request"])) {
    return [
      { label: "url", detail: "HTTP URL", insertText: "url: ${1:" + API_EXAMPLE_REQUEST_URL + "}" },
      { label: "method", detail: "HTTP method", insertText: "method: ${1:GET}" },
      { label: "headers", detail: "HTTP headers", insertText: "headers:\n  Accept: application/json" },
    ];
  }

  if (pathEndsWith(path, ["parameters", "response"])) {
    return [
      { label: "records_path", detail: "Dot path to records", insertText: "records_path: ${1:\"\"}" },
      { label: "fields", detail: "Output field mapping", insertText: "fields:\n  ${1:column_name}: ${2:json.path}" },
    ];
  }

  if (pathEndsWith(path, ["parameters", "load"])) {
    return [
      { label: "target", insertText: "target: ${1:duckdb-default}" },
      { label: "object", insertText: "object: ${1:schema.table}" },
      { label: "mode", insertText: "mode: ${1:full-refresh}" },
    ];
  }

  return [];
}

function apiValueSuggestionsForField(fieldContext: YamlFieldContext): APIValueSuggestion[] {
  if (pathEndsWith(fieldContext.path, ["parameters", "request"]) && fieldContext.key === "method") {
    return ["GET", "POST", "PUT", "PATCH", "DELETE"].map((value) => ({ value, detail: "HTTP method" }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "method") {
    return ["GET", "POST", "PUT", "PATCH", "DELETE"].map((value) => ({ value, detail: "OpenAPI operation method" }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "url") {
    return [{ value: API_EXAMPLE_OPENAPI_URL, detail: "sample OpenAPI spec" }];
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "path") {
    return [{ value: "/pet/findByStatus", detail: "sample OpenAPI path" }];
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "response_status") {
    return ["200", "201", "202", "default"].map((value) => ({ value, detail: "OpenAPI response status" }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "request"]) && fieldContext.key === "url") {
    return [{ value: API_EXAMPLE_REQUEST_URL, detail: "sample API endpoint" }];
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "response"]) && fieldContext.key === "records_path") {
    return [
      { value: "\"\"", detail: "response root" },
      { value: "data", detail: "common records property" },
      { value: "items", detail: "common records property" },
      { value: "results", detail: "common records property" },
    ];
  }
  return [];
}

function pathEndsWith(path: string[], suffix: string[]) {
  if (suffix.length > path.length) {
    return false;
  }
  return suffix.every((part, index) => path[path.length - suffix.length + index] === part);
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
    insertTextRules?: MonacoNS.languages.CompletionItemInsertTextRule;
  }
): MonacoNS.languages.CompletionItem {
  return {
    detail: item.detail,
    insertText: item.insertText ?? item.label,
    insertTextRules: item.insertTextRules,
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
    path: yamlParentPath(model, position.lineNumber, match[1].length),
    quoted: startsWithQuote(rawValue),
    range: new monaco.Range(
      position.lineNumber,
      valueStartOffset + 1,
      position.lineNumber,
      lineText.length + 1
    ),
  };
}

function getYamlKeyContext(
  monaco: typeof MonacoNS,
  model: MonacoNS.editor.ITextModel,
  position: MonacoNS.Position
): YamlKeyContext | null {
  const lineText = model.getLineContent(position.lineNumber);
  const beforeCursor = lineText.slice(0, position.column - 1);
  if (beforeCursor.includes(":")) {
    return null;
  }

  const match = lineText.match(/^(\s*)([A-Za-z_][\w-]*)?\s*$/);
  if (!match) {
    return null;
  }

  const indent = match[1].length;
  const prefix = match[2] ?? "";
  return {
    path: yamlParentPath(model, position.lineNumber, indent),
    prefix,
    range: new monaco.Range(position.lineNumber, indent + 1, position.lineNumber, lineText.length + 1),
  };
}

function yamlParentPath(model: MonacoNS.editor.ITextModel, lineNumber: number, currentIndent: number) {
  const parents: string[] = [];
  let maxParentIndent = currentIndent;

  for (let currentLine = lineNumber - 1; currentLine >= 1; currentLine -= 1) {
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
    if (indent >= maxParentIndent) {
      continue;
    }
    if (value === "") {
      parents.unshift(key);
    }
    maxParentIndent = indent;
  }

  return parents;
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

function isAPIYaml(content: string) {
  return /^\s*type:\s*api\s*$/m.test(content);
}

function isYamlPath(path: string) {
  const lower = path.toLowerCase();
  return lower.endsWith(".yml") || lower.endsWith(".yaml");
}
