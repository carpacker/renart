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
  input: { environment?: string; start?: string; end?: string; trigger?: string } = {},
) {
  return fetchJSONWithBody<TriggerPipelineResponse>(
    `/api/pipelines/${pipelineId}/trigger`,
    "POST",
    input,
  );
}

export async function getRuns(limit = 100) {
  return fetchJSON<RunsResponse>(`/api/runs?limit=${encodeURIComponent(String(limit))}`);
}

export async function getRun(runId: PipelineRun["id"]) {
  return fetchJSON<RunDetailResponse>(`/api/runs/${runId}`);
}

export type { PipelineRun, PipelineSchedule };
