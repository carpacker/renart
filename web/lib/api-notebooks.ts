import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import { WebNotebook, WebNotebookBlock } from "@/lib/types";

export type NotebookImportRecord = {
  ref: string;
  object_name: string;
  imported_at: string;
  row_count: number;
  complete: boolean;
};

export type VizKind = "table" | "bar" | "line" | "area" | "pie" | "kpi";

export type VizDirective = {
  kind: VizKind;
  options: Record<string, string | number | boolean | string[]>;
};

export type VizDiagnostic = {
  message: string;
  severity: "error" | "warning";
  line: number;
  col: number;
  end_col: number;
};

export type NotebookCellRunResult = {
  cell_id: string;
  name: string;
  object_name: string;
  status: "ok" | "error" | "blocked";
  error?: string;
  columns: string[];
  rows: unknown[][];
  total_rows: number;
  materialized: "view" | "table";
  imports?: NotebookImportRecord[];
  rewritten_sql?: string;
  logs?: string;
  duration_ms: number;
  viz?: VizDirective | null;
  viz_diagnostics?: VizDiagnostic[];
};

export type RunNotebookResponse = {
  status: "ok" | "error" | "cancelled";
  results: NotebookCellRunResult[];
};

// The server's auto-recompute state for a notebook: which cells are stale,
// which of those it will refresh on its own, and the last result per cell.
export type NotebookRuntimeSnapshot = {
  auto_recompute: boolean;
  stale: string[];
  auto_pending: string[];
  results: Record<string, NotebookCellRunResult>;
};

// Pushed on the SSE stream when a notebook's recompute state changes.
export type NotebookRuntimeEvent = {
  type: "notebook.runtime";
  notebook_id: string;
  auto_recompute: boolean;
  stale: string[];
  auto_pending: string[];
  running: string[];
  results?: Record<string, NotebookCellRunResult>;
};

export async function getNotebookRuntime(notebookId: string) {
  return fetchJSON<NotebookRuntimeSnapshot>(`/api/notebooks/${notebookId}/runtime`, {
    cache: "no-store",
  });
}

export async function setNotebookSettings(
  notebookId: string,
  input: { auto_recompute: boolean; environment?: string },
) {
  return fetchJSONWithBody<{ status: string }>(
    `/api/notebooks/${notebookId}/settings`,
    "PUT",
    input,
  );
}

export async function cancelNotebookRun(notebookId: string) {
  return fetchJSONWithBody<{ status: string }>(`/api/notebooks/${notebookId}/cancel`, "POST", {});
}

type NotebookEnvelope = {
  status: "ok" | "error";
  notebook: WebNotebook;
};

export async function getNotebook(notebookId: string) {
  const payload = await fetchJSON<NotebookEnvelope>(`/api/notebooks/${notebookId}`, {
    cache: "no-store",
  });
  return payload.notebook;
}

export async function createNotebook(input: { title: string; path?: string }) {
  const payload = await fetchJSONWithBody<NotebookEnvelope>("/api/notebooks", "POST", input);
  return payload.notebook;
}

export async function deleteNotebook(notebookId: string) {
  return fetchJSON<{ status: string }>(`/api/notebooks/${notebookId}`, { method: "DELETE" });
}

export async function closeNotebookSession(notebookId: string) {
  return fetchJSON<{ status: string }>(`/api/notebooks/${notebookId}/session`, {
    method: "DELETE",
  });
}

export async function createNotebookCell(
  notebookId: string,
  input: { name?: string; language?: "sql" | "python" } = {},
) {
  const payload = await fetchJSONWithBody<NotebookEnvelope>(
    `/api/notebooks/${notebookId}/cells`,
    "POST",
    input,
  );
  return payload.notebook;
}

export async function updateNotebookCell(notebookId: string, cellId: string, content: string) {
  const payload = await fetchJSONWithBody<NotebookEnvelope>(
    `/api/notebooks/${notebookId}/cells/${cellId}`,
    "PUT",
    { content },
  );
  return payload.notebook;
}

export async function renameNotebookCell(notebookId: string, cellId: string, name: string) {
  const payload = await fetchJSONWithBody<NotebookEnvelope>(
    `/api/notebooks/${notebookId}/cells/${cellId}/rename`,
    "POST",
    { name },
  );
  return payload.notebook;
}

export async function deleteNotebookCell(notebookId: string, cellId: string) {
  const payload = await fetchJSON<NotebookEnvelope>(
    `/api/notebooks/${notebookId}/cells/${cellId}`,
    {
      method: "DELETE",
    },
  );
  return payload.notebook;
}

export async function updateNotebookBlocks(notebookId: string, blocks: WebNotebookBlock[]) {
  const payload = await fetchJSONWithBody<NotebookEnvelope>(
    `/api/notebooks/${notebookId}/blocks`,
    "PUT",
    { blocks },
  );
  return payload.notebook;
}

export async function updateNotebookDependencies(notebookId: string, dependencies: string[]) {
  const payload = await fetchJSONWithBody<NotebookEnvelope>(
    `/api/notebooks/${notebookId}/dependencies`,
    "PUT",
    { content: dependencies.join("\n") },
  );
  return payload.notebook;
}

export type PromoteCellResponse = {
  status: "ok" | "error";
  asset_path: string;
  asset_paths?: string[];
  promoted_count: number;
  dialect_warning?: string;
  notebook: WebNotebook;
};

export async function promoteNotebookCell(
  notebookId: string,
  cellId: string,
  input: {
    pipeline_id: string;
    target_name: string;
    include_upstream?: boolean;
    include_downstream?: boolean;
  },
) {
  return fetchJSONWithBody<PromoteCellResponse>(
    `/api/notebooks/${notebookId}/cells/${cellId}/promote`,
    "POST",
    input,
  );
}

export async function runNotebook(
  notebookId: string,
  input: {
    all?: boolean;
    from?: string;
    cells?: string[];
    refresh_imports?: boolean;
    environment?: string;
    start_date?: string;
    end_date?: string;
  },
  signal?: AbortSignal,
) {
  return fetchJSONWithBody<RunNotebookResponse>(`/api/notebooks/${notebookId}/run`, "POST", input, {
    signal,
  });
}

/**
 * Splits a cell file into its Bruin frontmatter header and the SQL body, so
 * the editor can show just the query while saves preserve the header.
 */
export function splitCellContent(content: string): { header: string; body: string } {
  const lines = content.split("\n");
  let opener = -1;
  let closer = -1;
  for (let index = 0; index < lines.length; index += 1) {
    if (!lines[index].includes("@bruin")) {
      continue;
    }
    if (opener === -1) {
      opener = index;
      continue;
    }
    closer = index;
    break;
  }
  if (opener === -1 || closer === -1) {
    return { header: "", body: content };
  }

  const header = lines.slice(0, closer + 1).join("\n");
  let bodyStart = closer + 1;
  while (bodyStart < lines.length && lines[bodyStart].trim() === "") {
    bodyStart += 1;
  }
  return { header, body: lines.slice(bodyStart).join("\n") };
}

/** Reassembles a cell file from its header and edited body. */
export function joinCellContent(header: string, body: string): string {
  if (!header) {
    return body;
  }
  return `${header}\n\n${body.replace(/\s+$/, "")}\n`;
}
