"use client";

import { useEffect, useRef } from "react";

import { inferAssetColumns } from "@/lib/api";
import { RegisterAssetColumnsPayload } from "@/lib/atoms/suggestion-types";
import { WebAsset } from "@/lib/types";

type SchemaSuggestionTable = {
  assetId?: string;
  name: string;
  columns: Array<{ sourceMethods: string[] }>;
};

export function useAssetEditorSchemaInference({
  asset,
  schemaSuggestionTables,
  onRegisterAssetColumns,
}: {
  asset: WebAsset | null;
  schemaSuggestionTables: SchemaSuggestionTable[];
  onRegisterAssetColumns: (input: RegisterAssetColumnsPayload) => void;
}) {
  const requestedInferenceAssetIdsRef = useRef(new Set<string>());

  useEffect(() => {
    if (!asset) {
      return;
    }

    const upstreamNameSet = new Set(
      (asset.upstreams ?? []).map((name) => name.toLowerCase())
    );
    const tablesNeedingInference = schemaSuggestionTables.filter(
      (table) =>
        table.assetId &&
        table.columns.length === 0 &&
        upstreamNameSet.has(table.name.toLowerCase())
    );

    for (const table of tablesNeedingInference) {
      const tableAssetId = table.assetId;
      if (
        !tableAssetId ||
        requestedInferenceAssetIdsRef.current.has(tableAssetId)
      ) {
        continue;
      }

      requestedInferenceAssetIdsRef.current.add(tableAssetId);
      void inferAssetColumns(tableAssetId)
        .then((response) => {
          onRegisterAssetColumns({
            assetId: tableAssetId,
            method: "asset-column-inference",
            columns: (response.columns ?? []).map((column) => ({
              name: column.name,
              type: column.type,
              description: column.description,
              primaryKey: column.primary_key,
            })),
          });
        })
        .catch(() => {
          // noop: debug panel will still show missing columns
        });
    }
  }, [asset, onRegisterAssetColumns, schemaSuggestionTables]);
}
