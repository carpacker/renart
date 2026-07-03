"use client";

import { useNavigate } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import {
  AlertTriangle,
  AreaChart,
  ArrowUpFromLine,
  BarChart3,
  BookOpen,
  Check,
  ChevronRight,
  Database,
  Hash,
  LineChart,
  Loader2,
  MoreHorizontal,
  Package,
  Pencil,
  PieChart,
  Play,
  Plus,
  RotateCw,
  Square,
  Table2,
  Trash2,
} from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import {
  cancelNotebookRun,
  closeNotebookSession,
  createNotebook,
  createNotebookCell,
  deleteNotebook,
  deleteNotebookCell,
  getNotebook,
  getNotebookRuntime,
  joinCellContent,
  NotebookCellRunResult,
  NotebookRuntimeEvent,
  promoteNotebookCell,
  renameNotebookCell,
  runNotebook,
  setNotebookSettings,
  splitCellContent,
  updateNotebookBlocks,
  updateNotebookCell,
  updateNotebookDependencies,
  VizKind,
} from "@/lib/api-notebooks";
import { selectedEnvironmentAtom, selectedExecutionTimeWindowAtom, workspaceAtom } from "@/lib/atoms/domains/workspace";
import { addDependency, missingPythonImports } from "@/lib/notebook-python-deps";
import { WebAsset, WebNotebook, WebNotebookBlock } from "@/lib/types";
import { cn } from "@/lib/utils";

import { MissingPythonDepsBanner } from "./missing-python-deps";
import { buildNotebookSchemaTables, NotebookCellMonaco } from "./notebook-cell-editor";
import { applyVizKind } from "./notebook-viz-directive";
import { NotebookVizRenderer } from "./notebook-viz";
import { PageHeader, RedesignPage, RedesignPanel, SimpleTable } from "./redesign-primitives";

const VIZ_KIND_ICONS: Record<VizKind, typeof Table2> = {
  table: Table2,
  bar: BarChart3,
  line: LineChart,
  area: AreaChart,
  pie: PieChart,
  kpi: Hash,
};

const ReactMarkdown = lazy(() => import("react-markdown"));

const RESULT_DISPLAY_CAP = 50;
// How long to wait after the last keystroke before auto-committing a cell's
// draft. The save marks the cell stale on the server, which drives recompute.
const AUTO_COMMIT_DEBOUNCE_MS = 350;

