"use client";

import type { Monaco } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";
import { useCallback, useEffect, useRef, useState } from "react";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { useJinjaIntellisense } from "@/hooks/use-jinja-intellisense";
import { usePythonIntellisense } from "@/hooks/use-python-intellisense";
import { useSQLIntellisense } from "@/hooks/use-sql-intellisense";
import { useSQLLSP } from "@/hooks/use-sql-lsp";
import { useVizIntellisense } from "@/hooks/use-viz-intellisense";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { formatSQLAsset } from "@/lib/api";
import { defineBruinMonacoThemes } from "@/lib/monaco-theme";
import { normalizeVizDirectiveLine } from "@/components/app/notebook-viz-directive";
import {
  buildSchemaForAsset,
  parseQualifiedTableName,
  SchemaColumn,
  SchemaTable,
} from "@/lib/sql-schema";
import { WebAsset, WebColumn, WorkspaceState } from "@/lib/types";

/**
 * Build the completion/parse-context schema for a notebook cell.
 *
 * Sibling cells come first: those are the tables (`cell_<name>` views) that
 * actually exist in the notebook's local DuckDB session, so they are the most
 * useful completion targets. Pipeline assets on the same connection are added
 * after, mirroring the asset editor.
 */
export function buildNotebookSchemaTables(
  workspace: WorkspaceState | null,
  cells: WebAsset[],
  currentCell: WebAsset,
  resultColumnsByCell?: Map<string, string[]>,
): SchemaTable[] {
  const tables: SchemaTable[] = [];
  const seen = new Set<string>();

  for (const cell of cells) {
    if (cell.cell_id && currentCell.cell_id && cell.cell_id === currentCell.cell_id) {
      continue;
    }
    const name = cell.name;
    if (!name || seen.has(name.toLowerCase())) {
      continue;
    }
    seen.add(name.toLowerCase());
    const parts = parseQualifiedTableName(name);
    // Prefer the cell's last-run columns: they are the cell's actual output
    // (including `select *` expansions the static parser can't see), which lets
    // parse-context resolve both the table and its columns.
    const runColumns = cell.cell_id ? resultColumnsByCell?.get(cell.cell_id) : undefined;
    const columns: SchemaColumn[] =
      runColumns && runColumns.length > 0
        ? runColumns.map((column) => ({ name: column, sourceMethods: ["notebook-run"] }))
        : toSchemaColumns(cell.columns);
    tables.push({
      name,
      shortName: parts.shortName,
      columns,
      isBruinAsset: true,
      assetId: cell.id,
      assetPath: cell.path,
      databaseName: parts.databaseName,
      sourceMethods: ["notebook-cell"],
    });
  }

  if (workspace) {
    for (const table of buildSchemaForAsset(workspace, currentCell)) {
      if (seen.has(table.name.toLowerCase())) {
        continue;
      }
      seen.add(table.name.toLowerCase());
      tables.push(table);
    }
  }

  return tables;
}

function toSchemaColumns(columns?: WebColumn[]): SchemaColumn[] {
  if (!columns || columns.length === 0) {
    return [];
  }
  return columns.map((column) => ({
    name: column.name,
    type: column.type,
    description: column.description,
    primaryKey: column.primary_key,
    sourceMethods: ["notebook-cell"],
  }));
}

/**
 * Monaco editor for a single notebook SQL cell. Mirrors the ad hoc editor's
 * intellisense wiring, but the cell is a real (backend-resolvable) asset, so
 * parse-context diagnostics, completion, and go-to work against the cell id.
 * Content is a controlled draft owned by the cell card.
 */
