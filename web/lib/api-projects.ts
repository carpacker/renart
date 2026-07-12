import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import type {
  BrowseDirsResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  OpenProjectResponse,
  ProjectListResponse,
  ProjectTemplatesResponse,
} from "@/lib/generated/api-types";

// These endpoints are process-level (the project directory itself), so they
// are never rewritten onto a project mount by projectApiPath.

export async function listProjects(): Promise<ProjectListResponse> {
  return fetchJSON<ProjectListResponse>("/api/projects", { cache: "no-store" });
}

export async function openProject(path: string): Promise<OpenProjectResponse> {
  return fetchJSONWithBody<OpenProjectResponse>("/api/projects/open", "POST", { path });
}

export async function getProjectTemplates(): Promise<ProjectTemplatesResponse> {
  return fetchJSON<ProjectTemplatesResponse>("/api/projects/templates", { cache: "no-store" });
}

export async function createProject(input: CreateProjectRequest): Promise<CreateProjectResponse> {
  return fetchJSONWithBody<CreateProjectResponse>("/api/projects", "POST", input);
}

export async function removeProject(id: string): Promise<ProjectListResponse> {
  return fetchJSONWithBody<ProjectListResponse>(
    `/api/projects/${encodeURIComponent(id)}`,
    "DELETE",
  );
}

export async function browseProjectDirs(path?: string): Promise<BrowseDirsResponse> {
  const query = path ? `?path=${encodeURIComponent(path)}` : "";
  return fetchJSON<BrowseDirsResponse>(`/api/projects/browse${query}`, { cache: "no-store" });
}
