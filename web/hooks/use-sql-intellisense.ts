"use client";

import { useEffect, useMemo, useRef } from "react";
import type * as MonacoNS from "monaco-editor";

import {
  getSQLDatabases,
  getSQLPathSuggestions,
  getSQLTableColumns,
  getSQLTables,
} from "@/lib/api";
import {
  registerSQLProviders,
  resolveTableAtPosition,
} from "@/lib/monaco-sql-providers";
import { resolveConnection, SchemaTable } from "@/lib/sql-schema";
import { SqlParseContextDiagnostic, WebAsset } from "@/lib/types";
import { useAtomValue, useSetAtom } from "jotai";

import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import {
  sqlDiscoveryCacheAtom,
  sqlDiscoveryColumnsAtom,
  sqlDiscoveryTablesAtom,
} from "@/lib/atoms/sql-discovery";
import { useSQLParseContext } from "@/hooks/use-sql-parse-context";
import { useSQLSemanticDecorations } from "@/hooks/use-sql-semantic-decorations";
import { fetchJSON } from "@/lib/api-core";
import {
  buildInspectDiagnosticMarker,
  InspectDiagnosticSnapshot,
} from "@/lib/inspect-diagnostics";

function buildValueSuggestionQuery(
  assetType: string | undefined,
  quotedTable: string,
  quotedColumn: string,
  trimmedPrefix: string,
) {
  const escapedPrefix = trimmedPrefix.replaceAll("'", "''");
  const normalizedAssetType = assetType?.toLowerCase() ?? "";

  switch (normalizedAssetType) {
    case "duckdb.sql":
    case "pg.sql":
    case "rs.sql":
    case "bq.sql":
    case "sf.sql":
    case "athena.sql":
    case "databricks.sql":
    case "ms.sql":
    case "synapse.sql":
    case "my.sql":
    default:
      return trimmedPrefix
        ? `select distinct ${quotedColumn} as value from ${quotedTable} where lower(cast(${quotedColumn} as varchar)) like lower('%${escapedPrefix}%') order by 1 limit 10`
        : `select distinct ${quotedColumn} as value from ${quotedTable} order by 1 limit 10`;
  }
}

function quoteSQLIdentifier(identifier: string) {
  return identifier
    .split(".")
    .map(
      (part) =>
        `"${part
          .trim()
          .replace(/^[\[\]"'`]+|[\[\]"'`]+$/g, "")
          .replaceAll('"', '""')}"`,
    )
    .join(".");
}

