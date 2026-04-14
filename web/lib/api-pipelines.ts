import {
  fetchJSON,
  fetchJSONWithBody,
  MaterializeStreamPayload,
} from "@/lib/api-core";
import { streamMaterialization } from "@/lib/api-streams";
import { PipelineMaterializationResponse } from "@/lib/types";

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

export async function materializePipelineStream(
  pipelineId: string,
  handlers: {
    onChunk?: (chunk: string) => void;
    onDone?: (payload: MaterializeStreamPayload) => void;
  }
) {
  return streamMaterialization(
    `/api/pipelines/${pipelineId}/materialize/stream`,
    handlers,
    "Pipeline materialization stream ended unexpectedly."
  );
}

export async function getPipelineMaterialization(pipelineId: string) {
  return fetchJSON<PipelineMaterializationResponse>(
    `/api/pipelines/${pipelineId}/materialization`,
    {
      method: "GET",
      cache: "no-store",
    }
  );
}
