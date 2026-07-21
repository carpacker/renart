"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useEffect, useRef, type MutableRefObject } from "react";
import type * as MonacoNS from "monaco-editor";

import { getIngestrSuggestions, getOpenAPISuggestions } from "@/lib/api";
import type {
  OpenAPIQueryParameterSuggestion,
  OpenAPISuggestionsResult,
} from "@/lib/generated/api-types";
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
  contentStartColumn: number;
  cursorOffset: number;
  key: string;
  inParameters: boolean;
  normalizedValue: string;
  path: string[];
  quoted: boolean;
  quote: '"' | "'" | null;
  rawValue: string;
  replaceQuotedContentOnly: boolean;
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

type OpenAPIResponsePathSuggestion = {
  path: string;
  detail?: string;
};

type OpenAPISuggestionsWithResponsePaths = OpenAPISuggestionsResult & {
  response_paths?: OpenAPIResponsePathSuggestion[];
};

type ParsedAPIYaml = {
  openapiUrl: string;
  requestUrl: string;
  method: string;
  recordsPath: string;
  nextURLPath: string;
  paginationCursorPath: string;
  hasMorePath: string;
  validationMode: string;
  fieldLines: Record<string, number>;
};

const SUPPORTED_DESTINATIONS = ["postgres", "duckdb", "s3"];
const API_EXAMPLE_OPENAPI_URL = "https://api.weather.gov/openapi.json";
const API_EXAMPLE_REQUEST_URL = "https://api.weather.gov/alerts/active?area=CA";

