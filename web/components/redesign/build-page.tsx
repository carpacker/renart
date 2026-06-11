import { Link, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useAtomValue, useSetAtom } from "jotai";
import {
  Activity,
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronsUpDown,
  Circle,
  ClipboardCheck,
  Cpu,
  Database,
  Eye,
  FileCode,
  GitBranch,
  GitCompare,
  Hammer,
  History,
  Layers,
  MoreHorizontal,
  Network,
  Package,
  PanelLeft,
  PanelRight,
  Play,
  Plus,
  Search,
  Sliders,
  Table2,
  Terminal,
  XCircle,
} from "lucide-react";
import { ComponentType, ReactNode, createContext, useContext, useEffect, useMemo, useState } from "react";

import { Badge } from "@/components/ui/badge";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { routeSelectionAtom, workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { WebAsset, WebPipeline } from "@/lib/types";
import { cn } from "@/lib/utils";
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
  queryRows,
  renderedPipelineName,
  renderedPipelineSchedule,
  schemaRows,
  tests,
} from "./redesign-data";
import { RedesignAssetEditor } from "./asset-editor";
import { RedesignLineageCanvas, assetDisplayName, assetGroupName, assetNameParts, type RedesignLineageCanvasAsset } from "./lineage-canvas";
import { IntegrationBadge, RedesignPage, RedesignPanel, SectionCard, SeverityIcon, SimpleTable, StatusPill } from "./redesign-primitives";

export type RedesignBuildView = "canvas" | "split" | "code";
export type RedesignResultTab = "inspect" | "materialize" | "query" | "tests" | "diagnostics" | "metadata" | "shell" | "history";
export type RedesignEditorMode = "asset" | "adhoc";

export type RedesignBuildSearch = {
  result?: RedesignResultTab;
  editor?: RedesignEditorMode;
  variant?: string;
};

