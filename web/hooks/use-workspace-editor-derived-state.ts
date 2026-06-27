"use client";

import { useAtomValue } from "jotai";
import { useMemo } from "react";

import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import {
  getConnectionTypeForAssetType,
  isSqlAssetType,
} from "@/lib/asset-types";
import {
  selectedAssetColumnEntriesAtom,
  selectedAssetInspectColumnsAtom,
  selectedAssetSchemaSuggestionTablesAtom,
  selectedAssetSchemaTablesAtom,
} from "@/lib/atoms/domains/suggestions";
import { parseAssetProvenance } from "@/lib/asset-provenance";
import { selectedEnvironmentAtom } from "@/lib/atoms/domains/workspace";
import { SuggestionTableState } from "@/lib/atoms/suggestion-types";
import { resolveEffectiveConfigEnvironment } from "@/lib/settings-environment";
import { WebAsset } from "@/lib/types";

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

  // Manual dependencies are the ones the user added explicitly, tracked in the
  // renart provenance (renart_dep_add); everything else in `upstreams` was
  // inferred from the SQL. This mirrors the redesign's classifyDependencies.
  const manualUpstreamNames = useMemo(() => {
    const provenance = parseAssetProvenance(asset?.meta);
    const manualKeys = new Set(provenance.depAdd.map((dep) => dep.value.toLowerCase()));
    return (asset?.upstreams ?? []).filter((name) => manualKeys.has(name.toLowerCase()));
  }, [asset?.meta, asset?.upstreams]);

  const inferredUpstreamNames = useMemo(() => {
    const manual = new Set(manualUpstreamNames.map((name) => name.toLowerCase()));
    return (asset?.upstreams ?? []).filter((name) => !manual.has(name.toLowerCase()));
  }, [asset?.upstreams, manualUpstreamNames]);

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
