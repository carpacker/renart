"use client";

import { useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo } from "react";
import type * as MonacoNS from "monaco-editor";

import { formatSQLAsset } from "@/lib/api";
import { isQuerySensorAssetType, isSqlAssetType } from "@/lib/asset-types";
import { editorDraftAtom, editorProgrammaticContentAtom } from "@/lib/atoms/domains/editor";
import { WebAsset } from "@/lib/types";

export function useSQLFormatting(
  asset: WebAsset | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  monaco: typeof MonacoNS | null,
) {
  const setEditorDraft = useSetAtom(editorDraftAtom);
  const setEditorProgrammaticContent = useSetAtom(editorProgrammaticContentAtom);
  const isQuerySensor = isQuerySensorAssetType(asset?.type);
  const isSqlAsset = useMemo(() => {
    if (!asset) {
      return false;
    }

    return (
      isSqlAssetType(asset.type) ||
      isQuerySensorAssetType(asset.type) ||
      asset.path.toLowerCase().endsWith(".sql")
    );
  }, [asset]);

  const shortcutLabel = useMemo(() => "⌘ + ⇧ + I", []);

  const formatSQL = useCallback(() => {
    if (!editor || !isSqlAsset || !asset?.id) {
      return;
    }

    const content = editor.getValue();

    void formatSQLAsset(asset.id, content)
      .then((response) => {
        if (response.status !== "ok") {
          return;
        }

        if (editor.getValue() !== response.content) {
          const model = editor.getModel();
          if (model) {
            editor.executeEdits("format-sql", [
              {
                range: model.getFullModelRange(),
                text: response.content,
                forceMoveMarkers: true,
              },
            ]);
          }
        }

        // Query sensors persist SQL inside parameters.query, not the asset's
        // raw YAML content. Keeping their formatted text out of the generic
        // executable-content draft prevents later actions from treating the
        // query as a replacement for the .asset.yml document.
        if (!isQuerySensor) {
          setEditorDraft((previous) => ({
            ...previous,
            [asset.id]: response.content,
          }));
          setEditorProgrammaticContent((previous) => ({
            ...previous,
            [asset.id]: {
              content: response.content,
              revision: (previous[asset.id]?.revision ?? 0) + 1,
            },
          }));
        }
      })
      .catch(() => undefined);
  }, [asset?.id, editor, isQuerySensor, isSqlAsset, setEditorDraft, setEditorProgrammaticContent]);

  useEffect(() => {
    if (!editor || !monaco || !isSqlAsset) {
      return;
    }

    const subscription = editor.onKeyDown((event) => {
      const ctrlOrCmd = event.ctrlKey || event.metaKey;
      if (!ctrlOrCmd || !event.shiftKey) {
        return;
      }

      if (event.keyCode !== monaco.KeyCode.KeyI) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      formatSQL();
    });

    return () => {
      subscription.dispose();
    };
  }, [editor, formatSQL, isSqlAsset, monaco]);

  return {
    isSqlAsset,
    formatSQL,
    shortcutLabel,
  };
}
