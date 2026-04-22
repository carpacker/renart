"use client";

import { AssetSqlDebugPanel } from "@/components/asset-sql-debug-panel";
import {
  WorkspaceResolvedUpstreamTable,
} from "@/hooks/use-workspace-editor-derived-state";
import { SuggestionTableState } from "@/lib/atoms/suggestion-types";
import { WebAsset } from "@/lib/types";

export function WorkspaceEditorFooter({
  asset,
  assetInspectColumns,
  debugResolvedUpstreamTables,
  declaredColumnNames,
  mergedColumnNames,
  schemaSuggestionTables,
  schemaTablesCount,
}: {
  asset: WebAsset | null;
  assetInspectColumns: string[];
  debugResolvedUpstreamTables: WorkspaceResolvedUpstreamTable[];
  declaredColumnNames: string[];
  mergedColumnNames: string[];
  schemaSuggestionTables: SuggestionTableState[];
  schemaTablesCount: number;
}) {
  return (
    <>
      {asset ? (
        <AssetSqlDebugPanel
          asset={asset}
          assetInspectColumns={assetInspectColumns}
          declaredColumnNames={declaredColumnNames}
          debugResolvedUpstreamTables={debugResolvedUpstreamTables}
          mergedColumnNames={mergedColumnNames}
          schemaSuggestionTables={schemaSuggestionTables}
          schemaTablesCount={schemaTablesCount}
        />
      ) : null}
    </>
  );
}
