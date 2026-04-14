"use client";

import { useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo } from "react";
import type * as MonacoNS from "monaco-editor";

import { formatSQLAsset } from "@/lib/api";
import { isSqlAssetType } from "@/lib/asset-types";
import { editorDraftAtom } from "@/lib/atoms/domains/editor";
import { WebAsset } from "@/lib/types";

export function useSQLFormatting(
  asset: WebAsset | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  monaco: typeof MonacoNS | null,
) {
  const setEditorDraft = useSetAtom(editorDraftAtom);
  const isSqlAsset = useMemo(() => {
    if (!asset) {
      return false;
    }

    return isSqlAssetType(asset.type) || asset.path.toLowerCase().endsWith(".sql");
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

        setEditorDraft((previous) => ({
          ...previous,
          [asset.id]: response.content,
        }));
      })
      .catch(() => undefined);
  }, [asset?.id, editor, isSqlAsset, setEditorDraft]);

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