const resultTabs: RedesignResultTab[] = ["inspect", "materialize", "query", "tests", "diagnostics", "metadata", "shell", "history"];
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
  editorMode: RedesignEditorMode;
  declaredDependencies: string[];
  addDependency: (dependency: string) => void;
  selectAsset: (assetId: string) => void;
  goToAsset: (pipelineId: string, assetId: string) => void;
  openBottom: (tab: RedesignResultTab) => void;
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
  onEditorModeChange,
  onVariantChange,
  onAssetSelect,
}: {
  pipelineId?: string;
  selectedAssetId?: string;
  resultTab?: RedesignResultTab;
  editorMode?: RedesignEditorMode;
  variant?: string;
  onResultTabChange?: (tab: RedesignResultTab) => void;
  onEditorModeChange?: (mode: RedesignEditorMode) => void;
  onVariantChange?: (variant: string) => void;
  onAssetSelect?: (assetId: string) => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const navigate = useNavigate();
  const adhoc = editorMode === "adhoc";
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
  const displayedPipelineAssets = useMemo(
    () => pipelineAssets.map((asset) => ({
      ...asset,
      status: materializationStatusByAssetId[asset.id]?.status ?? asset.status,
      materializedAt: labelForRedesignMaterializationState(materializationStatusByAssetId[asset.id]),
    })),
    [materializationStatusByAssetId, pipelineAssets]
  );
  const firstAssetId = displayedPipelineAssets[0]?.id ?? "revenue_daily";
  const [visualSelectedAssetId, setVisualSelectedAssetId] = useState(selectedAssetId ?? firstAssetId);
  const effectiveSelectedAssetId = visualSelectedAssetId ?? selectedAssetId ?? firstAssetId;
  const selectedAsset = displayedPipelineAssets.find((asset) => asset.id === effectiveSelectedAssetId) ?? displayedPipelineAssets[0] ?? fallbackBuildAssets()[0];
  const [explorerOpen, setExplorerOpen] = useState(false);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [newAssetOpen, setNewAssetOpen] = useState(false);
  const [pipelineSettingsOpen, setPipelineSettingsOpen] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [addedDependencies, setAddedDependencies] = useState<string[]>([]);
  const [history, setHistory] = useState<Array<{ id: number; kind: string; target: string; status: string; time: string; variant: string }>>([]);
  const declaredDependencies = [...pipelineDependencies, ...addedDependencies];

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
  };
  const addDependency = (dependency: string) => {
    setAddedDependencies((current) => current.includes(dependency) ? current : [...current, dependency]);
  };
  const logHistory = (kind: string, target: string) => {
    setHistory((current) => [{ id: Date.now(), kind, target, status: "success", time: new Date().toLocaleTimeString(), variant }, ...current].slice(0, 20));
  };
  const runAction = () => {
    logHistory("materialize", selectedAsset?.name ?? pipelineId);
    openBottom("materialize");
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
  const buildContext: BuildContextValue = {
    pipelineId,
    pipeline: activePipeline,
    pipelineAssets: displayedPipelineAssets,
    selectedAssetId: effectiveSelectedAssetId,
    selectedAsset,
    editorMode,
    declaredDependencies,
    addDependency,
    selectAsset,
    goToAsset,
    openBottom,
  };

  return (
    <BuildContext.Provider value={buildContext}>
    <RedesignPage>
      <BuildTopBar
        pipelineId={pipelineId}
        pipelineLabel={activePipeline?.name ?? pipelineId}
        selectedAsset={selectedAsset}
        selectedAssetId={effectiveSelectedAssetId}
        view={view}
        resultTab={resultTab}
        editorMode={editorMode}
        variant={variant}
        historyCount={history.length}
        onOpenExplorer={() => setExplorerOpen(true)}
        onOpenInspector={() => setInspectorOpen(true)}
        onOpenDiagnostics={() => openBottom("diagnostics")}
        onOpenHistory={() => openBottom("history")}
        onOpenPlan={() => setPlanOpen(true)}
        onVariantChange={onVariantChange}
        onRun={runAction}
      />
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3 xl:grid-cols-[248px_minmax(0,1fr)_320px]">
        <RedesignPanel className="hidden min-h-0 xl:flex xl:flex-col">
          <Explorer
            pipelineId={pipelineId}
            selectedAssetId={effectiveSelectedAssetId}
            buildSearch={buildSearch}
            onAssetSelect={selectAsset}
            onAdhoc={() => onEditorModeChange?.("adhoc")}
            onNewAsset={() => setNewAssetOpen(true)}
            onPipelineSettings={() => setPipelineSettingsOpen(true)}
          />
        </RedesignPanel>

        <div className="flex min-h-0 flex-col gap-3">
          <RedesignPanel className="flex min-h-0 flex-1 overflow-hidden">
            <DelimitedCardContent className="h-full min-h-0 flex-1 p-0">
              <Outlet />
            </DelimitedCardContent>
          </RedesignPanel>

          <ResultsPanel activeTab={resultTab} onTabChange={openBottom} variant={variant} history={history} onHistoryOpen={(tab) => openBottom(tab)} />
        </div>

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
            onAdhoc={() => onEditorModeChange?.("adhoc")}
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

      <NewAssetDialog open={newAssetOpen} onOpenChange={setNewAssetOpen} />
      <PipelineSettingsDialog open={pipelineSettingsOpen} onOpenChange={setPipelineSettingsOpen} pipelineId={pipelineId} />
      <PlanDialog open={planOpen} onOpenChange={setPlanOpen} />
    </RedesignPage>
    </BuildContext.Provider>
  );
}

