import { APIError, fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import type {
  PipelineRun,
  PipelineSchedule,
  RunDetailResponse,
  RunsResponse,
  SchedulesResponse,
  TriggerPipelineResponse,
  UpdateScheduleResponse,
} from "@/lib/types";

export async function getSchedules() {
  return fetchJSON<SchedulesResponse>("/api/schedules");
}

export async function updatePipelineSchedule(
  pipelineId: string,
  input: {
    enabled: boolean;
    schedule: string;
    timezone: string;
    catchup: boolean;
  },
) {
  return fetchJSONWithBody<UpdateScheduleResponse>(
    `/api/pipelines/${pipelineId}/schedule`,
    "PUT",
    input,
  );
}

export type PipelineRunSource =
  | { source: "working_tree"; snapshot_version_id?: never }
  | { source: "snapshot"; snapshot_version_id: string };

export type TriggerPipelineRunInput = PipelineRunSource & {
  environment?: string;
  start?: string;
  end?: string;
  full_refresh?: boolean;
  backfill?: boolean;
  confirmed_environment?: string;
  sensor_mode?: "once" | "wait" | "skip";
};

export type ActivePipelineRunConflict = {
  pipelineId: string;
  activeRunId: string;
};

export function activePipelineRunConflict(error: unknown): ActivePipelineRunConflict | null {
  if (
    !(error instanceof APIError) ||
    error.status !== 409 ||
    error.code !== "pipeline_run_active"
  ) {
    return null;
  }
  if (!error.details || typeof error.details !== "object") {
    return null;
  }
  const details = error.details as Record<string, unknown>;
  if (typeof details.pipeline_id !== "string" || typeof details.active_run_id !== "string") {
    return null;
  }
  return {
    pipelineId: details.pipeline_id,
    activeRunId: details.active_run_id,
  };
}

export async function triggerPipelineRun(pipelineId: string, input: TriggerPipelineRunInput) {
  return fetchJSONWithBody<TriggerPipelineResponse>(
    `/api/pipelines/${pipelineId}/trigger`,
    "POST",
    input,
  );
}

export type GetRunsOptions = {
  pipelineId?: string;
  environment?: string;
  status?: PipelineRun["status"];
  q?: string;
  limit?: number;
  offset?: number;
  page?: number;
};

export async function getRuns(options: number | GetRunsOptions = {}) {
  const normalized = typeof options === "number" ? { limit: options } : options;
  const params = new URLSearchParams();
  if (normalized.pipelineId) params.set("pipeline_id", normalized.pipelineId);
  if (normalized.environment) params.set("environment", normalized.environment);
  if (normalized.status) params.set("status", normalized.status);
  if (normalized.q) params.set("q", normalized.q);
  if (normalized.limit) params.set("limit", String(normalized.limit));
  if (normalized.offset) params.set("offset", String(normalized.offset));
  if (normalized.page) params.set("page", String(normalized.page));
  const query = params.toString();
  return fetchJSON<RunsResponse>(`/api/runs${query ? `?${query}` : ""}`);
}

export async function getRun(runId: PipelineRun["id"]) {
  return fetchJSON<RunDetailResponse>(`/api/runs/${runId}`);
}

export type { PipelineRun, PipelineSchedule };