export function useYAMLIntellisense(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  asset: WebAsset | null,
) {
  const catalog = useAtomValue(suggestionCatalogAtom);
  const connections = useAtomValue(connectionSuggestionsAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const registerConnectionTables = useSetAtom(registerConnectionTablesAtom);
  const cacheRef = useRef(
    new Map<string, Promise<Array<{ value: string; detail?: string; kind?: string }>>>(),
  );
  // OpenAPI spec fetches are relatively expensive and stable per (spec URL,
  // request URL, method); cache the in-flight/settled result so keystroke-driven
  // completion requests don't refetch.
  const openapiCacheRef = useRef(new Map<string, Promise<OpenAPISuggestionsResult | null>>());

  // The completion provider reads its live inputs through this ref so an SSE
  // update (which changes catalog/connections/asset) does not re-register it.
  // Re-registering fires the shared completion registry's onDidChange and resets
  // any open suggestion widget — including the SQL editor's, since the registry
  // spans all languages.
  const yamlStateRef = useRef({
    asset,
    catalog,
    connections,
    selectedEnvironment,
    registerConnectionTables,
  });
  yamlStateRef.current = {
    asset,
    catalog,
    connections,
    selectedEnvironment,
    registerConnectionTables,
  };

  useEffect(() => {
    if (!monaco) {
      return;
    }

    const disposable = monaco.languages.registerCompletionItemProvider("yaml", {
      triggerCharacters: [":", "/", ".", "_", "?", "&", "=", ","],
      provideCompletionItems: async (
        model: MonacoNS.editor.ITextModel,
        position: MonacoNS.Position,
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
            return {
              suggestions: buildAPIValueSuggestions({ fieldContext, monaco }),
            };
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
    if (
      !monaco ||
      !editor ||
      !asset ||
      !isYamlPath(asset.path) ||
      asset.type.toLowerCase() !== "api"
    ) {
      return;
    }
    const owner = "renart-api-yaml";
    let timer: ReturnType<typeof setTimeout> | null = null;
    let version = 0;
    const validate = async () => {
      const model = editor.getModel();
      if (!model) return;
      const currentVersion = ++version;
      const parsed = parseAPIYaml(model.getValue());
      const markers: MonacoNS.editor.IMarkerData[] = [];
      const addMarker = (path: string, message: string) => {
        const lineNumber = parsed.fieldLines[path];
        if (!lineNumber) return;
        const line = model.getLineContent(lineNumber);
        const colon = line.indexOf(":");
        markers.push({
          severity: monaco.MarkerSeverity.Warning,
          message,
          startLineNumber: lineNumber,
          endLineNumber: lineNumber,
          startColumn: Math.max(1, colon + 2),
          endColumn: line.length + 1,
        });
      };

      if (parsed.validationMode && !["off", "warn", "error"].includes(parsed.validationMode)) {
        addMarker(
          "parameters.openapi.validation",
          "OpenAPI validation must be off, warn, or error.",
        );
      }
      // A failed suggestions fetch must still fall through to setModelMarkers:
      // returning early would leave markers from a previous pass on screen
      // after the user fixes the field.
      const result =
        parsed.openapiUrl && parsed.requestUrl
          ? await fetchOpenAPISuggestionsCached(openapiCacheRef, parsed)
          : null;
      if (currentVersion !== version) return;
      if (result) {
        const typed = result as OpenAPISuggestionsWithResponsePaths;
        const recordPaths = new Set((typed.records_paths ?? []).map((item) => item.path));
        const responsePaths = typed.response_paths ?? [];
        const responsePath = (path: string) => responsePaths.find((item) => item.path === path);
        if (parsed.recordsPath && !recordPaths.has(parsed.recordsPath)) {
          addMarker(
            "parameters.response.records_path",
            `records_path ${parsed.recordsPath} is not present in the OpenAPI response schema.`,
          );
        }
        if (parsed.nextURLPath) {
          const match = responsePath(parsed.nextURLPath);
          if (!match || (match.detail && !match.detail.includes("string"))) {
            addMarker(
              "parameters.pagination.next_url_path",
              `next_url_path ${parsed.nextURLPath} is not a string response field.`,
            );
          }
        }
        if (parsed.paginationCursorPath && !responsePath(parsed.paginationCursorPath)) {
          addMarker(
            "parameters.pagination.cursor_path",
            `cursor_path ${parsed.paginationCursorPath} is not present in the OpenAPI response schema.`,
          );
        }
        if (parsed.hasMorePath) {
          const match = responsePath(parsed.hasMorePath);
          if (!match || (match.detail && !match.detail.includes("boolean"))) {
            addMarker(
              "parameters.pagination.has_more_path",
              `has_more_path ${parsed.hasMorePath} is not a boolean response field.`,
            );
          }
        }
      }
      monaco.editor.setModelMarkers(model, owner, markers);
    };
    const schedule = () => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => void validate(), 500);
    };
    schedule();
    const disposable = editor.onDidChangeModelContent(schedule);
    return () => {
      version += 1;
      if (timer) clearTimeout(timer);
      disposable.dispose();
      const model = editor.getModel();
      if (model) monaco.editor.setModelMarkers(model, owner, []);
    };
  }, [asset, editor, monaco]);

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
      },
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
      }),
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
      filterText: filterTextForCurrentYamlValue(fieldContext, item.value),
      insertText: insertTextForYamlValue(item.value, fieldContext),
      kind: monaco.languages.CompletionItemKind.Value,
      label: item.value,
      range: fieldContext.range,
    }),
  );
}

