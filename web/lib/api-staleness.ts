import { fetchJSON, type MaterializeStreamPayload } from "@/lib/api-core";
import { streamMaterialization, type StreamAssetEvent } from "@/lib/api-streams";

export type AssetStalenessStatus =
  | "fresh"
  | "stale_edited"
  | "stale_upstream"
  | "partial"
  | "never_built"
  | "missing";

export type StalenessInterval = {
  start: string;
  end: string;
};

export type AssetStaleness = {
  asset_id: string;
  asset_name: string;
  status: AssetStalenessStatus;
  fingerprint: string;
  interval_aware: boolean;
  covered_seconds?: number;
  total_seconds?: number;
  gaps?: StalenessInterval[];
  last_materialized_at?: string;
  // Most recent run attempt, orthogonal to `status`. Together they distinguish an
  // untested edit from an edit that was run and failed, and surface unchanged code
  // whose last run failed. `last_run_on_current_content` is true when that run was
  // on the content currently on disk.
  last_run_status?: "succeeded" | "failed";
  last_run_at?: string;
  last_run_on_current_content?: boolean;
};

export type PipelineStalenessResponse = {
  pipeline_id: string;
  pipeline_uuid: string;
  environment: string;
  assets: AssetStaleness[];
};

export type StalenessUpdatedEvent = {
  type: "staleness.updated";
  pipeline_id: string;
  pipeline_uuid: string;
  environment: string;
  start?: string;
  end?: string;
  assets: AssetStaleness[];
};

// buildStalePipelineStream runs the server-side "build stale assets"
// operation: the server recomputes the stale set for this selection and
// rebuilds it in one streamed run (topological order, single combined log).
export async function buildStalePipelineStream(
  pipelineId: string,
  handlers: {
    onChunk?: (chunk: string) => void;
    onDone?: (payload: MaterializeStreamPayload) => void;
    onAssetEvent?: (event: StreamAssetEvent) => void;
  },
  options: { environment?: string; start?: string; end?: string } = {},
) {
  const params = new URLSearchParams();
  if (options.environment) params.set("environment", options.environment);
  if (options.start) params.set("start", options.start);
  if (options.end) params.set("end", options.end);
  const query = params.toString();
  return streamMaterialization(
    `/api/pipelines/${pipelineId}/build-stale/stream${query ? `?${query}` : ""}`,
    handlers,
    "Stale build stream ended unexpectedly.",
  );
}

export async function getPipelineStaleness(
  pipelineId: string,
  options: { environment?: string; start?: string; end?: string } = {},
): Promise<PipelineStalenessResponse> {
  const params = new URLSearchParams();
  if (options.environment) params.set("environment", options.environment);
  if (options.start) params.set("start", options.start);
  if (options.end) params.set("end", options.end);
  const query = params.toString();
  return fetchJSON<PipelineStalenessResponse>(
    `/api/pipelines/${pipelineId}/staleness${query ? `?${query}` : ""}`,
    { cache: "no-store" },
  );
}
