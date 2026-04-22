import { atom } from "jotai";

import {
  getConfiguredConnectionTypes,
  getPreferredSqlAssetType,
} from "@/lib/asset-types";
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
export const selectedEnvironmentOverrideAtom = atom<string | undefined>(undefined);
export const selectedEnvironmentAtom = atom<string | undefined>((get) =>
	get(selectedEnvironmentOverrideAtom) || get(workspaceAtom)?.selected_environment || undefined
);

export const configuredConnectionTypesAtom = atom<Set<string>>((get) =>
  getConfiguredConnectionTypes(get(workspaceAtom)?.connections)
);

export const preferredSqlAssetTypeAtom = atom<string>((get) =>
  getPreferredSqlAssetType(get(workspaceAtom)?.connections)
);
