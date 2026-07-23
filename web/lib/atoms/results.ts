import { atom } from "jotai";

import type { StalenessUpdatedEvent } from "@/lib/api-staleness";
import type { NotebookRuntimeEvent } from "@/lib/api-notebooks";
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
  | {
      type: "run.queued" | "run.started" | "run.finished" | "run.cancellation_requested";
      run: PipelineRun;
    }
  | { type: "run.log"; run: { run_id: string; log: PipelineRunLogLine } }
  | { type: "run.step"; run: PipelineRunStep }
  | { type: "run.unit"; run: { run_id: string; unit: PipelineRunUnit } };

export type ScheduleOccurrenceEvent = {
  type: "schedule.occurrence";
  pipeline_uuid: string;
  environment: string;
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

export const schedulerRunEventAtom = atom<SchedulerRunEvent | null>(null);

export const scheduleOccurrenceEventAtom = atom<ScheduleOccurrenceEvent | null>(null);

export const stalenessEventAtom = atom<StalenessUpdatedEvent | null>(null);

export type NotebookRuntimeEvents = Record<string, NotebookRuntimeEvent>;

export function mergeNotebookRuntimeEvent(
  current: NotebookRuntimeEvents,
  event: NotebookRuntimeEvent,
): NotebookRuntimeEvents {
  const previous = current[event.notebook_id];
  return {
    ...current,
    [event.notebook_id]: {
      ...event,
      // Runtime events carry result deltas. Keep the accumulated snapshot so a
      // following state-only event cannot erase a result before React observes
      // it (several recompute events can arrive in one browser task).
      results: {
        ...(previous?.results ?? {}),
        ...(event.results ?? {}),
      },
    },
  };
}

export const notebookRuntimeEventsAtom = atom<NotebookRuntimeEvents>({});
