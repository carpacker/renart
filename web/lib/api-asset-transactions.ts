import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import { WebColumn } from "@/lib/types";

/**
 * Client for the asset provenance/transaction endpoints (§11 of the asset
 * editing concept). UI cards emit semantic transactions instead of writing
 * YAML directly; the backend enforces ownership and reconciliation.
 */

export type AssetReconcileItem = {
  kind: string;
  column: string;
  detail: string;
};

export type AssetTransactionResult = {
  status: string;
  upstreams: string[];
  columns: WebColumn[];
  reconcile_items?: AssetReconcileItem[];
};

export type AssetTransaction =
  | { type: "dependency.manual.add"; dependency: { asset?: string; uri?: string; mode?: "full" | "symbolic" } }
  | { type: "dependency.manual.remove"; dependency_key: string }
  | { type: "dependency.inferred.ignore"; dependency_key: string }
  | { type: "dependency.inferred.restore"; dependency_key: string }
  | { type: "column.manual.add"; column_def: WebColumn }
  | { type: "column.inferred.drop"; column: string }
  | { type: "column.inferred.restore"; column: string }
  | { type: "column.field.own"; column: string; field: string }
  | { type: "column.field.disown"; column: string; field: string }
  | { type: "column.check.add"; column: string; check: { name: string; value?: unknown; blocking?: boolean; description?: string } }
  | { type: "column.check.remove"; column: string; check: { name: string } }
  | { type: "column.description.set"; column: string; description: string };

/** Apply a single semantic transaction to an asset. */
export async function applyAssetTransaction(assetId: string, tx: AssetTransaction) {
  return fetchJSONWithBody<AssetTransactionResult>(
    `/api/assets/${assetId}/transactions`,
    "POST",
    tx
  );
}

export type ReconcileColumnsResult = {
  status: string;
  columns: WebColumn[];
  reconcile_items?: AssetReconcileItem[];
};

/**
 * Merge a freshly inferred column set into the asset's columns, preserving
 * user-authored metadata and returning any stale-column reconcile items.
 */
export async function reconcileAssetColumns(assetId: string, inferred: WebColumn[]) {
  return fetchJSONWithBody<ReconcileColumnsResult>(
    `/api/assets/${assetId}/columns/reconcile`,
    "POST",
    { columns: inferred }
  );
}

/**
 * Derive the asset's columns from its rendered SQL definition and the declared
 * columns of upstream assets (assets as the source of truth, not the warehouse),
 * then reconcile them into the asset preserving user-authored metadata.
 */
export async function refreshAssetColumnsFromDefinition(assetId: string) {
  return fetchJSON<ReconcileColumnsResult>(
    `/api/assets/${assetId}/columns/refresh-from-definition`,
    { method: "POST" }
  );
}

/**
 * Build the normalized dependency key (a:<asset>#<mode> / u:<uri>#<mode>) the
 * backend uses to identify a dependency, matching assetmeta.DependencyKey.
 */
export function dependencyKey(value: string, mode: "full" | "symbolic" = "full", kind: "asset" | "uri" = "asset") {
  return `${kind === "uri" ? "u" : "a"}:${value.trim()}#${mode}`;
}
