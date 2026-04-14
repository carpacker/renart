"use client";

import { Database } from "lucide-react";

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
  selectedEnvironment,
}: {
  asset: WebAsset | null;
  assetInspectColumns: string[];
  debugResolvedUpstreamTables: WorkspaceResolvedUpstreamTable[];
  declaredColumnNames: string[];
  mergedColumnNames: string[];
  schemaSuggestionTables: SuggestionTableState[];
  schemaTablesCount: number;
  selectedEnvironment?: string | null;
}) {
  return (
    <>
      <div className="mt-3 flex items-center gap-2 text-xs opacity-70">
        <Database className="size-3" />
        Environment: {selectedEnvironment || "default"}
      </div>

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
