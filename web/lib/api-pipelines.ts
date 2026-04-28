import {
  buildQueryString,
  fetchJSON,
  fetchJSONWithBody,
  MaterializeStreamPayload,
} from "@/lib/api-core";
import { streamMaterialization } from "@/lib/api-streams";
import {
  PipelineConfigResponse,
  PipelineMaterializationResponse,
  UpdatePipelineConfigRequest,
} from "@/lib/types";

export async function createPipeline(input: {
  path: string;
  name?: string;
  content?: string;
}) {
  return fetchJSONWithBody<Record<string, string>>(
    "/api/pipelines",
    "POST",
    input
  );
}

export async function deletePipeline(pipelineId: string) {
  return fetchJSON<Record<string, string>>(`/api/pipelines/${pipelineId}`, {
    method: "DELETE",
  });
}

export async function updatePipeline(input: {
  id: string;
  name?: string;
  content?: string;
}) {
  return fetchJSONWithBody<Record<string, string>>(
    "/api/pipelines",
    "PUT",
    input
  );
}

export async function getPipelineConfig(pipelineId: string) {
	return fetchJSON<PipelineConfigResponse>(`/api/pipelines/${pipelineId}/config`, {
		method: "GET",
		cache: "no-store",
	});
}

export async function updatePipelineConfig(
	pipelineId: string,
	input: UpdatePipelineConfigRequest
) {
	return fetchJSONWithBody<PipelineConfigResponse>(
		`/api/pipelines/${pipelineId}/config`,
		"PUT",
		input
	);
}

export async function materializePipelineStream(
  pipelineId: string,
  handlers: {
    onChunk?: (chunk: string) => void;
    onDone?: (payload: MaterializeStreamPayload) => void;
  },
  options?: { environment?: string }
) {
  return streamMaterialization(
    `/api/pipelines/${pipelineId}/materialize/stream${buildQueryString({
      environment: options?.environment,
    })}`,
    handlers,
    "Pipeline materialization stream ended unexpectedly."
  );
}

export async function getPipelineMaterialization(
  pipelineId: string,
  options?: { environment?: string }
) {
  return fetchJSON<PipelineMaterializationResponse>(
    `/api/pipelines/${pipelineId}/materialization${buildQueryString({
      environment: options?.environment,
    })}`,
    {
      method: "GET",
      cache: "no-store",
    }
  );
}
