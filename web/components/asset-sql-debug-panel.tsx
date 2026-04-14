"use client";

import { Bug, ChevronDown } from "lucide-react";

import {
  WorkspaceResolvedUpstreamTable,
} from "@/hooks/use-workspace-editor-derived-state";
import { SuggestionTableState } from "@/lib/atoms/suggestion-types";
import { WebAsset } from "@/lib/types";

export function AssetSqlDebugPanel({
  asset,
  assetInspectColumns,
  declaredColumnNames,
  debugResolvedUpstreamTables,
  mergedColumnNames,
  schemaSuggestionTables,
  schemaTablesCount,
}: {
  asset: WebAsset;
  assetInspectColumns: string[];
  declaredColumnNames: string[];
  debugResolvedUpstreamTables: WorkspaceResolvedUpstreamTable[];
  mergedColumnNames: string[];
  schemaSuggestionTables: SuggestionTableState[];
  schemaTablesCount: number;
}) {
  return (
    <details className="mt-3 rounded-md border bg-muted/20 p-2 text-[10px] leading-4">
      <summary className="flex cursor-pointer list-none items-center gap-1 font-medium text-muted-foreground">
        <Bug className="size-3" />
        SQL column debug
        <ChevronDown className="size-3" />
      </summary>

      <div className="mt-2 grid gap-2 text-[10px]">
        <DebugList
          items={asset.upstreams ?? []}
          title={`Parsed upstreams (${asset.upstreams?.length ?? 0})`}
        />

        <DebugList
          items={declaredColumnNames}
          title={`Selected asset declared columns (${declaredColumnNames.length})`}
        />

        <DebugList
          items={assetInspectColumns}
          title={`Selected asset inspect columns (${assetInspectColumns.length})`}
        />

        <DebugList
          items={mergedColumnNames}
          title={`Merged visualization/editor columns (${mergedColumnNames.length})`}
        />

        <div className="grid gap-1">
          <div className="font-medium text-muted-foreground">
            Same-connection schema tables ({schemaTablesCount})
          </div>
          <div className="max-h-36 overflow-auto rounded border bg-background/70 p-2 font-mono">
            {schemaSuggestionTables.length > 0 ? (
              schemaSuggestionTables.map((table) => (
                <div className="mb-1 break-all last:mb-0" key={table.name}>
                  <div>
                    {table.name} · {table.columns.length} cols
                    {table.columns.some((column) =>
                      column.sourceMethods.includes("asset-column-inference")
                    )
                      ? " · inferred"
                      : " · declared"}
                  </div>
                  <div className="text-muted-foreground">
                    {table.columns.map((column) => column.name).join(", ") ||
                      "(no columns)"}
                  </div>
                </div>
              ))
            ) : (
              <div className="text-muted-foreground">No schema tables available.</div>
            )}
          </div>
        </div>

        <div className="grid gap-1">
          <div className="font-medium text-muted-foreground">
            Upstream tables resolved for completion ({debugResolvedUpstreamTables.length})
          </div>
          <div className="max-h-36 overflow-auto rounded border bg-background/70 p-2 font-mono">
            {debugResolvedUpstreamTables.length > 0 ? (
              debugResolvedUpstreamTables.map(({ upstreamName, table, source }) => (
                <div className="mb-1 break-all last:mb-0" key={upstreamName}>
                  <div>
                    {upstreamName}
                    {table ? ` -> ${table.name}` : " -> unresolved"}
                    {source ? ` · ${source}` : ""}
                  </div>
                  <div className="text-muted-foreground">
                    {table
                      ? table.columns.map((column) => column.name).join(", ") ||
                        "(resolved, but no columns)"
                      : "(not found in same-connection schema tables)"}
                  </div>
                </div>
              ))
            ) : (
              <div className="text-muted-foreground">
                No parsed upstreams on the selected asset.
              </div>
            )}
          </div>
        </div>
      </div>
    </details>
  );
}

function DebugList({ title, items }: { title: string; items: string[] }) {
  return (
    <div className="grid gap-1">
      <div className="font-medium text-muted-foreground">{title}</div>
      <div className="rounded border bg-background/70 p-2 font-mono break-all">
        {items.length > 0 ? items.join(", ") : "(empty)"}
      </div>
    </div>
  );
}