function BuildTopBar({
  pipelineId,
  pipelineLabel,
  selectedAsset,
  selectedAssetId,
  view,
  resultTab,
  editorMode,
  variant,
  historyCount,
  onOpenExplorer,
  onOpenInspector,
  onOpenDiagnostics,
  onOpenHistory,
  onOpenPlan,
  onVariantChange,
  onRun,
}: {
  pipelineId: string;
  pipelineLabel: string;
  selectedAsset: BuildAsset;
  selectedAssetId: string;
  view: RedesignBuildView;
  resultTab: RedesignResultTab;
  editorMode: RedesignEditorMode;
  variant: string;
  historyCount: number;
  onOpenExplorer: () => void;
  onOpenInspector: () => void;
  onOpenDiagnostics: () => void;
  onOpenHistory: () => void;
  onOpenPlan: () => void;
  onVariantChange?: (variant: string) => void;
  onRun: () => void;
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
      <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
        <ViewLink pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={view} view="canvas" search={search} icon={Layers} label="Canvas" />
        <ViewLink pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={view} view="split" search={search} icon={Layers} label="Split" className="hidden sm:inline-flex" />
        <ViewLink pipelineId={pipelineId} selectedAssetId={selectedAssetId} currentView={view} view="code" search={search} icon={FileCode} label="Code" />
      </div>
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
      <Button asChild variant={editorMode === "adhoc" ? "secondary" : "outline"} size="sm" className="hidden lg:inline-flex">
        <Link to="/redesign/pipelines/$pipelineId/assets/$assetId/code" params={{ pipelineId, assetId: selectedAssetId }} search={{ result: resultTab, editor: "adhoc", variant }}>
          <Terminal className="size-3.5" /> Ad-hoc
        </Link>
      </Button>
      <Button variant="outline" size="sm" onClick={onOpenDiagnostics}>
        <Activity className="size-3.5" /> Diagnostics <span className="rounded-full bg-red-500 px-1 text-[10px] text-white">4</span>
      </Button>
      <Button variant="outline" size="sm" onClick={onOpenPlan}>
        <ClipboardCheck className="size-3.5" /> Plan
      </Button>
      {historyCount > 0 ? <Button variant="ghost" size="sm" onClick={onOpenHistory}><History className="size-3.5" />{historyCount}</Button> : null}
      <Button size="sm" onClick={onRun}><Play className="size-3.5" /> Run</Button>
      <Button variant="ghost" size="sm" className="xl:hidden" onClick={onOpenInspector}><PanelRight className="size-3.5" /></Button>
    </div>
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
  className,
}: {
  pipelineId: string;
  selectedAssetId: string;
  currentView: RedesignBuildView;
  view: RedesignBuildView;
  search: RedesignBuildSearch;
  icon: ComponentType<{ className?: string }>;
  label: string;
  className?: string;
}) {
  return (
    <Button asChild variant={currentView === view ? "secondary" : "ghost"} size="sm" className={className}>
      <Link to={redesignAssetViewPath(view)} params={{ pipelineId, assetId: selectedAssetId }} search={search}>
        <Icon className="size-3.5" /> {label}
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
  const pipelineGroup = objectGroups.find((group) => group.id === "pipeline");
  const PipelineIcon = pipelineGroup?.icon ?? Layers;
  const { declaredDependencies, pipelineAssets } = useBuildContext();
  const pipelineItems = workspace?.pipelines.length
    ? workspace.pipelines
    : [{ id: "simple", name: "simple", path: "", assets: [] } satisfies WebPipeline];
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
                            {groupAssets.map((asset) => <AssetButton key={asset.id} asset={asset} declaredDependencies={declaredDependencies} selected={selectedAssetId === asset.id} onSelect={() => onAssetSelect(asset.id)} />)}
                          </div>
                        )) : <div className="px-2 py-1 text-xs text-muted-foreground">No assets found.</div>}
                        <div className="mt-1 border-t pt-1">
                          <button className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs text-muted-foreground hover:bg-muted" onClick={onAdhoc}>
                            <Terminal className="size-3.5" /> Ad-hoc query
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
      {missingCount > 0 ? <span title={`${missingCount} imports not in dependencies`} className="size-1.5 rounded-full bg-amber-500" /> : null}
    </button>
  );
}

function PipelineCanvas({ selectedAssetId, onAssetSelect }: { pipelineId: string; selectedAssetId: string; onAssetSelect: (assetId: string) => void }) {
  const { pipelineAssets } = useBuildContext();
  return <RedesignLineageCanvas assets={pipelineAssets} selectedAssetId={selectedAssetId} onAssetSelect={onAssetSelect} />;
}

export function RedesignBuildCanvasView() {
  const { pipelineId, selectedAssetId, selectAsset } = useBuildContext();
  return <PipelineCanvas pipelineId={pipelineId} selectedAssetId={selectedAssetId} onAssetSelect={selectAsset} />;
}

export function RedesignBuildSplitView() {
  const { pipelineId, selectedAssetId, selectedAsset, selectAsset, openBottom } = useBuildContext();
  return (
    <div className="grid h-full min-h-0 grid-cols-1 md:grid-cols-2">
      <div className="min-h-0 border-b md:border-b-0 md:border-r">
        <PipelineCanvas pipelineId={pipelineId} selectedAssetId={selectedAssetId} onAssetSelect={selectAsset} />
      </div>
      <div className="min-h-0">
        <EditorWorkspace asset={selectedAsset} adhoc={false} compact onInspect={() => openBottom("inspect")} onRun={() => openBottom("materialize")} />
      </div>
    </div>
  );
}

