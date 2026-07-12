import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import type { PipelineRun } from "@/lib/types";

export type CatchupPolicy = "skip" | "run_once" | "backfill";
export type EnvScheduleStatus = "active" | "paused" | "archived" | "delegated";

export type EnvSchedule = {
  pipeline_uuid: string;
  environment: string;
  snapshot_version_id?: string;
  cron: string;
  timezone: string;
  vars?: Record<string, unknown>;
  catchup_policy: CatchupPolicy;
  status: EnvScheduleStatus;
  archived_reason?: string;
  next_run_at?: string;
  created_at: string;
  updated_at: string;
  pipeline_id?: string;
  pipeline_name?: string;
  last_run?: PipelineRun;
};

export type EnvSchedulesResponse = {
  status: "ok" | "error";
  schedules: EnvSchedule[];
  archived: EnvSchedule[];
};

export type UpsertEnvScheduleInput = {
  cron: string;
  timezone?: string;
  vars?: Record<string, unknown>;
  catchup_policy?: CatchupPolicy;
  snapshot_version_id?: string;
  deploy_now?: boolean;
  paused?: boolean;
};

export async function getEnvSchedules(): Promise<EnvSchedulesResponse> {
  return fetchJSON<EnvSchedulesResponse>("/api/env-schedules", { cache: "no-store" });
}

export async function upsertEnvSchedule(
  pipelineId: string,
  environment: string,
  input: UpsertEnvScheduleInput,
): Promise<{ status: string; schedule: EnvSchedule }> {
  return fetchJSONWithBody(
    `/api/pipelines/${pipelineId}/env-schedules/${encodeURIComponent(environment)}`,
    "PUT",
    input,
  );
}

export async function setEnvScheduleStatus(
  pipelineId: string,
  environment: string,
  status: "active" | "paused",
): Promise<{ status: string }> {
  return fetchJSONWithBody(
    `/api/pipelines/${pipelineId}/env-schedules/${encodeURIComponent(environment)}/status`,
    "POST",
    { status },
  );
}

export async function archiveEnvSchedule(
  pipelineId: string,
  environment: string,
): Promise<{ status: string }> {
  return fetchJSON(
    `/api/pipelines/${pipelineId}/env-schedules/${encodeURIComponent(environment)}`,
    {
      method: "DELETE",
    },
  );
}
