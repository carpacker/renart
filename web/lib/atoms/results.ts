import { atom } from "jotai";

export type AssetResultTab = "inspect" | "materialize";

export type MaterializeHistoryEntry = {
  id: string;
  kind: "asset" | "pipeline" | "batch";
  label: string;
  assetId?: string | null;
  assetName?: string | null;
  pipelineId?: string | null;
  pipelineName?: string | null;
  output: string;
  status: "ok" | "error" | null;
  error: string;
  loading: boolean;
  createdAt: number;
  updatedAt: number;
};

export type AssetResultsState = {
  resultTab: AssetResultTab;
  selectedMaterializeEntryId: string | null;
  materializeHistory: MaterializeHistoryEntry[];
};

export const assetResultsAtom = atom<AssetResultsState>({
  resultTab: "inspect",
  selectedMaterializeEntryId: null,
  materializeHistory: [],
});

export const changedAssetIdsAtom = atom<Set<string>>(new Set<string>());
