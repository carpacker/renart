import { fetchJSON } from "@/lib/api-core";
import { WorkspaceState } from "@/lib/types";

export async function getWorkspace(): Promise<WorkspaceState> {
  return fetchJSON<WorkspaceState>("/api/workspace", { cache: "no-store" });
}
