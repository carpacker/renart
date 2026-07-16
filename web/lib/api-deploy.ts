import { fetchJSON } from "@/lib/api-core";

export type SnapshotSummary = {
  version_id: string;
  pipeline_id: string;
  merkle_root: string;
  file_count: number;
  git_sha?: string;
  git_dirty?: boolean;
  created_at: string;
  created_by?: string;
};

export type DeployStatus = {
  has_snapshot: boolean;
  executable: boolean;
  integrity_error?: string;
  in_sync: boolean;
  version_id?: string;
  created_at?: string;
  changed_files?: string[];
  added_files?: string[];
  removed_files?: string[];
  snapshot_count: number;
};

export type DeployResponse = {
  status: "ok" | "error";
  created: boolean;
  message: string;
  snapshot: SnapshotSummary;
};

export async function getDeployStatus(pipelineId: string): Promise<DeployStatus> {
  return fetchJSON<DeployStatus>(`/api/pipelines/${pipelineId}/deploy/status`, {
    cache: "no-store",
  });
}

export async function deployPipeline(pipelineId: string): Promise<DeployResponse> {
  return fetchJSON<DeployResponse>(`/api/pipelines/${pipelineId}/deploy`, { method: "POST" });
}

export async function listSnapshots(pipelineId: string): Promise<{ snapshots: SnapshotSummary[] }> {
  return fetchJSON<{ snapshots: SnapshotSummary[] }>(`/api/pipelines/${pipelineId}/snapshots`, {
    cache: "no-store",
  });
}