export function RedesignBuildCodeView() {
  const { selectedAsset, editorMode, openBottom } = useBuildContext();
  const adhoc = editorMode === "adhoc";
  return <EditorWorkspace asset={selectedAsset} adhoc={adhoc} onInspect={() => openBottom("inspect")} onRun={() => openBottom(adhoc ? "query" : "materialize")} />;
}

function EditorWorkspace({
  asset,
  adhoc,
  compact,
  onInspect,
  onRun,
}: {
  asset: BuildAsset;
  adhoc: boolean;
  compact?: boolean;
  onInspect: () => void;
  onRun: () => void;
}) {
  const { declaredDependencies, addDependency, goToAsset, openBottom } = useBuildContext();

  if (adhoc) {
    return <AdhocEditor onRun={onRun} />;
  }

  const meta = kindMeta[asset.kind];
  const Icon = meta.icon;
  const missingDependencies = asset.kind === "python" ? missingPythonDependencies(asset, declaredDependencies) : [];
  const actionLabel = asset.kind === "source" ? "Validate" : asset.kind === "ingestr" || asset.kind === "sling" ? "Run" : "Materialize";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex min-h-11 items-center gap-2 border-b px-3">
        <Icon className="size-4 text-primary" />
        <div className="min-w-0">
          <div className="truncate font-mono text-xs font-medium">{asset.dir ? `assets/${asset.dir}/` : ""}{asset.name}{meta.ext}</div>
          {!compact ? <div className="truncate text-[11px] text-muted-foreground">{meta.description}</div> : null}
        </div>
        {missingDependencies.length > 0 ? (
          <Button variant="outline" size="xs" className="border-amber-300 bg-amber-50 text-amber-700 hover:bg-amber-100" onClick={() => openBottom("diagnostics")}>
            <AlertTriangle className="size-3" />{missingDependencies.length} not in deps
          </Button>
        ) : null}
        <div className="ml-auto flex items-center gap-2">
          {!compact ? <Button variant="outline" size="sm"><Database className="size-3.5" /> duckdb-default</Button> : null}
          <Button size="sm" onClick={onRun}><Hammer className="size-3.5" />{actionLabel}</Button>
          {asset.kind !== "source" ? <Button variant="outline" size="sm" onClick={onInspect}><Eye className="size-3.5" />Inspect</Button> : null}
        </div>
      </div>
      {asset.workspaceAsset && asset.pipelineId ? (
        <RedesignAssetEditor
          asset={asset.workspaceAsset}
          pipelineId={asset.pipelineId}
          onInspect={onInspect}
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
  );
}

function AdhocEditor({ onRun }: { onRun: () => void }) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex min-h-11 items-center gap-2 border-b px-3">
        <Terminal className="size-4 text-primary" />
        <span className="font-medium">Ad-hoc</span>
        <Button variant="outline" size="sm" className="ml-auto"><Database className="size-3.5" /> duckdb-default</Button>
        <Button size="sm" onClick={onRun}><Play className="size-3.5" />Run</Button>
      </div>
      <CodeBlock lines={["select * from revenue_daily limit 100;"]} />
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
  variant,
  history,
  onHistoryOpen,
}: {
  activeTab: RedesignResultTab;
  onTabChange: (tab: RedesignResultTab) => void;
  variant: string;
  history: Array<{ id: number; kind: string; target: string; status: string; time: string; variant: string }>;
  onHistoryOpen: (tab: RedesignResultTab) => void;
}) {
  return (
    <RedesignPanel className="h-56 shrink-0">
        <Tabs value={activeTab} onValueChange={(value) => { if (resultTabs.includes(value as RedesignResultTab)) onTabChange(value as RedesignResultTab); }} className="flex h-full min-h-0 flex-col">
        <DelimitedCardHeader className="min-h-9 py-1">
          <ScrollArea className="min-w-0 flex-1" horizontalScrollBarClassName="hidden" viewportClassName="w-full">
            <TabsList className={scrollableTabsListClass}>
              <TabsTrigger value="inspect" className={scrollableTabsTriggerClass}><Table2 className="size-3.5" />Inspect</TabsTrigger>
              <TabsTrigger value="materialize" className={scrollableTabsTriggerClass}><Hammer className="size-3.5" />Materialize</TabsTrigger>
              <TabsTrigger value="query" className={scrollableTabsTriggerClass}><Terminal className="size-3.5" />Query</TabsTrigger>
              <TabsTrigger value="tests" className={scrollableTabsTriggerClass}><ClipboardCheck className="size-3.5" />Tests</TabsTrigger>
              <TabsTrigger value="diagnostics" className={scrollableTabsTriggerClass}><Activity className="size-3.5" />Diagnostics</TabsTrigger>
              <TabsTrigger value="metadata" className={scrollableTabsTriggerClass}><GitCompare className="size-3.5" />Metadata</TabsTrigger>
              <TabsTrigger value="shell" className={scrollableTabsTriggerClass}><Terminal className="size-3.5" />Shell</TabsTrigger>
              <TabsTrigger value="history" className={scrollableTabsTriggerClass}><History className="size-3.5" />History{history.length > 0 ? <span className="ml-1 text-[10px] text-muted-foreground">{history.length}</span> : null}</TabsTrigger>
            </TabsList>
          </ScrollArea>
        </DelimitedCardHeader>
        <TabsContent value="inspect" className="min-h-0 flex-1 overflow-hidden p-0">
          <SimpleTable columns={["#", "day", "revenue"]} rows={queryRows.map((row) => [...row])} />
        </TabsContent>
        <TabsContent value="materialize" className="min-h-0 flex-1 p-3 font-mono text-xs">
          <p><span className="text-emerald-600">PASS</span> simple.stripe_orders</p>
          <p><span className="text-emerald-600">PASS</span> simple.revenue_daily</p>
          <p className="mt-2">bruin run completed <span className="text-emerald-600">successfully</span> in 1m57s</p>
        </TabsContent>
        <TabsContent value="query" className="min-h-0 flex-1 overflow-hidden p-0">
          <SimpleTable columns={["#", "day", "revenue"]} rows={queryRows.map((row) => [...row])} />
        </TabsContent>
        <TabsContent value="tests" className="min-h-0 flex-1 overflow-auto p-3"><UnitTests /></TabsContent>
        <TabsContent value="diagnostics" className="min-h-0 flex-1 overflow-auto p-0"><DiagnosticsList /></TabsContent>
        <TabsContent value="metadata" className="min-h-0 flex-1 overflow-auto p-0"><MetadataPanel /></TabsContent>
        <TabsContent value="shell" className="min-h-0 flex-1 overflow-hidden p-0"><ShellPanel variant={variant} /></TabsContent>
        <TabsContent value="history" className="min-h-0 flex-1 overflow-auto p-0"><HistoryPanel history={history} onOpen={onHistoryOpen} /></TabsContent>
      </Tabs>
    </RedesignPanel>
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

function NewAssetDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [kind, setKind] = useState<keyof typeof kindMeta>("sql");
  const kinds = Object.entries(kindMeta) as Array<[keyof typeof kindMeta, typeof kindMeta[keyof typeof kindMeta]]>;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Plus className="size-4 text-primary" />New asset</DialogTitle>
          <DialogDescription>Static creation wizard preview for the redesign.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-5">
          <div className="grid gap-2 sm:grid-cols-3">
            {["duckdb-default", "bq-prod", "stripe"].map((connection) => (
              <button key={connection} className="rounded-lg border p-3 text-left text-sm hover:bg-muted">
                <Database className="mb-2 size-4 text-muted-foreground" />
                <span className="font-mono">{connection}</span>
              </button>
            ))}
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {kinds.map(([id, meta]) => (
              <button key={id} className={cn("rounded-lg border p-3 text-left hover:bg-muted", kind === id ? "border-primary ring-1 ring-primary" : null)} onClick={() => setKind(id)}>
                <meta.icon className="size-5 text-primary" />
                <div className="mt-2 font-medium">{meta.label}</div>
                <div className="text-xs text-muted-foreground">{meta.description}</div>
              </button>
            ))}
          </div>
          <div className="grid gap-2">
            <Label>Asset name</Label>
            <Input className="font-mono" placeholder="my_asset" />
            <p className="text-xs text-muted-foreground">Creates <span className="font-mono">assets/my_asset{kindMeta[kind].ext}</span>.</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => onOpenChange(false)}><CheckCircle2 className="size-4" />Create</Button>
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

function FieldInput({ label, value }: { label: string; value: string }) {
  return <label className="block space-y-1.5"><span className="text-xs font-medium text-muted-foreground">{label}</span><Input defaultValue={value} /></label>;
}

function SettingsIcon() {
  return <Sliders className="size-3.5" />;
}
