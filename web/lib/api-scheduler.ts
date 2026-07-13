import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
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

export async function triggerPipelineRun(
  pipelineId: string,
  input: {
    environment?: string;
    start?: string;
    end?: string;
    trigger?: string;
    backfill?: boolean;
    confirmed_environment?: string;
  } = {},
) {
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
