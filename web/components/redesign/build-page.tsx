import { Link, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useAtomValue, useSetAtom } from "jotai";
import {
  Group as PanelGroup,
  Panel,
  type PanelImperativeHandle,
  Separator as PanelResizeHandle,
} from "react-resizable-panels";
import {
  Activity,
  AlertTriangle,
  Bell,
  BookOpen,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ChevronsUpDown,
  ChevronUp,
  Circle,
  ClipboardCheck,
  Columns2,
  Cpu,
  Database,
  Download,
  Eye,
  FileCode,
  GitBranch,
  Globe,
  GitCompare,
  Hammer,
  History,
  Layers,
  Loader2,
  MoreHorizontal,
  Network,
  Package,
  PanelLeft,
  PanelRight,
  Play,
  Plus,
  RotateCw,
  Search,
  Sliders,
  Table2,
  Terminal,
  XCircle,
} from "lucide-react";
import { ComponentType, ReactNode, createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { ButtonGroup } from "@/components/ui/button-group";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { createAsset, deleteAsset } from "@/lib/api-assets";
import { buildCreateAssetInput, buildSuggestedAssetName } from "@/lib/workspace-shell-helpers";
import type { NewAssetKind } from "@/components/new-asset-node";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AssetInspectView } from "@/components/asset-inspect-view";
import { InspectWarningCard } from "@/components/inspect-warning-card";
import { WorkspaceMaterializeOutputView } from "@/components/workspace-materialize-output-view";
import { Spinner } from "@/components/ui/spinner";
import { runSQLQuery } from "@/lib/api";
import type { MaterializeStreamPayload } from "@/lib/api-core";
import { typeCheckPipeline, type PipelineTypeCheckReport } from "@/lib/api-pipelines";
import { createNotebook } from "@/lib/api-notebooks";
import type { AssetStaleness } from "@/lib/api-staleness";
import { isSqlAssetType } from "@/lib/asset-types";
import { editorDraftAtom } from "@/lib/atoms/domains/editor";
import type { MaterializeHistoryEntry } from "@/lib/atoms/results";
import {
  routeSelectionAtom,
  selectedEnvironmentAtom,
  selectedExecutionTimeWindowAtom,
  workspaceAtom,
} from "@/lib/atoms/domains/workspace";
import { renderJinjaAsset } from "@/lib/jinja-intellisense";
import { resolveConnection } from "@/lib/sql-schema";
import type { AssetInspectResponse, SqlQueryResponse, WebAsset, WebPipeline } from "@/lib/types";
import { cn } from "@/lib/utils";
import { useAssetResults } from "@/hooks/use-asset-results";
import { useSelectedEnvironmentPolicy } from "@/hooks/use-environment-policy";
import { useIsMobile } from "@/hooks/use-mobile";
import { usePipelineDeploy, type PipelineDeployState } from "@/hooks/use-pipeline-deploy";
import { isStaleStatus, usePipelineStaleness } from "@/hooks/use-pipeline-staleness";
import type { MaterializeScope } from "@/lib/materialize-scope";
import { labelForRedesignMaterializationState, useRedesignAssetMaterializationStatus } from "@/hooks/use-redesign-asset-materialization-status";

import {
  assets,
  changeTypeMeta,
  diagnostics,
  editorLinesFor,
  impactPlan,
  type AssetKind,
  kindMeta,
  missingPythonDependencies,
  objectGroups,
  packageForImport,
  parsePythonImport,
  pipelineDependencies,
  pipelineVariables,
  pipelineVariants,
  renderedPipelineName,
  renderedPipelineSchedule,
  schemaRows,
  tests,
} from "./redesign-data";
import { RedesignAdhocEditor, useAdhocQueryDraft } from "./adhoc-editor";
import { RedesignAssetEditor } from "./asset-editor";
import { AssetGuidedCards } from "./asset-guided-cards";
import { AssetYamlEditor } from "./asset-yaml-editor";
import { SlingParametersEditor } from "./sling-parameters-editor";
import { RedesignLineageCanvas, assetDisplayName, assetGroupName, assetNameParts, type RedesignLineageCanvasAsset } from "./lineage-canvas";
import { IntegrationBadge, RedesignPage, RedesignPanel, SectionCard, SeverityIcon, SimpleTable, StalenessBadge, StatusPill, stalenessDotClassName, stalenessLabel } from "./redesign-primitives";

export type RedesignBuildView = "canvas" | "split" | "code";
export type RedesignResultTab = "inspect" | "materialize" | "query" | "typecheck" | "tests" | "diagnostics" | "metadata" | "shell" | "history";
export type RedesignEditorMode = "asset" | "adhoc";

export type RedesignBuildSearch = {
  result?: RedesignResultTab;
  editor?: RedesignEditorMode;
  variant?: string;
};

const resultTabs: RedesignResultTab[] = ["inspect", "materialize", "query", "typecheck", "tests", "diagnostics", "metadata", "shell", "history"];
const editorModes: RedesignEditorMode[] = ["asset", "adhoc"];
const variantIds = pipelineVariants.map((variant) => variant.id);

export function normalizeRedesignBuildSearch(search: Record<string, unknown>): RedesignBuildSearch {
  return {
    result: resultTabs.includes(search.result as RedesignResultTab) ? (search.result as RedesignResultTab) : undefined,
    editor: editorModes.includes(search.editor as RedesignEditorMode) ? (search.editor as RedesignEditorMode) : undefined,
    variant: variantIds.includes(search.variant as never) ? (search.variant as string) : undefined,
  };
}

const scrollableTabsListClass = "w-max max-w-none";
const scrollableTabsTriggerClass = "flex-none";

type BuildAsset = RedesignLineageCanvasAsset & {
  workspaceAsset?: WebAsset;
  pipelineId?: string;
  displayName?: string;
  prefix?: string;
  path?: string;
  type?: string;
  connection?: string;
  upstreams?: string[];
};

type BuildContextValue = {
  pipelineId: string;
  pipeline?: WebPipeline;
  pipelineAssets: BuildAsset[];
  selectedAssetId: string;
  selectedAsset: BuildAsset;
  view: RedesignBuildView;
  buildSearch: RedesignBuildSearch;
  editorMode: RedesignEditorMode;
  declaredDependencies: string[];
  addDependency: (dependency: string) => void;
  selectAsset: (assetId: string) => void;
  goToAsset: (pipelineId: string, assetId: string) => void;
  runAssetById: (assetId: string) => void;
  deleteAssetById: (assetId: string) => Promise<void>;
  goToCatalog: (assetId?: string) => void;
  openNewAsset: () => void;
  createDownstreamAsset: (source: { id: string; name: string }) => void;
  openBottom: (tab: RedesignResultTab) => void;
  materializeSelectedAsset: () => void;
  inspectSelectedAsset: () => void;
  runAdhocQuery: () => void;
  adhocContextAsset: WebAsset | null;
  adhocLoading: boolean;
  materializeLoading: boolean;
  inspectLoading: boolean;
  executionBlocked: boolean;
};

const BuildContext = createContext<BuildContextValue | null>(null);

function useBuildContext() {
  const context = useContext(BuildContext);
  if (!context) {
    throw new Error("Build view components must be rendered inside RedesignBuildPage");
  }
  return context;
}

function fallbackBuildAssets(): BuildAsset[] {
  return assets;
}

function assetsForPipeline(pipeline: WebPipeline): BuildAsset[] {
  return pipeline.assets.map((asset) => ({
    ...assetDisplayFields(asset, pipeline),
    workspaceAsset: asset,
    pipelineId: pipeline.id,
    path: asset.path,
    type: asset.type,
    connection: asset.connection,
    upstreams: asset.upstreams,
    x: 0,
    y: 0,
  }));
}

function assetDisplayFields(asset: WebAsset, pipeline: WebPipeline): Omit<BuildAsset, "workspaceAsset" | "path" | "type" | "connection" | "upstreams" | "x" | "y"> {
  const canonicalName = asset.name || assetFileName(asset.path);
  const { prefix, title } = assetNameParts(canonicalName);
  return {
    id: asset.id,
    name: canonicalName,
    displayName: title,
    prefix: prefix ?? assetDirectory(asset.path, pipeline.path),
    kind: kindForAssetType(asset.type),
    group: prefix ?? "ASSETS",
    integration: integrationForAsset(asset),
    description: asset.meta?.description ?? asset.path,
    dir: assetDirectory(asset.path, pipeline.path),
    imports: importsFromContent(asset.content),
    status: asset.is_materialized ? "success" : "pending",
    materializedAt: asset.is_materialized ? "current" : "not materialized",
  };
}

function kindForAssetType(type: string): AssetKind {
  const normalized = type.toLowerCase();
  if (normalized.includes("python")) return "python";
  if (normalized.includes("sling")) return "sling";
  if (normalized.includes("ingestr")) return "ingestr";
  if (normalized.includes("source")) return "source";
  if (normalized.includes("test")) return "unittest";
  return "sql";
}

function integrationForAsset(asset: WebAsset) {
  if (asset.connection) {
    return asset.connection;
  }
  const provider = asset.type.split(".")[0]?.toLowerCase();
  if (provider === "duckdb") return "DuckDB";
  if (provider === "python") return "Python";
  if (provider === "sling") return "Sling";
  if (provider === "ingestr") return "ingestr";
  return provider || "Asset";
}

function assetDirectory(assetPath: string, pipelinePath: string) {
  const pipelineRoot = pipelinePath.replace(/\/?pipeline\.ya?ml$/i, "");
  let relative = assetPath;
  if (pipelineRoot && relative.startsWith(`${pipelineRoot}/`)) {
    relative = relative.slice(pipelineRoot.length + 1);
  }
  if (relative.startsWith("assets/")) {
    relative = relative.slice("assets/".length);
  }
  const dir = relative.split("/").slice(0, -1).join("/");
  return dir || undefined;
}

function assetFileName(assetPath: string) {
  const file = assetPath.split("/").pop() ?? assetPath;
  return file.replace(/\.[^.]+$/, "");
}

function assetSidebarName(asset: BuildAsset) {
  if (asset.path) {
    const file = asset.path.split("/").pop() ?? asset.name;
    const prefix = asset.prefix;
    if (prefix && file.startsWith(`${prefix}.`)) {
      return file.slice(prefix.length + 1);
    }
    return file;
  }
  return `${assetDisplayName(asset)}${kindMeta[asset.kind].ext}`;
}

function importsFromContent(content: string) {
  return content
    .split(/\r?\n/)
    .map(parsePythonImport)
    .filter((value): value is string => Boolean(value));
}