export function RedesignNotebooksIndexPage() {
  const workspace = useAtomValue(workspaceAtom);
  const navigate = useNavigate();
  const notebooks = workspace?.notebooks ?? [];
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  const handleCreate = async () => {
    const title = window.prompt("New notebook title", "Exploration");
    if (title === null) {
      return;
    }
    setCreating(true);
    setError("");
    try {
      const created = await createNotebook({ title: title.trim() || "Untitled" });
      void navigate({ to: "/redesign/notebooks/$notebookId", params: { notebookId: created.id } });
    } catch (createError) {
      setError(String(createError));
    } finally {
      setCreating(false);
    }
  };

  return (
    <RedesignPage>
      <PageHeader
        title="Notebooks"
        subtitle="Exploratory SQL against a local DuckDB session — promote cells to pipelines when ready"
        actions={(
          <Button size="sm" disabled={creating} onClick={() => void handleCreate()}>
            {creating ? <Spinner className="size-3.5" /> : <Plus className="size-3.5" />}New notebook
          </Button>
        )}
      />
      {error ? (
        <div className="mx-3 mb-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-200">{error}</div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-auto px-3 pb-3">
        {notebooks.length === 0 ? (
          <div className="mx-auto mt-12 max-w-md rounded-xl border border-dashed p-8 text-center">
            <BookOpen className="mx-auto mb-3 size-8 text-muted-foreground" />
            <div className="text-sm font-medium">No notebooks yet</div>
            <p className="mt-1 text-xs text-muted-foreground">
              Notebooks are folders of SQL cells that run in a disposable local DuckDB session.
            </p>
            <Button size="sm" className="mt-4" disabled={creating} onClick={() => void handleCreate()}>
              <Plus className="size-3.5" />New notebook
            </Button>
          </div>
        ) : (
          <div className="mx-auto grid max-w-5xl gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {notebooks.map((notebook) => (
              <button
                key={notebook.id}
                type="button"
                onClick={() => void navigate({ to: "/redesign/notebooks/$notebookId", params: { notebookId: notebook.id } })}
                className="flex flex-col gap-2 rounded-xl border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-muted/40"
              >
                <div className="flex items-center gap-2">
                  <BookOpen className="size-4 text-primary" />
                  <span className="min-w-0 flex-1 truncate font-medium">{notebook.title}</span>
                </div>
                <div className="font-mono text-[11px] text-muted-foreground">{notebook.path}</div>
                <div className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
                  <span>{notebook.cells.length} cell{notebook.cells.length === 1 ? "" : "s"}</span>
                  {notebook.problems?.length ? (
                    <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400">
                      <AlertTriangle className="size-3" />{notebook.problems.length}
                    </span>
                  ) : null}
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </RedesignPage>
  );
}

export function RedesignNotebookLivePage({ notebookId }: { notebookId: string }) {
  const workspace = useAtomValue(workspaceAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const selectedExecutionTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const navigate = useNavigate();

  const stateNotebook = useMemo(
    () => workspace?.notebooks?.find((candidate) => candidate.id === notebookId) ?? null,
    [notebookId, workspace?.notebooks]
  );
  // Mutations return the fresh notebook before the SSE state catches up;
  // prefer the newer of the two.
  const [mutated, setMutated] = useState<WebNotebook | null>(null);
  const [loadError, setLoadError] = useState("");
  const notebook = mutated ?? stateNotebook;

  useEffect(() => {
    setMutated(null);
  }, [stateNotebook]);

  useEffect(() => {
    if (stateNotebook || mutated) {
      return;
    }
    let cancelled = false;
    getNotebook(notebookId)
      .then((loaded) => {
        if (!cancelled) {
          setMutated(loaded);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setLoadError(String(error));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [mutated, notebookId, stateNotebook]);

  // Staleness, results, the running set, and which stale cells will auto-update
  // are all owned by the server now; the client renders what the runtime SSE
  // stream (and the initial snapshot) report. See
  // architecture/notebooks.md.
  const [results, setResults] = useState<Record<string, NotebookCellRunResult>>({});
  const [staleCells, setStaleCells] = useState<Set<string>>(new Set());
  const [autoPending, setAutoPending] = useState<Set<string>>(new Set());
  const [runningCells, setRunningCells] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [notebookScrolled, setNotebookScrolled] = useState(false);
  const [depsOpen, setDepsOpen] = useState(false);
  const [promoting, setPromoting] = useState<WebAsset | null>(null);
  const [autoRecompute, setAutoRecompute] = useState(
    () => typeof window === "undefined" || window.localStorage.getItem("renart-notebook-autorecompute") !== "off"
  );
  useEffect(() => {
    window.localStorage.setItem("renart-notebook-autorecompute", autoRecompute ? "on" : "off");
  }, [autoRecompute]);

  // Mirror the toggle (and import environment) to the server, which owns the
  // recompute loop. Runs on load and whenever either changes.
  useEffect(() => {
    void setNotebookSettings(notebookId, {
      auto_recompute: autoRecompute,
      environment: selectedEnvironment,
    }).catch(() => undefined);
  }, [notebookId, autoRecompute, selectedEnvironment]);

  useEffect(() => {
    setNotebookScrolled(false);
  }, [notebookId]);

  const cellsById = useMemo(() => {
    const map = new Map<string, WebAsset>();
    for (const cell of notebook?.cells ?? []) {
      if (cell.cell_id) {
        map.set(cell.cell_id, cell);
      }
    }
    return map;
  }, [notebook?.cells]);

  // name → downstream cell ids, for client-side stale propagation.
  // Merge run results into the local map for immediate feedback after a manual
  // run. Staleness is reconciled by the runtime SSE stream, not here.
  const applyResults = useCallback((runResults: NotebookCellRunResult[]) => {
    setResults((current) => {
      const next = { ...current };
      for (const result of runResults) {
        next[result.cell_id] = result;
      }
      return next;
    });
  }, []);

  // Apply a runtime snapshot/event from the server: the authoritative stale,
  // auto-pending, and running sets, plus any result deltas.
  const applyRuntime = useCallback(
    (runtime: { stale: string[]; auto_pending: string[]; running?: string[]; results?: Record<string, NotebookCellRunResult> }) => {
      setStaleCells(new Set(runtime.stale));
      setAutoPending(new Set(runtime.auto_pending));
      if (runtime.running) {
        setRunningCells(new Set(runtime.running));
      }
      if (runtime.results && Object.keys(runtime.results).length > 0) {
        setResults((current) => ({ ...current, ...runtime.results }));
      }
    },
    []
  );

  // In-flight cell saves, keyed by cell id. A run must wait for these to land
  // on disk first: otherwise the backend reloads the cell before the save's
  // write completes and runs stale SQL (the "run twice for @viz" bug).
  const pendingSavesRef = useRef<Map<string, Promise<void>>>(new Map());
  const saveSeqRef = useRef<Map<string, number>>(new Map());

  const saveCellBody = useCallback(
    (cell: WebAsset, body: string): Promise<void> => {
      const { header } = splitCellContent(cell.content);
      const cellId = cell.cell_id ?? "";
      const seq = (saveSeqRef.current.get(cellId) ?? 0) + 1;
      saveSeqRef.current.set(cellId, seq);
      const promise = (async () => {
        try {
          // Saving marks the cell + descendants stale on the server, which then
          // drives auto-recompute and pushes the new state over SSE.
          const updated = await updateNotebookCell(notebookId, cellId, joinCellContent(header, body));
          setMutated(updated);
        } catch (error) {
          setActionError(String(error));
        } finally {
          // Only the most recent save for this cell clears the pending slot,
          // so a slower earlier save cannot drop a newer one.
          if (saveSeqRef.current.get(cellId) === seq) {
            pendingSavesRef.current.delete(cellId);
          }
        }
      })();
      pendingSavesRef.current.set(cellId, promise);
      return promise;
    },
    [notebookId]
  );

  const flushPendingSaves = useCallback(async () => {
    const pending = [...pendingSavesRef.current.values()];
    if (pending.length > 0) {
      await Promise.allSettled(pending);
    }
  }, []);

  // The in-flight run's abort handle, so the user can stop a slow cell. Aborting
  // disconnects the request, which cancels the server's context and interrupts
  // the running DuckDB statement.
  const runAbortRef = useRef<AbortController | null>(null);
  const runRequest = useCallback(
    async (input: { all?: boolean; from?: string; cells?: string[]; refresh_imports?: boolean }, targetIds: string[]) => {
      const controller = new AbortController();
      runAbortRef.current = controller;
      setBusy(true);
      setActionError("");
      setRunningCells(new Set(targetIds));
      try {
        // Make sure any unsaved cell edits have landed before the backend
        // reloads the notebook, so the run sees the latest SQL and directives.
        await flushPendingSaves();
        const response = await runNotebook(notebookId, {
          ...input,
          environment: selectedEnvironment,
          // Render Jinja against the same execution window the editor previews.
          start_date: selectedExecutionTimeWindow?.start,
          end_date: selectedExecutionTimeWindow?.end,
        }, controller.signal);
        applyResults(response.results);
      } catch (error) {
        if (!controller.signal.aborted) {
          setActionError(String(error));
        }
        // On abort the server parks the cells (via the cancel call below); the
        // runtime SSE stream reconciles staleness, so nothing to do here.
      } finally {
        if (runAbortRef.current === controller) {
          runAbortRef.current = null;
        }
        setBusy(false);
        setRunningCells(new Set());
      }
    },
    [applyResults, flushPendingSaves, notebookId, selectedEnvironment, selectedExecutionTimeWindow]
  );

  // Stop both a manual run (abort the request → cancels the server context and
  // interrupts the DuckDB statement) and any server-side auto-recompute pass.
  const cancelRun = useCallback(() => {
    runAbortRef.current?.abort();
    void cancelNotebookRun(notebookId).catch(() => undefined);
  }, [notebookId]);

  const allCellIds = useMemo(
    () => (notebook?.cells ?? []).map((cell) => cell.cell_id ?? "").filter(Boolean),
    [notebook?.cells]
  );

  // Seed from the server's current runtime, then follow it live: the server
  // owns staleness, the running set, auto-pending, and results, pushing changes
  // as notebook.runtime events on the workspace SSE stream.
  useEffect(() => {
    let cancelled = false;
    getNotebookRuntime(notebookId)
      .then((snapshot) => {
        if (!cancelled) {
          applyRuntime(snapshot);
        }
      })
      .catch(() => undefined);

    const source = new EventSource("/api/events");
    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as Partial<NotebookRuntimeEvent>;
        if (payload?.type === "notebook.runtime" && payload.notebook_id === notebookId) {
          applyRuntime(payload as NotebookRuntimeEvent);
        }
      } catch {
        // ignore malformed events
      }
    };
    return () => {
      cancelled = true;
      source.close();
    };
  }, [notebookId, applyRuntime]);

  // Each cell's last successful run columns, so a cell that reads from a sibling
  // gets that sibling's real output columns for intellisense and parse-context.
  const resultColumnsByCell = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const [cellId, result] of Object.entries(results)) {
      if (result?.status === "ok" && result.columns.length > 0) {
        map.set(cellId, result.columns);
      }
    }
    return map;
  }, [results]);

  const mutate = useCallback(async (operation: () => Promise<WebNotebook>) => {
    setActionError("");
    try {
      setMutated(await operation());
    } catch (error) {
      setActionError(String(error));
    }
  }, []);

  const dependencies = useMemo(() => notebook?.dependencies ?? [], [notebook?.dependencies]);
  const installedModules = useMemo(
    () => notebook?.installed_modules ?? [],
    [notebook?.installed_modules]
  );
  const hasPythonCell = useMemo(
    () =>
      (notebook?.cells ?? []).some(
        (cell) => cell.type?.toLowerCase() === "python" || cell.path.toLowerCase().endsWith(".py")
      ),
    [notebook?.cells]
  );
  const updateDependencies = useCallback(
    (next: string[]) => mutate(() => updateNotebookDependencies(notebookId, next)),
    [mutate, notebookId]
  );

  const pipelines = workspace?.pipelines ?? [];
  const promoteCell = useCallback(
    (cell: WebAsset) => {
      if (pipelines.length === 0) {
        setActionError("No pipeline to promote into; create one first.");
        return;
      }
      setActionError("");
      setPromoting(cell);
    },
    [pipelines]
  );

  const runPromote = useCallback(
    async (
      cell: WebAsset,
      input: { pipeline_id: string; target_name: string; include_upstream: boolean; include_downstream: boolean }
    ) => {
      setActionError("");
      try {
        const response = await promoteNotebookCell(notebookId, cell.cell_id ?? "", input);
        setMutated(response.notebook);
        setPromoting(null);
        if (response.dialect_warning) {
          const where = response.promoted_count > 1 ? `${response.promoted_count} assets` : response.asset_path;
          setActionError(`Promoted ${where}. ${response.dialect_warning}`);
        }
      } catch (error) {
        setActionError(String(error));
      }
    },
    [notebookId]
  );

  if (!notebook) {
    return (
      <RedesignPage>
        <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
          {loadError ? `Failed to load notebook: ${loadError}` : "Loading notebook..."}
        </div>
      </RedesignPage>
    );
  }

  // Only cells the user must act on: those auto-recompute won't refresh on its
  // own. When auto-recompute is off, the server reports no auto-pending cells,
  // so this is every stale cell.
  const manualStaleCells = [...staleCells].filter((id) => !autoPending.has(id));
  const staleCount = manualStaleCells.length;

  return (
    <RedesignPage>
      <div className={cn("relative z-10 shrink-0 transition-shadow", notebookScrolled && "shadow-sm")}>
        <PageHeader
          title={notebook.title}
          subtitle={`Notebook · ${notebook.path} · runs in a local DuckDB session`}
          actions={(
            <div className="flex items-center gap-2">
              {staleCount > 0 ? (
                <div className="flex items-center gap-1">
                  <Badge variant="outline" className="border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-200">
                    <AlertTriangle className="size-3" />
                    {staleCount} stale
                  </Badge>
                  <Button size="sm" variant="outline" aria-label="Recompute" disabled={busy} onClick={() => void runRequest({ cells: manualStaleCells }, manualStaleCells)}>
                    <RotateCw className="size-3.5" /><span className="hidden sm:inline">Recompute</span>
                  </Button>
                </div>
              ) : null}
              {hasPythonCell ? (
                <Button variant="outline" size="sm" aria-label="Dependencies" onClick={() => setDepsOpen(true)}>
                  <Package className="size-3.5" /><span className="hidden sm:inline">Dependencies</span>
                </Button>
              ) : null}
              {busy || runningCells.size > 0 ? (
                <Button size="sm" variant="outline" onClick={cancelRun}>
                  <Square className="size-3.5 fill-current" />Stop
                </Button>
              ) : (
                <Button size="sm" disabled={allCellIds.length === 0} onClick={() => void runRequest({ all: true }, allCellIds)}>
                  <Play className="size-3.5" />Run all
                </Button>
              )}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="icon-sm"><MoreHorizontal className="size-3.5" /></Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-64">
                  <DropdownMenuCheckboxItem
                    checked={autoRecompute}
                    onCheckedChange={(checked) => setAutoRecompute(checked === true)}
                    onSelect={(event) => event.preventDefault()}
                  >
                    Auto-recompute stale cells
                  </DropdownMenuCheckboxItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem disabled={busy} onSelect={() => void runRequest({ all: true, refresh_imports: true }, allCellIds)}>
                    <RotateCw className="size-4" />Run all, refresh imports
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() => {
                      void closeNotebookSession(notebookId).then(() => {
                        setResults({});
                        setStaleCells(new Set(allCellIds));
                      }).catch((error) => setActionError(String(error)));
                    }}
                  >
                    <Database className="size-4" />Reset session (delete local DB)
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() => {
                      if (!window.confirm(`Delete notebook "${notebook.title}" and its files?`)) {
                        return;
                      }
                      void deleteNotebook(notebookId)
                        .then(() => navigate({ to: "/redesign" }))
                        .catch((error) => setActionError(String(error)));
                    }}
                  >
                    <Trash2 className="size-4" />Delete notebook
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )}
        />
      </div>

      <NotebookDependenciesDialog
        open={depsOpen}
        onOpenChange={setDepsOpen}
        dependencies={dependencies}
        onSave={updateDependencies}
      />

      <PromoteCellDialog
        cell={promoting}
        cells={notebook.cells}
        pipelines={pipelines.map((pipeline) => ({ id: pipeline.id, name: pipeline.name }))}
        onOpenChange={(open) => {
          if (!open) {
            setPromoting(null);
          }
        }}
        onPromote={runPromote}
      />

      {notebook.problems?.length ? (
        <div className="mx-3 mb-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-200">
          {notebook.problems.map((problem) => <div key={problem}>{problem}</div>)}
        </div>
      ) : null}
      {actionError ? (
        <div className="mx-3 mb-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-200">{actionError}</div>
      ) : null}
      <ScrollArea
        className="min-h-0 flex-1"
        viewportClassName="px-3 pb-3"
        onViewportScroll={(event) => {
          const nextScrolled = event.currentTarget.scrollTop > 0;
          setNotebookScrolled((current) => current === nextScrolled ? current : nextScrolled);
        }}
      >
        <div className="mx-auto flex max-w-5xl flex-col gap-3">
          {notebook.blocks.map((block, index) =>
            block.cell ? (
              (() => {
                const cell = cellsById.get(block.cell);
                if (!cell) {
                  return null;
                }
                return (
                  <NotebookCellCard
                    key={block.cell}
                    cell={cell}
                    cells={notebook.cells}
                    dependencies={dependencies}
                    installedModules={installedModules}
                    onAddDependency={(pkg) => updateDependencies(addDependency(dependencies, pkg))}
                    resultColumnsByCell={resultColumnsByCell}
                    result={results[block.cell]}
                    stale={staleCells.has(block.cell)}
                    running={runningCells.has(block.cell)}
                    busy={busy}
                    onRun={() => void runRequest({ cells: [block.cell ?? ""] }, [block.cell ?? ""])}
                    onCancel={cancelRun}
                    onRunFromHere={() => void runRequest({ from: block.cell }, [block.cell ?? ""])}
                    onDelete={() => {
                      if (!window.confirm(`Delete cell "${cell.name}"?`)) {
                        return;
                      }
                      void mutate(() => deleteNotebookCell(notebookId, block.cell ?? ""));
                    }}
                    onRename={(name) => mutate(() => renameNotebookCell(notebookId, block.cell ?? "", name))}
                    onPromote={() => void promoteCell(cell)}
                    onSaveBody={(body) => saveCellBody(cell, body)}
                    autoCommit={autoRecompute}
                    pendingAuto={autoPending.has(block.cell ?? "")}
                  />
                );
              })()
            ) : (
              <MarkdownBlockCard
                key={`md-${index}`}
                markdown={block.markdown ?? ""}
                onSave={(markdown) => {
                  const blocks: WebNotebookBlock[] = notebook.blocks.map((candidate, candidateIndex) =>
                    candidateIndex === index ? { markdown } : candidate
                  );
                  void mutate(() => updateNotebookBlocks(notebookId, blocks));
                }}
                onDelete={() => {
                  const blocks = notebook.blocks.filter((_, candidateIndex) => candidateIndex !== index);
                  void mutate(() => updateNotebookBlocks(notebookId, blocks));
                }}
              />
            )
          )}

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => void mutate(() => createNotebookCell(notebookId))}>
              <Plus className="size-3.5" />SQL cell
            </Button>
            <Button variant="outline" size="sm" onClick={() => void mutate(() => createNotebookCell(notebookId, { language: "python" }))}>
              <Plus className="size-3.5" />Python cell
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                const blocks: WebNotebookBlock[] = [...notebook.blocks, { markdown: "## Notes" }];
                void mutate(() => updateNotebookBlocks(notebookId, blocks));
              }}
            >
              <Plus className="size-3.5" />Markdown
            </Button>
          </div>
        </div>
      </ScrollArea>
    </RedesignPage>
  );
}

