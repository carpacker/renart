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
import { WebAsset } from "@/lib/types";
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

    const diagnostics = (parseContext?.diagnostics ?? [])
      .filter((diagnostic) => diagnostic.range)
      .map((diagnostic) => ({
        severity:
          diagnostic.severity === "warning"
            ? monaco.MarkerSeverity.Warning
            : diagnostic.severity === "info"
              ? monaco.MarkerSeverity.Info
              : monaco.MarkerSeverity.Error,
        message: diagnostic.message,
        startLineNumber: diagnostic.range!.line,
        startColumn: diagnostic.range!.col,
        endLineNumber: diagnostic.range!.end_line,
        endColumn: diagnostic.range!.end_col,
      }));

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
  }, [editor, inspectDiagnosticSnapshot, monaco, parseContextKey]);
}
