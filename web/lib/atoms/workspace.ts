import { atom } from "jotai";

import { WorkspaceState } from "@/lib/types";

export type WorkspaceSyncMethod = "workspace-load" | "workspace-event";

export type WorkspaceSyncSource = {
  method: WorkspaceSyncMethod;
  recordedAt: string;
  revision?: number;
  eventType?: string;
  eventPath?: string;
  lite?: boolean;
  changedAssetIds?: string[];
};

export const workspaceAtom = atom<WorkspaceState | null>(null);
export const workspaceSyncSourceAtom = atom<WorkspaceSyncSource | null>(null);
export const selectedEnvironmentAtom = atom<string | undefined>((get) =>
  get(workspaceAtom)?.selected_environment || undefined
);