function statusDotClass(result: NotebookCellRunResult | undefined, stale: boolean) {
  if (result?.status === "error") return "bg-red-500";
  if (result?.status === "blocked") return "bg-amber-500";
  if (stale) return "bg-amber-400";
  if (result?.status === "ok") return "bg-emerald-500";
  return "bg-muted-foreground/40";
}

function NotebookCellCard({
  cell,
  cells,
  dependencies,
  installedModules,
  onAddDependency,
  resultColumnsByCell,
  result,
  stale,
  running,
  busy,
  onRun,
  onCancel,
  onRunFromHere,
  onDelete,
  onRename,
  onPromote,
  onSaveBody,
  autoCommit,
  pendingAuto,
}: {
  cell: WebAsset;
  cells: WebAsset[];
  dependencies: string[];
  installedModules: string[];
  onAddDependency: (pkg: string) => void;
  resultColumnsByCell: Map<string, string[]>;
  result?: NotebookCellRunResult;
  stale: boolean;
  running: boolean;
  busy: boolean;
  onRun: () => void;
  onCancel: () => void;
  onRunFromHere: () => void;
  onDelete: () => void;
  onRename: (name: string) => Promise<void>;
  onPromote: () => void;
  onSaveBody: (body: string) => Promise<void>;
  /** Save the draft on a typing debounce (drives auto-recompute without a blur). */
  autoCommit: boolean;
  /** Stale, but auto-recompute will refresh it on its own — don't flag it stale. */
  pendingAuto: boolean;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const schemaTables = useMemo(
    () => buildNotebookSchemaTables(workspace, cells, cell, resultColumnsByCell),
    [workspace, cells, cell, resultColumnsByCell]
  );
  const isPythonCell =
    cell.type?.toLowerCase() === "python" || cell.path.toLowerCase().endsWith(".py");
  const { body } = useMemo(() => splitCellContent(cell.content), [cell.content]);
  const [draft, setDraft] = useState(body);

  const missingDeps = useMemo(
    () => (isPythonCell ? missingPythonImports(draft, dependencies, installedModules) : []),
    [isPythonCell, draft, dependencies, installedModules]
  );
  const lastSavedRef = useRef(body);
  const savingBodyRef = useRef<string | null>(null);
  useEffect(() => {
    // Adopt the incoming body, but never clobber unsaved local edits: with
    // auto-commit the cell saves mid-typing, and the save's echo (or any other
    // refresh) must not overwrite characters the user typed while it was in
    // flight. Only reset when the draft matches what we last persisted or the
    // incoming server body has caught up to the current draft.
    setDraft((current) => {
      if (savingBodyRef.current === body) {
        savingBodyRef.current = null;
      }
      if (current !== lastSavedRef.current && current !== body) {
        return current;
      }
      lastSavedRef.current = body;
      return body;
    });
  }, [body]);

  const [renaming, setRenaming] = useState(false);
  const [nameDraft, setNameDraft] = useState(cell.name);
  useEffect(() => {
    setNameDraft(cell.name);
  }, [cell.name]);

  const commitRename = () => {
    const trimmed = nameDraft.trim();
    setRenaming(false);
    if (trimmed && trimmed !== cell.name) {
      void onRename(trimmed);
    } else {
      setNameDraft(cell.name);
    }
  };

  const commit = () => {
    if (draft !== lastSavedRef.current && draft !== savingBodyRef.current) {
      savingBodyRef.current = draft;
      void onSaveBody(draft).finally(() => {
        if (savingBodyRef.current === draft) {
          savingBodyRef.current = null;
        }
      });
    }
  };

  // Auto-commit while typing: when enabled, persist the draft a beat after the
  // user pauses, so staleness and auto-recompute kick in without needing a blur.
  // Debounced on the draft alone (via a ref for the save callback) so unrelated
  // re-renders can't keep resetting the timer and starving the save. A blur
  // still commits immediately; broken in-progress SQL stays put because
  // auto-recompute only runs cells the parser reports as clean.
  const onSaveBodyRef = useRef(onSaveBody);
  onSaveBodyRef.current = onSaveBody;
  useEffect(() => {
    if (!autoCommit || draft === lastSavedRef.current || draft === savingBodyRef.current) {
      return;
    }
    const timer = window.setTimeout(() => {
      if (draft !== lastSavedRef.current && draft !== savingBodyRef.current) {
        savingBodyRef.current = draft;
        void onSaveBodyRef.current(draft).finally(() => {
          if (savingBodyRef.current === draft) {
            savingBodyRef.current = null;
          }
        });
      }
    }, AUTO_COMMIT_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [autoCommit, draft]);

  // Chart type is a view over the @viz directive line: changing it rewrites
  // the cell body (text stays the source of truth, invariant 3).
  const setChartKind = (kind: VizKind) => {
    const next = applyVizKind(draft, kind, result?.columns ?? []);
    setDraft(next);
    savingBodyRef.current = next;
    void onSaveBody(next).finally(() => {
      if (savingBodyRef.current === next) {
        savingBodyRef.current = null;
      }
    });
  };

  const vizDiagnostics = result?.viz_diagnostics ?? [];
  const rowsShown = Math.min(result?.rows?.length ?? 0, RESULT_DISPLAY_CAP);
  // Only surface staleness for cells the user must act on. A cell auto-recompute
  // is about to refresh is left unmarked — flagging it would just flicker.
  const showStale = stale && !pendingAuto;

  return (
    <RedesignPanel>
      <DelimitedCardHeader className={cn(showStale && "notebook-stale-hatch")}>
        <span className={cn("size-2 rounded-full", statusDotClass(result, showStale))} />
        {renaming ? (
          <input
            autoFocus
            value={nameDraft}
            spellCheck={false}
            onChange={(event) => setNameDraft(event.target.value)}
            onBlur={commitRename}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commitRename();
              } else if (event.key === "Escape") {
                event.preventDefault();
                setNameDraft(cell.name);
                setRenaming(false);
              }
            }}
            className="w-40 rounded border bg-background px-1.5 py-0.5 font-mono text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        ) : (
          <button
            type="button"
            className="rounded font-mono text-sm font-medium hover:bg-muted"
            title="Rename cell (F2)"
            onClick={() => setRenaming(true)}
          >
            {cell.name}
          </button>
        )}
        <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">{isPythonCell ? "python" : "sql"}</span>
        {result?.materialized === "table" ? (
          <span className="rounded bg-sky-50 px-1.5 py-0.5 text-[10px] text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">table</span>
        ) : null}
        {result?.imports?.map((imported) => (
          <span key={imported.ref} title={`imported ${imported.imported_at}${imported.complete ? "" : " · truncated"}`} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
            {imported.ref}{imported.complete ? "" : " ⚠"}
          </span>
        ))}
        <span className="ml-auto text-[11px] text-muted-foreground">
          {running ? "running…" : result?.status === "ok" ? `${result.total_rows} rows · ${result.duration_ms} ms` : null}
        </span>
        {running ? (
          <Button variant="ghost" size="icon-sm" className="group" onClick={onCancel} title="Stop cell">
            <Loader2 className="size-3.5 animate-spin group-hover:hidden" />
            <Square className="hidden size-3.5 fill-current group-hover:block" />
          </Button>
        ) : (
          <Button variant="ghost" size="icon-sm" disabled={busy} onClick={onRun} title="Run cell">
            <Play className="size-3.5" />
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Cell actions"><MoreHorizontal className="size-3.5" /></Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuItem disabled={busy} onSelect={onRunFromHere}><Play className="size-4" />Run from here</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => setRenaming(true)}><Pencil className="size-4" />Rename cell</DropdownMenuItem>
            <DropdownMenuItem onSelect={onPromote}><ArrowUpFromLine className="size-4" />Promote to pipeline</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}><Trash2 className="size-4" />Delete cell</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </DelimitedCardHeader>
      <DelimitedCardContent className="space-y-3">
        <NotebookCellMonaco
          cell={cell}
          value={draft}
          schemaTables={schemaTables}
          resultColumns={result?.columns ?? []}
          environment={selectedEnvironment}
          onChange={setDraft}
          onCommit={commit}
          onRun={onRun}
          onRename={() => setRenaming(true)}
        />
        <MissingPythonDepsBanner missingImports={missingDeps} onAddDependency={onAddDependency} />
        {result?.status === "error" ? (
          <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 font-mono text-xs text-red-800 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-200">{result.error}</div>
        ) : null}
        {result?.status === "blocked" ? (
          <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-200">{result.error}</div>
        ) : null}
        {result?.logs ? (
          <NotebookCellLogs logs={result.logs} isError={result.status === "error"} />
        ) : null}
        {vizDiagnostics.length > 0 ? (
          <div className="space-y-1">
            {vizDiagnostics.map((diagnostic, index) => (
              <div
                key={index}
                className={cn(
                  "rounded border px-2 py-1 text-[11px]",
                  diagnostic.severity === "error"
                    ? "border-red-200 bg-red-50 text-red-700 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300"
                    : "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/25 dark:bg-amber-500/10 dark:text-amber-300"
                )}
              >
                @viz: {diagnostic.message}
              </div>
            ))}
          </div>
        ) : null}
        {result?.status === "ok" && result.columns.length > 0 && !isPythonCell ? (
          <div className="flex items-center gap-1">
            <span className="mr-1 text-[11px] text-muted-foreground">View</span>
            {(["table", "bar", "line", "area", "pie", "kpi"] as VizKind[]).map((kind) => {
              const Icon = VIZ_KIND_ICONS[kind];
              const active = (result.viz?.kind ?? "table") === kind;
              return (
                <Button
                  key={kind}
                  variant={active ? "secondary" : "ghost"}
                  size="icon-sm"
                  title={kind}
                  onClick={() => setChartKind(kind)}
                >
                  <Icon className="size-3.5" />
                </Button>
              );
            })}
          </div>
        ) : null}
        {result?.status === "ok" && result.columns.length > 0 ? (
          result.viz && result.viz.kind !== "table" ? (
            <NotebookVizRenderer result={result} />
          ) : (
            <div className="overflow-hidden rounded-lg border">
              <SimpleTable
                viewportClassName="max-h-72"
                columns={result.columns}
                rows={result.rows.slice(0, RESULT_DISPLAY_CAP).map((row) =>
                  row.map((value) => (value === null || value === undefined ? "" : String(value)))
                )}
              />
              {result.rows.length > rowsShown || result.total_rows > result.rows.length ? (
                <div className="border-t bg-muted/30 px-2 py-1 text-[11px] text-muted-foreground">
                  showing {rowsShown} of {result.total_rows} rows
                </div>
              ) : null}
            </div>
          )
        ) : null}
      </DelimitedCardContent>
    </RedesignPanel>
  );
}

// NotebookCellLogs shows a Python cell's captured stdout/stderr, collapsed by
// default and auto-expanded when the run errored.
function NotebookCellLogs({ logs, isError }: { logs: string; isError: boolean }) {
  const [open, setOpen] = useState(isError);
  // Re-apply the default whenever a new run lands (the logs/error may change).
  useEffect(() => {
    setOpen(isError);
  }, [logs, isError]);

  return (
    <div className="overflow-hidden rounded-lg border bg-muted/30">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex w-full items-center gap-1.5 px-3 py-1.5 text-left text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        Output
      </button>
      {open ? (
        <ScrollArea viewportClassName="max-h-72" className="border-t">
          <pre
            data-testid="cell-logs"
            className="px-3 py-2 font-mono text-[11px] leading-5 whitespace-pre-wrap break-words"
          >
            {logs}
          </pre>
        </ScrollArea>
      ) : null}
    </div>
  );
}

function NotebookDependenciesDialog({
  open,
  onOpenChange,
  dependencies,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  dependencies: string[];
  onSave: (dependencies: string[]) => void;
}) {
  const saved = useMemo(() => dependencies.join("\n"), [dependencies]);
  const [draft, setDraft] = useState(saved);
  // Re-sync the draft whenever the dialog opens or the saved value changes.
  useEffect(() => {
    if (open) {
      setDraft(saved);
    }
  }, [open, saved]);

  const count = useMemo(() => draft.split("\n").filter((line) => line.trim()).length, [draft]);
  const dirty = draft !== saved;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Package className="size-4 text-primary" />Python dependencies
          </DialogTitle>
          <DialogDescription>
            One package per line. Managed with uv in the notebook&apos;s pyproject.toml and installed on the next run.
          </DialogDescription>
        </DialogHeader>
        <textarea
          autoFocus
          value={draft}
          spellCheck={false}
          aria-label="dependencies"
          placeholder="pandas&#10;duckdb&#10;requests==2.31.0"
          onChange={(event) => setDraft(event.target.value)}
          rows={10}
          className="w-full resize-y rounded-lg border bg-background p-3 font-mono text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <DialogFooter className="items-center sm:justify-between">
          <span className="text-[11px] text-muted-foreground">
            {count} package{count === 1 ? "" : "s"}
          </span>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Close
            </Button>
            <Button
              size="sm"
              disabled={!dirty}
              onClick={() => {
                onSave(draft.split("\n").map((line) => line.trim()).filter(Boolean));
                onOpenChange(false);
              }}
            >
              <Check className="size-3.5" />Save
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PromoteCellDialog({
  cell,
  cells,
  pipelines,
  onOpenChange,
  onPromote,
}: {
  cell: WebAsset | null;
  cells: WebAsset[];
  pipelines: Array<{ id: string; name: string }>;
  onOpenChange: (open: boolean) => void;
  onPromote: (
    cell: WebAsset,
    input: { pipeline_id: string; target_name: string; include_upstream: boolean; include_downstream: boolean }
  ) => void;
}) {
  const [pipelineId, setPipelineId] = useState("");
  const [targetName, setTargetName] = useState("");
  const [includeUpstream, setIncludeUpstream] = useState(false);
  const [includeDownstream, setIncludeDownstream] = useState(false);

  // Whether the cell has upstream/downstream sibling cells, so the options are
  // only offered when they would actually pull anything in.
  const { hasUpstream, hasDownstream } = useMemo(() => {
    if (!cell) {
      return { hasUpstream: false, hasDownstream: false };
    }
    const cellNames = new Set(cells.map((candidate) => candidate.name.toLowerCase()));
    const up = (cell.upstreams ?? []).some((upstream) => cellNames.has(upstream.toLowerCase()));
    const down = cells.some(
      (candidate) =>
        candidate.cell_id !== cell.cell_id &&
        (candidate.upstreams ?? []).some((upstream) => upstream.toLowerCase() === cell.name.toLowerCase())
    );
    return { hasUpstream: up, hasDownstream: down };
  }, [cell, cells]);

  // Re-seed the form each time a new cell opens the dialog.
  useEffect(() => {
    if (!cell) {
      return;
    }
    setPipelineId(pipelines[0]?.id ?? "");
    setTargetName(`marts.${cell.name}`);
    setIncludeUpstream(false);
    setIncludeDownstream(false);
  }, [cell, pipelines]);

  const canSubmit = !!cell && !!pipelineId && targetName.trim().length > 0;

  return (
    <Dialog open={!!cell} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ArrowUpFromLine className="size-4 text-primary" />Promote to pipeline
          </DialogTitle>
          <DialogDescription>
            Move {cell ? <span className="font-mono">{cell.name}</span> : "this cell"} into a pipeline as a
            real asset. Cells left behind that referenced it are rewritten to read the new asset.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {pipelines.length > 1 ? (
            <div className="space-y-1.5">
              <Label htmlFor="promote-pipeline">Pipeline</Label>
              <select
                id="promote-pipeline"
                value={pipelineId}
                onChange={(event) => setPipelineId(event.target.value)}
                className="w-full rounded-lg border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {pipelines.map((pipeline) => (
                  <option key={pipeline.id} value={pipeline.id}>
                    {pipeline.name}
                  </option>
                ))}
              </select>
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="promote-name">Target asset name</Label>
            <input
              id="promote-name"
              autoFocus
              value={targetName}
              spellCheck={false}
              placeholder="schema.table"
              onChange={(event) => setTargetName(event.target.value)}
              className="w-full rounded-lg border bg-background px-3 py-2 font-mono text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>

          {hasUpstream || hasDownstream ? (
            <div className="space-y-2 rounded-lg border bg-muted/30 p-3">
              <div className="text-[11px] font-medium text-muted-foreground">Also promote connected cells</div>
              {hasUpstream ? (
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={includeUpstream}
                    onCheckedChange={(value) => setIncludeUpstream(value === true)}
                  />
                  Upstream assets (its sources)
                </label>
              ) : null}
              {hasDownstream ? (
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={includeDownstream}
                    onCheckedChange={(value) => setIncludeDownstream(value === true)}
                  />
                  Downstream assets (what depends on it)
                </label>
              ) : null}
              <p className="text-[11px] text-muted-foreground">
                Connected cells are named in the same schema (e.g. <span className="font-mono">marts.&lt;cell&gt;</span>).
              </p>
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={!canSubmit}
            onClick={() => {
              if (!cell) {
                return;
              }
              onPromote(cell, {
                pipeline_id: pipelineId,
                target_name: targetName.trim(),
                include_upstream: includeUpstream,
                include_downstream: includeDownstream,
              });
            }}
          >
            <ArrowUpFromLine className="size-3.5" />Promote
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MarkdownBlockCard({
  markdown,
  onSave,
  onDelete,
}: {
  markdown: string;
  onSave: (markdown: string) => void;
  onDelete: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(markdown);
  useEffect(() => {
    setDraft(markdown);
  }, [markdown]);

  return (
    <RedesignPanel>
      <DelimitedCardHeader>
        <BookOpen className="size-4 text-primary" />
        <DelimitedCardTitle>Markdown</DelimitedCardTitle>
        <span className="ml-auto" />
        {editing ? (
          <Button variant="ghost" size="icon-sm" title="Save" onClick={() => { setEditing(false); if (draft !== markdown) onSave(draft); }}>
            <Check className="size-3.5" />
          </Button>
        ) : (
          <Button variant="ghost" size="icon-sm" title="Edit" onClick={() => setEditing(true)}>
            <Pencil className="size-3.5" />
          </Button>
        )}
        <Button variant="ghost" size="icon-sm" title="Delete block" onClick={onDelete}><Trash2 className="size-3.5" /></Button>
      </DelimitedCardHeader>
      <DelimitedCardContent>
        {editing ? (
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            rows={Math.min(Math.max(draft.split("\n").length, 3), 16)}
            className="w-full resize-y rounded-lg border bg-background p-3 font-mono text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        ) : (
          <article className="prose prose-sm max-w-none text-sm leading-6 text-foreground [&_h1]:mb-2 [&_h1]:text-xl [&_h1]:font-semibold [&_h2]:mb-2 [&_h2]:text-lg [&_h2]:font-semibold [&_p]:mb-2 [&_ul]:list-disc [&_ul]:pl-5">
            <Suspense fallback={<span className="text-muted-foreground">…</span>}>
              <ReactMarkdown>{markdown || "*empty*"}</ReactMarkdown>
            </Suspense>
          </article>
        )}
      </DelimitedCardContent>
    </RedesignPanel>
  );
}
