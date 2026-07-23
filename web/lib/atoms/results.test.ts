import { describe, expect, it } from "vitest";

import type { NotebookCellRunResult, NotebookRuntimeEvent } from "@/lib/api-notebooks";
import { mergeNotebookRuntimeEvent } from "@/lib/atoms/domains/results";

function result(cellId: string, value: number): NotebookCellRunResult {
  return {
    cell_id: cellId,
    name: cellId,
    object_name: cellId,
    status: "ok",
    columns: ["value"],
    rows: [[value]],
    total_rows: 1,
    materialized: "view",
    duration_ms: 1,
  };
}

function runtimeEvent(overrides: Partial<NotebookRuntimeEvent> = {}): NotebookRuntimeEvent {
  return {
    type: "notebook.runtime",
    notebook_id: "notebook-1",
    auto_recompute: true,
    stale: [],
    auto_pending: [],
    running: [],
    ...overrides,
  };
}

describe("mergeNotebookRuntimeEvent", () => {
  it("retains result deltas when a state-only event follows in the same update batch", () => {
    const withResult = mergeNotebookRuntimeEvent(
      {},
      runtimeEvent({ results: { cell_a: result("cell_a", 222) } }),
    );
    const settled = mergeNotebookRuntimeEvent(withResult, runtimeEvent());

    expect(settled["notebook-1"]?.results?.cell_a.rows).toEqual([[222]]);
  });

  it("replaces a cell result with its newest delta", () => {
    const first = mergeNotebookRuntimeEvent(
      {},
      runtimeEvent({ results: { cell_a: result("cell_a", 111) } }),
    );
    const second = mergeNotebookRuntimeEvent(
      first,
      runtimeEvent({ results: { cell_a: result("cell_a", 222) } }),
    );

    expect(second["notebook-1"]?.results?.cell_a.rows).toEqual([[222]]);
  });
});