export function RedesignBuildPage({
  pipelineId = "simple",
  selectedAssetId,
  resultTab = "inspect",
  editorMode = "asset",
  variant = "default",
  onResultTabChange,
  onVariantChange,
  onAssetSelect,
}: {
  pipelineId?: string;
  selectedAssetId?: string;
  resultTab?: RedesignResultTab;
  editorMode?: RedesignEditorMode;
  variant?: string;
  onResultTabChange?: (tab: RedesignResultTab) => void;
  onVariantChange?: (variant: string) => void;
  onAssetSelect?: (assetId: string) => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const navigate = useNavigate();
  const location = useLocation();
  const view = redesignBuildViewFromPath(location.pathname);
  const buildSearch: RedesignBuildSearch = useMemo(
    () => ({ result: resultTab, editor: editorMode, variant }),
    [editorMode, resultTab, variant]
  );
  const activePipeline = useMemo(
    () => workspace?.pipelines.find((pipeline) => pipeline.id === pipelineId),
    [pipelineId, workspace?.pipelines]
  );
  const pipelineAssets = useMemo(
    () => activePipeline ? assetsForPipeline(activePipeline) : fallbackBuildAssets(),
    [activePipeline]
  );
  const existingAssetNames = useMemo(
    () => new Set((activePipeline?.assets ?? []).map((asset) => asset.name)),
    [activePipeline?.assets]
  );
  const materializationAssets = useMemo(
    () => pipelineAssets.map((asset) => ({
      id: asset.id,
      name: asset.name,
      pipelineId: asset.pipelineId,
      isMaterialized: asset.workspaceAsset?.is_materialized ?? (asset.status === "success" || asset.status === "ok"),
    })),
    [pipelineAssets]
  );
  const materializationStatusByAssetId = useRedesignAssetMaterializationStatus(materializationAssets);
  const staleness = usePipelineStaleness(activePipeline?.id);
  const deployState = usePipelineDeploy(activePipeline?.id);
  const environmentPolicy = useSelectedEnvironmentPolicy();
  const executionBlocked = Boolean(environmentPolicy?.protected);
  const assetResults = useAssetResults();
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const selectedExecutionTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const editorDraft = useAtomValue(editorDraftAtom);
  const [adhocResult, setAdhocResult] = useState<SqlQueryResponse | null>(null);
  const [adhocRenderedQuery, setAdhocRenderedQuery] = useState<string | null>(null);
  const [adhocLoading, setAdhocLoading] = useState(false);
  const [adhocQuery] = useAdhocQueryDraft(pipelineId);
  const displayedPipelineAssets = useMemo(
    () => pipelineAssets.map((asset) => ({
      ...asset,
      status: materializationStatusByAssetId[asset.id]?.status ?? asset.status,
      materializedAt: labelForRedesignMaterializationState(materializationStatusByAssetId[asset.id]),
      staleness: staleness.byAssetName[asset.name],
    })),
    [materializationStatusByAssetId, pipelineAssets, staleness.byAssetName]
  );
  // Transitive stale upstreams of an asset, walked over the dependency graph.
  // Materializing an asset while these are stale reads their outdated tables, so
  // the asset cannot become fresh — we warn before building (§9 / §17).
  const assetsByName = useMemo(() => {
    const map = new Map<string, WebAsset>();
    for (const asset of activePipeline?.assets ?? []) {
      map.set(asset.name, asset);
    }
    return map;
  }, [activePipeline?.assets]);
  const staleUpstreamsOf = useCallback(
    (assetName: string): string[] => {
      const stale: string[] = [];
      const seen = new Set<string>();
      const walk = (name: string) => {
        const asset = assetsByName.get(name);
        if (!asset) return;
        for (const upstream of asset.upstreams ?? []) {
          if (seen.has(upstream)) continue;
          seen.add(upstream);
          const status = staleness.byAssetName[upstream];
          if (status && isStaleStatus(status.status)) stale.push(upstream);
          walk(upstream);
        }
      };
      walk(assetName);
      return stale;
    },
    [assetsByName, staleness.byAssetName]
  );
  const [staleBuildPrompt, setStaleBuildPrompt] = useState<{ assetId: string; assetName: string; staleUpstreams: string[] } | null>(null);
  const firstAssetId = displayedPipelineAssets[0]?.id ?? "revenue_daily";
  const [visualSelectedAssetId, setVisualSelectedAssetId] = useState(selectedAssetId ?? firstAssetId);
  const effectiveSelectedAssetId = visualSelectedAssetId ?? selectedAssetId ?? firstAssetId;
  const selectedAsset = displayedPipelineAssets.find((asset) => asset.id === effectiveSelectedAssetId) ?? displayedPipelineAssets[0] ?? fallbackBuildAssets()[0];
  const [explorerOpen, setExplorerOpen] = useState(false);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [newAssetOpen, setNewAssetOpen] = useState(false);
  const [downstreamSource, setDownstreamSource] = useState<{ id: string; name: string } | null>(null);
  const [pipelineSettingsOpen, setPipelineSettingsOpen] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [buildStaleOpen, setBuildStaleOpen] = useState(false);
  const [addedDependencies, setAddedDependencies] = useState<string[]>([]);
  const [history, setHistory] = useState<Array<{ id: number; kind: string; target: string; status: string; time: string; variant: string }>>([]);
  const declaredDependencies = [...pipelineDependencies, ...addedDependencies];
  const [typeCheckReport, setTypeCheckReport] = useState<PipelineTypeCheckReport | null>(null);
  const [typeCheckLoading, setTypeCheckLoading] = useState(false);
  const resultsPanelRef = useRef<PanelImperativeHandle | null>(null);
  const [resultsCollapsed, setResultsCollapsed] = useState(false);
  const toggleResultsPanel = () => {
    const panel = resultsPanelRef.current;
    if (!panel) {
      return;
    }
    if (panel.isCollapsed()) {
      panel.expand();
    } else {
      panel.collapse();
    }
  };

  useEffect(() => {
    setVisualSelectedAssetId(selectedAssetId ?? firstAssetId);
  }, [firstAssetId, selectedAssetId]);

  // Keep the global selection atoms pointed at the asset shown here so the
  // selection-derived state (editor drafts, schema suggestion tables,
  // intellisense context) works the same as on the classic workspace page.
  const setRouteSelection = useSetAtom(routeSelectionAtom);
  useEffect(() => {
    if (!activePipeline) {
      return;
    }

    setRouteSelection({
      pipeline: activePipeline.id,
      asset: effectiveSelectedAssetId ?? null,
    });
  }, [activePipeline, effectiveSelectedAssetId, setRouteSelection]);

  useEffect(() => {
    if (!workspace?.pipelines.length || activePipeline) {
      return;
    }

    navigate({
      to: "/redesign/pipelines/$pipelineId/canvas",
      params: { pipelineId: workspace.pipelines[0].id },
      search: buildSearch,
      replace: true,
    });
  }, [activePipeline, buildSearch, navigate, workspace?.pipelines]);

  const openBottom = (tab: RedesignResultTab) => {
    onResultTabChange?.(tab);
    // Make sure the results are visible when something routes output here.
    resultsPanelRef.current?.expand();
  };
  const addDependency = (dependency: string) => {
    setAddedDependencies((current) => current.includes(dependency) ? current : [...current, dependency]);
  };
  const logHistory = (kind: string, target: string) => {
    setHistory((current) => [{ id: Date.now(), kind, target, status: "success", time: new Date().toLocaleTimeString(), variant }, ...current].slice(0, 20));
  };
  const runTypeCheck = useCallback(async (openTab = false) => {
    if (!activePipeline) {
      return;
    }
    if (openTab) {
      openBottom("typecheck");
    }
    setTypeCheckLoading(true);
    try {
      const report = await typeCheckPipeline(activePipeline.id, {
        startDate: selectedExecutionTimeWindow?.start,
        endDate: selectedExecutionTimeWindow?.end,
      });
      setTypeCheckReport(report);
    } catch {
      setTypeCheckReport(null);
    } finally {
      setTypeCheckLoading(false);
    }
  }, [activePipeline, selectedExecutionTimeWindow?.start, selectedExecutionTimeWindow?.end]);
  // Run the type check once per pipeline so the notification badge reflects the
  // current state; the user can re-run from the bell to pick up edits.
  useEffect(() => {
    if (!activePipeline) {
      return;
    }
    void runTypeCheck(false);
  }, [activePipeline?.id, runTypeCheck]);
  const runAction = () => {
    if (!activePipeline) {
      return;
    }
    logHistory("run", activePipeline.name || pipelineId);
    openBottom("materialize");
    void assetResults.runMaterializePipeline(activePipeline.id);
  };
  const runMaterialize = (assetId: string, name: string, scope: MaterializeScope = "asset") => {
    logHistory("materialize", name);
    openBottom("materialize");
    void assetResults.runMaterializeForAsset(assetId, scope);
  };
  // Guarded materialize: if the asset depends on stale upstreams, prompt before
  // building (the build would read outdated upstream data and stay stale).
  const requestMaterialize = (assetId: string, name: string) => {
    const stale = staleUpstreamsOf(name);
    if (stale.length > 0) {
      setStaleBuildPrompt({ assetId, assetName: name, staleUpstreams: stale });
      return;
    }
    runMaterialize(assetId, name);
  };
  const materializeSelectedAsset = () => {
    const workspaceAsset = selectedAsset?.workspaceAsset;
    if (!activePipeline || !workspaceAsset) {
      return;
    }
    requestMaterialize(workspaceAsset.id, selectedAsset.name);
  };
  const inspectSelectedAsset = () => {
    const workspaceAsset = selectedAsset?.workspaceAsset;
    if (!activePipeline || !workspaceAsset) {
      return;
    }
    logHistory("inspect", selectedAsset.name);
    openBottom("inspect");
    void assetResults.runInspectForAsset(
      workspaceAsset.id,
      editorDraft[workspaceAsset.id] ?? workspaceAsset.content
    );
  };
  // SQL context for the ad hoc editor: the selected asset when it is SQL,
  // otherwise the first SQL asset of the pipeline (dialect + connection).
  const adhocContextAsset = useMemo(() => {
    const candidates = activePipeline?.assets ?? [];
    const isSql = (asset: WebAsset) =>
      isSqlAssetType(asset.type) || asset.path.toLowerCase().endsWith(".sql");
    const selected = candidates.find((asset) => asset.id === effectiveSelectedAssetId);
    if (selected && isSql(selected)) {
      return selected;
    }
    return candidates.find(isSql) ?? null;
  }, [activePipeline?.assets, effectiveSelectedAssetId]);
  const runAdhocQuery = async () => {
    if (!activePipeline) {
      return;
    }
    openBottom("query");
    const connection = adhocContextAsset
      ? resolveConnection(adhocContextAsset, workspace?.connections ?? {})
      : null;
    if (!connection || !adhocContextAsset) {
      setAdhocRenderedQuery(null);
      setAdhocResult({
        status: "error",
        columns: [],
        rows: [],
        error: "No SQL connection found for this pipeline; add a SQL asset or configure a connection first.",
      });
      return;
    }
    logHistory("query", connection);
    setAdhocLoading(true);
    try {
      // Ad hoc queries are Jinja templates: render them with the pipeline's
      // variables (and the selected execution window) before executing.
      let queryText = adhocQuery;
      try {
        const rendered = await renderJinjaAsset({
          assetId: adhocContextAsset.id,
          content: adhocQuery,
          timeWindow: selectedExecutionTimeWindow,
        });
        if (rendered.status === "error") {
          setAdhocRenderedQuery(null);
          setAdhocResult({
            status: "error",
            columns: [],
            rows: [],
            error: `Jinja rendering failed: ${rendered.error || "unknown error"}`,
          });
          return;
        }
        if (rendered.rendered?.trim()) {
          queryText = rendered.rendered;
        }
      } catch {
        // Rendering is best-effort; fall back to the raw query text.
      }
      setAdhocRenderedQuery(queryText);
      const result = await runSQLQuery({
        connection,
        environment: selectedEnvironment,
        query: queryText,
        limit: 500,
      });
      setAdhocResult(result);
    } catch (error) {
      setAdhocResult({ status: "error", columns: [], rows: [], error: String(error) });
    } finally {
      setAdhocLoading(false);
    }
  };
  const selectAsset = (assetId: string) => {
    setVisualSelectedAssetId(assetId);
    onAssetSelect?.(assetId);
    setExplorerOpen(false);
  };
  const goToAsset = (targetPipelineId: string, assetId: string) => {
    void navigate({
      to: redesignAssetViewPath(view),
      params: { pipelineId: targetPipelineId, assetId },
      search: { ...buildSearch, editor: "asset" },
    });
  };
  const runAssetById = (assetId: string) => {
    const target = displayedPipelineAssets.find((asset) => asset.id === assetId);
    const workspaceAsset = target?.workspaceAsset;
    if (!activePipeline || !workspaceAsset) {
      return;
    }
    setVisualSelectedAssetId(assetId);
    requestMaterialize(workspaceAsset.id, target?.name ?? assetId);
  };
  const deleteAssetById = async (assetId: string) => {
    if (!activePipeline) {
      return;
    }
    // The workspace event stream refreshes the atom once the file is gone.
    await deleteAsset(activePipeline.id, assetId);
  };
  const goToCatalog = (assetId?: string) => {
    void navigate({ to: "/redesign/catalog", search: assetId ? { asset: assetId } : {} });
  };
  // The ad hoc editor only renders in the code/split editor panes, so opening
  // it from the explorer also navigates to the code view.
  const openAdhoc = () => {
    setExplorerOpen(false);
    void navigate({
      to: redesignAssetViewPath("code"),
      params: { pipelineId, assetId: effectiveSelectedAssetId },
      search: { ...buildSearch, editor: "adhoc" },
    });
  };
  const openNewAsset = () => {
    setDownstreamSource(null);
    setNewAssetOpen(true);
  };
  const createDownstreamAsset = (source: { id: string; name: string }) => {
    setDownstreamSource(source);
    setNewAssetOpen(true);
  };
  const buildContext: BuildContextValue = {
    pipelineId,
    pipeline: activePipeline,
    pipelineAssets: displayedPipelineAssets,
    selectedAssetId: effectiveSelectedAssetId,
    selectedAsset,
    view,
    buildSearch,
    editorMode,
    declaredDependencies,
    addDependency,
    selectAsset,
    goToAsset,
    runAssetById,
    deleteAssetById,
    goToCatalog,
    openNewAsset,
    createDownstreamAsset,
    openBottom,
    materializeSelectedAsset,
    inspectSelectedAsset,
    runAdhocQuery,
    adhocContextAsset,
    adhocLoading,
    materializeLoading: assetResults.materializeLoading,
    inspectLoading: assetResults.inspectLoading,
    executionBlocked,
  };

  return (
    <BuildContext.Provider value={buildContext}>
    <RedesignPage>
      <BuildTopBar
        pipelineId={pipelineId}
        pipelineLabel={activePipeline?.name ?? pipelineId}
        selectedAsset={selectedAsset}
        selectedAssetId={effectiveSelectedAssetId}
        resultTab={resultTab}
        editorMode={editorMode}
        variant={variant}
        historyCount={history.length}
        onOpenExplorer={() => setExplorerOpen(true)}
        onOpenInspector={() => setInspectorOpen(true)}
        onOpenHistory={() => openBottom("history")}
        onOpenPlan={() => setPlanOpen(true)}
        onVariantChange={onVariantChange}
        onRun={runAction}
        typeCheckReport={typeCheckReport}
        typeCheckLoading={typeCheckLoading}
        onTypeCheck={() => void runTypeCheck(true)}
        staleCount={staleness.staleAssets.length}
        onBuildStale={() => setBuildStaleOpen(true)}
        deployState={deployState}
        executionBlocked={executionBlocked}
      />
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3 xl:grid-cols-[248px_minmax(0,1fr)_320px]">
        <RedesignPanel className="hidden min-h-0 xl:flex xl:flex-col">
          <Explorer
            pipelineId={pipelineId}
            selectedAssetId={effectiveSelectedAssetId}
            buildSearch={buildSearch}
            onAssetSelect={selectAsset}
            onAdhoc={openAdhoc}
            onNewAsset={() => setNewAssetOpen(true)}
            onPipelineSettings={() => setPipelineSettingsOpen(true)}
          />
        </RedesignPanel>

        <PanelGroup orientation="vertical" className="h-full min-h-0">
          <Panel minSize="120px" className="min-h-0">
            <RedesignPanel className="relative flex h-full min-h-0 overflow-hidden">
              <DelimitedCardContent className="h-full min-h-0 flex-1 p-0">
                <Outlet />
              </DelimitedCardContent>
              {view !== "code" ? (
                <FloatingViewSwitcher
                  pipelineId={pipelineId}
                  selectedAssetId={effectiveSelectedAssetId}
                  currentView={view}
                  search={buildSearch}
                  onNewAsset={openNewAsset}
                />
              ) : null}
            </RedesignPanel>
          </Panel>
          <PanelResizeHandle
            className={cn(
              "my-1.5 h-1.5 shrink-0 cursor-row-resize rounded-full bg-border transition-colors hover:bg-primary/40",
              resultsCollapsed && "pointer-events-none opacity-0"
            )}
          />
          <Panel
            collapsible
            collapsedSize="38px"
            minSize="120px"
            defaultSize="224px"
            panelRef={resultsPanelRef}
            onResize={() => setResultsCollapsed(Boolean(resultsPanelRef.current?.isCollapsed()))}
            className="min-h-0"
          >
            <ResultsPanel
              activeTab={resultTab}
              onTabChange={openBottom}
              collapsed={resultsCollapsed}
              onToggleCollapse={toggleResultsPanel}
              variant={variant}
              history={history}
              typeCheckReport={typeCheckReport}
              typeCheckLoading={typeCheckLoading}
              onRunTypeCheck={() => void runTypeCheck(false)}
              onSelectAsset={selectAsset}
              onHistoryOpen={(tab) => openBottom(tab)}
              inspectResult={assetResults.inspectResult}
              inspectLoading={assetResults.inspectLoading}
              canLoadMoreInspectRows={assetResults.canLoadMoreInspectRows}
              onLoadMoreInspectRows={assetResults.loadMoreInspectRows}
              selectedMaterializeEntry={assetResults.selectedMaterializeEntry}
              materializeOutputHtml={assetResults.materializeOutputHtml}
              pipelineMaterializeLoading={assetResults.pipelineMaterializeLoading}
              adhocResult={adhocResult}
              adhocRenderedQuery={adhocRenderedQuery}
              adhocLoading={adhocLoading}
            />
          </Panel>
        </PanelGroup>

        <RedesignPanel className="hidden min-h-0 xl:flex xl:flex-col">
          <Inspector asset={selectedAsset} declaredDependencies={declaredDependencies} addedDependencies={addedDependencies} onAddDependency={addDependency} onOpenPipelineSettings={() => setPipelineSettingsOpen(true)} onOpenResults={openBottom} />
        </RedesignPanel>
      </div>

      <Sheet open={explorerOpen} onOpenChange={setExplorerOpen}>
        <SheetContent side="left" className="w-80 gap-0 p-0 sm:max-w-80">
          <SheetTitle className="sr-only">Explorer</SheetTitle>
          <Explorer
            pipelineId={pipelineId}
            selectedAssetId={effectiveSelectedAssetId}
            buildSearch={buildSearch}
            onAssetSelect={selectAsset}
            onAdhoc={openAdhoc}
            onNewAsset={() => setNewAssetOpen(true)}
            onPipelineSettings={() => setPipelineSettingsOpen(true)}
          />
        </SheetContent>
      </Sheet>
      <Sheet open={inspectorOpen} onOpenChange={setInspectorOpen}>
        <SheetContent side="right" className="w-[22rem] gap-0 p-0 sm:max-w-[22rem]">
          <SheetTitle className="sr-only">Inspector</SheetTitle>
          <Inspector asset={selectedAsset} declaredDependencies={declaredDependencies} addedDependencies={addedDependencies} onAddDependency={addDependency} onOpenPipelineSettings={() => setPipelineSettingsOpen(true)} onOpenResults={openBottom} />
        </SheetContent>
      </Sheet>

      <NewAssetDialog
        open={newAssetOpen}
        onOpenChange={(open) => {
          setNewAssetOpen(open);
          if (!open) {
            setDownstreamSource(null);
          }
        }}
        pipelineId={activePipeline?.id}
        pipelineName={activePipeline?.name}
        existingAssetNames={existingAssetNames}
        downstreamSource={downstreamSource}
        onCreated={(assetId) => goToAsset(activePipeline?.id ?? pipelineId, assetId)}
      />
      <PipelineSettingsDialog open={pipelineSettingsOpen} onOpenChange={setPipelineSettingsOpen} pipelineId={pipelineId} />
      <PlanDialog open={planOpen} onOpenChange={setPlanOpen} />
      <Dialog open={staleBuildPrompt !== null} onOpenChange={(open) => { if (!open) setStaleBuildPrompt(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><AlertTriangle className="size-4 text-amber-500" />Upstream is out of date</DialogTitle>
            <DialogDescription>
              <span className="font-mono">{staleBuildPrompt?.assetName}</span> depends on {staleBuildPrompt?.staleUpstreams.length} stale upstream{staleBuildPrompt?.staleUpstreams.length === 1 ? "" : "s"}. Building now reads their outdated tables, so this asset will stay stale until its upstreams are current. Build the upstreams first to get an up-to-date result.
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-40 space-y-1 overflow-y-auto rounded-md border p-2 font-mono text-xs">
            {staleBuildPrompt?.staleUpstreams.map((name) => (
              <div key={name} className="flex items-center gap-1.5"><span className="size-1.5 rounded-full bg-amber-500" />{name}</div>
            ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setStaleBuildPrompt(null)}>Cancel</Button>
            <Button
              variant="outline"
              onClick={() => {
                if (staleBuildPrompt) runMaterialize(staleBuildPrompt.assetId, staleBuildPrompt.assetName, "asset");
                setStaleBuildPrompt(null);
              }}
            >
              Build anyway
            </Button>
            <Button
              onClick={() => {
                if (staleBuildPrompt) runMaterialize(staleBuildPrompt.assetId, staleBuildPrompt.assetName, "asset_with_upstreams");
                setStaleBuildPrompt(null);
              }}
            >
              <Hammer className="size-4" />Build upstreams first
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <BuildStaleDialog
        open={buildStaleOpen}
        onOpenChange={setBuildStaleOpen}
        staleAssets={staleness.staleAssets}
        pipelineAssets={displayedPipelineAssets}
        onBuildAsset={(assetId, options) =>
          assetResults.runMaterializeForAsset(assetId, "asset", undefined, options)
        }
        onCompleted={() => {
          logHistory("build stale", `${staleness.staleAssets.length} assets`);
          openBottom("materialize");
        }}
      />
    </RedesignPage>
    </BuildContext.Provider>
  );
}

function BuildTopBar({
  pipelineId,
  pipelineLabel,
  selectedAsset,
  selectedAssetId,
  resultTab,
  editorMode,
  variant,
  historyCount,
  onOpenExplorer,
  onOpenInspector,
  onOpenHistory,
  onOpenPlan,
  onVariantChange,
  onRun,
  staleCount = 0,
  onBuildStale,
  deployState,
  executionBlocked = false,
  typeCheckReport,
  typeCheckLoading = false,
  onTypeCheck,
}: {
  pipelineId: string;
  pipelineLabel: string;
  selectedAsset: BuildAsset;
  selectedAssetId: string;
  resultTab: RedesignResultTab;
  editorMode: RedesignEditorMode;
  variant: string;
  historyCount: number;
  onOpenExplorer: () => void;
  onOpenInspector: () => void;
  onOpenHistory: () => void;
  onOpenPlan: () => void;
  onVariantChange?: (variant: string) => void;
  onRun: () => void;
  staleCount?: number;
  onBuildStale?: () => void;
  deployState?: PipelineDeployState;
  executionBlocked?: boolean;
  typeCheckReport?: PipelineTypeCheckReport | null;
  typeCheckLoading?: boolean;
  onTypeCheck?: () => void;
}) {
  const search: RedesignBuildSearch = { result: resultTab, editor: editorMode, variant };

  return (
    <div className="flex min-h-12 shrink-0 items-center gap-2 px-3">
      <Button variant="ghost" size="sm" className="xl:hidden" onClick={onOpenExplorer}><PanelLeft className="size-3.5" /></Button>
      <Breadcrumb className="min-w-0 flex-1">
        <BreadcrumbList className="flex-nowrap text-xs">
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbLink asChild className="truncate">
              <Link to="/redesign">data_platform</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <span className="text-muted-foreground">pipeline</span>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbLink asChild className="truncate font-mono">
              <Link to="/redesign/pipelines/$pipelineId/canvas" params={{ pipelineId }} search={search}>{pipelineLabel}</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbPage className="truncate font-mono">{selectedAsset.name}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" className={cn("hidden font-mono lg:inline-flex", variant !== "default" ? "text-primary" : null)}>
            <GitBranch className="size-3.5" />{variant}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-64">
          <DropdownMenuLabel>Variant</DropdownMenuLabel>
          {pipelineVariants.map((item) => (
            <DropdownMenuItem key={item.id} onSelect={() => onVariantChange?.(item.id)}>
              <GitBranch className="size-4" />
              <div className="min-w-0 flex-1">
                <div className="truncate font-mono text-xs">{item.id}</div>
                <div className="truncate font-mono text-[10px] text-muted-foreground">{renderedPipelineName(item.id)} · {renderedPipelineSchedule(item.id)}</div>
              </div>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={onOpenPlan}><ClipboardCheck className="size-4" />Review impact plan</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button
        asChild
        variant={editorMode === "adhoc" ? "secondary" : "outline"}
        size="sm"
        className={cn("hidden lg:inline-flex", editorMode === "adhoc" ? "text-primary ring-1 ring-primary/30" : null)}
      >
        <Link to="/redesign/pipelines/$pipelineId/assets/$assetId/code" params={{ pipelineId, assetId: selectedAssetId }} search={{ result: resultTab, editor: "adhoc", variant }}>
          <Terminal className="size-3.5" /> Ad-hoc
        </Link>
      </Button>
      {historyCount > 0 ? <Button variant="ghost" size="sm" onClick={onOpenHistory}><History className="size-3.5" />{historyCount}</Button> : null}
      <TypeCheckBell report={typeCheckReport} loading={typeCheckLoading} onClick={onTypeCheck} />
      {staleCount > 0 ? (
        <Button
          variant="outline"
          size="sm"
          onClick={onBuildStale}
          disabled={executionBlocked}
          title={executionBlocked ? "This environment is protected: interactive execution is disabled" : undefined}
        >
          <Hammer className="size-3.5" /> Build stale <span className="rounded-full bg-amber-500 px-1 text-[10px] text-white">{staleCount}</span>
        </Button>
      ) : null}
      {deployState ? <DeployButton deployState={deployState} /> : null}
      <Button
        size="sm"
        onClick={onRun}
        disabled={executionBlocked}
        title={executionBlocked ? "This environment is protected: interactive execution is disabled; deploy and schedule instead" : undefined}
      >
        <Play className="size-3.5" /> Run
      </Button>
      <Button variant="ghost" size="sm" className="xl:hidden" onClick={onOpenInspector}><PanelRight className="size-3.5" /></Button>
    </div>
  );
}

// TypeCheckBell is the repurposed notification bell: it shows the pipeline
// type-check status as a badge and opens the Type check results tab on click.
function TypeCheckBell({
  report,
  loading,
  onClick,
}: {
  report?: PipelineTypeCheckReport | null;
  loading?: boolean;
  onClick?: () => void;
}) {
  const errors = report?.summary.errors ?? 0;
  const warnings = report?.summary.warnings ?? 0;
  const total = errors + warnings;
  const title = loading
    ? "Type checking…"
    : report
      ? `Type check: ${errors} error${errors === 1 ? "" : "s"}, ${warnings} warning${warnings === 1 ? "" : "s"}`
      : "Run type check";
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      className="relative"
      onClick={onClick}
      aria-label="Type check"
      title={title}
    >
      {loading ? <Loader2 className="size-4 animate-spin" /> : <Bell className="size-4" />}
      {!loading && total > 0 ? (
        <span
          className={cn(
            "absolute -right-1 -top-1 flex h-3.5 min-w-3.5 items-center justify-center rounded-full px-1 text-[9px] font-semibold text-white",
            errors > 0 ? "bg-red-500" : "bg-amber-500"
          )}
        >
          {total > 99 ? "99+" : total}
        </span>
      ) : null}
    </Button>
  );
}

function FloatingViewSwitcher({
  pipelineId,
  selectedAssetId,
  currentView,
  search,
  onNewAsset,
}: {
  pipelineId: string;
  selectedAssetId: string;
  currentView: RedesignBuildView;
  search: RedesignBuildSearch;
  onNewAsset?: () => void;
}) {
  return (
    <div className="absolute right-1 top-1 z-20 flex items-center gap-2">
      {onNewAsset ? (
        <Button size="sm" onClick={onNewAsset} className="shadow-sm">
          <Plus className="size-3.5" /> New asset
        </Button>
      ) : null}
      <BuildViewButtonGroup pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={currentView} search={search} className="rounded-lg border bg-background/90 shadow-sm backdrop-blur" />
    </div>
  );
}

function BuildViewButtonGroup({
  pipelineId,
  selectedAssetId,
  currentView,
  search,
  className,
}: {
  pipelineId: string;
  selectedAssetId: string;
  currentView: RedesignBuildView;
  search: RedesignBuildSearch;
  className?: string;
}) {
  return (
    <ButtonGroup className={className}>
      <ViewLink pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={currentView} view="code" search={search} icon={FileCode} label="Code" />
      <ViewLink pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={currentView} view="split" search={search} icon={Columns2} label="Split" />
      <ViewLink pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={currentView} view="canvas" search={search} icon={Layers} label="Canvas" />
    </ButtonGroup>
  );
}

function ViewLink({
  pipelineId,
  selectedAssetId,
  currentView,
  view,
  search,
  icon: Icon,
  label,
}: {
  pipelineId: string;
  selectedAssetId: string;
  currentView: RedesignBuildView;
  view: RedesignBuildView;
  search: RedesignBuildSearch;
  icon: ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <Button asChild variant={currentView === view ? "secondary" : "outline"} size="icon-sm">
      <Link to={redesignAssetViewPath(view)} params={{ pipelineId, assetId: selectedAssetId }} search={search} aria-label={`${label} view`} title={`${label} view`}>
        <Icon className="size-3.5" />
        <span className="sr-only">{label}</span>
      </Link>
    </Button>
  );
}

export function redesignAssetViewPath(view: RedesignBuildView) {
  if (view === "split") return "/redesign/pipelines/$pipelineId/assets/$assetId/split" as const;
  if (view === "code") return "/redesign/pipelines/$pipelineId/assets/$assetId/code" as const;
  return "/redesign/pipelines/$pipelineId/assets/$assetId/canvas" as const;
}

export function redesignBuildViewFromPath(pathname: string): RedesignBuildView {
  if (pathname.endsWith("/split")) return "split";
  if (pathname.endsWith("/code")) return "code";
  return "canvas";
}

function Explorer({
  pipelineId,
  selectedAssetId,
  buildSearch,
  onAssetSelect,
  onAdhoc,
  onNewAsset,
  onPipelineSettings,
}: {
  pipelineId: string;
  selectedAssetId: string;
  buildSearch: RedesignBuildSearch;
  onAssetSelect: (assetId: string) => void;
  onAdhoc: () => void;
  onNewAsset: () => void;
  onPipelineSettings: () => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const navigate = useNavigate();
  const pipelineGroup = objectGroups.find((group) => group.id === "pipeline");
  const notebookGroup = objectGroups.find((group) => group.id === "notebook");
  const PipelineIcon = pipelineGroup?.icon ?? Layers;
  const NotebookIcon = notebookGroup?.icon ?? BookOpen;
  const { declaredDependencies, pipelineAssets } = useBuildContext();
  const adhocActive = buildSearch.editor === "adhoc";
  const pipelineItems = workspace?.pipelines.length
    ? workspace.pipelines
    : [{ id: "simple", name: "simple", path: "", assets: [] } satisfies WebPipeline];
  const notebookItems = workspace?.notebooks ?? [];
  const [creatingNotebook, setCreatingNotebook] = useState(false);
  const handleCreateNotebook = async () => {
    const title = window.prompt("New notebook title", "Exploration");
    if (title === null) {
      return;
    }
    setCreatingNotebook(true);
    try {
      const created = await createNotebook({ title: title.trim() || "Untitled" });
      await navigate({ to: "/redesign/notebooks/$notebookId", params: { notebookId: created.id } });
    } catch {
      // Surfaced on the notebook page; keep the explorer quiet.
    } finally {
      setCreatingNotebook(false);
    }
  };
  const assetsByGroup = pipelineAssets.reduce<Record<string, BuildAsset[]>>((groups, asset) => {
    const group = assetGroupName(asset);
    groups[group] = [...(groups[group] ?? []), asset];
    return groups;
  }, {});

  return (
    <>
      <DelimitedCardHeader>
        <Database className="size-4 text-primary" />
        <DelimitedCardTitle>Explorer</DelimitedCardTitle>
        <Button size="icon-sm" variant="ghost" className="ml-auto" onClick={onNewAsset}><Plus className="size-3.5" /></Button>
      </DelimitedCardHeader>
      <div className="border-b p-2">
        <div className="flex h-8 items-center gap-2 rounded-md border bg-background px-2 text-xs text-muted-foreground">
          <Search className="size-3.5" />
          <span>Filter assets...</span>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-2 p-2">
          <ExplorerSection label={pipelineGroup?.label ?? "Pipelines"} icon={PipelineIcon} count={pipelineItems.length}>
              {pipelineItems.map((item) => {
                const activePipeline = item.id === pipelineId;
                return (
                  <div key={item.id}>
                    <Link
                      to="/redesign/pipelines/$pipelineId/canvas"
                      params={{ pipelineId: item.id }}
                      search={buildSearch}
                      className={cn(
                        "flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs hover:bg-muted",
                        activePipeline ? "bg-muted text-foreground" : "text-muted-foreground"
                      )}
                    >
                      <PipelineIcon className="size-3.5 text-primary" />
                      <span className="truncate">{item.name || item.path || item.id}</span>
                    </Link>
                    {activePipeline ? (
                      <div className="mt-1 space-y-0.5 border-l pl-3 ml-3">
                        {Object.entries(assetsByGroup).length > 0 ? Object.entries(assetsByGroup).map(([group, groupAssets]) => (
                          <div key={group}>
                            <div className="px-2 py-1 font-mono text-[11px] text-muted-foreground">{group}/</div>
                            {groupAssets.map((asset) => <AssetButton key={asset.id} asset={asset} declaredDependencies={declaredDependencies} selected={!adhocActive && selectedAssetId === asset.id} onSelect={() => onAssetSelect(asset.id)} />)}
                          </div>
                        )) : <div className="px-2 py-1 text-xs text-muted-foreground">No assets found.</div>}
                        <div className="mt-1 border-t pt-1">
                          <button
                            className={cn(
                              "flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs hover:bg-muted",
                              adhocActive ? "bg-primary/10 text-foreground ring-1 ring-primary/20" : "text-muted-foreground"
                            )}
                            onClick={onAdhoc}
                          >
                            <Terminal className={cn("size-3.5", adhocActive ? "text-primary" : null)} /> Ad-hoc query
                          </button>
                          <button className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs text-muted-foreground hover:bg-muted" onClick={onPipelineSettings}>
                            <SettingsIcon /> Pipeline settings
                          </button>
                        </div>
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </ExplorerSection>
          <button onClick={onNewAsset} className="mt-2 flex h-8 w-full items-center gap-2 rounded-md border border-dashed px-2 text-left text-xs text-muted-foreground hover:bg-muted">
            <Plus className="size-3.5" /> New asset
          </button>

          <ExplorerSection label={notebookGroup?.label ?? "Notebooks"} icon={NotebookIcon} count={notebookItems.length}>
            {notebookItems.length > 0 ? (
              notebookItems.map((notebook) => (
                <Link
                  key={notebook.id}
                  to="/redesign/notebooks/$notebookId"
                  params={{ notebookId: notebook.id }}
                  className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs text-muted-foreground hover:bg-muted"
                  activeProps={{ className: "bg-muted text-foreground" }}
                >
                  <NotebookIcon className="size-3.5 text-primary" />
                  <span className="truncate">{notebook.title || notebook.path || notebook.id}</span>
                </Link>
              ))
            ) : (
              <div className="px-2 py-1 text-xs text-muted-foreground">No notebooks yet.</div>
            )}
          </ExplorerSection>
          <button
            onClick={() => void handleCreateNotebook()}
            disabled={creatingNotebook}
            className="mt-1 flex h-8 w-full items-center gap-2 rounded-md border border-dashed px-2 text-left text-xs text-muted-foreground hover:bg-muted disabled:opacity-50"
          >
            <Plus className="size-3.5" /> New notebook
          </button>
        </div>
      </ScrollArea>
    </>
  );
}

function ExplorerSection({
  label,
  icon: Icon,
  count,
  children,
}: {
  label: string;
  icon: ComponentType<{ className?: string }>;
  count: number;
  children: ReactNode;
}) {
  return (
    <div>
      <div className="flex h-7 items-center gap-1.5 rounded-md px-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        <Icon className="size-3.5" />{label}<span className="ml-auto">{count}</span>
      </div>
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}

function AssetButton({
  asset,
  declaredDependencies,
  selected,
  onSelect,
}: {
  asset: BuildAsset;
  declaredDependencies: string[];
  selected: boolean;
  onSelect: () => void;
}) {
  const Icon = kindMeta[asset.kind].icon;
  const missingCount = asset.kind === "python" ? missingPythonDependencies(asset, declaredDependencies).length : 0;
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs hover:bg-muted",
        selected ? "bg-primary/10 text-foreground ring-1 ring-primary/20" : "text-muted-foreground"
      )}
      >
        <Icon className="size-3.5 text-primary" />
      <span className="min-w-0 flex-1 truncate">{assetSidebarName(asset)}</span>
      {asset.staleness && asset.staleness.status !== "fresh" ? (
        <span
          title={`Staleness: ${stalenessLabel(asset.staleness)}`}
          className={cn("size-1.5 rounded-full", stalenessDotClassName(asset.staleness.status))}
        />
      ) : null}
      {missingCount > 0 ? <span title={`${missingCount} imports not in dependencies`} className="size-1.5 rounded-full bg-amber-500" /> : null}
    </button>
  );
}

function PipelineCanvas({ selectedAssetId, onAssetSelect }: { pipelineId: string; selectedAssetId: string; onAssetSelect: (assetId: string) => void }) {
  const { pipelineAssets, createDownstreamAsset, runAssetById, deleteAssetById, goToCatalog } = useBuildContext();
  return (
    <RedesignLineageCanvas
      assets={pipelineAssets}
      selectedAssetId={selectedAssetId}
      onAssetSelect={onAssetSelect}
      onRunAsset={runAssetById}
      onDeleteAsset={deleteAssetById}
      onGoToAsset={(assetId) => goToCatalog(assetId)}
      goToLabel="Open in catalog"
      onCreateDownstream={(assetId) => {
        const source = pipelineAssets.find((asset) => asset.id === assetId);
        if (source) {
          createDownstreamAsset({ id: source.id, name: source.name });
        }
      }}
    />
  );
}

export function RedesignBuildCanvasView() {
  const { pipelineId, selectedAssetId, selectAsset } = useBuildContext();
  return <PipelineCanvas pipelineId={pipelineId} selectedAssetId={selectedAssetId} onAssetSelect={selectAsset} />;
}

export function RedesignBuildSplitView() {
  const { pipelineId, selectedAssetId, selectedAsset, selectAsset, editorMode } = useBuildContext();
  return (
    <PanelGroup orientation="horizontal" className="h-full min-h-0 min-w-0">
      <Panel defaultSize={50} minSize={28} className="min-w-0">
        <EditorWorkspace asset={selectedAsset} adhoc={editorMode === "adhoc"} />
      </Panel>
      <PanelResizeHandle className="w-px bg-border" />
      <Panel defaultSize={50} minSize={28} className="min-w-0">
        <PipelineCanvas pipelineId={pipelineId} selectedAssetId={selectedAssetId} onAssetSelect={selectAsset} />
      </Panel>
    </PanelGroup>
  );
}

export function RedesignBuildCodeView() {
  const { selectedAsset, editorMode } = useBuildContext();
  return <EditorWorkspace asset={selectedAsset} adhoc={editorMode === "adhoc"} />;
}

function EditorWorkspace({
  asset,
  adhoc,
}: {
  asset: BuildAsset;
  adhoc: boolean;
}) {
  const {
    pipelineId,
    selectedAssetId,
    view,
    buildSearch,
    declaredDependencies,
    addDependency,
    goToAsset,
    openBottom,
    materializeSelectedAsset,
    inspectSelectedAsset,
    materializeLoading,
    inspectLoading,
    executionBlocked,
  } = useBuildContext();
  const isMobile = useIsMobile();
  const editorOnly = view === "code";
  const showActionLabels = editorOnly && !isMobile;
  // Asset-properties cards live in an on-demand side sheet (overlay) rather than
  // an inline panel, so they never steal editor width on desktop and stay
  // reachable on mobile. Closed by default.
  const [metadataOpen, setMetadataOpen] = useState(false);
  // Two presentations of the same editable metadata: focused cards, or an
  // interactive YAML-shaped view. Both drive the same asset API.
  const [propsView, setPropsView] = useState<"cards" | "yaml">("cards");

  if (adhoc) {
    return <AdhocEditor showActionLabels={showActionLabels} />;
  }

  // Real workspace assets get the live missing-dependency banner from
  // RedesignAssetEditor (driven by the asset's actual requirements.txt); this
  // mock affordance only covers the demo/sample assets that have no editor.
  const missingDependencies = !asset.workspaceAsset && asset.kind === "python" ? missingPythonDependencies(asset, declaredDependencies) : [];
  const actionLabel = asset.kind === "source" ? "Validate" : asset.kind === "ingestr" || asset.kind === "sling" ? "Run" : "Materialize";
  const filename = asset.path ?? `${asset.dir ? `${asset.dir}/` : ""}${asset.name}${kindMeta[asset.kind].ext}`;

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      <EditorFilenameHeader filename={filename}>
        <EditorActionButtons
          actionLabel={actionLabel}
          showLabels={showActionLabels}
          showInspect={asset.kind !== "source"}
          onRun={materializeSelectedAsset}
          onInspect={inspectSelectedAsset}
          runDisabled={materializeLoading || executionBlocked || !asset.workspaceAsset}
          runBlockedReason={executionBlocked ? "This environment is protected: interactive execution is disabled" : undefined}
          runLoading={materializeLoading}
          inspectDisabled={inspectLoading || !asset.workspaceAsset}
          inspectLoading={inspectLoading}
        />
        {asset.workspaceAsset ? (
          <Button
            variant="ghost"
            size="xs"
            className={metadataOpen ? "text-foreground" : "text-muted-foreground"}
            onClick={() => setMetadataOpen((open) => !open)}
            title="Asset properties"
            aria-label="Asset properties"
            aria-pressed={metadataOpen}
          >
            <PanelRight className="size-3.5" />
            {showActionLabels ? <span className="ml-1">Properties</span> : null}
          </Button>
        ) : null}
        {editorOnly ? (
          <BuildViewButtonGroup pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={view} search={buildSearch} />
        ) : null}
      </EditorFilenameHeader>
      {missingDependencies.length > 0 ? (
        <Button variant="outline" size="xs" className="absolute left-3 top-9 z-20 border-amber-300 bg-amber-50 text-amber-700 shadow-sm hover:bg-amber-100" onClick={() => openBottom("diagnostics")}>
          <AlertTriangle className="size-3" />{missingDependencies.length} not in deps
        </Button>
      ) : null}
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col">
          {asset.workspaceAsset && asset.pipelineId && asset.workspaceAsset.type.toLowerCase() === "sling" ? (
            <SlingParametersEditor asset={asset.workspaceAsset} pipelineId={asset.pipelineId} />
          ) : asset.workspaceAsset && asset.pipelineId ? (
            <RedesignAssetEditor
              asset={asset.workspaceAsset}
              pipelineId={asset.pipelineId}
              onInspect={inspectSelectedAsset}
              onGoToAsset={goToAsset}
            />
          ) : (
            <CodeBlock
              lines={editorLinesFor(asset)}
              asset={asset}
              declaredDependencies={declaredDependencies}
              onAddDependency={addDependency}
            />
          )}
        </div>
      </div>
      {asset.workspaceAsset && asset.pipelineId ? (
        <Sheet open={metadataOpen} onOpenChange={setMetadataOpen}>
          <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-md">
            <SheetTitle className="sr-only">Asset properties</SheetTitle>
            <div className="flex shrink-0 items-center gap-2 border-b py-2 pl-3 pr-12">
              <span className="text-xs font-medium text-muted-foreground">Asset properties</span>
              <div className="ml-auto flex overflow-hidden rounded-md border text-[11px]">
                {(["cards", "yaml"] as const).map((option) => (
                  <button
                    key={option}
                    type="button"
                    className={cn(
                      "px-2 py-0.5 capitalize transition-colors",
                      propsView === option ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted"
                    )}
                    aria-pressed={propsView === option}
                    onClick={() => setPropsView(option)}
                  >
                    {option}
                  </button>
                ))}
              </div>
            </div>
            {propsView === "cards" ? (
              <AssetGuidedCards asset={asset.workspaceAsset} pipelineId={asset.pipelineId} />
            ) : (
              <AssetYamlEditor asset={asset.workspaceAsset} pipelineId={asset.pipelineId} />
            )}
          </SheetContent>
        </Sheet>
      ) : null}
    </div>
  );
}

function EditorFilenameHeader({ filename, children }: { filename: string; children?: ReactNode }) {
  return (
    <div className="flex h-10 min-w-0 shrink-0 items-center gap-2 overflow-hidden border-b bg-background/70 px-3">
      <span className="block min-w-0 flex-[1_1_0] truncate font-mono text-[11px] text-muted-foreground">{filename}</span>
      {children ? <div className="ml-auto flex shrink-0 items-center gap-1.5">{children}</div> : null}
    </div>
  );
}

function EditorActionButtons({
  actionLabel,
  showLabels,
  showInspect,
  onRun,
  onInspect,
  runDisabled = false,
  runBlockedReason,
  runLoading = false,
  inspectDisabled = false,
  inspectLoading = false,
}: {
  actionLabel: string;
  showLabels: boolean;
  showInspect: boolean;
  onRun: () => void;
  onInspect: () => void;
  runDisabled?: boolean;
  runBlockedReason?: string;
  runLoading?: boolean;
  inspectDisabled?: boolean;
  inspectLoading?: boolean;
}) {
  const runLabel = runLoading ? "Running..." : actionLabel;
  const inspectLabel = inspectLoading ? "Loading..." : "Inspect";
  return (
    <>
      <Button
        size={showLabels ? "sm" : "icon-sm"}
        onClick={onRun}
        disabled={runDisabled}
        aria-label={actionLabel}
        title={runBlockedReason ?? actionLabel}
      >
        <Hammer className="size-3.5" />
        {showLabels ? runLabel : <span className="sr-only">{runLabel}</span>}
      </Button>
      {showInspect ? (
        <Button
          variant="outline"
          size={showLabels ? "sm" : "icon-sm"}
          onClick={onInspect}
          disabled={inspectDisabled}
          aria-label="Inspect"
          title="Inspect"
        >
          <Eye className="size-3.5" />
          {showLabels ? inspectLabel : <span className="sr-only">{inspectLabel}</span>}
        </Button>
      ) : null}
    </>
  );
}

function AdhocEditor({ showActionLabels }: { showActionLabels: boolean }) {
  const {
    pipelineId,
    selectedAssetId,
    view,
    buildSearch,
    adhocContextAsset,
    adhocLoading,
    runAdhocQuery,
    goToAsset,
  } = useBuildContext();
  return (
    <div className="relative flex h-full min-h-0 flex-col">
      <EditorFilenameHeader filename="Ad-hoc query">
        <Button
          size={showActionLabels ? "sm" : "icon-sm"}
          onClick={runAdhocQuery}
          disabled={adhocLoading || !adhocContextAsset}
          aria-label="Run"
          title="Run (⌘ + ↵)"
        >
          <Play className="size-3.5" />
          {showActionLabels ? (adhocLoading ? "Running..." : "Run") : <span className="sr-only">Run</span>}
        </Button>
        {view === "code" ? (
          <BuildViewButtonGroup pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={view} search={buildSearch} />
        ) : null}
      </EditorFilenameHeader>
      {adhocContextAsset ? (
        <RedesignAdhocEditor
          pipelineId={pipelineId}
          contextAsset={adhocContextAsset}
          onRunQuery={runAdhocQuery}
          onGoToAsset={goToAsset}
        />
      ) : (
        <CodeBlock lines={["select * from revenue_daily limit 100;"]} />
      )}
    </div>
  );
}

function CodeBlock({
  lines,
  asset,
  declaredDependencies = [],
  onAddDependency,
}: {
  lines: string[];
  asset?: BuildAsset;
  declaredDependencies?: string[];
  onAddDependency?: (dependency: string) => void;
}) {
  const isPython = asset?.kind === "python";
  return (
    <ScrollArea className="min-h-0 flex-1 bg-zinc-950 font-mono text-xs text-zinc-100">
      <div className="py-3">
        {lines.map((line, index) => {
          const importName = isPython ? parsePythonImport(line) : null;
          const dependency = importName ? packageForImport(importName) : null;
          const missing = Boolean(dependency && !declaredDependencies.includes(dependency));
          return (
          <div key={index} className={cn("flex min-h-5 items-center", missing ? "bg-amber-500/10 shadow-[inset_2px_0_0_#f59e0b]" : null)}>
            <span className={cn("w-11 shrink-0 select-none pr-3 text-right", missing ? "text-amber-400" : "text-zinc-500")}>{index + 1}</span>
            <pre className="min-w-0 whitespace-pre">{line}</pre>
            {missing && dependency ? (
              <Button variant="outline" size="xs" className="ml-3 h-5 border-amber-300 bg-amber-50 px-1.5 text-[10px] text-amber-700 hover:bg-amber-100" onClick={() => onAddDependency?.(dependency)}>
                <Package className="size-3" />add {dependency}
              </Button>
            ) : null}
          </div>
          );
        })}
      </div>
    </ScrollArea>
  );
}

function ResultsPanel({
  activeTab,
  onTabChange,
  collapsed,
  onToggleCollapse,
  variant,
  history,
  typeCheckReport,
  typeCheckLoading,
  onRunTypeCheck,
  onSelectAsset,
  onHistoryOpen,
  inspectResult,
  inspectLoading,
  canLoadMoreInspectRows,
  onLoadMoreInspectRows,
  selectedMaterializeEntry,
  materializeOutputHtml,
  pipelineMaterializeLoading,
  adhocResult,
  adhocRenderedQuery,
  adhocLoading,
}: {
  activeTab: RedesignResultTab;
  onTabChange: (tab: RedesignResultTab) => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
  variant: string;
  history: Array<{ id: number; kind: string; target: string; status: string; time: string; variant: string }>;
  typeCheckReport?: PipelineTypeCheckReport | null;
  typeCheckLoading?: boolean;
  onRunTypeCheck?: () => void;
  onSelectAsset?: (assetId: string) => void;
  onHistoryOpen: (tab: RedesignResultTab) => void;
  inspectResult: AssetInspectResponse | null;
  inspectLoading: boolean;
  canLoadMoreInspectRows: boolean;
  onLoadMoreInspectRows: () => void;
  selectedMaterializeEntry: MaterializeHistoryEntry | null;
  materializeOutputHtml: string | null;
  pipelineMaterializeLoading: boolean;
  adhocResult: SqlQueryResponse | null;
  adhocRenderedQuery: string | null;
  adhocLoading: boolean;
}) {
  return (
    <RedesignPanel className="flex h-full min-h-0 flex-col">
        <Tabs value={activeTab} onValueChange={(value) => { if (resultTabs.includes(value as RedesignResultTab)) onTabChange(value as RedesignResultTab); }} className="flex h-full min-h-0 flex-col">
        <DelimitedCardHeader className="min-h-9 gap-1 py-1">
          <ScrollArea className="min-w-0 flex-1" horizontalScrollBarClassName="hidden" viewportClassName="w-full">
            <TabsList className={scrollableTabsListClass}>
              <TabsTrigger value="inspect" className={scrollableTabsTriggerClass}><Table2 className="size-3.5" />Inspect</TabsTrigger>
              <TabsTrigger value="materialize" className={scrollableTabsTriggerClass}><Hammer className="size-3.5" />Materialize</TabsTrigger>
              <TabsTrigger value="query" className={scrollableTabsTriggerClass}><Terminal className="size-3.5" />Query</TabsTrigger>
              <TabsTrigger value="typecheck" className={scrollableTabsTriggerClass}>
                <Bell className="size-3.5" />Type check
                {typeCheckReport && typeCheckReport.summary.errors + typeCheckReport.summary.warnings > 0 ? (
                  <span className={cn(
                    "ml-1 rounded-full px-1 text-[10px] text-white",
                    typeCheckReport.summary.errors > 0 ? "bg-red-500" : "bg-amber-500"
                  )}>{typeCheckReport.summary.errors + typeCheckReport.summary.warnings}</span>
                ) : null}
              </TabsTrigger>
              <TabsTrigger value="history" className={scrollableTabsTriggerClass}><History className="size-3.5" />History{history.length > 0 ? <span className="ml-1 text-[10px] text-muted-foreground">{history.length}</span> : null}</TabsTrigger>
            </TabsList>
          </ScrollArea>
          <Button
            variant="ghost"
            size="icon-sm"
            className="shrink-0"
            onClick={onToggleCollapse}
            aria-label={collapsed ? "Expand results panel" : "Collapse results panel"}
            title={collapsed ? "Expand" : "Collapse"}
          >
            {collapsed ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
          </Button>
        </DelimitedCardHeader>
        <TabsContent value="inspect" className="flex min-h-0 flex-1 flex-col overflow-hidden p-0">
          {inspectLoading && !inspectResult ? (
            <ResultsLoading label="Inspecting asset..." />
          ) : inspectResult?.error ? (
            <div className="flex h-full min-h-0 items-center justify-center overflow-auto p-3">
              <InspectWarningCard message={inspectResult.error} testId="redesign-inspect-warning" />
            </div>
          ) : inspectResult ? (
            <>
              <RenderedQueryDisclosure query={inspectResult.operation?.query} />
              <div className="min-h-0 flex-1">
                <AssetInspectView
                  columns={inspectResult.columns ?? []}
                  rows={inspectResult.rows ?? []}
                  loading={inspectLoading}
                  canLoadMore={canLoadMoreInspectRows}
                  onLoadMore={onLoadMoreInspectRows}
                  warning={inspectResult.warning}
                  frameless
                />
              </div>
            </>
          ) : (
            <ResultsEmpty label="Inspect an asset to preview its data here." />
          )}
        </TabsContent>
        <TabsContent value="materialize" className="min-h-0 flex-1 overflow-hidden p-2">
          <WorkspaceMaterializeOutputView
            entry={selectedMaterializeEntry}
            outputHtml={materializeOutputHtml ?? ""}
            pipelineMaterializeLoading={pipelineMaterializeLoading}
          />
        </TabsContent>
        <TabsContent value="query" className="flex min-h-0 flex-1 flex-col overflow-hidden p-0">
          {adhocLoading ? (
            <ResultsLoading label="Running query..." />
          ) : adhocResult?.error ? (
            <div className="flex h-full min-h-0 items-center justify-center overflow-auto p-3">
              <InspectWarningCard message={adhocResult.error} testId="redesign-query-warning" />
            </div>
          ) : adhocResult ? (
            <>
              <RenderedQueryDisclosure query={adhocRenderedQuery} />
              <div className="min-h-0 flex-1">
                <AssetInspectView
                  columns={adhocResult.columns ?? []}
                  rows={(adhocResult.rows ?? []) as Record<string, unknown>[]}
                  warning={adhocResult.truncated ? "Result truncated; showing the first rows only." : undefined}
                  frameless
                />
              </div>
            </>
          ) : (
            <ResultsEmpty label="Run an ad hoc query to see results here." />
          )}
        </TabsContent>
        <TabsContent value="tests" className="min-h-0 flex-1 overflow-auto p-3"><UnitTests /></TabsContent>
        <TabsContent value="diagnostics" className="min-h-0 flex-1 overflow-auto p-0"><DiagnosticsList /></TabsContent>
        <TabsContent value="metadata" className="min-h-0 flex-1 overflow-auto p-0"><MetadataPanel /></TabsContent>
        <TabsContent value="shell" className="min-h-0 flex-1 overflow-hidden p-0"><ShellPanel variant={variant} /></TabsContent>
        <TabsContent value="typecheck" className="min-h-0 flex-1 overflow-auto p-0">
          <TypeCheckPanel report={typeCheckReport ?? null} loading={Boolean(typeCheckLoading)} onRun={onRunTypeCheck} onSelectAsset={onSelectAsset} />
        </TabsContent>
        <TabsContent value="history" className="min-h-0 flex-1 overflow-auto p-0"><HistoryPanel history={history} onOpen={onHistoryOpen} /></TabsContent>
      </Tabs>
    </RedesignPanel>
  );
}

// RenderedQueryDisclosure shows the query that actually ran (post-Jinja) as a
// single collapsed line above a results table, expandable to the full text.
function RenderedQueryDisclosure({ query }: { query?: string | null }) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const trimmed = query?.trim();
  if (!trimmed) {
    return null;
  }

  const copyQuery = async () => {
    try {
      await navigator.clipboard.writeText(trimmed);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      // Clipboard access denied; nothing to do.
    }
  };

  return (
    <div className="shrink-0 border-b bg-muted/30" data-testid="rendered-query-disclosure">
      <div className="flex min-w-0 items-center">
        <button
          type="button"
          className="flex h-8 min-w-0 flex-1 items-center gap-1.5 px-2 text-left text-[11px] text-muted-foreground hover:bg-muted"
          onClick={() => setOpen((value) => !value)}
          aria-expanded={open}
        >
          <ChevronRight className={cn("size-3 shrink-0 transition-transform", open ? "rotate-90" : null)} />
          <Terminal className="size-3 shrink-0" />
          <span className="shrink-0 font-semibold uppercase tracking-wide">Query</span>
          {!open ? (
            <span className="min-w-0 flex-1 truncate font-mono">{trimmed.replace(/\s+/g, " ")}</span>
          ) : null}
        </button>
        <Button
          variant="outline"
          size="xs"
          className="mr-2 h-6 shrink-0 px-1.5 text-[10px] text-muted-foreground"
          onClick={() => void copyQuery()}
          aria-label="Copy rendered query"
        >
          {copied ? "copied" : "copy"}
        </Button>
      </div>
      {open ? (
        <pre className="max-h-28 overflow-auto whitespace-pre-wrap border-t bg-background px-2 py-1.5 font-mono text-[11px]">{trimmed}</pre>
      ) : null}
    </div>
  );
}

function ResultsLoading({ label }: { label: string }) {
  return (
    <div className="flex h-full min-h-0 items-center justify-center bg-background">
      <div className="flex items-center gap-2 text-xs opacity-80">
        <Spinner className="size-4" />
        <span>{label}</span>
      </div>
    </div>
  );
}

function ResultsEmpty({ label }: { label: string }) {
  return (
    <div className="flex h-full min-h-0 items-center justify-center bg-background px-4 text-center text-xs text-muted-foreground">
      {label}
    </div>
  );
}

function TypeCheckPanel({
  report,
  loading,
  onRun,
  onSelectAsset,
}: {
  report: PipelineTypeCheckReport | null;
  loading: boolean;
  onRun?: () => void;
  onSelectAsset?: (assetId: string) => void;
}) {
  if (loading && !report) {
    return <ResultsLoading label="Type checking pipeline…" />;
  }
  if (!report) {
    return (
      <div className="flex h-full min-h-0 flex-col items-center justify-center gap-3 bg-background text-xs text-muted-foreground">
        <span>Type check assets for column and type errors.</span>
        <Button size="sm" variant="outline" onClick={onRun}><Bell className="size-3.5" />Run type check</Button>
      </div>
    );
  }

  const flagged = report.assets.filter((asset) => asset.findings.length > 0);
  const checkedAt = report.start_date ? new Date(report.start_date) : null;

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5 text-xs">
        <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400"><XCircle className="size-3.5" />{report.summary.errors}</span>
        <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400"><AlertTriangle className="size-3.5" />{report.summary.warnings}</span>
        <span className="text-muted-foreground">{report.summary.assets} asset{report.summary.assets === 1 ? "" : "s"} checked</span>
        {checkedAt ? <span className="hidden text-muted-foreground/70 sm:inline">· window {checkedAt.toISOString().slice(0, 10)}</span> : null}
        <Button size="xs" variant="outline" className="ml-auto" onClick={onRun} disabled={loading}>
          {loading ? <Loader2 className="size-3 animate-spin" /> : <RotateCw className="size-3" />}Re-run
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {flagged.length === 0 ? (
          <div className="flex items-center gap-2 px-2 py-3 text-xs text-emerald-600 dark:text-emerald-400">
            <CheckCircle2 className="size-4" />No type errors found across {report.summary.assets} asset{report.summary.assets === 1 ? "" : "s"}.
          </div>
        ) : (
          <div className="space-y-2">
            {flagged.map((asset) => (
              <div key={asset.name} className="rounded-md border">
                <button
                  type="button"
                  className="flex w-full items-center gap-2 border-b bg-muted/30 px-2.5 py-1.5 text-left text-xs hover:bg-muted disabled:cursor-default"
                  onClick={() => asset.id && onSelectAsset?.(asset.id)}
                  disabled={!asset.id}
                >
                  {asset.status === "error" ? <XCircle className="size-3.5 shrink-0 text-red-500" /> : <AlertTriangle className="size-3.5 shrink-0 text-amber-500" />}
                  <span className="min-w-0 flex-1 truncate font-mono font-medium">{asset.name}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{asset.type}</span>
                </button>
                <ul className="divide-y">
                  {asset.findings.map((finding, index) => (
                    <li key={index} className="flex items-start gap-2 px-2.5 py-1.5 text-xs">
                      {finding.severity === "error" ? <XCircle className="mt-0.5 size-3.5 shrink-0 text-red-500" /> : <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />}
                      <span className="min-w-0 flex-1">{finding.message}</span>
                      {finding.line ? <span className="shrink-0 font-mono text-[10px] text-muted-foreground">L{finding.line}:C{finding.column}</span> : null}
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function ShellPanel({ variant }: { variant: string }) {
  const [command, setCommand] = useState("");
  const [lines, setLines] = useState<Array<{ type: "cmd" | "out" | "ok" | "err"; value: string }>>([
    { type: "out", value: "Renart shell: runs against the active project, environment and variant. Type help." },
  ]);
  const prompt = `renart:simple(${variant}) $`;
  const runCommand = () => {
    const value = command.trim();
    if (!value) return;
    if (value === "clear") {
      setLines([]);
      setCommand("");
      return;
    }
    const output: typeof lines = [{ type: "cmd", value: `${prompt} ${value}` }];
    if (/^(renart|bruin)\s+run/.test(value)) output.push({ type: "ok", value: `2 assets executed for ${renderedPipelineName(variant)} in 111ms` });
    else if (/plan/.test(value)) output.push({ type: "out", value: "Changes: 1 breaking, 1 non-breaking; 3 to backfill. Apply with renart plan --apply." });
    else if (/test/.test(value)) output.push({ type: "out", value: "2 passed, 1 failed: nulls_count_as_zero." });
    else if (/list-variants|variants/.test(value)) output.push({ type: "out", value: pipelineVariants.map((item) => item.id).join(" · ") });
    else if (value === "help") output.push({ type: "out", value: "run · plan · validate · test · asset · metadata · schedule · git · internal list-variants · clear" });
    else output.push({ type: "err", value: `command not found: ${value.split(" ")[0]}` });
    setLines((current) => [...current, ...output]);
    setCommand("");
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-zinc-950 font-mono text-xs text-zinc-100">
      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-1 p-3">
          {lines.map((line, index) => <div key={index} className={cn(line.type === "ok" ? "text-emerald-400" : line.type === "err" ? "text-red-400" : line.type === "cmd" ? "text-zinc-200" : "text-zinc-400")}>{line.value}</div>)}
        </div>
      </ScrollArea>
      <div className="flex items-center gap-2 border-t border-zinc-800 px-3 py-2">
        <span className="text-emerald-400">{prompt}</span>
        <input className="min-w-0 flex-1 bg-transparent outline-none" value={command} onChange={(event) => setCommand(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") runCommand(); }} />
      </div>
    </div>
  );
}

function HistoryPanel({
  history,
  onOpen,
}: {
  history: Array<{ id: number; kind: string; target: string; status: string; time: string; variant: string }>;
  onOpen: (tab: RedesignResultTab) => void;
}) {
  if (history.length === 0) {
    return <div className="p-4 text-xs text-muted-foreground">No local actions yet. Run, inspect, query, or test something to populate history.</div>;
  }
  return (
    <div>
      {history.map((item) => (
        <button key={item.id} className="flex w-full items-center gap-3 border-b px-3 py-2 text-left text-xs hover:bg-muted" onClick={() => onOpen(item.kind === "test" ? "tests" : item.kind === "query" ? "query" : item.kind === "inspect" ? "inspect" : "materialize")}>
          <History className="size-3.5 text-muted-foreground" />
          <span className="font-mono text-primary">{item.kind}</span>
          <span className="min-w-0 flex-1 truncate font-mono">{item.target}</span>
          <span className="font-mono text-muted-foreground">{item.variant}</span>
          <span className="text-muted-foreground">{item.time}</span>
        </button>
      ))}
    </div>
  );
}

function Inspector({
  asset,
  declaredDependencies,
  addedDependencies,
  onAddDependency,
  onOpenPipelineSettings,
  onOpenResults,
}: {
  asset: BuildAsset;
  declaredDependencies: string[];
  addedDependencies: string[];
  onAddDependency: (dependency: string) => void;
  onOpenPipelineSettings: () => void;
  onOpenResults: (tab: RedesignResultTab) => void;
}) {
  const missingDependencies = asset.kind === "python" ? missingPythonDependencies(asset, declaredDependencies) : [];
  const [fullEditorOpen, setFullEditorOpen] = useState(false);
  return (
    <>
      <DelimitedCardHeader>
        <Sliders className="size-4 text-primary" />
        <div className="min-w-0">
          <DelimitedCardTitle>{asset.name}</DelimitedCardTitle>
          <p className="truncate text-[11px] text-muted-foreground">{asset.path ?? "asset"} · {asset.integration}</p>
        </div>
      </DelimitedCardHeader>
      <Tabs defaultValue="config" className="min-h-0 flex-1">
        <ScrollArea className="mx-3 mt-3 max-w-[calc(100%-1.5rem)]" horizontalScrollBarClassName="hidden" viewportClassName="w-full">
          <TabsList className={scrollableTabsListClass}>
            <TabsTrigger value="config" className={scrollableTabsTriggerClass}>Config</TabsTrigger>
            <TabsTrigger value="deps" className={scrollableTabsTriggerClass}>Lineage</TabsTrigger>
            <TabsTrigger value="tests" className={scrollableTabsTriggerClass}>Tests</TabsTrigger>
            <TabsTrigger value="schema" className={scrollableTabsTriggerClass}>Schema</TabsTrigger>
            <TabsTrigger value="preview" className={scrollableTabsTriggerClass}>Preview</TabsTrigger>
            {asset.kind === "python" ? <TabsTrigger value="python" className={scrollableTabsTriggerClass}>Python</TabsTrigger> : null}
          </TabsList>
        </ScrollArea>
        <ScrollArea className="min-h-0 flex-1">
          <TabsContent value="config" className="space-y-4 p-3 text-sm">
            <div className="space-y-3 text-xs">
              <div className="flex items-center justify-between">
                <span className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Definition</span>
                <Button variant="outline" size="xs" onClick={() => setFullEditorOpen(true)}><Sliders className="size-3" />Edit</Button>
              </div>
              <Field label="Name" value={asset.workspaceAsset?.name ?? (asset.dir ? `${asset.dir}.${asset.name}` : asset.name)} />
              <Field label="Type" value={asset.type ?? kindMeta[asset.kind].label} />
              <Field label="Materialization" value={asset.workspaceAsset?.materialization_type || (asset.kind === "source" ? "none" : "view")} />
              <Field label="Owner" value="team@acme.io" />
              <div className="flex items-center justify-between"><span className="text-muted-foreground">Connection</span><IntegrationBadge name={asset.connection || asset.integration} /></div>
              <div className="flex items-center justify-between"><span className="text-muted-foreground">Tags</span><span className="rounded-md border px-1.5 py-0.5 text-[11px]">core</span></div>
              <label className="block space-y-1.5">
                <span className="text-xs text-muted-foreground">Description</span>
                <Input defaultValue={asset.description} />
              </label>
              <Button
                variant="link"
                size="xs"
                className="h-auto w-full justify-start whitespace-normal p-0 text-left leading-snug"
                onClick={() => setFullEditorOpen(true)}
              >
                Open full editor: depends, columns, checks, hooks, meta, raw YAML
              </Button>
            </div>
          </TabsContent>
          <TabsContent value="deps" className="space-y-4 p-3 text-sm">
            <div className="space-y-4">
              <div>
                <div className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground"><Network className="size-3.5" />Upstream</div>
                <DependencyList names={asset.upstreams?.length ? asset.upstreams : ["No upstream assets"]} />
              </div>
              <div>
                <div className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground"><Network className="size-3.5" />Downstream</div>
                <DependencyList names={["predicted_orders", "model_revenue"]} />
              </div>
            </div>
          </TabsContent>
          <TabsContent value="tests" className="p-3"><UnitTests compact onOpenResults={() => onOpenResults("tests")} /></TabsContent>
          <TabsContent value="schema" className="p-3"><MetadataPanel compact onFull={() => onOpenResults("metadata")} /></TabsContent>
          <TabsContent value="preview" className="space-y-3 p-3 text-sm">
            <ToggleCard title="Show data preview on node" description="Render sample rows in the canvas card." />
            <label className="block space-y-1.5 text-xs text-muted-foreground">Row limit<Input defaultValue="5" /></label>
          </TabsContent>
          <TabsContent value="python" className="space-y-3 p-3">
            <SectionCard title="Requirements" icon={Cpu}>
              <div className="space-y-3">
                <SimpleTable columns={["Package", "Status"]} rows={declaredDependencies.map((dependency) => [<span key={dependency} className="font-mono">{dependency}</span>, addedDependencies.includes(dependency) ? "added in editor" : "declared"])} />
                {missingDependencies.length > 0 ? (
                  <div className="rounded-lg border border-amber-300 bg-amber-50 p-2 text-xs text-amber-800">
                    <div className="mb-2 flex items-center gap-1.5 font-medium"><AlertTriangle className="size-3.5" />Missing imports</div>
                    <div className="space-y-1">
                      {missingDependencies.map((dependency) => (
                        <div key={dependency} className="flex items-center gap-2">
                          <span className="min-w-0 flex-1 truncate font-mono">{dependency}</span>
                          <Button size="xs" variant="outline" className="h-6" onClick={() => onAddDependency(dependency)}>Add</Button>
                        </div>
                      ))}
                    </div>
                  </div>
                ) : <p className="text-xs text-muted-foreground">All detected imports are covered by pipeline dependencies.</p>}
                <Button variant="outline" size="xs" onClick={onOpenPipelineSettings}><Package className="size-3" />Manage pipeline deps</Button>
              </div>
            </SectionCard>
          </TabsContent>
        </ScrollArea>
      </Tabs>
      <FullAssetEditorDialog open={fullEditorOpen} onOpenChange={setFullEditorOpen} asset={asset} />
    </>
  );
}

function FullAssetEditorDialog({
  open,
  onOpenChange,
  asset,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  asset: BuildAsset;
}) {
  const typeByKind: Record<string, string> = {
    sql: "duckdb.sql",
    python: "python",
    ingestr: "ingestr",
    sling: "ingestr",
    source: "duckdb.source",
    unittest: "duckdb.sql",
  };
  const ext = kindMeta[asset.kind].ext;
  const inline = asset.kind === "sql" || asset.kind === "python";
  const wrap = asset.kind === "sql" ? ["/* @bruin", "@bruin */"] : asset.kind === "python" ? ['\"\"\" @bruin', '@bruin \"\"\"'] : null;
  const file = inline ? `assets/${asset.dir ?? "marts"}/${asset.name}${ext}` : `assets/${asset.dir ?? "marts"}/${asset.name}.asset.yml`;
  const type = typeByKind[asset.kind] ?? "duckdb.sql";
  const [name, setName] = useState(asset.dir ? `${asset.dir}.${asset.name}` : asset.name);
  const [description, setDescription] = useState(asset.description);
  const [owner, setOwner] = useState("team@acme.io");
  const [tags, setTags] = useState(["core"]);
  const [domains, setDomains] = useState(["sales"]);
  const [metaPairs, setMetaPairs] = useState<Array<[string, string]>>([["sla", "99.9%"]]);
  const [dependencies, setDependencies] = useState<Array<{ asset: string; mode: string }>>(asset.kind === "sql" ? [{ asset: "orders_cleaned", mode: "full" }] : []);
  const [materialization, setMaterialization] = useState(asset.kind === "source" || asset.kind === "python" ? "none" : "view");
  const [strategy, setStrategy] = useState("create+replace");
  const [objectName, setObjectName] = useState(asset.name);
  const [partitionBy, setPartitionBy] = useState("day");
  const [incrementalKey, setIncrementalKey] = useState("created_at");
  const [clusterBy, setClusterBy] = useState("region");
  const [intervalStart, setIntervalStart] = useState("");
  const [intervalEnd, setIntervalEnd] = useState("");
  const [cooldown, setCooldown] = useState("300");
  const [retries, setRetries] = useState("2");
  const [retryDelay, setRetryDelay] = useState("30s");
  const [timeout, setTimeoutValue] = useState("15m");
  const [priority, setPriority] = useState("normal");
  const [hooks, setHooks] = useState<Array<{ phase: string; command: string }>>([
    { phase: "pre", command: "query: \"INSTALL httpfs\"" },
    { phase: "post", command: "query: \"SET s3_region=''\"" },
  ]);
  const [columns, setColumns] = useState<Array<{ name: string; type: string; description: string; checks: string }>>(schemaRows.map((row) => ({ name: row.name, type: row.declared, description: row.description, checks: row.status === "match" ? "not_null" : "" })));
  const [customChecks, setCustomChecks] = useState<Array<{ name: string; query: string }>>([
    { name: "positive_revenue", query: "select * from revenue_daily where revenue < 0" },
  ]);
  const yaml = buildDefinitionYaml({ name, type, description, owner, tags, domains, metaPairs, dependencies, materialization, strategy, objectName, partitionBy, incrementalKey, clusterBy, intervalStart, intervalEnd, cooldown, retries, retryDelay, timeout, priority, hooks, columns, customChecks });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[90dvh] max-h-[90dvh] w-[94vw] flex-col overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="mb-0 flex h-14 shrink-0 justify-center border-b px-5 py-0">
          <DialogTitle className="flex min-w-0 items-center gap-2 text-base">
            <Sliders className="size-4 text-primary" />
            <span className="shrink-0">Edit definition</span>
            <span className="min-w-0 truncate font-mono text-xs font-normal text-muted-foreground">· {name}</span>
          </DialogTitle>
          <DialogDescription className="sr-only">Edit asset definition form fields or raw Bruin YAML.</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="form" className="min-h-0 flex-1 gap-0">
          <div className="flex h-11 shrink-0 items-center gap-3 border-b px-5">
            <TabsList>
              <TabsTrigger value="form">Form</TabsTrigger>
              <TabsTrigger value="yaml">YAML</TabsTrigger>
            </TabsList>
            <div className="min-w-0 flex-1 truncate text-right font-mono text-[11px] text-muted-foreground">
              {inline ? "inline in " : ""}{file}
            </div>
          </div>
          <TabsContent value="form" className="m-0 min-h-0 flex-1 overflow-hidden data-[state=inactive]:hidden">
            <ScrollArea className="h-[calc(90dvh-9.5rem)]" viewportClassName="h-full">
              <div className="flex flex-col gap-5 p-5">
                <EditorSection title="Identity">
                  <EditorTextField label="Name (schema.table)" value={name} onChange={setName} className="font-mono" />
                  <div className="grid gap-2 sm:grid-cols-2">
                    <EditorSelectField label="Type" value={type} options={[type]} />
                    <EditorSelectField label="Connection" value="duckdb-default" options={["duckdb-default", "bq-prod", "stripe"]} />
                  </div>
                  <EditorTextField label="Owner" value={owner} onChange={setOwner} />
                  <EditorTextareaField label="Description" value={description} onChange={setDescription} />
                </EditorSection>

                <EditorSection title="Classification">
                  <MultiSelectCombobox label="Tags" items={tags} options={["core", "finance", "daily", "source", "ml", "quality"]} onChange={setTags} />
                  <MultiSelectCombobox label="Domains" items={domains} options={["sales", "marketing", "product", "finance", "ops"]} onChange={setDomains} />
                  <KeyValueEditor pairs={metaPairs} onChange={setMetaPairs} />
                </EditorSection>

                <EditorSection title="Dependencies">
                  <DependencyRows dependencies={dependencies} onChange={setDependencies} />
                </EditorSection>

                <EditorSection title="Materialization">
                  <div className="grid gap-2 sm:grid-cols-2">
                    <EditorSelectField label="Type" value={materialization} options={["none", "view", "table"]} onChange={setMaterialization} />
                    {materialization === "table" ? <EditorSelectField label="Strategy" value={strategy} options={["create+replace", "append", "merge", "delete+insert", "time_interval"]} onChange={setStrategy} /> : null}
                    <EditorTextField label="Object name" value={objectName} onChange={setObjectName} className="font-mono" />
                    <EditorTextField label="Partition by" value={partitionBy} onChange={setPartitionBy} className="font-mono" />
                    <EditorTextField label="Incremental key" value={incrementalKey} onChange={setIncrementalKey} className="font-mono" />
                    <EditorTextField label="Cluster by" value={clusterBy} onChange={setClusterBy} className="font-mono" />
                  </div>
                </EditorSection>

                <EditorSection title="Scheduling & retries">
                  <div className="grid gap-2 sm:grid-cols-3">
                    <EditorTextField label="Interval start" value={intervalStart} onChange={setIntervalStart} placeholder="-2h" className="font-mono" />
                    <EditorTextField label="Interval end" value={intervalEnd} onChange={setIntervalEnd} placeholder="1h" className="font-mono" />
                    <EditorTextField label="Rerun cooldown (s)" value={cooldown} onChange={setCooldown} className="font-mono" />
                    <EditorTextField label="Retries" value={retries} onChange={setRetries} className="font-mono" />
                    <EditorTextField label="Retry delay" value={retryDelay} onChange={setRetryDelay} className="font-mono" />
                    <EditorTextField label="Timeout" value={timeout} onChange={setTimeoutValue} className="font-mono" />
                    <EditorSelectField label="Priority" value={priority} options={["low", "normal", "high", "critical"]} onChange={setPriority} />
                  </div>
                </EditorSection>

                <EditorSection title="Hooks">
                  <HookRows hooks={hooks} onChange={setHooks} />
                </EditorSection>

                <EditorSection title="Columns & checks">
                  <ColumnRows columns={columns} onChange={setColumns} />
                  <CustomCheckRows checks={customChecks} onChange={setCustomChecks} />
                </EditorSection>
              </div>
            </ScrollArea>
          </TabsContent>
          <TabsContent value="yaml" className="m-0 min-h-0 flex-1 overflow-hidden data-[state=inactive]:hidden">
            <ScrollArea className="h-[calc(90dvh-9.5rem)]" viewportClassName="h-full">
              <div className="p-5">
                <div className="mb-2 text-[11px] text-muted-foreground">
                  {inline ? "Written inline at the top of " : "Stored as "}<span className="font-mono">{file}</span>{inline ? " between the @bruin markers." : "."}
                </div>
                <pre className="overflow-auto rounded-lg bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-100">
                  {wrap ? <div className="text-zinc-500">{wrap[0]}</div> : null}
                  <div className="whitespace-pre">{yaml}</div>
                  {wrap ? <div className="text-zinc-500">{wrap[1]}</div> : null}
                  {inline ? <div className="whitespace-pre text-zinc-600">{"\n"}{asset.kind === "python" ? "# python code follows..." : "SELECT ..."}</div> : null}
                </pre>
              </div>
            </ScrollArea>
          </TabsContent>
        </Tabs>
        <DialogFooter className="mt-0 shrink-0 border-t px-5 py-3">
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => onOpenChange(false)}><CheckCircle2 className="size-4" />Save definition</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function EditorSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div>
      <div className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{title}</div>
      <div className="flex flex-col gap-2.5">{children}</div>
    </div>
  );
}

function EditorTextField({ label, value, onChange, placeholder, className }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; className?: string }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <Input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className={className} />
    </label>
  );
}

function EditorTextareaField({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <textarea className="min-h-20 w-full resize-none rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function EditorSelectField({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange?: (value: string) => void }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <select value={value} onChange={(event) => onChange?.(event.target.value)} className="h-9 w-full rounded-md border bg-background px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {options.map((option) => <option key={option} value={option}>{option}</option>)}
      </select>
    </label>
  );
}

function MultiSelectCombobox({ label, items, options, onChange }: { label: string; items: string[]; options: string[]; onChange: (items: string[]) => void }) {
  const [open, setOpen] = useState(false);
  const toggleItem = (item: string) => {
    onChange(items.includes(item) ? items.filter((value) => value !== item) : [...items, item]);
  };

  return (
    <div className="space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <div className="flex flex-wrap gap-1.5 rounded-md border bg-background p-1.5">
        {items.map((item) => (
          <Badge key={item} variant="outline" className="gap-1 font-mono">
            {item}
            <button className="text-muted-foreground hover:text-foreground" onClick={() => toggleItem(item)} type="button">×</button>
          </Badge>
        ))}
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button variant="outline" size="xs" className="h-6 border-dashed">
              + add <ChevronsUpDown className="size-3" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-72">
            <Command>
              <CommandInput placeholder={`Search ${label.toLowerCase()}...`} />
              <CommandList>
                <CommandEmpty>No option found.</CommandEmpty>
                <CommandGroup>
                  {options.map((option) => {
                    const selected = items.includes(option);
                    return (
                      <CommandItem key={option} value={option} onSelect={() => toggleItem(option)} data-checked={selected}>
                        <Check className={cn("size-4", selected ? "opacity-100" : "opacity-0")} />
                        <span className="font-mono">{option}</span>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}

function KeyValueEditor({ pairs, onChange }: { pairs: Array<[string, string]>; onChange: (pairs: Array<[string, string]>) => void }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">Meta (key / value)</span>
      <div className="space-y-1.5">
        {pairs.map(([key, value], index) => (
          <div key={index} className="flex items-center gap-1.5">
            <Input value={key} onChange={(event) => onChange(pairs.map((pair, pairIndex) => pairIndex === index ? [event.target.value, pair[1]] : pair))} placeholder="key" className="h-7 flex-1 font-mono text-xs" />
            <Input value={value} onChange={(event) => onChange(pairs.map((pair, pairIndex) => pairIndex === index ? [pair[0], event.target.value] : pair))} placeholder="value" className="h-7 flex-1 font-mono text-xs" />
            <Button variant="ghost" size="icon-sm" onClick={() => onChange(pairs.filter((_, pairIndex) => pairIndex !== index))}><XCircle className="size-3.5" /></Button>
          </div>
        ))}
        <Button variant="outline" size="xs" className="h-6 border-dashed" onClick={() => onChange([...pairs, ["", ""]])}>+ add pair</Button>
      </div>
    </label>
  );
}

function DependencyRows({ dependencies, onChange }: { dependencies: Array<{ asset: string; mode: string }>; onChange: (dependencies: Array<{ asset: string; mode: string }>) => void }) {
  return (
    <div className="space-y-1.5">
      {dependencies.map((dependency, index) => (
        <div key={index} className="flex items-center gap-1.5">
          <Input value={dependency.asset} onChange={(event) => onChange(dependencies.map((item, itemIndex) => itemIndex === index ? { ...item, asset: event.target.value } : item))} placeholder="upstream_asset" className="h-7 flex-1 font-mono text-xs" />
          <select value={dependency.mode} onChange={(event) => onChange(dependencies.map((item, itemIndex) => itemIndex === index ? { ...item, mode: event.target.value } : item))} className="h-7 rounded-md border bg-background px-1.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <option value="full">full</option>
            <option value="symbolic">symbolic</option>
          </select>
          <Button variant="ghost" size="icon-sm" onClick={() => onChange(dependencies.filter((_, itemIndex) => itemIndex !== index))}><XCircle className="size-3.5" /></Button>
        </div>
      ))}
      <Button variant="outline" size="xs" className="h-6 border-dashed" onClick={() => onChange([...dependencies, { asset: "", mode: "full" }])}>+ add dependency</Button>
    </div>
  );
}

function HookRows({ hooks, onChange }: { hooks: Array<{ phase: string; command: string }>; onChange: (hooks: Array<{ phase: string; command: string }>) => void }) {
  return (
    <div className="space-y-1.5">
      {hooks.map((hook, index) => (
        <div key={index} className="grid items-center gap-1.5 sm:grid-cols-[7rem_minmax(0,1fr)_2rem]">
          <select value={hook.phase} onChange={(event) => onChange(hooks.map((item, itemIndex) => itemIndex === index ? { ...item, phase: event.target.value } : item))} className="h-8 rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <option value="pre">pre</option>
            <option value="post">post</option>
            <option value="on_failure">on_failure</option>
          </select>
          <Input value={hook.command} onChange={(event) => onChange(hooks.map((item, itemIndex) => itemIndex === index ? { ...item, command: event.target.value } : item))} className="h-8 font-mono text-xs" />
          <Button variant="ghost" size="icon-sm" onClick={() => onChange(hooks.filter((_, itemIndex) => itemIndex !== index))}><XCircle className="size-3.5" /></Button>
        </div>
      ))}
      <Button variant="outline" size="xs" className="h-6 w-fit border-dashed" onClick={() => onChange([...hooks, { phase: "pre", command: "query: \"select 1\"" }])}><Plus className="size-3" />add hook</Button>
    </div>
  );
}

function ColumnRows({ columns, onChange }: { columns: Array<{ name: string; type: string; description: string; checks: string }>; onChange: (columns: Array<{ name: string; type: string; description: string; checks: string }>) => void }) {
  return (
    <div className="space-y-1.5">
      <div className="grid gap-1.5 text-[10px] font-semibold uppercase text-muted-foreground sm:grid-cols-[1fr_7rem_1fr_7rem_2rem]"><span>Name</span><span>Type</span><span>Description</span><span>Checks</span><span /></div>
      {columns.map((column, index) => (
        <div key={index} className="grid items-center gap-1.5 sm:grid-cols-[1fr_7rem_1fr_7rem_2rem]">
          <Input value={column.name} onChange={(event) => onChange(columns.map((item, itemIndex) => itemIndex === index ? { ...item, name: event.target.value } : item))} className="h-8 font-mono text-xs" />
          <Input value={column.type} onChange={(event) => onChange(columns.map((item, itemIndex) => itemIndex === index ? { ...item, type: event.target.value } : item))} className="h-8 font-mono text-xs" />
          <Input value={column.description} onChange={(event) => onChange(columns.map((item, itemIndex) => itemIndex === index ? { ...item, description: event.target.value } : item))} className="h-8 text-xs" />
          <Input value={column.checks} onChange={(event) => onChange(columns.map((item, itemIndex) => itemIndex === index ? { ...item, checks: event.target.value } : item))} placeholder="not_null" className="h-8 font-mono text-xs" />
          <Button variant="ghost" size="icon-sm" onClick={() => onChange(columns.filter((_, itemIndex) => itemIndex !== index))}><XCircle className="size-3.5" /></Button>
        </div>
      ))}
      <Button variant="outline" size="xs" className="h-6 w-fit border-dashed" onClick={() => onChange([...columns, { name: "", type: "", description: "", checks: "" }])}><Plus className="size-3" />add column</Button>
    </div>
  );
}

function CustomCheckRows({ checks, onChange }: { checks: Array<{ name: string; query: string }>; onChange: (checks: Array<{ name: string; query: string }>) => void }) {
  return (
    <div className="space-y-1.5">
      <div className="text-xs font-medium text-muted-foreground">Custom SQL checks</div>
      {checks.map((check, index) => (
        <div key={index} className="grid items-start gap-1.5 sm:grid-cols-[10rem_minmax(0,1fr)_2rem]">
          <Input value={check.name} onChange={(event) => onChange(checks.map((item, itemIndex) => itemIndex === index ? { ...item, name: event.target.value } : item))} className="h-8 font-mono text-xs" />
          <textarea value={check.query} onChange={(event) => onChange(checks.map((item, itemIndex) => itemIndex === index ? { ...item, query: event.target.value } : item))} className="min-h-16 rounded-md border bg-background px-2 py-1.5 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring" />
          <Button variant="ghost" size="icon-sm" onClick={() => onChange(checks.filter((_, itemIndex) => itemIndex !== index))}><XCircle className="size-3.5" /></Button>
        </div>
      ))}
      <Button variant="outline" size="xs" className="h-6 w-fit border-dashed" onClick={() => onChange([...checks, { name: "new_check", query: "select * from " }])}><Plus className="size-3" />add custom check</Button>
    </div>
  );
}

function buildDefinitionYaml({ name, type, description, owner, tags, domains, metaPairs, dependencies, materialization, strategy, objectName, partitionBy, incrementalKey, clusterBy, intervalStart, intervalEnd, cooldown, retries, retryDelay, timeout, priority, hooks, columns, customChecks }: { name: string; type: string; description: string; owner: string; tags: string[]; domains: string[]; metaPairs: Array<[string, string]>; dependencies: Array<{ asset: string; mode: string }>; materialization: string; strategy: string; objectName: string; partitionBy: string; incrementalKey: string; clusterBy: string; intervalStart: string; intervalEnd: string; cooldown: string; retries: string; retryDelay: string; timeout: string; priority: string; hooks: Array<{ phase: string; command: string }>; columns: Array<{ name: string; type: string; description: string; checks: string }>; customChecks: Array<{ name: string; query: string }> }) {
  const lines = [`name: ${name}`, `type: ${type}`, `owner: ${owner}`];
  if (description) lines.push(`description: ${description}`);
  if (tags.length) lines.push(`tags: [${tags.join(", ")}]`);
  if (domains.length) lines.push(`domains: [${domains.join(", ")}]`);
  if (metaPairs.length) {
    lines.push("meta:");
    metaPairs.forEach(([key, value]) => lines.push(`  ${key}: ${value}`));
  }
  if (dependencies.length) {
    lines.push("depends:");
    dependencies.forEach((dependency) => lines.push(dependency.mode === "symbolic" ? `  - asset: ${dependency.asset}\n    mode: symbolic` : `  - ${dependency.asset}`));
  }
  lines.push("materialization:", `  type: ${materialization}`);
  if (materialization === "table") lines.push(`  strategy: ${strategy}`);
  if (objectName) lines.push(`  object: ${objectName}`);
  if (partitionBy) lines.push(`  partition_by: ${partitionBy}`);
  if (incrementalKey) lines.push(`  incremental_key: ${incrementalKey}`);
  if (clusterBy) lines.push(`  cluster_by: [${clusterBy}]`);
  if (intervalStart || intervalEnd) lines.push("interval_modifiers:", `  start: ${intervalStart || "-"}`, `  end: ${intervalEnd || "-"}`);
  if (cooldown !== "300") lines.push(`rerun_cooldown: ${cooldown}`);
  if (retries) lines.push("retries:", `  count: ${retries}`, `  delay: ${retryDelay}`);
  if (timeout) lines.push(`timeout: ${timeout}`);
  if (priority !== "normal") lines.push(`priority: ${priority}`);
  if (hooks.length) {
    lines.push("hooks:");
    hooks.forEach((hook) => lines.push(`  ${hook.phase}:`, `    - ${hook.command}`));
  }
  if (columns.length) {
    lines.push("columns:");
    columns.forEach((column) => {
      lines.push(`  - name: ${column.name}`, `    type: ${column.type}`);
      if (column.description) lines.push(`    description: ${column.description}`);
      if (column.checks) lines.push("    checks:", ...column.checks.split(",").map((check) => `      - name: ${check.trim()}`));
    });
  }
  if (customChecks.length) {
    lines.push("custom_checks:");
    customChecks.forEach((check) => lines.push(`  - name: ${check.name}`, "    query: |", ...check.query.split("\n").map((line) => `      ${line}`)));
  }
  return lines.join("\n");
}

function Field({ label, value }: { label: string; value: string }) {
  return <div className="flex items-center justify-between gap-3"><span className="text-muted-foreground">{label}</span><span className="font-mono">{value}</span></div>;
}

function DependencyList({ names }: { names: string[] }) {
  return <div className="space-y-1 font-mono text-xs">{names.map((name) => <div key={name} className="rounded-md bg-muted px-2 py-1">{name}</div>)}</div>;
}

function ToggleCard({ title, description }: { title: string; description: string }) {
  const [enabled, setEnabled] = useState(true);
  return (
    <button type="button" className="flex w-full items-start justify-between gap-3 rounded-lg border p-3 text-left" onClick={() => setEnabled((value) => !value)}>
      <span><span className="block text-sm font-medium">{title}</span><span className="text-xs text-muted-foreground">{description}</span></span>
      <span className={cn("relative h-5 w-9 rounded-full border", enabled ? "border-primary bg-primary" : "bg-muted")}>
        <span className={cn("absolute top-0.5 h-4 w-4 rounded-full bg-white shadow", enabled ? "left-4" : "left-0.5")} />
      </span>
    </button>
  );
}

function UnitTests({ compact, onOpenResults }: { compact?: boolean; onOpenResults?: () => void }) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium">Unit tests</span>
        <Badge variant="outline" className="bg-emerald-50 text-emerald-700">2 passed</Badge>
        <Badge variant="outline" className="bg-red-50 text-red-700">1 failed</Badge>
        <Button variant="outline" size="xs" className="ml-auto"><Plus className="size-3" />New</Button>
        <Button size="xs" onClick={onOpenResults}><Play className="size-3" />Run all</Button>
      </div>
      {tests.map((test) => (
        <div key={test.id} className="rounded-lg border p-2 text-xs">
          <div className="flex items-center gap-2"><StatusPill status={test.status} /><span className="min-w-0 flex-1 truncate font-mono font-medium">{test.name}</span>{compact ? <MoreHorizontal className="size-3.5 text-muted-foreground" /> : null}</div>
          <div className="mt-1 flex flex-wrap gap-3 text-muted-foreground"><span>given: {test.given}</span><span>expect: {test.expect}</span></div>
          {"got" in test ? <div className="mt-2 rounded bg-muted p-2 font-mono text-red-600">got: {test.got}</div> : null}
        </div>
      ))}
    </div>
  );
}

function DiagnosticsList() {
  return (
    <div>
      <div className="sticky top-0 flex h-9 items-center gap-3 border-b bg-card px-3 text-xs">
        <span className="flex items-center gap-1 text-red-500"><XCircle className="size-3.5" />2</span>
        <span className="flex items-center gap-1 text-amber-500"><AlertTriangle className="size-3.5" />2</span>
        <span className="flex items-center gap-1 text-muted-foreground"><Activity className="size-3.5" />0</span>
      </div>
      {diagnostics.map((diagnostic) => (
        <div key={`${diagnostic.asset}-${diagnostic.message}`} className="flex items-start gap-2 border-b px-3 py-2 text-xs">
          <SeverityIcon severity={diagnostic.severity} />
          <div className="min-w-0 flex-1"><span className="font-mono text-primary">{diagnostic.asset}</span><span className="text-muted-foreground"> - {diagnostic.message}</span></div>
          <Button variant="outline" size="xs">{diagnostic.action}</Button>
        </div>
      ))}
    </div>
  );
}

function MetadataPanel({ compact, onFull }: { compact?: boolean; onFull?: () => void }) {
  if (compact) {
    return (
      <div className="space-y-2">
        <div className="flex items-center gap-2 text-xs">
          <GitCompare className="size-3.5 text-muted-foreground" /> Declared vs table
          <Badge variant="outline" className="bg-amber-50 text-amber-700">drift detected</Badge>
          <Button variant="link" size="xs" className="ml-auto h-auto p-0" onClick={onFull}>Full diff</Button>
        </div>
        {schemaRows.map((row) => <div key={row.name} className="rounded-lg border p-2 text-xs"><div className="flex items-center justify-between"><span className="font-mono">{row.name}</span><SchemaStatus status={row.status} /></div><div className="mt-1 flex gap-3 font-mono text-muted-foreground"><span>decl: {row.declared}</span><span>table: {row.actual}</span></div></div>)}
      </div>
    );
  }

  return <SimpleTable columns={["Column", "Declared", "In table", "Description", "Status"]} rows={schemaRows.map((row) => [row.name, row.declared, row.actual, row.description || "no description", <SchemaStatus key={row.name} status={row.status} />])} />;
}

function SchemaStatus({ status }: { status: string }) {
  if (status === "match") return <Badge variant="outline" className="bg-emerald-50 text-emerald-700"><CheckCircle2 className="size-3" />in sync</Badge>;
  if (status === "drift") return <Badge variant="outline" className="bg-amber-50 text-amber-700"><AlertTriangle className="size-3" />type drift</Badge>;
  if (status === "missing") return <Badge variant="outline" className="bg-muted"><Circle className="size-3" />missing</Badge>;
  return <Badge variant="outline" className="bg-sky-50 text-sky-700"><Plus className="size-3" />extra</Badge>;
}

// Asset kinds the creation dialog can produce, mapped to real backend create
// calls. Standalone: SQL/Python transforms, "HTTP API" (Bruin api asset) and
// "Load" (Bruin sling asset). Downstream (created from a canvas node): SQL,
// Python (via the Bruin Python SDK) and Sling, each depending on the source.
type AssetKindOption = {
  id: NewAssetKind;
  label: string;
  description: string;
  icon: ComponentType<{ className?: string }>;
};

const CREATABLE_ASSETS: AssetKindOption[] = [
  { id: "sql", label: "SQL asset", description: "Transform with a SELECT", icon: FileCode },
  { id: "python", label: "Python asset", description: "Custom Python transform", icon: Cpu },
  { id: "api", label: "HTTP API", description: "Pull records from an HTTP API endpoint", icon: Globe },
  { id: "sling", label: "Load", description: "Replicate data between connections with Sling", icon: Download },
];

const DOWNSTREAM_ASSETS: AssetKindOption[] = [
  { id: "sql", label: "SQL", description: "select * from the upstream table", icon: FileCode },
  { id: "python", label: "Python", description: "Read the upstream via the Bruin Python SDK", icon: Cpu },
  { id: "sling", label: "Sling", description: "Replicate downstream with Sling", icon: Download },
];

// A downstream asset reuses the source's prefix and appends _downstream, kept
// unique against existing names (the backend also requires a prefixed name).
function suggestDownstreamName(sourceName: string, existing: Set<string>): string {
  const parts = sourceName.split(".").filter(Boolean);
  const leaf = parts.pop() ?? "asset";
  const prefix = parts.join(".");
  const base = prefix ? `${prefix}.${leaf}_downstream` : `${leaf}_downstream`;
  if (!existing.has(base)) {
    return base;
  }
  let index = 2;
  while (existing.has(`${base}_${index}`)) {
    index += 1;
  }
  return `${base}_${index}`;
}

function NewAssetDialog({
  open,
  onOpenChange,
  pipelineId,
  pipelineName,
  existingAssetNames,
  downstreamSource,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId?: string;
  pipelineName?: string;
  existingAssetNames: Set<string>;
  downstreamSource?: { id: string; name: string } | null;
  onCreated?: (assetId: string) => void;
}) {
  const [kind, setKind] = useState<NewAssetKind>("sql");
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  const isDownstream = Boolean(downstreamSource);
  const options = isDownstream ? DOWNSTREAM_ASSETS : CREATABLE_ASSETS;
  const selected = options.find((option) => option.id === kind) ?? options[0];

  // Seed a unique, prefixed name suggestion (the backend requires a prefix).
  const suggestedName = useMemo(() => {
    if (isDownstream && downstreamSource) {
      return suggestDownstreamName(downstreamSource.name, existingAssetNames);
    }
    return buildSuggestedAssetName(selected.id, existingAssetNames, pipelineName);
  }, [isDownstream, downstreamSource, selected.id, existingAssetNames, pipelineName]);

  // Reset to a valid kind whenever the dialog (or its mode) opens.
  useEffect(() => {
    if (open) {
      setKind("sql");
      setError("");
    }
  }, [open, isDownstream]);
  useEffect(() => {
    if (open) {
      setName(suggestedName);
    }
  }, [open, suggestedName]);

  const create = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Asset name is required.");
      return;
    }
    if (!pipelineId) {
      setError("Select a pipeline before creating an asset.");
      return;
    }
    if (existingAssetNames.has(trimmed)) {
      setError(`An asset named "${trimmed}" already exists.`);
      return;
    }
    const input =
      isDownstream && downstreamSource
        ? selected.id === "sql"
          ? { name: trimmed, source_asset_id: downstreamSource.id }
          : { name: trimmed, source_asset_id: downstreamSource.id, type: selected.id }
        : buildCreateAssetInput(trimmed, selected.id);
    setCreating(true);
    setError("");
    try {
      const response = await createAsset(pipelineId, input);
      onOpenChange(false);
      if (response.asset_id) {
        onCreated?.(response.asset_id);
      }
    } catch (caught) {
      setError(String(caught));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90dvh] max-w-2xl flex-col overflow-hidden">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Plus className="size-4 text-primary" />{isDownstream ? "New downstream asset" : "New asset"}</DialogTitle>
          <DialogDescription>
            {isDownstream && downstreamSource ? (
              <>Depends on <span className="font-mono">{downstreamSource.name}</span>.</>
            ) : (
              <>Create an asset in {pipelineName ? <span className="font-mono">{pipelineName}</span> : "this pipeline"}.</>
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="grid min-h-0 flex-1 gap-5 overflow-y-auto">
          <div className="grid gap-2 sm:grid-cols-2">
            {options.map((option) => (
              <button
                key={option.id}
                type="button"
                className={cn("rounded-lg border p-3 text-left hover:bg-muted", selected.id === option.id ? "border-primary ring-1 ring-primary" : null)}
                onClick={() => setKind(option.id)}
              >
                <option.icon className="size-5 text-primary" />
                <div className="mt-2 font-medium">{option.label}</div>
                <div className="text-xs text-muted-foreground">{option.description}</div>
              </button>
            ))}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="new-asset-name">Asset name</Label>
            <Input
              id="new-asset-name"
              className="font-mono"
              placeholder="analytics.my_asset"
              value={name}
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !creating) {
                  void create();
                }
              }}
              autoFocus
            />
            <p className="text-xs text-muted-foreground">Use a <span className="font-mono">prefix.name</span> to group it under <span className="font-mono">assets/prefix/</span>.</p>
          </div>
          {error ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300">{error}</div>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={creating}>Cancel</Button>
          <Button onClick={() => void create()} disabled={creating || !pipelineId}>
            {creating ? <Spinner className="size-4" /> : <CheckCircle2 className="size-4" />}Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PipelineSettingsDialog({ open, onOpenChange, pipelineId }: { open: boolean; onOpenChange: (open: boolean) => void; pipelineId: string }) {
  const [section, setSection] = useState("general");
  const sections = ["general", "schedule", "variants", "python", "variables", "notifications"];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>Pipeline settings <span className="font-mono text-xs text-muted-foreground">· {pipelineId}</span></DialogTitle>
          <DialogDescription>Static settings modal matching the Claude mock.</DialogDescription>
        </DialogHeader>
        <div className="grid min-h-80 gap-4 md:grid-cols-[180px_minmax(0,1fr)]">
          <div className="flex gap-2 overflow-x-auto md:block md:space-y-1">
            {sections.map((item) => <Button key={item} variant={section === item ? "secondary" : "ghost"} className="justify-start capitalize" onClick={() => setSection(item)}>{item}</Button>)}
          </div>
          <div className="space-y-4 rounded-lg border p-4">
            {section === "general" ? <><FieldInput label="Pipeline name" value={pipelineId} /><FieldInput label="Owner" value="team@acme.io" /></> : null}
            {section === "schedule" ? <><FieldInput label="Schedule" value="@daily" /><FieldInput label="Timezone" value="Europe/Berlin" /></> : null}
            {section === "variants" ? <PipelineVariantsPanel /> : null}
            {section === "python" ? <SimpleTable columns={["Package", "Version"]} rows={[["scikit-learn", "1.4.0"], ["pandas", "2.2.1"], ["dlt", "0.4.12"]]} /> : null}
            {section === "variables" ? <SimpleTable columns={["Variable", "Value"]} rows={[["env", "prod"], ["lookback_days", "30"]]} /> : null}
            {section === "notifications" ? <ToggleCard title="Notify on failure" description="Send a Slack message when this pipeline fails." /> : null}
          </div>
        </div>
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PipelineVariantsPanel() {
  return (
    <div className="space-y-4">
      <div>
        <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Variables</div>
        <SimpleTable columns={["Variable", "Type", "Default"]} rows={pipelineVariables.map(([name, type, value]) => [<span key={name} className="font-mono">{name}</span>, type, <span key={value} className="font-mono">{value}</span>])} />
      </div>
      <div>
        <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Variants</div>
        <SimpleTable
          columns={["Variant", "Rendered name", "Schedule", "Overrides"]}
          rows={pipelineVariants.filter((variant) => variant.id !== "default").map((variant) => [
            <span key="variant" className="font-mono">{variant.id}</span>,
            <span key="name" className="font-mono text-primary">{renderedPipelineName(variant.id)}</span>,
            <span key="schedule" className="font-mono">{renderedPipelineSchedule(variant.id)}</span>,
            <span key="overrides" className="font-mono text-muted-foreground">{Object.entries(variant.overrides).map(([key, value]) => `${key}=${value}`).join(", ")}</span>,
          ])}
        />
      </div>
      <p className="text-xs text-muted-foreground">One <span className="font-mono">pipeline.yml</span> renders multiple concrete pipelines. Run with <span className="font-mono">bruin run --variant &lt;name&gt;</span>, or pick a variant from the Build toolbar.</p>
    </div>
  );
}

function PlanDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><ClipboardCheck className="size-4 text-primary" />Impact plan</DialogTitle>
          <DialogDescription>Static preview of changed assets, breaking impact, and backfill scope before running.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_18rem]">
          <div className="overflow-hidden rounded-lg border">
            <SimpleTable
              columns={["Asset", "Change", "Note"]}
              rows={impactPlan.changes.map((change) => {
                const meta = changeTypeMeta[change.type];
                return [
                  <span key="asset" className="font-mono">{change.name}</span>,
                  <span key="change" className={cn("rounded px-1.5 py-0.5 text-[11px]", meta.className)}>{meta.label}</span>,
                  change.note,
                ];
              })}
            />
          </div>
          <div className="space-y-3">
            <div className="rounded-lg border bg-muted/30 p-3">
              <div className="text-sm font-medium">Backfill required</div>
              <div className="mt-1 text-2xl font-semibold">3 assets</div>
              <p className="mt-1 text-xs text-muted-foreground">Breaking and downstream changes need recompute.</p>
            </div>
            {impactPlan.backfill.map((item) => (
              <div key={item.name} className="rounded-lg border p-2 text-xs">
                <div className="font-mono font-medium">{item.name}</div>
                <div className="mt-1 flex items-center justify-between text-muted-foreground"><span>{item.reason}</span><span>{item.rows}</span></div>
              </div>
            ))}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Close</Button>
          <Button onClick={() => onOpenChange(false)}><Play className="size-4" />Run plan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// DeployButton shows drift between the working tree and the latest deployed
// snapshot and redeploys on click.
function DeployButton({ deployState }: { deployState: PipelineDeployState }) {
  const { status, deploying, deploy, driftedFileCount } = deployState;
  if (!status) return null;

  if (status.has_snapshot && status.in_sync) {
    return (
      <Button variant="ghost" size="sm" disabled title={`Deployed ${status.version_id ?? ""}`}>
        <Package className="size-3.5 text-emerald-600" /> Deployed
      </Button>
    );
  }

  const label = status.has_snapshot
    ? `Redeploy (${driftedFileCount} file${driftedFileCount === 1 ? "" : "s"} changed)`
    : "Deploy";
  const title = status.has_snapshot
    ? `Working tree differs from deployed ${status.version_id ?? ""}`
    : "No deployed snapshot yet; scheduled runs use the working tree until you deploy";
  return (
    <Button variant="outline" size="sm" onClick={() => void deploy()} disabled={deploying} title={title}>
      <Package className={cn("size-3.5", status.has_snapshot ? "text-amber-600" : undefined)} />
      {deploying ? "Deploying…" : label}
    </Button>
  );
}

type BuildStaleProgress = "pending" | "running" | "done" | "failed";

// BuildStaleDialog compiles the stale set into a build plan: every stale
// asset, and for partially-covered incrementals exactly the uncovered gap
// intervals. Building runs the real materialize stream per asset/gap.
function BuildStaleDialog({
  open,
  onOpenChange,
  staleAssets,
  pipelineAssets,
  onBuildAsset,
  onCompleted,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  staleAssets: AssetStaleness[];
  pipelineAssets: BuildAsset[];
  onBuildAsset: (
    assetId: string,
    options: { assetName?: string; timeWindow?: { start: string; end: string } | null }
  ) => Promise<MaterializeStreamPayload | null>;
  onCompleted: () => void;
}) {
  const [progress, setProgress] = useState<Record<string, BuildStaleProgress>>({});
  const [building, setBuilding] = useState(false);

  const assetIdByName = useMemo(() => {
    const map = new Map<string, string>();
    for (const asset of pipelineAssets) {
      map.set(asset.name, asset.id);
    }
    return map;
  }, [pipelineAssets]);

  useEffect(() => {
    if (!open) {
      setProgress({});
      setBuilding(false);
    }
  }, [open]);

  const buildAll = async () => {
    setBuilding(true);
    for (const stale of staleAssets) {
      const encodedAssetId = assetIdByName.get(stale.asset_name);
      if (!encodedAssetId) continue;
      setProgress((current) => ({ ...current, [stale.asset_name]: "running" }));
      try {
        // A null window means "build the whole asset"; gaps build only the
        // uncovered intervals. Each build streams into the materialize history
        // panel via onBuildAsset so progress is visible in the output.
        const windows: ({ start: string; end: string } | null)[] = stale.gaps?.length
          ? stale.gaps.map((gap) => ({ start: gap.start, end: gap.end }))
          : [null];
        let failed = false;
        for (const timeWindow of windows) {
          const result = await onBuildAsset(encodedAssetId, { assetName: stale.asset_name, timeWindow });
          if (!result || result.status !== "ok") {
            failed = true;
          }
        }
        setProgress((current) => ({ ...current, [stale.asset_name]: failed ? "failed" : "done" }));
      } catch {
        setProgress((current) => ({ ...current, [stale.asset_name]: "failed" }));
      }
    }
    setBuilding(false);
    onCompleted();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Hammer className="size-4 text-primary" />Build stale assets</DialogTitle>
          <DialogDescription>
            {staleAssets.length} asset{staleAssets.length === 1 ? "" : "s"} out of date for this environment and time range. Partial incrementals rebuild only the uncovered gaps.
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-80 space-y-1 overflow-y-auto">
          {staleAssets.map((stale) => (
            <div key={stale.asset_id} className="flex items-center gap-2 rounded-md border px-2 py-1.5 text-xs">
              <span className="min-w-0 flex-1 truncate font-mono">{stale.asset_name}</span>
              <StalenessBadge staleness={stale} />
              {stale.gaps?.length ? (
                <span className="text-[10px] text-muted-foreground">{stale.gaps.length} gap{stale.gaps.length === 1 ? "" : "s"}</span>
              ) : null}
              {progress[stale.asset_name] === "running" ? <span className="text-[10px] text-sky-600">building…</span> : null}
              {progress[stale.asset_name] === "done" ? <Check className="size-3.5 text-emerald-600" /> : null}
              {progress[stale.asset_name] === "failed" ? <XCircle className="size-3.5 text-red-600" /> : null}
            </div>
          ))}
          {staleAssets.length === 0 ? <p className="text-xs text-muted-foreground">Everything is fresh.</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={building}>Close</Button>
          <Button onClick={buildAll} disabled={building || staleAssets.length === 0}>
            <Play className="size-4" />{building ? "Building…" : `Build ${staleAssets.length} asset${staleAssets.length === 1 ? "" : "s"}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function FieldInput({ label, value }: { label: string; value: string }) {
  return <label className="block space-y-1.5"><span className="text-xs font-medium text-muted-foreground">{label}</span><Input defaultValue={value} /></label>;
}

function SettingsIcon() {
  return <Sliders className="size-3.5" />;
}
