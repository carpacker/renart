import { fetchJSON } from "@/lib/api-core";

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

export async function getPipelineStaleness(
  pipelineId: string,
  options: { environment?: string; start?: string; end?: string } = {}
): Promise<PipelineStalenessResponse> {
  const params = new URLSearchParams();
  if (options.environment) params.set("environment", options.environment);
  if (options.start) params.set("start", options.start);
  if (options.end) params.set("end", options.end);
  const query = params.toString();
  return fetchJSON<PipelineStalenessResponse>(
    `/api/pipelines/${pipelineId}/staleness${query ? `?${query}` : ""}`,
    { cache: "no-store" }
  );
}
