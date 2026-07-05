import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import type {
  SourceControlBranchesResponse,
  SourceControlActionResponse,
  SourceControlCommitResponse,
  SourceControlDiffResponse,
  SourceControlStatusResponse,
} from "@/lib/types";

export async function getSourceControlStatus() {
  return fetchJSON<SourceControlStatusResponse>("/api/source-control/status");
}

export async function getSourceControlBranches() {
  return fetchJSON<SourceControlBranchesResponse>("/api/source-control/branches");
}

export async function getSourceControlDiff(path: string, staged: boolean) {
  const params = new URLSearchParams({ path, staged: String(staged) });
  return fetchJSON<SourceControlDiffResponse>(`/api/source-control/diff?${params.toString()}`);
}

export async function initSourceControlRepository() {
  return fetchJSONWithBody<SourceControlActionResponse>("/api/source-control/init", "POST", {});
}

export async function stageSourceControlPaths(paths: string[]) {
  return fetchJSONWithBody<SourceControlActionResponse>("/api/source-control/stage", "POST", { paths });
}

export async function unstageSourceControlPaths(paths: string[]) {
  return fetchJSONWithBody<SourceControlActionResponse>("/api/source-control/unstage", "POST", { paths });
}

export async function checkoutSourceControlBranch(branch: string) {
  return fetchJSONWithBody<SourceControlActionResponse>("/api/source-control/checkout", "POST", { branch });
}

export async function commitSourceControlChanges(message: string) {
  return fetchJSONWithBody<SourceControlCommitResponse>("/api/source-control/commit", "POST", { message });
}
