import { atom } from "jotai";

import type { StalenessUpdatedEvent } from "@/lib/api-staleness";
import type {
  PipelineRun,
  PipelineRunLogLine,
  PipelineRunStep,
  PipelineRunUnit,
} from "@/lib/types";

export type AssetResultTab = "inspect" | "materialize";

export type MaterializeHistoryEntry = {
  id: string;
  kind: "asset" | "pipeline" | "batch";
  label: string;
  assetId?: string | null;
  assetName?: string | null;
  pipelineId?: string | null;
  pipelineName?: string | null;
  runId?: string | null;
  output: string;
  status: "ok" | "error" | null;
  error: string;
  warnings?: string[];
  loading: boolean;
  createdAt: number;
  updatedAt: number;
  timeWindow?: { start: string; end: string } | null;
};

export type SchedulerRunEvent =
  | { type: "run.queued" | "run.started" | "run.finished"; run: PipelineRun }
  | { type: "run.log"; run: { run_id: string; log: PipelineRunLogLine } }
  | { type: "run.step"; run: PipelineRunStep }
  | { type: "run.unit"; run: { run_id: string; unit: PipelineRunUnit } };

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

export const schedulerRunEventAtom = atom<SchedulerRunEvent | null>(null);

export const stalenessEventAtom = atom<StalenessUpdatedEvent | null>(null);
