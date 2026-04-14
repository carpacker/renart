"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useMemo } from "react";

import { useAssetEditorSchemaInference } from "@/hooks/use-asset-editor-schema-inference";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import {
  getConnectionTypeForAssetType,
  isSqlAssetType,
} from "@/lib/asset-types";
import {
  registerAssetColumnsAtom,
  selectedAssetColumnEntriesAtom,
  selectedAssetInspectColumnsAtom,
  selectedAssetSchemaSuggestionTablesAtom,
  selectedAssetSchemaTablesAtom,
} from "@/lib/atoms/domains/suggestions";
import { selectedEnvironmentAtom } from "@/lib/atoms/domains/workspace";
import { SuggestionTableState } from "@/lib/atoms/suggestion-types";
import { resolveEffectiveConfigEnvironment } from "@/lib/settings-environment";
import { WebAsset } from "@/lib/types";

const BRUIN_WEB_INFERRED_UPSTREAMS_META_KEY = "bruin_web_inferred_upstreams";

export type WorkspaceResolvedUpstreamTable = {
  upstreamName: string;
  table: SuggestionTableState | undefined;
  source: string;
};

export function useWorkspaceEditorDerivedState({
  asset,
  selectedAssetType,
}: {
  asset: WebAsset | null;
  selectedAssetType: string;
}) {
  const { workspaceConfig } = useWorkspaceSettingsData();
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const assetColumns = useAtomValue(selectedAssetColumnEntriesAtom);
  const assetInspectColumns = useAtomValue(selectedAssetInspectColumnsAtom);
  const schemaTables = useAtomValue(selectedAssetSchemaTablesAtom);
  const schemaSuggestionTables = useAtomValue(selectedAssetSchemaSuggestionTablesAtom);
  const registerAssetColumns = useSetAtom(registerAssetColumnsAtom);

  useAssetEditorSchemaInference({
    asset,
    schemaSuggestionTables,
    onRegisterAssetColumns: registerAssetColumns,
  });

  const requiredConnectionType = useMemo(
    () => getConnectionTypeForAssetType(selectedAssetType),
    [selectedAssetType]
  );

  const activeConfigEnvironment = useMemo(
    () =>
      resolveEffectiveConfigEnvironment({
        environments: workspaceConfig?.environments ?? [],
        selectedEnvironmentName: selectedEnvironment,
        workspaceConfig,
      }),
    [selectedEnvironment, workspaceConfig]
  );

  const showMissingConnectionWarning = useMemo(() => {
    if (!isSqlAssetType(selectedAssetType) || !requiredConnectionType) {
      return false;
    }

    if (!activeConfigEnvironment) {
      return true;
    }

    return !activeConfigEnvironment.connections.some(
      (connection) => connection.type === requiredConnectionType
    );
  }, [activeConfigEnvironment, requiredConnectionType, selectedAssetType]);

  const debugResolvedUpstreamTables = useMemo(() => {
    if (!asset) {
      return [];
    }

    return (asset.upstreams ?? [])
      .map((upstreamName) => {
        const table = schemaSuggestionTables.find(
          (candidate) =>
            candidate.name.toLowerCase() === upstreamName.toLowerCase() ||
            candidate.shortName.toLowerCase() === upstreamName.toLowerCase()
        );

        const hasWorkspaceColumns = table?.columns.some((column) =>
          column.sourceMethods.some(
            (method) => method === "workspace-load" || method === "workspace-event"
          )
        );
        const hasInferredColumns = table?.columns.some((column) =>
          column.sourceMethods.includes("asset-column-inference")
        );
        const source = !table
          ? "unresolved"
          : table.columns.length === 0
            ? "resolved-without-columns"
            : hasWorkspaceColumns && hasInferredColumns
              ? "declared+inferred"
              : hasInferredColumns
                ? "inferred"
                : "declared";

        return { upstreamName, table, source };
      })
      .filter(
        (
          item
        ): item is WorkspaceResolvedUpstreamTable => Boolean(item.upstreamName)
      );
  }, [asset, schemaSuggestionTables]);

  const declaredColumnNames = useMemo(
    () => ((asset?.columns ?? []).map((column) => column.name).filter(Boolean) as string[]),
    [asset]
  );

  const inferredUpstreamNames = useMemo(() => {
    const raw = asset?.meta?.[BRUIN_WEB_INFERRED_UPSTREAMS_META_KEY] ?? "";
    return raw
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  }, [asset?.meta]);

  const manualUpstreamNames = useMemo(() => {
    const inferred = new Set(inferredUpstreamNames.map((name) => name.toLowerCase()));
    return (asset?.upstreams ?? []).filter(
      (name) => !inferred.has(name.toLowerCase())
    );
  }, [asset?.upstreams, inferredUpstreamNames]);

  const mergedColumnNames = useMemo(
    () => ((assetColumns ?? []).map((column) => column.name).filter(Boolean) as string[]),
    [assetColumns]
  );

  return {
    activeConfigEnvironment,
    assetInspectColumns,
    debugResolvedUpstreamTables,
    declaredColumnNames,
    inferredUpstreamNames,
    manualUpstreamNames,
    mergedColumnNames,
    requiredConnectionType,
    schemaSuggestionTables,
    schemaTables,
    selectedEnvironment,
    showMissingConnectionWarning,
  };
}