// buildAPIOpenAPIValueSuggestions resolves live completions for spec-driven
// fields — `request.url` (OpenAPI paths plus query names/enum values),
// `response.records_path` (record locations in the selected endpoint's response
// schema), `response.fields` mappings, plus pagination response paths. Returns null when the field isn't
// spec-driven or no OpenAPI URL is set yet, so the caller falls back to the
// static sample suggestions.
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
    pathEndsWith(fieldContext.path, ["parameters", "response"]) &&
    fieldContext.key === "records_path";
  const isNextURLPath =
    pathEndsWith(fieldContext.path, ["parameters", "pagination"]) &&
    fieldContext.key === "next_url_path";
  const isCursorPath =
    pathEndsWith(fieldContext.path, ["parameters", "pagination"]) &&
    fieldContext.key === "cursor_path";
  const isHasMorePath =
    pathEndsWith(fieldContext.path, ["parameters", "pagination"]) &&
    fieldContext.key === "has_more_path";
  const isResponseField = pathEndsWith(fieldContext.path, ["parameters", "response", "fields"]);
  if (
    !isRequestURL &&
    !isRecordsPath &&
    !isNextURLPath &&
    !isCursorPath &&
    !isHasMorePath &&
    !isResponseField
  ) {
    return null;
  }

  const { openapiUrl, requestUrl, method, recordsPath } = parseAPIYaml(content);
  if (!openapiUrl) {
    return null;
  }

  const result = await fetchOpenAPISuggestionsCached(cacheRef, {
    openapiUrl,
    requestUrl,
    method,
  });
  if (!result) {
    return null;
  }
  const typedResult = result as OpenAPISuggestionsWithResponsePaths;
  const requestURLs = Array.isArray(typedResult.request_urls) ? typedResult.request_urls : [];
  const recordsPaths = Array.isArray(typedResult.records_paths) ? typedResult.records_paths : [];
  const responsePaths = Array.isArray(typedResult.response_paths) ? typedResult.response_paths : [];

  if (isRequestURL) {
    const querySuggestions = buildAPIRequestURLQuerySuggestions(
      monaco,
      fieldContext,
      typedResult.query_parameters ?? [],
    );
    if (querySuggestions) {
      return querySuggestions;
    }
    if (requestURLs.length === 0) {
      return null;
    }
    return requestURLs.map((item) =>
      toCompletionItem(monaco, {
        detail: [item.method, item.summary].filter(Boolean).join(" · ") || undefined,
        filterText: filterTextForCurrentYamlValue(fieldContext, item.url),
        insertText: insertTextForYamlValue(item.url, fieldContext),
        kind: monaco.languages.CompletionItemKind.Value,
        label: item.url,
        range: fieldContext.range,
      }),
    );
  }

  if (isNextURLPath || isCursorPath || isHasMorePath) {
    const matchingPaths = responsePaths.filter((item) => {
      if (isHasMorePath) return !item.detail || item.detail.includes("boolean");
      if (isNextURLPath) return !item.detail || item.detail.includes("string");
      return true;
    });
    if (matchingPaths.length === 0) {
      return null;
    }
    return matchingPaths.map((item) =>
      toCompletionItem(monaco, {
        detail: item.detail,
        filterText: filterTextForCurrentYamlValue(fieldContext, item.path),
        insertText: insertTextForYamlValue(item.path, fieldContext),
        kind: monaco.languages.CompletionItemKind.Field,
        label: item.path,
        range: fieldContext.range,
      }),
    );
  }

  if (isResponseField) {
    const recordsPrefix = recordsPath ? `${recordsPath}.` : "";
    const fields = responsePaths
      .filter((item) => !recordsPrefix || item.path.startsWith(recordsPrefix))
      .map((item) => ({
        ...item,
        path: recordsPrefix ? item.path.slice(recordsPrefix.length) : item.path,
      }))
      .filter((item) => item.path !== "");
    if (fields.length === 0) {
      return null;
    }
    return fields.map((item) =>
      toCompletionItem(monaco, {
        detail: item.detail ? `${item.detail} response field` : "OpenAPI response field",
        filterText: filterTextForCurrentYamlValue(fieldContext, item.path),
        insertText: insertTextForYamlValue(item.path, fieldContext),
        kind: monaco.languages.CompletionItemKind.Field,
        label: item.path,
        range: fieldContext.range,
      }),
    );
  }

  if (recordsPaths.length === 0) {
    return null;
  }

  return recordsPaths.map((item) => {
    const isRoot = item.path === "";
    return toCompletionItem(monaco, {
      detail: item.detail,
      filterText: filterTextForCurrentYamlValue(fieldContext, item.path),
      insertText: isRoot
        ? insertTextForYamlValue('""', fieldContext)
        : insertTextForYamlValue(item.path, fieldContext),
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
  args: { openapiUrl: string; requestUrl: string; method: string },
): Promise<OpenAPISuggestionsResult | null> {
  // Query-string edits don't change the selected OpenAPI operation. Keying and
  // fetching by the URL without its query avoids a new spec request/cache entry
  // for every character typed after `?`.
  const requestUrl = requestURLWithoutQuery(args.requestUrl);
  const key = [args.openapiUrl, requestUrl, args.method].join("::");
  const existing = cacheRef.current.get(key);
  if (existing) {
    return existing;
  }
  const pending = getOpenAPISuggestions({
    openapiUrl: args.openapiUrl,
    requestUrl: requestUrl || undefined,
    method: args.method || undefined,
  }).catch(() => null);
  cacheRef.current.set(key, pending);
  return pending;
}

function requestURLWithoutQuery(value: string) {
  const query = value.indexOf("?");
  const fragment = value.indexOf("#");
  const end = [query, fragment]
    .filter((index) => index >= 0)
    .reduce((current, index) => Math.min(current, index), value.length);
  return value.slice(0, end);
}

function buildAPIRequestURLQuerySuggestions(
  monaco: typeof MonacoNS,
  fieldContext: YamlFieldContext,
  parameters: OpenAPIQueryParameterSuggestion[],
): MonacoNS.languages.CompletionItem[] | null {
  const value = fieldContext.normalizedValue;
  const cursor = Math.max(0, Math.min(fieldContext.cursorOffset, value.length));
  const queryStart = value.indexOf("?");
  if (queryStart < 0 || cursor <= queryStart) {
    return null;
  }

  const fragmentStart = value.indexOf("#", queryStart + 1);
  const queryEnd = fragmentStart >= 0 ? fragmentStart : value.length;
  if (cursor > queryEnd) {
    return null;
  }

  const segmentStart = Math.max(queryStart + 1, value.lastIndexOf("&", cursor - 1) + 1);
  const nextAmpersand = value.indexOf("&", cursor);
  const segmentEnd = nextAmpersand >= 0 && nextAmpersand < queryEnd ? nextAmpersand : queryEnd;
  const segment = value.slice(segmentStart, segmentEnd);
  const equals = segment.indexOf("=");
  const used = new Set(
    value
      .slice(queryStart + 1, queryEnd)
      .split("&")
      .map((part) => decodeURLComponent(part.split("=", 1)[0] ?? ""))
      .filter(Boolean),
  );

  const range = (start: number, end: number) =>
    new monaco.Range(
      fieldContext.range.startLineNumber,
      fieldContext.contentStartColumn + start,
      fieldContext.range.startLineNumber,
      fieldContext.contentStartColumn + end,
    );

  if (equals < 0 || cursor <= segmentStart + equals) {
    const currentName = decodeURLComponent(equals >= 0 ? segment.slice(0, equals) : segment);
    used.delete(currentName);
    const nameEnd = equals >= 0 ? segmentStart + equals : segmentEnd;
    return parameters
      .filter((parameter) => !used.has(parameter.name))
      .map((parameter) =>
        toCompletionItem(monaco, {
          detail: [
            "query parameter",
            parameter.required ? "required" : "optional",
            parameter.type,
            parameter.description,
          ]
            .filter(Boolean)
            .join(" · "),
          insertText: `${encodeURIComponent(parameter.name)}=`,
          kind: monaco.languages.CompletionItemKind.Property,
          label: parameter.name,
          range: range(segmentStart, nameEnd),
        }),
      );
  }

  const name = decodeURLComponent(segment.slice(0, equals));
  const parameter = parameters.find((candidate) => candidate.name === name);
  if (!parameter || parameter.values.length === 0) {
    return [];
  }

  const valueStart = segmentStart + equals + 1;
  const valueBeforeCursor = value.slice(valueStart, cursor);
  const lastComma = valueBeforeCursor.lastIndexOf(",");
  const tokenStart = valueStart + lastComma + 1;
  const nextComma = value.indexOf(",", cursor);
  const tokenEnd = nextComma >= 0 && nextComma < segmentEnd ? nextComma : segmentEnd;
  return parameter.values.map((candidate) =>
    toCompletionItem(monaco, {
      detail: parameter.description || `${parameter.name} value`,
      insertText: encodeURIComponent(candidate),
      kind: monaco.languages.CompletionItemKind.EnumMember,
      label: candidate,
      range: range(tokenStart, tokenEnd),
    }),
  );
}

function decodeURLComponent(value: string) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

// parseAPIYaml pulls the three spec-driven values out of an API asset by
// tracking indentation into a key path — enough to reach OpenAPI URL aliases,
// request.url, and method overrides without a full YAML parse (the buffer may
// be mid-edit / invalid).
function parseAPIYaml(content: string): ParsedAPIYaml {
  const stack: Array<{ indent: number; key: string }> = [];
  let openapiUrl = "";
  let requestUrl = "";
  let method = "";
  let recordsPath = "";
  let nextURLPath = "";
  let paginationCursorPath = "";
  let hasMorePath = "";
  let validationMode = "";
  const fieldLines: Record<string, number> = {};

  for (const [index, line] of content.split(/\r?\n/).entries()) {
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
    fieldLines[joined] = index + 1;

    if (value === "") {
      stack.push({ indent, key });
      continue;
    }
    if (
      joined === "parameters.openapi.url" ||
      joined === "parameters.openapi_url" ||
      joined === "openapi.url" ||
      joined === "openapi_url"
    ) {
      openapiUrl = value;
    } else if (joined === "parameters.request.url") {
      requestUrl = value;
    } else if (
      joined === "parameters.request.method" ||
      joined === "parameters.openapi.method" ||
      joined === "openapi.method"
    ) {
      method = value;
    } else if (joined === "parameters.response.records_path") {
      recordsPath = value;
    } else if (joined === "parameters.pagination.next_url_path") {
      nextURLPath = value;
    } else if (joined === "parameters.pagination.cursor_path") {
      paginationCursorPath = value;
    } else if (joined === "parameters.pagination.has_more_path") {
      hasMorePath = value;
    } else if (joined === "parameters.openapi.validation") {
      validationMode = value;
    }
  }

  return {
    openapiUrl,
    requestUrl,
    method,
    recordsPath,
    nextURLPath,
    paginationCursorPath,
    hasMorePath,
    validationMode,
    fieldLines,
  };
}

function apiKeySnippetsForPath(path: string[]): APIKeySuggestion[] {
  if (path.length === 0) {
    return [
      { label: "name", insertText: "name: ${1:schema.asset}" },
      { label: "type", insertText: "type: api" },
      { label: "connection", insertText: "connection: ${1:duckdb-default}" },
      {
        label: "parameters",
        insertText:
          "parameters:\n  openapi:\n    url: ${1:" +
          API_EXAMPLE_OPENAPI_URL +
          "}\n  request:\n    url: ${2:" +
          API_EXAMPLE_REQUEST_URL +
          '}\n    method: GET\n  response:\n    records_path: ${3:""}',
      },
      {
        label: "columns",
        insertText: "columns:\n  - name: ${1:column_name}\n    type: ${2:string}",
      },
    ];
  }

  if (pathEndsWith(path, ["parameters"])) {
    return [
      {
        label: "openapi",
        detail: "OpenAPI spec metadata",
        insertText: "openapi:\n  url: ${1:" + API_EXAMPLE_OPENAPI_URL + "}",
      },
      {
        label: "request",
        detail: "HTTP request",
        insertText:
          "request:\n  url: ${1:" +
          API_EXAMPLE_REQUEST_URL +
          "}\n  method: GET\n  headers:\n    Accept: application/json",
      },
      {
        label: "response",
        detail: "Response shape",
        insertText: 'response:\n  records_path: ${1:""}',
      },
      {
        label: "iterate",
        detail: "Repeat request over values",
        insertText: "iterate:\n  as: ${1:item}\n  over:\n    - ${2:value}",
      },
      {
        label: "auth",
        detail: "HTTP authentication",
        insertText: "auth:\n  type: ${1:bearer}\n  token: ${2:{{ env.API_TOKEN }}}",
      },
      {
        label: "pagination",
        detail: "HTTP pagination",
        insertText:
          "pagination:\n  type: ${1:page_number}\n  page_param: ${2:page}\n  start_page: 1\n  max_pages: ${3:10}",
      },
      {
        label: "load",
        detail: "Load override",
        insertText: "load:\n  target: ${1:duckdb-default}\n  object: ${2:schema.table}",
      },
    ];
  }

  if (pathEndsWith(path, ["parameters", "openapi"])) {
    return [
      {
        label: "url",
        detail: "OpenAPI JSON/YAML URL",
        insertText: "url: ${1:" + API_EXAMPLE_OPENAPI_URL + "}",
      },
      {
        label: "path",
        detail: "OpenAPI path override",
        insertText: "path: ${1:/alerts/active}",
      },
      {
        label: "method",
        detail: "OpenAPI method override",
        insertText: "method: ${1:GET}",
      },
      {
        label: "operation_id",
        detail: "OpenAPI operationId override",
        insertText: "operation_id: ${1:alerts_active}",
      },
      {
        label: "response_status",
        detail: "Response status to infer",
        insertText: "response_status: ${1:200}",
      },
      {
        label: "validation",
        detail: "Runtime schema mismatch behavior",
        insertText: "validation: ${1:warn}",
      },
    ];
  }

  if (pathEndsWith(path, ["parameters", "request"])) {
    return [
      {
        label: "url",
        detail: "HTTP URL",
        insertText: "url: ${1:" + API_EXAMPLE_REQUEST_URL + "}",
      },
      {
        label: "method",
        detail: "HTTP method",
        insertText: "method: ${1:GET}",
      },
      {
        label: "headers",
        detail: "HTTP headers",
        insertText: "headers:\n  Accept: application/json",
      },
      {
        label: "params",
        detail: "Query parameters",
        insertText: "params:\n  ${1:name}: ${2:value}",
      },
      {
        label: "body",
        detail: "JSON request body",
        insertText: "body:\n  ${1:name}: ${2:value}",
      },
    ];
  }

  if (pathEndsWith(path, ["parameters", "response"])) {
    return [
      {
        label: "records_path",
        detail: "Dot path to records",
        insertText: 'records_path: ${1:""}',
      },
      {
        label: "fields",
        detail: "Output field mapping",
        insertText: "fields:\n  ${1:column_name}: ${2:json.path}",
      },
    ];
  }

  if (pathEndsWith(path, ["parameters", "auth"])) {
    return [
      { label: "type", insertText: "type: ${1:bearer}" },
      { label: "token", insertText: "token: ${1:{{ env.API_TOKEN }}}" },
      { label: "username", insertText: "username: ${1:username}" },
      {
        label: "password",
        insertText: "password: ${1:{{ env.API_PASSWORD }}}",
      },
      { label: "name", insertText: "name: ${1:X-API-Key}" },
      { label: "value", insertText: "value: ${1:{{ env.API_KEY }}}" },
      { label: "in", insertText: "in: ${1:header}" },
    ];
  }

  if (pathEndsWith(path, ["parameters", "pagination"])) {
    return [
      { label: "type", insertText: "type: ${1:page_number}" },
      { label: "max_pages", insertText: "max_pages: ${1:10}" },
      { label: "page_param", insertText: "page_param: ${1:page}" },
      { label: "start_page", insertText: "start_page: ${1:1}" },
      { label: "offset_param", insertText: "offset_param: ${1:offset}" },
      { label: "limit_param", insertText: "limit_param: ${1:limit}" },
      { label: "limit", insertText: "limit: ${1:100}" },
      { label: "cursor_param", insertText: "cursor_param: ${1:cursor}" },
      {
        label: "cursor_path",
        insertText: "cursor_path: ${1:pagination.next_cursor}",
      },
      {
        label: "next_url_path",
        insertText: "next_url_path: ${1:pagination.next_url}",
      },
      { label: "next_url_header", insertText: "next_url_header: ${1:Link}" },
      {
        label: "has_more_path",
        insertText: "has_more_path: ${1:pagination.has_next_page}",
      },
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
    return ["GET", "POST", "PUT", "PATCH", "DELETE"].map((value) => ({
      value,
      detail: "HTTP method",
    }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "method") {
    return ["GET", "POST", "PUT", "PATCH", "DELETE"].map((value) => ({
      value,
      detail: "OpenAPI operation method",
    }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "url") {
    return [{ value: API_EXAMPLE_OPENAPI_URL, detail: "sample OpenAPI spec" }];
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "openapi"]) && fieldContext.key === "path") {
    return [{ value: "/alerts/active", detail: "sample OpenAPI path" }];
  }
  if (
    pathEndsWith(fieldContext.path, ["parameters", "openapi"]) &&
    fieldContext.key === "response_status"
  ) {
    return ["200", "201", "202", "default"].map((value) => ({
      value,
      detail: "OpenAPI response status",
    }));
  }
  if (
    pathEndsWith(fieldContext.path, ["parameters", "openapi"]) &&
    fieldContext.key === "validation"
  ) {
    return ["warn", "error", "off"].map((value) => ({ value, detail: "OpenAPI validation mode" }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "request"]) && fieldContext.key === "url") {
    return [{ value: API_EXAMPLE_REQUEST_URL, detail: "sample API endpoint" }];
  }
  if (
    pathEndsWith(fieldContext.path, ["parameters", "response"]) &&
    fieldContext.key === "records_path"
  ) {
    return [
      { value: '""', detail: "response root" },
      { value: "data", detail: "common records property" },
      { value: "items", detail: "common records property" },
      { value: "results", detail: "common records property" },
    ];
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "auth"]) && fieldContext.key === "type") {
    return ["bearer", "basic", "api_key"].map((value) => ({ value, detail: "auth type" }));
  }
  if (pathEndsWith(fieldContext.path, ["parameters", "auth"]) && fieldContext.key === "in") {
    return ["header", "query"].map((value) => ({ value, detail: "api key location" }));
  }
  if (
    pathEndsWith(fieldContext.path, ["parameters", "pagination"]) &&
    fieldContext.key === "type"
  ) {
    return ["page_number", "offset", "cursor", "next_url"].map((value) => ({
      value,
      detail: "pagination strategy",
    }));
  }
  if (
    pathEndsWith(fieldContext.path, ["parameters", "pagination"]) &&
    fieldContext.key === "next_url_path"
  ) {
    return [
      { value: "pagination.next", detail: "common next URL property" },
      { value: "links.next", detail: "common next URL property" },
      { value: "next", detail: "common next URL property" },
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
      }),
    );
  }

  if (fieldContext.inParameters && fieldContext.key === "destination") {
    const values = Array.from(
      new Set([
        ...SUPPORTED_DESTINATIONS,
        ...connections
          .map((entry) => entry.type)
          .filter((type) => SUPPORTED_DESTINATIONS.includes(type)),
      ]),
    ).sort();

    return values.map((value) =>
      toCompletionItem(monaco, {
        detail: "ingestr destination",
        kind: monaco.languages.CompletionItemKind.EnumMember,
        label: value,
        range: fieldContext.range,
      }),
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
      }),
    );
  }

  if (fieldContext.inParameters && fieldContext.key === "source_connection") {
    return connections.map((entry) =>
      toCompletionItem(monaco, {
        detail: entry.type,
        kind: monaco.languages.CompletionItemKind.Reference,
        label: entry.name,
        range: fieldContext.range,
      }),
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
        }),
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
      }),
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
    filterText?: string;
    insertText?: string;
    insertTextRules?: MonacoNS.languages.CompletionItemInsertTextRule;
  },
): MonacoNS.languages.CompletionItem {
  return {
    detail: item.detail,
    filterText: item.filterText,
    insertText: item.insertText ?? item.label,
    insertTextRules: item.insertTextRules,
    kind: item.kind,
    label: item.label,
    range: item.range,
  };
}

