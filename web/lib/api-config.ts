import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import { WorkspaceConfigResponse } from "@/lib/types";

export async function getWorkspaceConfig(): Promise<WorkspaceConfigResponse> {
  return fetchJSON<WorkspaceConfigResponse>("/api/config", {
    cache: "no-store",
  });
}

export async function createWorkspaceEnvironment(input: {
  name: string;
  schema_prefix?: string;
  set_as_default?: boolean;
}): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/environments",
    "POST",
    input
  );
}

export async function updateWorkspaceEnvironment(input: {
  name: string;
  new_name?: string;
  schema_prefix?: string;
  set_as_default?: boolean;
}): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/environments",
    "PUT",
    input
  );
}

export async function cloneWorkspaceEnvironment(input: {
  source_name: string;
  target_name: string;
  schema_prefix?: string;
  set_as_default?: boolean;
}): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/environments/clone",
    "POST",
    input
  );
}

export async function deleteWorkspaceEnvironment(
  name: string
): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/environments",
    "DELETE",
    { name }
  );
}

export async function createWorkspaceConnection(input: {
  environment_name: string;
  name: string;
  type: string;
  values: Record<string, unknown>;
}): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/connections",
    "POST",
    input
  );
}

export async function updateWorkspaceConnection(input: {
  environment_name: string;
  current_name?: string;
  name: string;
  type: string;
  values: Record<string, unknown>;
}): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/connections",
    "PUT",
    input
  );
}

export async function deleteWorkspaceConnection(input: {
  environment_name: string;
  name: string;
}): Promise<WorkspaceConfigResponse> {
  return fetchJSONWithBody<WorkspaceConfigResponse>(
    "/api/config/connections",
    "DELETE",
    input
  );
}

export async function testWorkspaceConnection(input: {
  environment_name: string;
  current_name?: string;
  name: string;
  type?: string;
  values?: Record<string, unknown>;
}): Promise<{ status: string; message?: string }> {
  return fetchJSONWithBody<{ status: string; message?: string }>(
    "/api/config/connections/test",
    "POST",
    input
  );
}