function schemaNameFromAssetName(name?: string | null) {
  if (!name) {
    return null;
  }

  const parts = name
    .split(".")
    .map((part) => part.trim().replace(/^['"`]+|['"`]+$/g, ""))
    .filter(Boolean);

  if (parts.length < 2) {
    return null;
  }

  return parts[parts.length - 2] ?? null;
}

type UnresolvedColumnSuggestion = {
  diagnostic: SqlParseContextDiagnostic;
  unknownColumn: string;
  replacementColumn: string;
  availableColumns: string[];
  tableIdentifier: string | null;
  range: MonacoNS.IRange;
};

type ColumnSuggestionContext = {
  columns: string[];
  tableIdentifier: string | null;
};

type ParseContextLike = {
  tables?: Array<{ name: string; alias?: string; resolved_name?: string; columns?: Record<string, string> }>;
} | null | undefined;

function normalizeIdentifierPart(value: string) {
  return value.trim().replace(/^[`"']+|[`"']+$/g, "");
}

function extractUnresolvedColumnName(message: string) {
  const match = message.match(/unresolved\s+column\s*:\s*((?:[`"']?[\w$]+[`"']?\.)?[`"']?[\w$]+[`"']?)/i);
  const identifier = match ? normalizeIdentifierPart(match[1]) : null;
  return identifier?.split(".").at(-1) ?? null;
}

function formatUnresolvedColumnMessage(unknownColumn: string, replacementColumn: string) {
  return `Unresolved column '${unknownColumn}'. Did you mean '${replacementColumn}'?`;
}

function levenshteinDistance(left: string, right: string) {
  const rows = left.length + 1;
  const columns = right.length + 1;
  const matrix = Array.from({ length: rows }, () => new Array<number>(columns).fill(0));

  for (let row = 0; row < rows; row += 1) {
    matrix[row][0] = row;
  }
  for (let column = 0; column < columns; column += 1) {
    matrix[0][column] = column;
  }

  for (let row = 1; row < rows; row += 1) {
    for (let column = 1; column < columns; column += 1) {
      const cost = left[row - 1] === right[column - 1] ? 0 : 1;
      matrix[row][column] = Math.min(
        matrix[row - 1][column] + 1,
        matrix[row][column - 1] + 1,
        matrix[row - 1][column - 1] + cost,
      );
    }
  }

  return matrix[left.length][right.length];
}

function findSimilarColumn(unknownColumn: string, columns: string[]) {
  const normalizedUnknown = unknownColumn.toLowerCase();
  const maxDistance = Math.max(2, Math.floor(normalizedUnknown.length / 3));

  let best: { column: string; distance: number } | null = null;
  for (const column of columns) {
    const distance = levenshteinDistance(normalizedUnknown, column.toLowerCase());
    if (distance > maxDistance) {
      continue;
    }
    if (!best || distance < best.distance || (distance === best.distance && column.length < best.column.length)) {
      best = { column, distance };
    }
  }

  return best?.column ?? null;
}

function buildAliasColumnMap(parseContext: ParseContextLike, tables: SchemaTable[]) {
  const aliasColumns = new Map<string, ColumnSuggestionContext>();
  for (const tableEntry of parseContext?.tables ?? []) {
    const tableName = tableEntry.resolved_name ?? tableEntry.name;
    const table = tables.find(
      (candidate) =>
        candidate.name.toLowerCase() === tableName.toLowerCase() ||
        candidate.shortName.toLowerCase() === tableName.toLowerCase(),
    );
    const columns = table?.columns.length
      ? table.columns.map((column) => column.name)
      : Object.keys(tableEntry.columns ?? {});
    if (columns.length === 0) {
      continue;
    }

    const tableIdentifier = table?.shortName ?? tableEntry.resolved_name ?? tableEntry.name;
    const context = { columns, tableIdentifier };
    aliasColumns.set(tableEntry.name.toLowerCase(), context);
    if (table) {
      aliasColumns.set(table.name.toLowerCase(), context);
      aliasColumns.set(table.shortName.toLowerCase(), context);
    }
    if (tableEntry.alias) {
      aliasColumns.set(tableEntry.alias.toLowerCase(), context);
    }
  }

  return aliasColumns;
}

function columnContextForDiagnostic(
  model: MonacoNS.editor.ITextModel,
  diagnostic: SqlParseContextDiagnostic,
  parseContext: ParseContextLike,
  tables: SchemaTable[],
): ColumnSuggestionContext {
  const range = diagnostic.range;
  if (!range) {
    return { columns: [], tableIdentifier: null };
  }

  const aliasColumnMap = buildAliasColumnMap(parseContext, tables);
  const linePrefix = model.getValueInRange({
    startLineNumber: range.line,
    startColumn: 1,
    endLineNumber: range.line,
    endColumn: range.col,
  });
  const qualifier = linePrefix.match(/([`"']?[\w$]+[`"']?)\.\s*$/)?.[1];
  if (qualifier) {
    return aliasColumnMap.get(normalizeIdentifierPart(qualifier).toLowerCase()) ?? { columns: [], tableIdentifier: null };
  }

  return {
    columns: Array.from(new Set(tables.flatMap((table) => table.columns.map((column) => column.name)))),
    tableIdentifier: null,
  };
}

function buildUnresolvedColumnSuggestions(
  model: MonacoNS.editor.ITextModel,
  diagnostics: SqlParseContextDiagnostic[],
  parseContext: ParseContextLike,
  tables: SchemaTable[],
): UnresolvedColumnSuggestion[] {
  return diagnostics.flatMap((diagnostic) => {
    if (!diagnostic.range) {
      return [];
    }

    const unknownColumn = extractUnresolvedColumnName(diagnostic.message);
    if (!unknownColumn) {
      return [];
    }

    const columnContext = columnContextForDiagnostic(model, diagnostic, parseContext, tables);
    const replacementColumn = findSimilarColumn(unknownColumn, columnContext.columns);
    if (!replacementColumn) {
      return [];
    }

    return [{
      diagnostic,
      unknownColumn,
      replacementColumn,
      availableColumns: columnContext.columns,
      tableIdentifier: columnContext.tableIdentifier,
      range: {
        startLineNumber: diagnostic.range.line,
        startColumn: diagnostic.range.col,
        endLineNumber: diagnostic.range.end_line,
        endColumn: diagnostic.range.end_col,
      },
    }];
  });
}

function positionInRange(position: MonacoNS.Position, range: MonacoNS.IRange) {
  if (position.lineNumber < range.startLineNumber || position.lineNumber > range.endLineNumber) {
    return false;
  }
  if (position.lineNumber === range.startLineNumber && position.column < range.startColumn) {
    return false;
  }
  if (position.lineNumber === range.endLineNumber && position.column > range.endColumn) {
    return false;
  }
  return true;
}

function formatAvailableColumnsMarkdown(columns: string[]) {
  return `(${columns.slice(0,5).map((column) => `\`${column}\``).join(", ")}${columns.length > 5 ? '...' : ''})`;
}

function remoteTablesForConnection(
  tablesByScope: Record<string, Array<{ name: string; short_name: string }>>,
  connectionName: string | null,
  environment?: string,
) {
  if (!connectionName) {
    return [];
  }

  return tablesByScope[`${connectionName}::${environment ?? ""}`] ?? [];
}

/**
 * React hook that registers Monaco SQL completion / definition / hover
 * providers scoped to the given schema tables.
 *
 * Providers are re-registered whenever the `tables` reference changes.
 * Call this once from the component that owns the Monaco editor.
 */
export function useSQLIntellisense(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  asset: WebAsset | null,
  sqlContent: string,
  tables: SchemaTable[],
  upstreamNames: string[],
  environment?: string,
  onGoToAsset?: (pipelineId: string, assetId: string) => void,
  inspectDiagnosticSnapshot?: InspectDiagnosticSnapshot | null,
) {
  const workspace = useAtomValue(workspaceAtom);
  const sqlDiscoveryCache = useAtomValue(sqlDiscoveryCacheAtom);
  const loadSQLDiscoveryColumns = useSetAtom(sqlDiscoveryColumnsAtom);
  const loadSQLDiscoveryTables = useSetAtom(sqlDiscoveryTablesAtom);
  const parseContext = useSQLParseContext(asset, sqlContent, tables);
  const parseContextKey = useMemo(() => JSON.stringify(parseContext ?? null), [parseContext]);
  useSQLSemanticDecorations(editor, parseContext);
  const lastGoodParseContextRef = useRef<typeof parseContext>(null);
  if (parseContext && (!parseContext.errors || parseContext.errors.length === 0)) {
    lastGoodParseContextRef.current = parseContext;
  }
  // Keep a stable ref to the latest callback so we don't re-register on
  // every render when the parent re-creates the function.
  const goToAssetRef = useRef(onGoToAsset);
  goToAssetRef.current = onGoToAsset;
  const activeParseContext =
    parseContext && (!parseContext.errors || parseContext.errors.length === 0)
      ? parseContext
      : lastGoodParseContextRef.current;

  useEffect(() => {
    if (!monaco) {
      return;
    }

    const connectionName =
      asset && workspace ? resolveConnection(asset, workspace.connections ?? {}) : null;
    const currentPipelineId = asset
      ? (workspace?.pipelines ?? []).find((pipeline) =>
          pipeline.assets.some((candidate) => candidate.id === asset.id),
        )?.id ?? null
      : null;

    const disposable = registerSQLProviders(monaco, tables, upstreamNames, {
      getParseContext: () => {
        if (parseContext && (!parseContext.errors || parseContext.errors.length === 0)) {
          return parseContext;
        }

        return lastGoodParseContextRef.current;
      },
      async provideTableContextSuggestions({ monaco: monacoInstance, prefix, range }) {
        if (!connectionName) {
          return [];
        }

        let remoteTables = sqlDiscoveryCache.tablesByScope[`${connectionName}::${environment ?? ""}`];
        try {
          remoteTables ??= await loadSQLDiscoveryTables({
            connection: connectionName,
            environment,
          });
        } catch {
          return [];
        }

        const normalizedPrefix = prefix.trim().toLowerCase();

        return (remoteTables ?? [])
          .filter((table) => {
            if (!normalizedPrefix) {
              return true;
            }

            return (
              table.name.toLowerCase().includes(normalizedPrefix) ||
              table.short_name.toLowerCase().includes(normalizedPrefix)
            );
          })
          .filter((table) => {
            return !tables.some(
              (candidate) => candidate.name.toLowerCase() === table.name.toLowerCase(),
            );
          })
          .map((table) => {
            const matchingAsset = tables.find(
              (candidate) => candidate.name.toLowerCase() === table.name.toLowerCase(),
            );
            const description = matchingAsset
              ? `Remote table + Bruin asset (${matchingAsset.assetPath ?? matchingAsset.name})`
              : "Remote table";

            const currentSchemaName = schemaNameFromAssetName(asset?.name)?.toLowerCase() ?? null;
            const tableSchemaName = schemaNameFromAssetName(table.name)?.toLowerCase() ?? null;
            const sameSchema = Boolean(currentSchemaName) && currentSchemaName === tableSchemaName;
            const sameAssetName = asset?.name?.toLowerCase() === table.name.toLowerCase();
            const rank = sameAssetName ? "20" : sameSchema ? "21" : "22";

            return {
              label: {
                label: table.name,
                description,
              },
              kind: monacoInstance.languages.CompletionItemKind.Struct,
              detail: description,
              insertText: table.name,
              range,
              sortText: `${rank}${table.name.toLowerCase()}`,
            };
          });
      },
      async provideColumnSuggestions({
        monaco: monacoInstance,
        tableIdentifier,
        columnPrefix,
        range,
      }) {
        if (!connectionName) {
          return [];
        }

        const normalizedIdentifier = tableIdentifier.trim().toLowerCase();
        const localTable = tables.find(
          (table) =>
            table.name.toLowerCase() === normalizedIdentifier ||
            table.shortName.toLowerCase() === normalizedIdentifier
        );

        const remoteTableName = localTable?.name ?? tableIdentifier;

        let columns = sqlDiscoveryCache.columnsByScope[
          `${connectionName}::${environment ?? ""}::${remoteTableName.toLowerCase()}`
        ];
        try {
          columns ??= await loadSQLDiscoveryColumns({
            connection: connectionName,
            table: remoteTableName,
            environment,
          });
        } catch {
          return [];
        }

        const normalizedPrefix = columnPrefix.trim().toLowerCase();

        return (columns ?? [])
          .filter((column) => {
            if (!normalizedPrefix) {
              return true;
            }

            return column.name.toLowerCase().includes(normalizedPrefix);
          })
          .map((column) => ({
            label: column.name,
            kind: monacoInstance.languages.CompletionItemKind.Field,
            detail: column.type
              ? `${remoteTableName}.${column.name} (${column.type})`
              : `${remoteTableName}.${column.name}`,
            insertText: column.name,
            range,
            sortText: "1",
          }));
      },
      async provideColumnValueSuggestions({
        monaco: monacoInstance,
        tableIdentifier,
        columnName,
        prefix,
        range,
        insideQuotes,
      }) {
        if (!connectionName || !activeParseContext) {
          return [];
        }

        const matchingColumn = (activeParseContext.columns ?? []).find((column) => {
          const columnPart = column.parts.findLast((part) => part.kind === "column");
          return (
            column.qualifier?.toLowerCase() === tableIdentifier.toLowerCase() &&
            columnPart?.name.toLowerCase() === columnName.toLowerCase() &&
            column.resolved_table
          );
        });

        const resolvedTable = matchingColumn?.resolved_table;
        if (!resolvedTable) {
          return [];
        }

        const trimmedPrefix = prefix.trim();
        const quotedTable = quoteSQLIdentifier(resolvedTable);
        const quotedColumn = quoteSQLIdentifier(columnName);
        const valueQuery = buildValueSuggestionQuery(
          asset?.type,
          quotedTable,
          quotedColumn,
          trimmedPrefix,
        );

        try {
          const payload = await fetchJSON<{
            values?: Array<string | number | boolean | null>;
          }>(`/api/sql/column-values`, {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
            },
            cache: "no-store",
            body: JSON.stringify({
              connection: connectionName,
              environment: environment ?? "",
              query: valueQuery,
            }),
          });

          return (payload.values ?? []).map((value, index) => ({
            label: String(value ?? "NULL"),
            kind: monacoInstance.languages.CompletionItemKind.Value,
            detail: `${resolvedTable}.${columnName}`,
            insertText:
              typeof value === "string"
                ? insideQuotes
                  ? String(value).replaceAll("'", "''")
                  : `'${String(value).replaceAll("'", "''")}'`
                : String(value ?? "NULL"),
            range,
            sortText: `0${index}`,
          }));
        } catch {
          return [];
        }
      },
      async providePathSuggestions({ monaco: monacoInstance, prefix, range }) {
        if (!asset?.id) {
          return [];
        }

        let response;
        try {
          response = await getSQLPathSuggestions({
            assetId: asset.id,
            prefix,
            environment,
          });
        } catch {
          return [];
        }

        return response.suggestions.map((suggestion) => ({
          label: suggestion.value,
          kind: suggestion.kind === "directory"
            ? monacoInstance.languages.CompletionItemKind.Folder
            : monacoInstance.languages.CompletionItemKind.File,
          detail: suggestion.detail,
          insertText: suggestion.value,
          range,
          sortText: suggestion.kind === "directory" ? "0" : "1",
        }));
      },
    }, {
      currentPipelineId,
      currentSchemaName: schemaNameFromAssetName(asset?.name),
      currentTableName: asset?.name ?? null,
      remoteTableNames: (remoteTablesForConnection(sqlDiscoveryCache.tablesByScope, connectionName, environment) ?? []).map(
        (table) => table.name,
      ),
    });

    return () => {
      disposable.dispose();
    };
  }, [activeParseContext, asset, editor, environment, loadSQLDiscoveryColumns, loadSQLDiscoveryTables, monaco, parseContextKey, sqlDiscoveryCache.columnsByScope, sqlDiscoveryCache.tablesByScope, tables, upstreamNames, workspace]);

  useEffect(() => {
    if (!editor || tables.length === 0) {
      return;
    }

    const disposable = editor.onMouseDown((event) => {
      if (!event.event.leftButton) {
        return;
      }

      if (!event.event.ctrlKey && !event.event.metaKey) {
        return;
      }

      const position = event.target.position;
      if (!position) {
        return;
      }

      const model = editor.getModel();
      if (!model) {
        return;
      }

      const table = resolveTableAtPosition(model, position, tables);
      if (!table?.assetId || !table.pipelineId) {
        return;
      }

      event.event.preventDefault();
      event.event.stopPropagation();
      goToAssetRef.current?.(table.pipelineId, table.assetId);
    });

    return () => {
      disposable.dispose();
    };
  }, [editor, tables]);

  useEffect(() => {
    if (!editor || !monaco) {
      return;
    }

    const model = editor.getModel();
    if (!model) {
      return;
    }

    const unresolvedColumnSuggestions = buildUnresolvedColumnSuggestions(
      model,
      parseContext?.diagnostics ?? [],
      parseContext,
      tables,
    );

    const diagnostics = (parseContext?.diagnostics ?? [])
      .filter((diagnostic) => diagnostic.range)
      .map((diagnostic) => {
        const unresolvedColumnSuggestion = unresolvedColumnSuggestions.find(
          (suggestion) => suggestion.diagnostic === diagnostic,
        );

        return {
        severity:
          diagnostic.severity === "warning"
            ? monaco.MarkerSeverity.Warning
            : diagnostic.severity === "info"
              ? monaco.MarkerSeverity.Info
              : monaco.MarkerSeverity.Error,
        message: unresolvedColumnSuggestion
          ? formatUnresolvedColumnMessage(
              unresolvedColumnSuggestion.unknownColumn,
              unresolvedColumnSuggestion.replacementColumn,
            )
          : diagnostic.message,
        startLineNumber: diagnostic.range!.line,
        startColumn: diagnostic.range!.col,
        endLineNumber: diagnostic.range!.end_line,
        endColumn: diagnostic.range!.end_col,
        };
      });

    const inspectDiagnostics = buildInspectDiagnosticMarker(
      model,
      inspectDiagnosticSnapshot ?? null,
    );

    monaco.editor.setModelMarkers(model, "bruin-sql-parse-context", [
      ...diagnostics,
      ...inspectDiagnostics,
    ]);

    return () => {
      monaco.editor.setModelMarkers(model, "bruin-sql-parse-context", []);
    };
  }, [editor, inspectDiagnosticSnapshot, monaco, parseContextKey, tables]);

  useEffect(() => {
    if (!editor || !monaco) {
      return;
    }

    const model = editor.getModel();
    if (!model) {
      return;
    }

    const disposable = monaco.languages.registerCodeActionProvider("sql", {
      provideCodeActions: (_model, _range, context) => {
        if (_model.uri.toString() !== model.uri.toString()) {
          return { actions: [], dispose: () => undefined };
        }

        const suggestions = buildUnresolvedColumnSuggestions(
          model,
          parseContext?.diagnostics ?? [],
          parseContext,
          tables,
        );

        const actions = suggestions
          .filter((suggestion) =>
            context.markers.some(
              (marker) =>
                marker.startLineNumber === suggestion.range.startLineNumber &&
                marker.startColumn === suggestion.range.startColumn &&
                marker.endLineNumber === suggestion.range.endLineNumber &&
                marker.endColumn === suggestion.range.endColumn,
            ),
          )
          .map((suggestion) => ({
            title: `Change '${suggestion.unknownColumn}' to '${suggestion.replacementColumn}'`,
            kind: "quickfix",
            diagnostics: context.markers,
            edit: {
              edits: [
                {
                  resource: model.uri,
                  versionId: model.getVersionId(),
                  textEdit: {
                    range: suggestion.range,
                    text: suggestion.replacementColumn,
                  },
                },
              ],
            },
            isPreferred: true,
          }));

        return { actions, dispose: () => undefined };
      },
    });

    return () => disposable.dispose();
  }, [editor, monaco, parseContextKey, tables]);

  useEffect(() => {
    if (!editor || !monaco) {
      return;
    }

    const model = editor.getModel();
    if (!model) {
      return;
    }

    const disposable = monaco.languages.registerHoverProvider("sql", {
      provideHover: (_model, position) => {
        if (_model.uri.toString() !== model.uri.toString()) {
          return null;
        }

        const suggestion = buildUnresolvedColumnSuggestions(
          model,
          parseContext?.diagnostics ?? [],
          parseContext,
          tables,
        ).find((candidate) => positionInRange(position, candidate.range));

        if (!suggestion) {
          return null;
        }

        return {
          range: suggestion.range,
          contents: [
            { value: `**Unresolved column** \`${suggestion.unknownColumn}\`` },
            {
              value: `Did you mean \`${suggestion.replacementColumn}\``,
            },
          ],
        };
      },
    });

    return () => disposable.dispose();
  }, [editor, monaco, parseContextKey, tables]);
}