function mapSuggestionKind(
  monaco: typeof MonacoNS,
  kind?: string,
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
  position: MonacoNS.Position,
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
  const quotedValue = quotedValueInfo(rawValue);
  const innerStartColumn = quotedValue ? valueStartOffset + 2 : null;
  const innerEndColumn = quotedValue ? valueStartOffset + quotedValue.contentEndOffset + 1 : null;
  const replaceQuotedContentOnly =
    quotedValue !== null &&
    innerStartColumn !== null &&
    innerEndColumn !== null &&
    position.column >= innerStartColumn &&
    position.column <= innerEndColumn;
  const range = replaceQuotedContentOnly
    ? new monaco.Range(position.lineNumber, innerStartColumn, position.lineNumber, innerEndColumn)
    : new monaco.Range(
        position.lineNumber,
        valueStartOffset + 1,
        position.lineNumber,
        lineText.length + 1,
      );
  const contentStartColumn =
    replaceQuotedContentOnly && innerStartColumn !== null ? innerStartColumn : valueStartOffset + 1;

  return {
    contentStartColumn,
    cursorOffset: Math.max(0, position.column - contentStartColumn),
    inParameters: isInsideParameters(model, position.lineNumber),
    key: match[2],
    normalizedValue: normalizeYamlValue(rawValue),
    path: yamlParentPath(model, position.lineNumber, match[1].length),
    quoted: quotedValue !== null,
    quote: quotedValue?.quote ?? null,
    rawValue,
    replaceQuotedContentOnly,
    range,
  };
}

