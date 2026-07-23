"use client";

import type { Monaco } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";
import { atom, useAtom, useAtomValue } from "jotai";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { useJinjaIntellisense } from "@/hooks/use-jinja-intellisense";
import { useSQLIntellisense } from "@/hooks/use-sql-intellisense";
import { useSQLLSP } from "@/hooks/use-sql-lsp";
import { useSQLCanvasHover } from "@/hooks/use-sql-canvas-hover";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { formatSQLAsset } from "@/lib/api-assets-crud";
import { selectedAssetSchemaTablesAtom } from "@/lib/atoms/domains/suggestions";
import { selectedEnvironmentAtom } from "@/lib/atoms/domains/workspace";
import { defineBruinMonacoThemes } from "@/lib/monaco-theme";
import { WebAsset } from "@/lib/types";

// Ad hoc query drafts keyed by pipeline id, so switching between the asset
// and ad hoc editors (or between pipelines) keeps the query text around.
const adhocDraftsAtom = atom<Record<string, string>>({});

const DEFAULT_ADHOC_QUERY = "select 1";

export function useAdhocQueryDraft(pipelineId: string) {
  const [drafts, setDrafts] = useAtom(adhocDraftsAtom);
  const value = drafts[pipelineId] ?? DEFAULT_ADHOC_QUERY;
  const setValue = useCallback(
    (nextValue: string) => {
      setDrafts((previous) => ({ ...previous, [pipelineId]: nextValue }));
    },
    [pipelineId, setDrafts],
  );
  return [value, setValue] as const;
}

/**
 * Monaco editor for ad hoc SQL inside the app build page. Borrows the
 * dialect, connection, and parse-context scope from a real SQL asset of the
 * pipeline (`contextAsset`), so completion, diagnostics, and remote table
 * discovery behave like the asset editor — but the content is a local draft
 * that is never saved to disk. It deliberately avoids useAssetMonaco /
 * useSQLFormatting, which write formatted output into the context asset's
 * global editor draft.
 */
export function AppAdhocEditor({
  pipelineId,
  contextAsset,
  onRunQuery,
  onGoToAsset,
}: {
  pipelineId: string;
  contextAsset: WebAsset;
  onRunQuery: () => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const [editorValue, setEditorValue] = useAdhocQueryDraft(pipelineId);
  const schemaTables = useAtomValue(selectedAssetSchemaTablesAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const [monacoInstance, setMonacoInstance] = useState<Monaco | null>(null);
  const [editorInstance, setEditorInstance] =
    useState<MonacoNS.editor.IStandaloneCodeEditor | null>(null);

  // The asset handed to the intellisense hooks: keeps the real asset id
  // (parse-context resolves dialect and pipeline scope by asset id) but
  // drops the name so the self-reference diagnostic does not fire when the
  // query reads from the context asset's own table.
  const adhocAsset = useMemo<WebAsset>(
    () => ({ ...contextAsset, name: "", path: "adhoc.sql" }),
    [contextAsset],
  );

  useSQLIntellisense(
    monacoInstance,
    editorInstance,
    adhocAsset,
    editorValue,
    schemaTables,
    adhocAsset.upstreams ?? [],
    selectedEnvironment,
    onGoToAsset,
    undefined,
    {
      registerGlobalProviders: false,
      registerLegacyDiagnosticProviders: false,
      registerParseContextMarkers: false,
      registerSemanticDecorations: false,
    },
  );
  useSQLLSP(
    monacoInstance,
    editorInstance,
    adhocAsset,
    editorValue,
    schemaTables,
    onGoToAsset,
    undefined,
    { documentContext: "adhoc" },
  );
  useJinjaIntellisense(monacoInstance, editorInstance, adhocAsset, editorValue);
  useSQLCanvasHover(monacoInstance, editorInstance, adhocAsset);

  // Monaco replaces the whole document when the `value` prop changes in a
  // post-commit effect, which can drop keystrokes if the live draft is fed
  // back. Only move the value handed to the editor for changes that did not
  // originate from the editor itself (pipeline switch, format).
  const lastEditorChangeRef = useRef<{ pipelineId: string; value: string } | null>(null);
  const displayValueRef = useRef<{ pipelineId: string | null; value: string }>({
    pipelineId: null,
    value: "",
  });
  const lastEditorChange = lastEditorChangeRef.current;
  const changeCameFromEditor =
    lastEditorChange?.pipelineId === pipelineId && lastEditorChange.value === editorValue;
  if (displayValueRef.current.pipelineId !== pipelineId || !changeCameFromEditor) {
    displayValueRef.current = { pipelineId, value: editorValue };
  }

  const formatSQL = useCallback(() => {
    if (!editorInstance) {
      return;
    }

    const content = editorInstance.getValue();
    void formatSQLAsset(contextAsset.id, content)
      .then((response) => {
        if (response.status === "ok") {
          setEditorValue(response.content);
        }
      })
      .catch(() => undefined);
  }, [contextAsset.id, editorInstance, setEditorValue]);

  useEffect(() => {
    if (!editorInstance || !monacoInstance) {
      return;
    }

    const subscription = editorInstance.onKeyDown((event) => {
      const ctrlOrCmd = event.ctrlKey || event.metaKey;
      if (!ctrlOrCmd) {
        return;
      }

      if (event.keyCode === monacoInstance.KeyCode.Enter) {
        event.preventDefault();
        event.stopPropagation();
        onRunQuery();
        return;
      }

      if (event.shiftKey && event.keyCode === monacoInstance.KeyCode.KeyI) {
        event.preventDefault();
        event.stopPropagation();
        formatSQL();
      }
    });

    return () => {
      subscription.dispose();
    };
  }, [editorInstance, formatSQL, monacoInstance, onRunQuery]);

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

  return (
    <AssetCodeEditor
      asset={adhocAsset}
      containerClassName="min-h-0 flex-1"
      editorModelPath={`inmemory://bruin/adhoc/${pipelineId}.sql`}
      editorValue={displayValueRef.current.value}
      editorHighlighted={false}
      helpMode={false}
      isSqlAsset
      formatShortcutLabel="⌘ + ⇧ + I"
      mobile={false}
      monacoTheme={monacoTheme}
      onChange={(value) => {
        const nextValue = value ?? "";
        lastEditorChangeRef.current = { pipelineId, value: nextValue };
        setEditorValue(nextValue);
      }}
      onBeforeMount={handleBeforeMount}
      onFormat={formatSQL}
      onMount={handleMount}
    />
  );
}