export function NotebookCellMonaco({
  cell,
  value,
  schemaTables,
  resultColumns,
  environment,
  onChange,
  onCommit,
  onRun,
  onRename,
  onGoToAsset,
  onGoToCell,
}: {
  cell: WebAsset;
  value: string;
  schemaTables: SchemaTable[];
  resultColumns: string[];
  environment?: string;
  onChange: (value: string) => void;
  onCommit: () => void;
  onRun: () => void;
  onRename: () => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
  onGoToCell?: (cellId: string) => void;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const [monacoInstance, setMonacoInstance] = useState<Monaco | null>(null);
  const [editorInstance, setEditorInstance] =
    useState<MonacoNS.editor.IStandaloneCodeEditor | null>(null);

  const cellId = cell.cell_id ?? cell.id;
  const isPython =
    cell.type?.toLowerCase() === "python" || cell.path.toLowerCase().endsWith(".py");
  const ext = isPython ? "py" : "sql";

  // SQL intellisense (completion, parse-context, @viz, Jinja) is SQL-only;
  // passing null monaco/editor disables the hooks for Python cells, which fall
  // back to Monaco's built-in Python highlighting.
  const sqlMonaco = isPython ? null : monacoInstance;
  const sqlEditor = isPython ? null : editorInstance;
  useSQLIntellisense(
    sqlMonaco,
    sqlEditor,
    cell,
    value,
    schemaTables,
    cell.upstreams ?? [],
    environment,
    onGoToAsset,
    undefined,
    // The SQL language server below owns diagnostics and decorations, same
    // split as the asset editor; the shared global providers stay on so cells
    // get schema-aware table/column completion (sibling cells + pipeline
    // assets from schemaTables) exactly like the asset editor.
    {
      registerLegacyDiagnosticProviders: false,
      registerParseContextMarkers: false,
      registerSemanticDecorations: false,
    },
  );
  useSQLLSP(sqlMonaco, sqlEditor, cell, value, schemaTables, onGoToAsset, onGoToCell);
  useJinjaIntellisense(sqlMonaco, sqlEditor, cell, value);
  useVizIntellisense(sqlMonaco, sqlEditor, value, resultColumns);

  // Python intellisense (ty: diagnostics, completion, hover, signature, goto,
  // format) is the mirror of the SQL hooks for Python cells; null monaco/editor
  // disables it for SQL cells.
  usePythonIntellisense(
    isPython ? monacoInstance : null,
    isPython ? editorInstance : null,
    cell,
    value,
  );

  // Monaco replaces the whole document when the `value` prop changes; only move
  // the value handed to the editor for changes that did not originate from the
  // editor itself (cell switch, @viz toggle, format), or keystrokes get dropped.
  const lastEditorChangeRef = useRef<{ cellId: string; value: string } | null>(null);
  const displayValueRef = useRef<{ cellId: string | null; value: string }>({
    cellId: null,
    value: "",
  });
  const lastEditorChange = lastEditorChangeRef.current;
  const changeCameFromEditor =
    lastEditorChange?.cellId === cellId && lastEditorChange.value === value;
  if (displayValueRef.current.cellId !== cellId || !changeCameFromEditor) {
    displayValueRef.current = { cellId, value };
  }

  const formatSQL = useCallback(() => {
    if (!editorInstance || isPython) {
      return;
    }
    const content = editorInstance.getValue();
    void formatSQLAsset(cell.id, content)
      .then((response) => {
        if (response.status === "ok") {
          // The formatter pulls the @viz block comment onto the SELECT line;
          // restore it to its own line so the directive reads as a header.
          onChange(normalizeVizDirectiveLine(response.content));
        }
      })
      .catch(() => undefined);
  }, [cell.id, editorInstance, onChange]);

  useEffect(() => {
    if (!editorInstance || !monacoInstance) {
      return;
    }

    const keySub = editorInstance.onKeyDown((event) => {
      if (event.keyCode === monacoInstance.KeyCode.F2) {
        event.preventDefault();
        event.stopPropagation();
        onRename();
        return;
      }

      const ctrlOrCmd = event.ctrlKey || event.metaKey;
      if (!ctrlOrCmd) {
        return;
      }

      if (event.keyCode === monacoInstance.KeyCode.Enter) {
        event.preventDefault();
        event.stopPropagation();
        onCommit();
        onRun();
        return;
      }

      if (event.shiftKey && event.keyCode === monacoInstance.KeyCode.KeyI) {
        event.preventDefault();
        event.stopPropagation();
        formatSQL();
      }
    });

    const blurSub = editorInstance.onDidBlurEditorText(() => {
      onCommit();
    });

    return () => {
      keySub.dispose();
      blurSub.dispose();
    };
  }, [editorInstance, formatSQL, monacoInstance, onCommit, onRename, onRun]);

  const handleBeforeMount = useCallback((monaco: Monaco) => {
    defineBruinMonacoThemes(monaco);
  }, []);

  const handleMount = useCallback(
    (editor: MonacoNS.editor.IStandaloneCodeEditor, monaco: Monaco) => {
      defineBruinMonacoThemes(monaco);
      setEditorInstance(editor);
      setMonacoInstance(monaco);
    },
    [],
  );

  // Grow with content like the old textarea, within bounds.
  const lineCount = displayValueRef.current.value.split("\n").length;
  const height = Math.min(Math.max(lineCount, 3), 24) * 19 + 16;

  return (
    <div className="overflow-hidden rounded-lg border" style={{ height }}>
      <AssetCodeEditor
        asset={cell}
        containerClassName="h-full"
        editorModelPath={`inmemory://bruin/notebook/${cellId}.${ext}`}
        editorValue={displayValueRef.current.value}
        editorHighlighted={false}
        helpMode={false}
        isSqlAsset={!isPython}
        formatShortcutLabel="⌘ + ⇧ + I"
        mobile={false}
        monacoTheme={monacoTheme}
        onChange={(next) => {
          const nextValue = next ?? "";
          lastEditorChangeRef.current = { cellId, value: nextValue };
          onChange(nextValue);
        }}
        onBeforeMount={handleBeforeMount}
        onFormat={formatSQL}
        onMount={handleMount}
      />
    </div>
  );
}