function getYamlKeyContext(
  monaco: typeof MonacoNS,
  model: MonacoNS.editor.ITextModel,
  position: MonacoNS.Position,
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
    range: new monaco.Range(
      position.lineNumber,
      indent + 1,
      position.lineNumber,
      lineText.length + 1,
    ),
  };
}

function yamlParentPath(
  model: MonacoNS.editor.ITextModel,
  lineNumber: number,
  currentIndent: number,
) {
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

function isInsideParameters(model: MonacoNS.editor.ITextModel, lineNumber: number): boolean {
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
    (withoutComment.startsWith('"') && withoutComment.endsWith('"')) ||
    (withoutComment.startsWith("'") && withoutComment.endsWith("'"))
  ) {
    return withoutComment.slice(1, -1);
  }
  return withoutComment;
}

function quoteValueIfNeeded(value: string, quoted: boolean) {
  return quoted ? `'${value}'` : value;
}

function insertTextForYamlValue(value: string, fieldContext: YamlFieldContext) {
  if (isEmptyQuotedLiteral(value)) {
    return fieldContext.replaceQuotedContentOnly ? "" : value;
  }
  if (fieldContext.replaceQuotedContentOnly) {
    return value;
  }
  if (!fieldContext.quoted) {
    return value;
  }
  const quote = fieldContext.quote ?? "'";
  return quote + value.replaceAll(quote, "\\" + quote) + quote;
}

function filterTextForCurrentYamlValue(fieldContext: YamlFieldContext, value: string) {
  if (!fieldContext.quoted || fieldContext.replaceQuotedContentOnly) {
    return undefined;
  }
  const rawValue = fieldContext.rawValue.trim();
  if (fieldContext.normalizedValue === "" && isEmptyQuotedLiteral(rawValue)) {
    return rawValue;
  }
  const quote = fieldContext.quote;
  if (!quote) {
    return undefined;
  }
  return isEmptyQuotedLiteral(value) || value === "" ? quote + quote : quote + value + quote;
}

function isEmptyQuotedLiteral(value: string) {
  return value === '""' || value === "''";
}

function quotedValueInfo(value: string): { quote: '"' | "'"; contentEndOffset: number } | null {
  if (value[0] !== '"' && value[0] !== "'") {
    return null;
  }
  const quote = value[0];
  for (let index = 1; index < value.length; index += 1) {
    if (value[index] === quote && value[index - 1] !== "\\") {
      return { quote, contentEndOffset: index };
    }
  }
  return { quote, contentEndOffset: value.length };
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
