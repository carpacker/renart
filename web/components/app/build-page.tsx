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
  ChevronUp,
  Circle,
  Columns2,
  Cpu,
  Database,
  Download,
  Eye,
  ExternalLink,
  FileCode,
  FilePlus2,
  FolderPlus,
  GitBranchPlus,
  Globe,
  GitCompare,
  Hammer,
  Layers,
  Loader2,
  MoreHorizontal,
  Package,
  PanelLeft,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRight,
  PanelRightClose,
  PanelRightOpen,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Radar,
  Search,
  Sliders,
  Table2,
  Terminal,
  Trash2,
  Sprout,
  X,
  XCircle,
} from "lucide-react";
import {
  ComponentType,
  ReactNode,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import { Input } from "@/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { createAsset, deleteAsset } from "@/lib/api-assets";
import { createPipeline, getPipelineConfig, updatePipelineConfig } from "@/lib/api-pipelines";
import { buildCreateAssetInput, buildSuggestedAssetName } from "@/lib/workspace-shell-helpers";
import { API_ASSET_TEMPLATES, type APIAssetTemplateId } from "@/lib/api-asset-templates";
import type { NewAssetKind } from "@/components/new-asset-node";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AssetInspectView } from "@/components/asset-inspect-view";
import { InspectWarningCard } from "@/components/inspect-warning-card";
import { InspectInfoCard } from "@/components/inspect-info-card";
import { WorkspaceMaterializeOutputView } from "@/components/workspace-materialize-output-view";
import { Spinner } from "@/components/ui/spinner";
import { runSQLQuery } from "@/lib/api";
import type { MaterializeStreamPayload } from "@/lib/api-core";
import type { PipelineRunSource } from "@/lib/api-scheduler";
import type { StreamAssetEvent } from "@/lib/api-streams";
import { typeCheckPipeline, type PipelineTypeCheckReport } from "@/lib/api-pipelines";
import { renderAsset, type AssetRenderResult } from "@/lib/api-asset-render";
import type { AssetStaleness } from "@/lib/api-staleness";
import { isSeedAssetType, isSensorAssetType, isSqlAssetType } from "@/lib/asset-types";
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
import { awaitWorkspaceSaves } from "@/lib/workspace-save-barrier";
import type {
  AssetInspectResponse,
  SqlQueryResponse,
  WebAsset,
  WebPipeline,
  PipelineConfigConnection,
  PipelineConfigVariable,
  UpdatePipelineConfigRequest,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import { deploymentLabel } from "@/lib/deployment-label";
import { copyTextToClipboard } from "@/lib/copy-to-clipboard";
import { useAssetResults } from "@/hooks/use-asset-results";
import { useSelectedEnvironmentPolicy } from "@/hooks/use-environment-policy";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useIsMobile } from "@/hooks/use-mobile";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { usePipelineDeploy, type PipelineDeployState } from "@/hooks/use-pipeline-deploy";
import { isStaleStatus, usePipelineStaleness } from "@/hooks/use-pipeline-staleness";
import type { MaterializeScope } from "@/lib/materialize-scope";
import {
  LOCAL_LOAD_CONNECTION,
  isLocalLoadConnection,
  loadConnectionCategory,
  loadConnectionsForEnvironment,
  loadTargetNeedsDestinationObject,
} from "@/lib/load-assets";
import {
  labelForAppMaterializationState,
  useAppAssetMaterializationStatus,
} from "@/hooks/use-app-asset-materialization-status";

import {
  assets,
  diagnostics,
  editorLinesFor,
  kindForAssetType,
  kindMeta,
  missingPythonDependencies,
  objectGroups,
  packageForImport,
  parsePythonImport,
  pipelineDependencies,
  schemaRows,
  tests,
} from "./app-data";
import { AppAdhocEditor, useAdhocQueryDraft } from "./adhoc-editor";
import { AppAssetEditor } from "./asset-editor";
import { ApiParametersEditor } from "./api-parameters-editor";
import { AssetGuidedCards } from "./asset-guided-cards";
import { ErrorBoundary } from "@/components/ui/error-boundary";
import { SqlPreview } from "./sql-preview";
import { LoadParametersEditor } from "./load-parameters-editor";
import { SemanticParametersEditor } from "./semantic-parameters-editor";
import { AssetRenderView } from "./asset-render-view";
import { PipelinePlanSheet } from "./pipeline-plan-sheet";
import { FilePathPicker } from "./file-path-picker";
import {
  SemanticAssetCreateFields,
  buildSemanticAssetCreatePayload,
  defaultSemanticAssetDraft,
  type SemanticAssetDraft,
  type SemanticAssetKind,
} from "./semantic-asset-create-fields";
import {
  AppLineageCanvas,
  assetDisplayName,
  assetGroupName,
  assetNameParts,
  type AppLineageCanvasAsset,
} from "./lineage-canvas";
import { MultiValueInput } from "./multi-value-input";
import { NewNotebookDialog } from "./new-notebook-dialog";
import {
  AppPage,
  AppPanel,
  lastRunLabel,
  SeverityIcon,
  SimpleTable,
  StalenessBadge,
  StatusPill,
  stalenessDotClassName,
  stalenessLabel,
} from "./app-primitives";

const WORKING_TREE_RUN_SOURCE: PipelineRunSource = { source: "working_tree" };

export type AppBuildView = "canvas" | "split" | "code";
export type AppResultTab =
  | "inspect"
  | "render"
  | "materialize"
  | "query"
  | "typecheck"
  | "tests"
  | "diagnostics"
  | "metadata";
export type AppEditorMode = "asset" | "adhoc";

export type AppBuildSearch = {
  result?: AppResultTab;
  editor?: AppEditorMode;
};

const resultTabs: AppResultTab[] = [
  "inspect",
  "render",
  "materialize",
  "query",
  "typecheck",
  "tests",
  "diagnostics",
  "metadata",
];
const editorModes: AppEditorMode[] = ["asset", "adhoc"];

export function normalizeAppBuildSearch(search: Record<string, unknown>): AppBuildSearch {
  return {
    result: resultTabs.includes(search.result as AppResultTab)
      ? (search.result as AppResultTab)
      : undefined,
    editor: editorModes.includes(search.editor as AppEditorMode)
      ? (search.editor as AppEditorMode)
      : undefined,
  };
}

const scrollableTabsListClass = "w-max max-w-none";
const scrollableTabsTriggerClass = "flex-none";

function toUTCDateTimeInput(value: string) {
  if (!value) return "";
  const timestamp = Date.parse(value);
  if (Number.isNaN(timestamp)) return "";
  return new Date(timestamp).toISOString().slice(0, 19);
}

function fromUTCDateTimeInput(value: string) {
  if (!value) return "";
  const timestamp = Date.parse(`${value}Z`);
  if (Number.isNaN(timestamp)) return "";
  return new Date(timestamp).toISOString();
}

function isValidExecutionWindow(start: string, end: string) {
  const startTimestamp = Date.parse(start);
  const endTimestamp = Date.parse(end);
  return (
    !Number.isNaN(startTimestamp) && !Number.isNaN(endTimestamp) && endTimestamp > startTimestamp
  );
}

type BuildAsset = AppLineageCanvasAsset & {
  workspaceAsset?: WebAsset;
  pipelineId?: string;
  displayName?: string;
  prefix?: string;
  path?: string;
  type?: string;
  connection?: string;
  upstreams?: string[];
};

type AssetRenderSourceState = {
  identity: string;
  assetId: string;
  savedIntentContent: string;
  workspaceContentAtStart: string;
};

type BuildContextValue = {
  pipelineId: string;
  pipeline?: WebPipeline;
  pipelineAssets: BuildAsset[];
  routedAssetId?: string;
  selectedAssetId: string;
  selectedAsset: BuildAsset;
  view: AppBuildView;
  buildSearch: AppBuildSearch;
  editorMode: AppEditorMode;
  declaredDependencies: string[];
  addDependency: (dependency: string) => void;
  selectAsset: (assetId: string) => void;
  goToAsset: (pipelineId: string, assetId: string) => void;
  runAssetById: (assetId: string) => void;
  deleteAssetById: (assetId: string) => Promise<void>;
  goToCatalog: (assetId?: string) => void;
  openPipelineConnections: () => void;
  openNewAsset: () => void;
  openNewAssetInGroup: (prefix?: string) => void;
  createDownstreamAsset: (source: { id: string; name: string }) => void;
  openInspector: () => void;
  openBottom: (tab: AppResultTab) => void;
  materializeSelectedAsset: () => void;
  fullRefreshSelectedAsset: () => void;
  backfillSelectedAsset: () => void;
  inspectSelectedAsset: () => void;
  renderSelectedAsset: () => void;
  runAdhocQuery: () => void;
  adhocContextAsset: WebAsset | null;
  adhocLoading: boolean;
  materializeLoading: boolean;
  inspectLoading: boolean;
  renderLoading: boolean;
  renderBlockedReason?: string;
  executionBlocked: boolean;
  executionBlockedReason?: string;
};

const BuildContext = createContext<BuildContextValue | null>(null);

function useBuildContext() {
  const context = useContext(BuildContext);
  if (!context) {
    throw new Error("Build view components must be rendered inside AppBuildPage");
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

function assetDisplayFields(
  asset: WebAsset,
  pipeline: WebPipeline,
): Omit<BuildAsset, "workspaceAsset" | "path" | "type" | "connection" | "upstreams" | "x" | "y"> {
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
    // No path fallback: the node header already carries the name and the
    // inspector shows the file; repeating the path on every node is noise.
    description: asset.meta?.description ?? "",
    dir: assetDirectory(asset.path, pipeline.path),
    imports: importsFromContent(asset.content),
    status: asset.is_materialized ? "success" : "pending",
    materializedAt: asset.is_materialized ? "current" : "not materialized",
    parseError: asset.parse_error,
  };
}

function integrationForAsset(asset: WebAsset) {
  if (asset.connection) {
    return asset.connection;
  }
  const provider = asset.type.split(".")[0]?.toLowerCase();
  if (provider === "duckdb") return "DuckDB";
  if (provider === "python") return "Python";
  if (provider === "load") return "Load";
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

function normalizeAssetContentIdentity(content: string) {
  // Monaco uses the browser/OS line ending while Bruin returns parsed asset
  // content with LF endings. Treat that save-time normalization as the same
  // source so an SSE refresh cannot invalidate a preview that was just
  // rendered from the successfully saved editor value.
  return content.replace(/\r\n?/g, "\n");
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

// Shown in the editor/canvas area while the workspace is still loading, so the
// page never flashes the placeholder demo assets on refresh.
function BuildLoadingState() {
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center px-3 pb-3">
      <Loader2 className="size-6 animate-spin text-muted-foreground" />
    </div>
  );
}

export function AppBuildPage({
  pipelineId = "simple",
  selectedAssetId,
  resultTab = "inspect",
  editorMode = "asset",
  onResultTabChange,
  onAssetSelect,
}: {
  pipelineId?: string;
  selectedAssetId?: string;
  resultTab?: AppResultTab;
  editorMode?: AppEditorMode;
  onResultTabChange?: (tab: AppResultTab) => void;
  onAssetSelect?: (assetId: string) => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const navigate = useNavigate();
  const location = useLocation();
  const view = appBuildViewFromPath(location.pathname);
  const buildSearch: AppBuildSearch = useMemo(
    () => ({ result: resultTab, editor: editorMode }),
    [editorMode, resultTab],
  );
  // Until the workspace has loaded (e.g. right after a page refresh) there is no
  // real pipeline to render; the derived assets fall back to placeholder demo
  // data, so we show a loading state over the content area instead.
  const isWorkspaceLoading = !workspace;
  const activePipeline = useMemo(
    () => workspace?.pipelines.find((pipeline) => pipeline.id === pipelineId),
    [pipelineId, workspace?.pipelines],
  );
  const pipelineAssets = useMemo(
    () => (activePipeline ? assetsForPipeline(activePipeline) : fallbackBuildAssets()),
    [activePipeline],
  );
  const existingAssetNames = useMemo(
    () => new Set((activePipeline?.assets ?? []).map((asset) => asset.name)),
    [activePipeline?.assets],
  );
  const staleness = usePipelineStaleness(activePipeline?.id);
  const materializationAssets = useMemo(
    () =>
      pipelineAssets.map((asset) => ({
        id: asset.id,
        name: asset.name,
        pipelineId: asset.pipelineId,
        isMaterialized:
          asset.workspaceAsset?.is_materialized ??
          (asset.status === "success" || asset.status === "ok"),
        staleness: staleness.byAssetName[asset.name],
      })),
    [pipelineAssets, staleness.byAssetName],
  );
  const materializationStatusByAssetId = useAppAssetMaterializationStatus(materializationAssets);
  const deployState = usePipelineDeploy(activePipeline?.id);
  const environmentPolicy = useSelectedEnvironmentPolicy();
  const executionBlocked = Boolean(
    environmentPolicy?.protected || environmentPolicy?.deployed_only,
  );
  const executionBlockedReason = environmentPolicy?.protected
    ? "This environment is protected: interactive execution is disabled"
    : environmentPolicy?.deployed_only
      ? "This environment only allows deployed pipeline runs; asset and stale builds use the working tree"
      : undefined;
  const assetResults = useAssetResults();
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const effectiveEnvironment = selectedEnvironment ?? workspace?.selected_environment ?? "";
  const pipelineRunSource = useMemo<PipelineRunSource | null>(() => {
    if (!environmentPolicy?.deployed_only) {
      return WORKING_TREE_RUN_SOURCE;
    }
    const versionId = deployState.status?.version_id?.trim();
    if (
      deployState.loading ||
      deployState.error ||
      !deployState.status?.has_snapshot ||
      !deployState.status.executable ||
      !versionId
    ) {
      return null;
    }
    return { source: "snapshot", snapshot_version_id: versionId };
  }, [
    deployState.error,
    deployState.loading,
    deployState.status?.has_snapshot,
    deployState.status?.executable,
    deployState.status?.version_id,
    environmentPolicy?.deployed_only,
  ]);
  const pipelineRunSourceLabel = environmentPolicy?.deployed_only
    ? deployState.loading
      ? "Resolving source"
      : pipelineRunSource?.source === "snapshot"
        ? `Run ${deploymentLabel(deployState.status?.ordinal, pipelineRunSource.snapshot_version_id, "deployment")}`
        : "Deployment required"
    : "Run workspace";
  const selectedExecutionTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const executionWindowBlockedReason = selectedExecutionTimeWindow
    ? undefined
    : "Resolving the execution window";
  const editorDraft = useAtomValue(editorDraftAtom);
  const setEditorDraft = useSetAtom(editorDraftAtom);
  const [adhocResult, setAdhocResult] = useState<SqlQueryResponse | null>(null);
  const [adhocRenderedQuery, setAdhocRenderedQuery] = useState<string | null>(null);
  const [adhocLoading, setAdhocLoading] = useState(false);
  const [assetRenderResult, setAssetRenderResult] = useState<AssetRenderResult | null>(null);
  const [assetRenderLoading, setAssetRenderLoading] = useState(false);
  const [assetRenderError, setAssetRenderError] = useState<string | null>(null);
  const [assetRenderSource, setAssetRenderSource] = useState<AssetRenderSourceState | null>(null);
  const assetRenderRequestId = useRef(0);
  const assetRenderIdentityRef = useRef<string | null>(null);
  const [adhocQuery] = useAdhocQueryDraft(pipelineId);
  const displayedPipelineAssets = useMemo(
    () =>
      pipelineAssets.map((asset) => ({
        ...asset,
        status: materializationStatusByAssetId[asset.id]?.status ?? asset.status,
        materializedAt: labelForAppMaterializationState(materializationStatusByAssetId[asset.id]),
        staleness: staleness.byAssetName[asset.name],
      })),
    [materializationStatusByAssetId, pipelineAssets, staleness.byAssetName],
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
    [assetsByName, staleness.byAssetName],
  );
  const [staleBuildPrompt, setStaleBuildPrompt] = useState<{
    assetId: string;
    assetName: string;
    staleUpstreams: string[];
  } | null>(null);
  const [destructiveMaterializationPrompt, setDestructiveMaterializationPrompt] = useState<{
    kind: "full-refresh" | "backfill";
    assetId: string;
    assetName: string;
    start: string;
    end: string;
  } | null>(null);
  const [destructiveMaterializationConfirmation, setDestructiveMaterializationConfirmation] =
    useState("");
  const firstAssetId = displayedPipelineAssets[0]?.id ?? "revenue_daily";
  const [visualSelectedAssetId, setVisualSelectedAssetId] = useState(
    selectedAssetId ?? firstAssetId,
  );
  const effectiveSelectedAssetId = visualSelectedAssetId ?? selectedAssetId ?? firstAssetId;
  const selectedAsset =
    displayedPipelineAssets.find((asset) => asset.id === effectiveSelectedAssetId) ??
    displayedPipelineAssets[0] ??
    fallbackBuildAssets()[0];
  const selectedWorkspaceAsset = selectedAsset?.workspaceAsset;
  const selectedAssetSavedIntentContent = selectedWorkspaceAsset
    ? (editorDraft[selectedWorkspaceAsset.id] ?? selectedWorkspaceAsset.content)
    : null;
  const selectedAssetSavedIntentIdentity =
    selectedAssetSavedIntentContent === null
      ? null
      : normalizeAssetContentIdentity(selectedAssetSavedIntentContent);
  const selectedAssetRenderIdentity =
    activePipeline &&
    selectedWorkspaceAsset &&
    selectedAssetSavedIntentIdentity !== null &&
    selectedExecutionTimeWindow
      ? JSON.stringify([
          activePipeline.id,
          selectedWorkspaceAsset.id,
          selectedAssetSavedIntentIdentity,
          effectiveEnvironment || "default",
          selectedExecutionTimeWindow.start,
          selectedExecutionTimeWindow.end,
          false,
        ])
      : null;
  assetRenderIdentityRef.current = selectedAssetRenderIdentity;
  const assetRenderWorkspaceContentCompatible = Boolean(
    assetRenderSource &&
    selectedWorkspaceAsset?.id === assetRenderSource.assetId &&
    (normalizeAssetContentIdentity(selectedWorkspaceAsset.content) ===
      normalizeAssetContentIdentity(assetRenderSource.savedIntentContent) ||
      normalizeAssetContentIdentity(selectedWorkspaceAsset.content) ===
        normalizeAssetContentIdentity(assetRenderSource.workspaceContentAtStart)),
  );
  const assetRenderMatchesSelection =
    selectedAssetRenderIdentity !== null &&
    assetRenderSource?.identity === selectedAssetRenderIdentity &&
    assetRenderWorkspaceContentCompatible;
  const visibleAssetRenderResult = assetRenderMatchesSelection ? assetRenderResult : null;
  const visibleAssetRenderLoading = assetRenderMatchesSelection && assetRenderLoading;
  const visibleAssetRenderError = assetRenderMatchesSelection ? assetRenderError : null;
  const [explorerOpen, setExplorerOpen] = useState(false);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  // Large-screen collapse for the side columns; small screens keep using the
  // Sheets above. Collapsed columns are dropped from the grid entirely and
  // reopened from the top-bar toggles.
  const [explorerCollapsed, setExplorerCollapsed] = useState(false);
  const [inspectorCollapsed, setInspectorCollapsed] = useState(false);
  const sidePanelGridColsClass = explorerCollapsed
    ? inspectorCollapsed
      ? "xl:grid-cols-[minmax(0,1fr)]"
      : "xl:grid-cols-[minmax(0,1fr)_320px]"
    : inspectorCollapsed
      ? "xl:grid-cols-[248px_minmax(0,1fr)]"
      : "xl:grid-cols-[248px_minmax(0,1fr)_320px]";
  const [newAssetOpen, setNewAssetOpen] = useState(false);
  const [newAssetPrefix, setNewAssetPrefix] = useState<string | null>(null);
  const [newPipelineOpen, setNewPipelineOpen] = useState(false);
  const [newFolderOpen, setNewFolderOpen] = useState(false);
  // Path of a pipeline just created here; once the workspace SSE update lists
  // it, we navigate onto it (the create response carries no ID).
  const [pendingPipelinePath, setPendingPipelinePath] = useState<string | null>(null);
  const [downstreamSource, setDownstreamSource] = useState<{
    id: string;
    name: string;
  } | null>(null);
  const [pipelineSettingsOpen, setPipelineSettingsOpen] = useState(false);
  const [pipelineSettingsSection, setPipelineSettingsSection] = useState<
    PipelineSettingsSection | undefined
  >(undefined);
  const openPipelineSettings = (section?: PipelineSettingsSection) => {
    setPipelineSettingsSection(section);
    setPipelineSettingsOpen(true);
  };
  const [buildStaleOpen, setBuildStaleOpen] = useState(false);
  const [pipelinePlanOpen, setPipelinePlanOpen] = useState(false);
  const [deploymentPlanOpen, setDeploymentPlanOpen] = useState(false);
  const [addedDependencies, setAddedDependencies] = useState<string[]>([]);
  const declaredDependencies = [...pipelineDependencies, ...addedDependencies];
  const [typeCheckReport, setTypeCheckReport] = useState<PipelineTypeCheckReport | null>(null);
  const [typeCheckLoading, setTypeCheckLoading] = useState(false);
  const [typeCheckError, setTypeCheckError] = useState<string | null>(null);
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

  useEffect(() => {
    assetRenderRequestId.current += 1;
    setAssetRenderSource(null);
    setAssetRenderResult(null);
    setAssetRenderError(null);
    setAssetRenderLoading(false);
  }, [selectedAssetRenderIdentity]);

  useEffect(() => {
    if (!assetRenderSource || selectedWorkspaceAsset?.id !== assetRenderSource.assetId) return;
    const workspaceContent = selectedWorkspaceAsset.content;
    if (
      normalizeAssetContentIdentity(workspaceContent) ===
        normalizeAssetContentIdentity(assetRenderSource.savedIntentContent) ||
      normalizeAssetContentIdentity(workspaceContent) ===
        normalizeAssetContentIdentity(assetRenderSource.workspaceContentAtStart)
    ) {
      return;
    }
    setEditorDraft((previous) => {
      if (previous[assetRenderSource.assetId] !== assetRenderSource.savedIntentContent) {
        return previous;
      }
      const next = { ...previous };
      delete next[assetRenderSource.assetId];
      return next;
    });
  }, [assetRenderSource, selectedWorkspaceAsset, setEditorDraft]);

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
      to: "/pipelines/$pipelineId/canvas",
      params: { pipelineId: workspace.pipelines[0].id },
      search: buildSearch,
      replace: true,
    });
  }, [activePipeline, buildSearch, navigate, workspace?.pipelines]);

  useEffect(() => {
    if (!pendingPipelinePath || !workspace?.pipelines?.length) {
      return;
    }
    const normalized = pendingPipelinePath.replace(/^\.?\//, "").replace(/\/+$/, "");
    const created = workspace.pipelines.find(
      (item) => item.path === normalized || item.path.startsWith(`${normalized}/`),
    );
    if (created) {
      setPendingPipelinePath(null);
      void navigate({
        to: "/pipelines/$pipelineId/canvas",
        params: { pipelineId: created.id },
        search: buildSearch,
      });
    }
  }, [buildSearch, navigate, pendingPipelinePath, workspace?.pipelines]);

  const openBottom = (tab: AppResultTab) => {
    onResultTabChange?.(tab);
    // Make sure the results are visible when something routes output here.
    resultsPanelRef.current?.expand();
  };
  const addDependency = (dependency: string) => {
    setAddedDependencies((current) =>
      current.includes(dependency) ? current : [...current, dependency],
    );
  };
  const runTypeCheck = useCallback(
    async (openTab = false) => {
      if (!activePipeline) {
        return;
      }
      if (openTab) {
        openBottom("typecheck");
      }
      setTypeCheckLoading(true);
      setTypeCheckError(null);
      try {
        await awaitWorkspaceSaves();
        const report = await typeCheckPipeline(activePipeline.id, {
          startDate: selectedExecutionTimeWindow?.start,
          endDate: selectedExecutionTimeWindow?.end,
        });
        setTypeCheckReport(report);
      } catch (cause) {
        setTypeCheckError(cause instanceof Error ? cause.message : "Type check failed.");
      } finally {
        setTypeCheckLoading(false);
      }
    },
    [activePipeline, selectedExecutionTimeWindow?.start, selectedExecutionTimeWindow?.end],
  );
  // Run the type check once per pipeline so the notification badge reflects the
  // current state; the user can re-run from the bell to pick up edits.
  useEffect(() => {
    if (!activePipeline) {
      return;
    }
    void runTypeCheck(false);
  }, [activePipeline?.id, runTypeCheck]);
  const runMaterialize = (assetId: string, name: string, scope: MaterializeScope = "asset") => {
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
    if (!activePipeline || !workspaceAsset || !selectedExecutionTimeWindow) {
      return;
    }
    requestMaterialize(workspaceAsset.id, selectedAsset.name);
  };
  const fullRefreshSelectedAsset = () => {
    const workspaceAsset = selectedAsset?.workspaceAsset;
    if (!activePipeline || !workspaceAsset) {
      return;
    }
    setDestructiveMaterializationConfirmation("");
    setDestructiveMaterializationPrompt({
      kind: "full-refresh",
      assetId: workspaceAsset.id,
      assetName: selectedAsset.name,
      start: selectedExecutionTimeWindow?.start ?? "",
      end: selectedExecutionTimeWindow?.end ?? "",
    });
  };
  const backfillSelectedAsset = () => {
    const workspaceAsset = selectedAsset?.workspaceAsset;
    if (!activePipeline || !workspaceAsset) {
      return;
    }
    setDestructiveMaterializationConfirmation("");
    setDestructiveMaterializationPrompt({
      kind: "backfill",
      assetId: workspaceAsset.id,
      assetName: selectedAsset.name,
      start: selectedExecutionTimeWindow?.start ?? "",
      end: selectedExecutionTimeWindow?.end ?? "",
    });
  };
  const confirmDestructiveMaterialization = () => {
    if (!destructiveMaterializationPrompt) return;
    const isBackfill = destructiveMaterializationPrompt.kind === "backfill";
    openBottom("materialize");
    void assetResults.runMaterializeForAsset(
      destructiveMaterializationPrompt.assetId,
      "asset",
      undefined,
      {
        assetName: destructiveMaterializationPrompt.assetName,
        fullRefresh: !isBackfill,
        backfill: isBackfill,
        timeWindow:
          destructiveMaterializationPrompt.start && destructiveMaterializationPrompt.end
            ? {
                start: destructiveMaterializationPrompt.start,
                end: destructiveMaterializationPrompt.end,
              }
            : null,
        confirmedEnvironment: environmentPolicy?.confirm_destructive
          ? destructiveMaterializationConfirmation.trim()
          : undefined,
      },
    );
    setDestructiveMaterializationPrompt(null);
    setDestructiveMaterializationConfirmation("");
  };
  const inspectSelectedAsset = () => {
    const workspaceAsset = selectedAsset?.workspaceAsset;
    if (!activePipeline || !workspaceAsset) {
      return;
    }
    openBottom("inspect");
    void assetResults.runInspectForAsset(
      workspaceAsset.id,
      editorDraft[workspaceAsset.id] ?? workspaceAsset.content,
    );
  };
  const renderSelectedAsset = async () => {
    const workspaceAsset = selectedAsset?.workspaceAsset;
    const executionWindow = selectedExecutionTimeWindow;
    if (!activePipeline || !workspaceAsset || !executionWindow) {
      return;
    }
    const sourceIdentity = selectedAssetRenderIdentity;
    const sourceIntentContent = selectedAssetSavedIntentContent;
    if (!sourceIdentity || sourceIntentContent === null) {
      return;
    }
    const requestId = ++assetRenderRequestId.current;
    openBottom("render");
    setAssetRenderSource({
      identity: sourceIdentity,
      assetId: workspaceAsset.id,
      savedIntentContent: sourceIntentContent,
      workspaceContentAtStart: workspaceAsset.content,
    });
    setAssetRenderLoading(true);
    setAssetRenderError(null);
    try {
      await awaitWorkspaceSaves();
      const result = await renderAsset(workspaceAsset.id, {
        environment: effectiveEnvironment || undefined,
        start_date: executionWindow.start,
        end_date: executionWindow.end,
        full_refresh: false,
      });
      if (
        assetRenderRequestId.current === requestId &&
        assetRenderIdentityRef.current === sourceIdentity
      ) {
        setAssetRenderResult(result);
      }
    } catch (cause) {
      if (
        assetRenderRequestId.current === requestId &&
        assetRenderIdentityRef.current === sourceIdentity
      ) {
        setAssetRenderError(cause instanceof Error ? cause.message : "Asset render failed.");
      }
    } finally {
      if (
        assetRenderRequestId.current === requestId &&
        assetRenderIdentityRef.current === sourceIdentity
      ) {
        setAssetRenderLoading(false);
      }
    }
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
        error:
          "No SQL connection found for this pipeline; add a SQL asset or configure a connection first.",
      });
      return;
    }
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
      setAdhocResult({
        status: "error",
        columns: [],
        rows: [],
        error: String(error),
      });
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
      to: appAssetViewPath(view),
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
    void navigate({
      to: "/catalog",
      search: assetId ? { asset: assetId } : {},
    });
  };
  // The ad hoc editor only renders in code/split editor panes. Preserve the
  // current editor layout when one exists, and add an editor beside the canvas
  // when it does not. Clicking again toggles back to the current asset.
  const openAdhoc = () => {
    setExplorerOpen(false);
    if (editorMode === "adhoc") {
      void navigate({
        to: appAssetViewPath(view),
        params: { pipelineId, assetId: effectiveSelectedAssetId },
        search: { ...buildSearch, editor: "asset" },
      });
      return;
    }
    void navigate({
      to: appAssetViewPath(view === "canvas" ? "split" : view),
      params: { pipelineId, assetId: effectiveSelectedAssetId },
      search: { ...buildSearch, editor: "adhoc" },
    });
  };
  const openNewAsset = () => {
    setDownstreamSource(null);
    setNewAssetPrefix(null);
    setNewAssetOpen(true);
  };
  // Canvas right-click entry point: seeds the dialog's name suggestion with
  // the prefix group the click landed in.
  const openNewAssetInGroup = (prefix?: string) => {
    setDownstreamSource(null);
    setNewAssetPrefix(prefix ?? null);
    setNewAssetOpen(true);
  };
  const createDownstreamAsset = (source: { id: string; name: string }) => {
    setDownstreamSource(source);
    setNewAssetPrefix(null);
    setNewAssetOpen(true);
  };
  const buildContext: BuildContextValue = {
    pipelineId,
    pipeline: activePipeline,
    pipelineAssets: displayedPipelineAssets,
    routedAssetId: selectedAssetId,
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
    openPipelineConnections: () => openPipelineSettings("connections"),
    openNewAsset,
    openNewAssetInGroup,
    createDownstreamAsset,
    openInspector: () => setInspectorOpen(true),
    openBottom,
    materializeSelectedAsset,
    fullRefreshSelectedAsset,
    backfillSelectedAsset,
    inspectSelectedAsset,
    renderSelectedAsset: () => void renderSelectedAsset(),
    runAdhocQuery,
    adhocContextAsset,
    adhocLoading,
    materializeLoading: assetResults.materializeLoading,
    inspectLoading: assetResults.inspectLoading,
    renderLoading: visibleAssetRenderLoading,
    renderBlockedReason: executionWindowBlockedReason,
    executionBlocked,
    executionBlockedReason,
  };

  return (
    <BuildContext.Provider value={buildContext}>
      <AppPage>
        <BuildTopBar
          pipelineId={pipelineId}
          pipelineLabel={activePipeline?.name ?? pipelineId}
          selectedAsset={selectedAsset}
          selectedAssetId={effectiveSelectedAssetId}
          assetCrumbLoading={!selectedAsset.workspaceAsset}
          resultTab={resultTab}
          editorMode={editorMode}
          currentView={view}
          onOpenExplorer={() => setExplorerOpen(true)}
          onOpenInspector={() => setInspectorOpen(true)}
          explorerCollapsed={explorerCollapsed}
          inspectorCollapsed={inspectorCollapsed}
          onToggleExplorer={() => setExplorerCollapsed((value) => !value)}
          onToggleInspector={() => setInspectorCollapsed((value) => !value)}
          onReviewRun={() => setPipelinePlanOpen(true)}
          onReviewDeploy={() => setDeploymentPlanOpen(true)}
          typeCheckReport={typeCheckReport}
          typeCheckLoading={typeCheckLoading}
          typeCheckError={typeCheckError}
          onTypeCheck={() => void runTypeCheck(true)}
          staleCount={staleness.staleAssets.length}
          stalenessLoading={staleness.loading}
          stalenessError={staleness.error}
          onBuildStale={() => setBuildStaleOpen(true)}
          deployState={deployState}
          executionBlocked={executionBlocked}
          executionBlockedReason={executionBlockedReason}
          runSourceLabel={pipelineRunSourceLabel.replace(/^Run /, "")}
          runDisabled={!activePipeline}
          runTitle="Review the saved source, readiness checks, and rendered operations before running"
        />
        {isWorkspaceLoading ? (
          <BuildLoadingState />
        ) : (
          <div
            className={cn(
              "grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3",
              sidePanelGridColsClass,
            )}
          >
            {!explorerCollapsed ? (
              <AppPanel className="hidden min-h-0 xl:flex xl:flex-col">
                <Explorer
                  pipelineId={pipelineId}
                  selectedAssetId={effectiveSelectedAssetId}
                  buildSearch={buildSearch}
                  onAssetSelect={selectAsset}
                  onAdhoc={openAdhoc}
                  onNewAsset={openNewAsset}
                  onNewPipeline={() => setNewPipelineOpen(true)}
                  onNewFolder={() => setNewFolderOpen(true)}
                  onPipelineSettings={() => openPipelineSettings()}
                />
              </AppPanel>
            ) : null}

            <PanelGroup orientation="vertical" className="h-full min-h-0">
              <Panel minSize="120px" className="min-h-0">
                <AppPanel className="relative flex h-full min-h-0 overflow-hidden">
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
                </AppPanel>
              </Panel>
              <PanelResizeHandle
                className={cn(
                  "my-1.5 h-1.5 shrink-0 cursor-row-resize rounded-full bg-border transition-colors hover:bg-primary/40",
                  resultsCollapsed && "pointer-events-none opacity-0",
                )}
              />
              <Panel
                collapsible
                collapsedSize="38px"
                minSize="120px"
                defaultSize="224px"
                panelRef={resultsPanelRef}
                onResize={() =>
                  setResultsCollapsed(Boolean(resultsPanelRef.current?.isCollapsed()))
                }
                className="min-h-0"
              >
                <ResultsPanel
                  activeTab={resultTab}
                  onTabChange={openBottom}
                  collapsed={resultsCollapsed}
                  onToggleCollapse={toggleResultsPanel}
                  typeCheckReport={typeCheckReport}
                  typeCheckLoading={typeCheckLoading}
                  typeCheckError={typeCheckError}
                  onRunTypeCheck={() => void runTypeCheck(false)}
                  onSelectAsset={selectAsset}
                  inspectResult={assetResults.inspectResult}
                  inspectLoading={assetResults.inspectLoading}
                  renderResult={visibleAssetRenderResult}
                  renderLoading={visibleAssetRenderLoading}
                  renderError={visibleAssetRenderError}
                  onRender={() => void renderSelectedAsset()}
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

            {!inspectorCollapsed ? (
              <AppPanel className="hidden min-h-0 xl:flex xl:flex-col">
                <Inspector asset={selectedAsset} />
              </AppPanel>
            ) : null}
          </div>
        )}

        <Sheet open={explorerOpen} onOpenChange={setExplorerOpen}>
          <SheetContent side="left" className="w-80 gap-0 p-0 sm:max-w-80">
            <SheetTitle className="sr-only">Explorer</SheetTitle>
            <Explorer
              pipelineId={pipelineId}
              selectedAssetId={effectiveSelectedAssetId}
              buildSearch={buildSearch}
              onAssetSelect={selectAsset}
              onAdhoc={openAdhoc}
              onNewAsset={openNewAsset}
              onNewPipeline={() => setNewPipelineOpen(true)}
              onNewFolder={() => setNewFolderOpen(true)}
              onPipelineSettings={() => openPipelineSettings()}
            />
          </SheetContent>
        </Sheet>
        <Sheet open={inspectorOpen} onOpenChange={setInspectorOpen}>
          <SheetContent side="right" className="w-[22rem] gap-0 p-0 sm:max-w-[22rem]">
            <SheetTitle className="sr-only">Asset properties</SheetTitle>
            <Inspector asset={selectedAsset} />
          </SheetContent>
        </Sheet>

        <PipelinePlanSheet
          open={pipelinePlanOpen}
          onOpenChange={setPipelinePlanOpen}
          pipelineId={activePipeline?.id ?? pipelineId}
          pipelineName={activePipeline?.name ?? pipelineId}
          environment={effectiveEnvironment}
          timeWindow={selectedExecutionTimeWindow}
          source={pipelineRunSource}
          confirmDestructive={Boolean(environmentPolicy?.confirm_destructive)}
          onAccepted={(run, plan) => {
            openBottom("materialize");
            assetResults.trackConfirmedPipelineRun(run, {
              start: plan.context.start_date,
              end: plan.context.end_date,
            });
          }}
        />
        <PipelinePlanSheet
          open={deploymentPlanOpen}
          onOpenChange={setDeploymentPlanOpen}
          pipelineId={activePipeline?.id ?? pipelineId}
          pipelineName={activePipeline?.name ?? pipelineId}
          environment={effectiveEnvironment}
          timeWindow={selectedExecutionTimeWindow}
          intent="deploy"
          onDeploy={(expectedSourceMerkle) => deployState.deploy(expectedSourceMerkle)}
        />

        <NewAssetDialog
          open={newAssetOpen}
          onOpenChange={(open) => {
            setNewAssetOpen(open);
            if (!open) {
              setDownstreamSource(null);
              setNewAssetPrefix(null);
            }
          }}
          pipelineId={activePipeline?.id}
          pipelineName={activePipeline?.name}
          existingAssetNames={existingAssetNames}
          downstreamSource={downstreamSource}
          namePrefix={newAssetPrefix}
          onCreated={(assetId) => goToAsset(activePipeline?.id ?? pipelineId, assetId)}
        />
        <NewPipelineDialog
          open={newPipelineOpen}
          onOpenChange={setNewPipelineOpen}
          existingPaths={new Set((workspace?.pipelines ?? []).map((item) => item.path))}
          onCreated={(path) => setPendingPipelinePath(path)}
        />
        <NewFolderDialog
          open={newFolderOpen}
          onOpenChange={setNewFolderOpen}
          pipelineName={activePipeline?.name}
          onConfirm={(prefix) => {
            setNewFolderOpen(false);
            openNewAssetInGroup(prefix);
          }}
        />
        <PipelineSettingsDialog
          open={pipelineSettingsOpen}
          onOpenChange={setPipelineSettingsOpen}
          pipelineId={pipelineId}
          initialSection={pipelineSettingsSection}
        />
        <Dialog
          open={destructiveMaterializationPrompt !== null}
          onOpenChange={(open) => {
            if (!open) {
              setDestructiveMaterializationPrompt(null);
              setDestructiveMaterializationConfirmation("");
            }
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>
                {destructiveMaterializationPrompt?.kind === "backfill"
                  ? "Backfill"
                  : "Full refresh"}{" "}
                {destructiveMaterializationPrompt?.assetName}?
              </DialogTitle>
              <DialogDescription>
                {destructiveMaterializationPrompt?.kind === "backfill"
                  ? "Run this asset for the exact UTC window below. Backfill is available only when independent windows can be replayed safely."
                  : "This rebuilds the table from scratch and can be expensive. Existing rows are replaced with the result for the selected execution window."}
              </DialogDescription>
            </DialogHeader>
            {destructiveMaterializationPrompt?.kind === "backfill" ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="backfill-start">Start (UTC)</Label>
                  <Input
                    id="backfill-start"
                    type="datetime-local"
                    step={1}
                    value={toUTCDateTimeInput(destructiveMaterializationPrompt.start)}
                    onChange={(event) =>
                      setDestructiveMaterializationPrompt((current) =>
                        current
                          ? { ...current, start: fromUTCDateTimeInput(event.target.value) }
                          : current,
                      )
                    }
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="backfill-end">End (UTC)</Label>
                  <Input
                    id="backfill-end"
                    type="datetime-local"
                    step={1}
                    value={toUTCDateTimeInput(destructiveMaterializationPrompt.end)}
                    onChange={(event) =>
                      setDestructiveMaterializationPrompt((current) =>
                        current
                          ? { ...current, end: fromUTCDateTimeInput(event.target.value) }
                          : current,
                      )
                    }
                  />
                </div>
              </div>
            ) : null}
            {environmentPolicy?.confirm_destructive ? (
              <div className="space-y-2">
                <Label htmlFor="destructive-materialization-environment-confirmation">
                  Type <span className="font-mono">{effectiveEnvironment}</span> to confirm
                </Label>
                <Input
                  id="destructive-materialization-environment-confirmation"
                  value={destructiveMaterializationConfirmation}
                  onChange={(event) =>
                    setDestructiveMaterializationConfirmation(event.target.value)
                  }
                  autoComplete="off"
                />
              </div>
            ) : null}
            <DialogFooter>
              <Button variant="outline" onClick={() => setDestructiveMaterializationPrompt(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={
                  (destructiveMaterializationPrompt?.kind === "backfill" &&
                    !isValidExecutionWindow(
                      destructiveMaterializationPrompt.start,
                      destructiveMaterializationPrompt.end,
                    )) ||
                  (Boolean(environmentPolicy?.confirm_destructive) &&
                    destructiveMaterializationConfirmation.trim() !== effectiveEnvironment)
                }
                onClick={confirmDestructiveMaterialization}
              >
                Run{" "}
                {destructiveMaterializationPrompt?.kind === "backfill"
                  ? "backfill"
                  : "full refresh"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <Dialog
          open={staleBuildPrompt !== null}
          onOpenChange={(open) => {
            if (!open) setStaleBuildPrompt(null);
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <AlertTriangle className="size-4 text-amber-500" />
                Upstream is out of date
              </DialogTitle>
              <DialogDescription>
                <span className="font-mono">{staleBuildPrompt?.assetName}</span> depends on{" "}
                {staleBuildPrompt?.staleUpstreams.length} stale upstream
                {staleBuildPrompt?.staleUpstreams.length === 1 ? "" : "s"}. Building now reads their
                outdated tables, so this asset will stay stale until its upstreams are current.
                Build the upstreams first to get an up-to-date result.
              </DialogDescription>
            </DialogHeader>
            <div className="max-h-40 space-y-1 overflow-y-auto rounded-md border p-2 font-mono text-xs">
              {staleBuildPrompt?.staleUpstreams.map((name) => (
                <div key={name} className="flex items-center gap-1.5">
                  <span className="size-1.5 rounded-full bg-amber-500" />
                  {name}
                </div>
              ))}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setStaleBuildPrompt(null)}>
                Cancel
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  if (staleBuildPrompt)
                    runMaterialize(staleBuildPrompt.assetId, staleBuildPrompt.assetName, "asset");
                  setStaleBuildPrompt(null);
                }}
              >
                Build anyway
              </Button>
              <Button
                onClick={() => {
                  if (staleBuildPrompt)
                    runMaterialize(
                      staleBuildPrompt.assetId,
                      staleBuildPrompt.assetName,
                      "asset_with_upstreams",
                    );
                  setStaleBuildPrompt(null);
                }}
              >
                <Hammer className="size-4" />
                Build upstreams first
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <BuildStaleDialog
          open={buildStaleOpen}
          onOpenChange={setBuildStaleOpen}
          staleAssets={staleness.staleAssets}
          onBuild={(onAssetEvent) => {
            openBottom("materialize");
            const idByName = new Map(
              displayedPipelineAssets.map((asset) => [asset.name, asset.id]),
            );
            const assetIds = staleness.staleAssets
              .map((stale) => idByName.get(stale.asset_name))
              .filter((id): id is string => Boolean(id));
            return assetResults.runBuildStale(activePipeline?.id ?? pipelineId, {
              assetIds,
              onAssetEvent,
            });
          }}
        />
      </AppPage>
    </BuildContext.Provider>
  );
}

function BuildTopBar({
  pipelineId,
  pipelineLabel,
  selectedAsset,
  selectedAssetId,
  assetCrumbLoading = false,
  resultTab,
  editorMode,
  currentView,
  onOpenExplorer,
  onOpenInspector,
  explorerCollapsed = false,
  inspectorCollapsed = false,
  onToggleExplorer,
  onToggleInspector,
  onReviewRun,
  onReviewDeploy,
  staleCount = 0,
  stalenessLoading = false,
  stalenessError,
  onBuildStale,
  deployState,
  executionBlocked = false,
  executionBlockedReason,
  runSourceLabel,
  runDisabled = false,
  runTitle,
  typeCheckReport,
  typeCheckLoading = false,
  typeCheckError,
  onTypeCheck,
}: {
  pipelineId: string;
  pipelineLabel: string;
  selectedAsset: BuildAsset;
  selectedAssetId: string;
  assetCrumbLoading?: boolean;
  resultTab: AppResultTab;
  editorMode: AppEditorMode;
  currentView: AppBuildView;
  onOpenExplorer: () => void;
  onOpenInspector: () => void;
  explorerCollapsed?: boolean;
  inspectorCollapsed?: boolean;
  onToggleExplorer?: () => void;
  onToggleInspector?: () => void;
  onReviewRun: () => void;
  onReviewDeploy: () => void;
  staleCount?: number;
  stalenessLoading?: boolean;
  stalenessError?: string | null;
  onBuildStale?: () => void;
  deployState?: PipelineDeployState;
  executionBlocked?: boolean;
  executionBlockedReason?: string;
  runSourceLabel?: string;
  runDisabled?: boolean;
  runTitle?: string;
  typeCheckReport?: PipelineTypeCheckReport | null;
  typeCheckLoading?: boolean;
  typeCheckError?: string | null;
  onTypeCheck?: () => void;
}) {
  const search: AppBuildSearch = { result: resultTab, editor: editorMode };

  return (
    <div className="flex min-h-12 shrink-0 items-center gap-2 px-3">
      <Button variant="ghost" size="sm" className="xl:hidden" onClick={onOpenExplorer}>
        <PanelLeft className="size-3.5" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="hidden xl:inline-flex"
        onClick={onToggleExplorer}
        aria-pressed={!explorerCollapsed}
        title={explorerCollapsed ? "Show explorer" : "Hide explorer"}
        aria-label={explorerCollapsed ? "Show explorer" : "Hide explorer"}
      >
        {explorerCollapsed ? (
          <PanelLeftOpen className="size-3.5" />
        ) : (
          <PanelLeftClose className="size-3.5" />
        )}
      </Button>
      <Breadcrumb className="min-w-0 flex-1">
        <BreadcrumbList className="flex-nowrap text-xs">
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbLink asChild className="truncate">
              <Link to="/">data_platform</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <span className="text-muted-foreground">pipeline</span>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbLink asChild className="truncate font-mono">
              <Link to="/pipelines/$pipelineId/canvas" params={{ pipelineId }} search={search}>
                {pipelineLabel}
              </Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem className="min-w-0">
            {assetCrumbLoading ? (
              <Skeleton className="h-4 w-32" aria-label="Loading asset" />
            ) : (
              <BreadcrumbPage className="truncate font-mono">{selectedAsset.name}</BreadcrumbPage>
            )}
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      {/* Toggle: a second click leaves ad-hoc mode and returns to the asset. */}
      <Button
        asChild
        variant={editorMode === "adhoc" ? "secondary" : "outline"}
        size="sm"
        className={cn(
          "hidden lg:inline-flex",
          editorMode === "adhoc" ? "text-primary ring-1 ring-primary/30" : null,
        )}
      >
        <Link
          to={appAssetViewPath(
            editorMode === "adhoc" ? currentView : currentView === "canvas" ? "split" : currentView,
          )}
          params={{ pipelineId, assetId: selectedAssetId }}
          search={{
            result: resultTab,
            editor: editorMode === "adhoc" ? "asset" : "adhoc",
          }}
          aria-pressed={editorMode === "adhoc"}
        >
          <Terminal className="size-3.5" /> Ad-hoc
        </Link>
      </Button>
      <ReadinessControl
        report={typeCheckReport}
        typeCheckLoading={typeCheckLoading}
        typeCheckError={typeCheckError}
        onTypeCheck={onTypeCheck}
        staleCount={staleCount}
        stalenessLoading={stalenessLoading}
        stalenessError={stalenessError}
        onBuildStale={onBuildStale}
        buildDisabled={executionBlocked}
        buildDisabledReason={executionBlockedReason}
      />
      {deployState ? <DeployButton deployState={deployState} onReview={onReviewDeploy} /> : null}
      <Button size="sm" onClick={onReviewRun} disabled={runDisabled} title={runTitle}>
        <Play data-icon="inline-start" /> Review run
        {runSourceLabel ? <span className="sr-only"> from {runSourceLabel}</span> : null}
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="hidden xl:inline-flex"
        onClick={onToggleInspector}
        aria-pressed={!inspectorCollapsed}
        title={inspectorCollapsed ? "Show properties" : "Hide properties"}
        aria-label={inspectorCollapsed ? "Show properties" : "Hide properties"}
      >
        {inspectorCollapsed ? (
          <PanelRightOpen className="size-3.5" />
        ) : (
          <PanelRightClose className="size-3.5" />
        )}
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="xl:hidden"
        onClick={onOpenInspector}
        title="Asset properties"
        aria-label="Asset properties"
      >
        <PanelRight className="size-3.5" />
      </Button>
    </div>
  );
}

// ReadinessControl keeps definition and data readiness in one secondary
// surface so Deploy and the primary run-review action retain clear hierarchy.
function ReadinessControl({
  report,
  typeCheckLoading,
  typeCheckError,
  onTypeCheck,
  staleCount,
  stalenessLoading,
  stalenessError,
  onBuildStale,
  buildDisabled,
  buildDisabledReason,
}: {
  report?: PipelineTypeCheckReport | null;
  typeCheckLoading?: boolean;
  typeCheckError?: string | null;
  onTypeCheck?: () => void;
  staleCount: number;
  stalenessLoading?: boolean;
  stalenessError?: string | null;
  onBuildStale?: () => void;
  buildDisabled?: boolean;
  buildDisabledReason?: string;
}) {
  const errors = report?.summary.errors ?? 0;
  const warnings = report?.summary.warnings ?? 0;
  const definitionProblems = errors + warnings;
  const problemCount = definitionProblems + (stalenessError ? 1 : staleCount);
  const loading = typeCheckLoading || stalenessLoading;
  const hasError = Boolean(typeCheckError || stalenessError || errors > 0);
  const definitionLabel = typeCheckLoading
    ? "Checking definition…"
    : typeCheckError
      ? "Definition check failed"
      : definitionProblems > 0
        ? `${errors} errors · ${warnings} warnings`
        : report
          ? "Definition ready"
          : "Definition not checked";
  const dataLabel = stalenessLoading
    ? "Checking data…"
    : stalenessError
      ? "Freshness unavailable"
      : staleCount > 0
        ? `${staleCount} asset${staleCount === 1 ? "" : "s"} need work`
        : "Data is current";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant={hasError ? "destructive" : "outline"}
          size="sm"
          aria-label={`Readiness: ${definitionLabel}; ${dataLabel}`}
          title={`${definitionLabel} · ${dataLabel}`}
        >
          {loading ? (
            <Loader2 data-icon="inline-start" className="animate-spin" />
          ) : hasError || problemCount > 0 ? (
            <AlertTriangle data-icon="inline-start" />
          ) : report ? (
            <CheckCircle2 data-icon="inline-start" />
          ) : (
            <Bell data-icon="inline-start" />
          )}
          <span className="hidden lg:inline">Readiness</span>
          {!loading && problemCount > 0 ? (
            <Badge variant={hasError ? "destructive" : "secondary"} size="xs">
              {problemCount}
            </Badge>
          ) : null}
          <ChevronDown data-icon="inline-end" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Definition</DropdownMenuLabel>
          <DropdownMenuItem onSelect={onTypeCheck} disabled={typeCheckLoading}>
            {typeCheckLoading ? (
              <Loader2 className="animate-spin" />
            ) : typeCheckError || definitionProblems > 0 ? (
              <AlertTriangle />
            ) : (
              <CheckCircle2 />
            )}
            <div className="min-w-0 flex-1">
              <div>Code checks</div>
              <div className="truncate text-[10px] text-muted-foreground">{definitionLabel}</div>
            </div>
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuGroup>
          <DropdownMenuLabel>Data</DropdownMenuLabel>
          <DropdownMenuItem
            onSelect={onBuildStale}
            disabled={
              stalenessLoading || Boolean(stalenessError) || staleCount === 0 || buildDisabled
            }
            title={buildDisabled ? buildDisabledReason : (stalenessError ?? undefined)}
          >
            {stalenessLoading ? (
              <Loader2 className="animate-spin" />
            ) : stalenessError ? (
              <AlertTriangle />
            ) : staleCount > 0 ? (
              <Hammer />
            ) : (
              <CheckCircle2 />
            )}
            <div className="min-w-0 flex-1">
              <div>Build needed</div>
              <div className="truncate text-[10px] text-muted-foreground">
                {buildDisabled ? buildDisabledReason : dataLabel}
              </div>
            </div>
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
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
  currentView: AppBuildView;
  search: AppBuildSearch;
  onNewAsset?: () => void;
}) {
  return (
    <div className="absolute right-1 top-1 z-20 flex items-center gap-2">
      {onNewAsset ? (
        <Button size="sm" onClick={onNewAsset} className="shadow-sm">
          <Plus className="size-3.5" /> New asset
        </Button>
      ) : null}
      <BuildViewButtonGroup
        pipelineId={pipelineId}
        selectedAssetId={selectedAssetId}
        currentView={currentView}
        search={search}
        className="rounded-lg border bg-background/90 shadow-sm backdrop-blur"
      />
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
  currentView: AppBuildView;
  search: AppBuildSearch;
  className?: string;
}) {
  return (
    <ButtonGroup className={className}>
      <ViewLink
        pipelineId={pipelineId}
        selectedAssetId={selectedAssetId}
        currentView={currentView}
        view="code"
        search={search}
        icon={FileCode}
        label="Code"
      />
      <ViewLink
        pipelineId={pipelineId}
        selectedAssetId={selectedAssetId}
        currentView={currentView}
        view="split"
        search={search}
        icon={Columns2}
        label="Split"
      />
      <ViewLink
        pipelineId={pipelineId}
        selectedAssetId={selectedAssetId}
        currentView={currentView}
        view="canvas"
        search={search}
        icon={Layers}
        label="Canvas"
      />
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
  currentView: AppBuildView;
  view: AppBuildView;
  search: AppBuildSearch;
  icon: ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <Button asChild variant={currentView === view ? "secondary" : "outline"} size="icon-sm">
      <Link
        to={appAssetViewPath(view)}
        params={{ pipelineId, assetId: selectedAssetId }}
        search={search}
        aria-label={`${label} view`}
        title={`${label} view`}
      >
        <Icon className="size-3.5" />
        <span className="sr-only">{label}</span>
      </Link>
    </Button>
  );
}

export function appAssetViewPath(view: AppBuildView) {
  if (view === "split") return "/pipelines/$pipelineId/assets/$assetId/split" as const;
  if (view === "code") return "/pipelines/$pipelineId/assets/$assetId/code" as const;
  return "/pipelines/$pipelineId/assets/$assetId/canvas" as const;
}

export function appBuildViewFromPath(pathname: string): AppBuildView {
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
  onNewPipeline,
  onNewFolder,
  onPipelineSettings,
}: {
  pipelineId: string;
  selectedAssetId: string;
  buildSearch: AppBuildSearch;
  onAssetSelect: (assetId: string) => void;
  onAdhoc: () => void;
  onNewAsset: () => void;
  onNewPipeline: () => void;
  onNewFolder: () => void;
  onPipelineSettings: () => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
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
  const [newNotebookOpen, setNewNotebookOpen] = useState(false);
  const [assetFilter, setAssetFilter] = useState("");
  const normalizedAssetFilter = assetFilter.trim().toLowerCase();
  const filteredAssets = useMemo(() => {
    if (!normalizedAssetFilter) {
      return pipelineAssets;
    }
    return pipelineAssets.filter((asset) =>
      [
        asset.name,
        asset.displayName,
        assetSidebarName(asset),
        assetGroupName(asset),
        asset.prefix,
        asset.path,
        asset.type,
        asset.connection,
      ]
        .filter((value): value is string => Boolean(value))
        .some((value) => value.toLowerCase().includes(normalizedAssetFilter)),
    );
  }, [normalizedAssetFilter, pipelineAssets]);
  const assetsByGroup = useMemo(
    () =>
      filteredAssets.reduce<Record<string, BuildAsset[]>>((groups, asset) => {
        const group = assetGroupName(asset);
        groups[group] = [...(groups[group] ?? []), asset];
        return groups;
      }, {}),
    [filteredAssets],
  );

  return (
    <>
      <DelimitedCardHeader>
        <Database className="size-4 text-primary" />
        <DelimitedCardTitle>Explorer</DelimitedCardTitle>
        <Button
          size="icon-sm"
          variant="ghost"
          className="ml-auto"
          onClick={onNewPipeline}
          aria-label="New pipeline"
          title="New pipeline"
        >
          <GitBranchPlus data-icon="inline-start" />
        </Button>
      </DelimitedCardHeader>
      <div className="border-b p-2">
        <InputGroup className="bg-background">
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
          <InputGroupInput
            value={assetFilter}
            onChange={(event) => setAssetFilter(event.target.value)}
            placeholder="Filter assets..."
            aria-label="Filter assets"
            autoComplete="off"
            className="font-mono text-xs"
          />
          {assetFilter ? (
            <InputGroupAddon align="inline-end">
              <InputGroupButton
                size="icon-xs"
                onClick={() => setAssetFilter("")}
                aria-label="Clear asset filter"
                title="Clear asset filter"
              >
                <X />
              </InputGroupButton>
            </InputGroupAddon>
          ) : null}
        </InputGroup>
      </div>
      {/* Force Radix's inline `display: table` content wrapper to block so long
          asset names truncate instead of widening the sidebar horizontally. */}
      <ScrollArea
        className="min-h-0 flex-1"
        horizontalScrollBarClassName="hidden"
        viewportClassName="[&>div]:!block"
      >
        <div className="space-y-2 p-2">
          <ExplorerSection
            label={pipelineGroup?.label ?? "Pipelines"}
            icon={PipelineIcon}
            count={pipelineItems.length}
          >
            {pipelineItems.map((item) => {
              const activePipeline = item.id === pipelineId;
              const pipelineLabel = item.name || item.path || item.id;
              return (
                <div key={item.id}>
                  <div
                    className={cn(
                      "group flex h-7 w-full items-center rounded-md hover:bg-muted",
                      activePipeline ? "bg-muted text-foreground" : "text-muted-foreground",
                    )}
                  >
                    <Link
                      to="/pipelines/$pipelineId/canvas"
                      params={{ pipelineId: item.id }}
                      search={buildSearch}
                      className="flex min-w-0 flex-1 items-center gap-1.5 px-2 text-left font-mono text-xs"
                    >
                      <PipelineIcon className="size-3.5 text-primary" />
                      <span className="truncate">{pipelineLabel}</span>
                    </Link>
                    {activePipeline ? (
                      <div className="flex shrink-0 items-center pr-0.5">
                        <Button
                          type="button"
                          size="icon-xs"
                          variant="ghost"
                          onClick={onNewAsset}
                          aria-label={`New asset in ${pipelineLabel}`}
                          title="New asset"
                        >
                          <FilePlus2 data-icon="inline-start" />
                        </Button>
                        <Button
                          type="button"
                          size="icon-xs"
                          variant="ghost"
                          onClick={onNewFolder}
                          aria-label={`New folder in ${pipelineLabel}`}
                          title="New folder"
                        >
                          <FolderPlus data-icon="inline-start" />
                        </Button>
                      </div>
                    ) : null}
                  </div>
                  {activePipeline ? (
                    <div className="mt-1 space-y-0.5 border-l pl-3 ml-3">
                      {Object.entries(assetsByGroup).length > 0 ? (
                        Object.entries(assetsByGroup).map(([group, groupAssets]) => (
                          <div key={group}>
                            <div className="px-2 py-1 font-mono text-[11px] text-muted-foreground">
                              {group}/
                            </div>
                            {groupAssets.map((asset) => (
                              <AssetButton
                                key={asset.id}
                                asset={asset}
                                declaredDependencies={declaredDependencies}
                                selected={!adhocActive && selectedAssetId === asset.id}
                                onSelect={() => onAssetSelect(asset.id)}
                              />
                            ))}
                          </div>
                        ))
                      ) : (
                        <div className="px-2 py-1 text-xs text-muted-foreground">
                          {normalizedAssetFilter ? "No matching assets." : "No assets found."}
                        </div>
                      )}
                      <div className="mt-1 border-t pt-1">
                        <button
                          className={cn(
                            "flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs hover:bg-muted",
                            adhocActive
                              ? "bg-primary/10 text-foreground ring-1 ring-primary/20"
                              : "text-muted-foreground",
                          )}
                          onClick={onAdhoc}
                        >
                          <Terminal
                            className={cn("size-3.5", adhocActive ? "text-primary" : null)}
                          />{" "}
                          Ad-hoc query
                        </button>
                        <button
                          className="flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs text-muted-foreground hover:bg-muted"
                          onClick={onPipelineSettings}
                        >
                          <SettingsIcon /> Pipeline settings
                        </button>
                      </div>
                    </div>
                  ) : null}
                </div>
              );
            })}
          </ExplorerSection>

          <ExplorerSection
            label={notebookGroup?.label ?? "Notebooks"}
            icon={NotebookIcon}
            count={notebookItems.length}
          >
            {notebookItems.length > 0 ? (
              notebookItems.map((notebook) => (
                <Link
                  key={notebook.id}
                  to="/notebooks/$notebookId"
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
            onClick={() => setNewNotebookOpen(true)}
            className="mt-1 flex h-8 w-full items-center gap-2 rounded-md border border-dashed px-2 text-left text-xs text-muted-foreground hover:bg-muted disabled:opacity-50"
          >
            <Plus className="size-3.5" /> New notebook
          </button>
        </div>
      </ScrollArea>
      <NewNotebookDialog open={newNotebookOpen} onOpenChange={setNewNotebookOpen} />
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
        <Icon className="size-3.5" />
        {label}
        <span className="ml-auto">{count}</span>
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
  const missingCount =
    asset.kind === "python" ? missingPythonDependencies(asset, declaredDependencies).length : 0;
  const latestAttemptFailed =
    asset.status === "failed" && asset.staleness?.last_run_status === "failed";
  const latestAttemptCancelled =
    asset.status !== "pending" && asset.staleness?.last_run_status === "cancelled";
  const attemptLabel = asset.staleness ? lastRunLabel(asset.staleness) : "Last run failed";
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left font-mono text-xs hover:bg-muted",
        selected ? "bg-primary/10 text-foreground ring-1 ring-primary/20" : "text-muted-foreground",
      )}
    >
      <Icon className="size-3.5 text-primary" />
      <span className="min-w-0 flex-1 truncate">{assetSidebarName(asset)}</span>
      {asset.staleness &&
      (asset.staleness.status !== "fresh" || latestAttemptFailed || latestAttemptCancelled) ? (
        <span
          title={
            latestAttemptFailed || latestAttemptCancelled
              ? attemptLabel
              : `Staleness: ${stalenessLabel(asset.staleness)}`
          }
          className={cn(
            "size-1.5 rounded-full",
            latestAttemptFailed
              ? "bg-destructive"
              : latestAttemptCancelled
                ? "bg-muted-foreground"
                : stalenessDotClassName(asset.staleness),
          )}
        />
      ) : null}
      {missingCount > 0 ? (
        <span
          title={`${missingCount} imports not in dependencies`}
          className="size-1.5 rounded-full bg-amber-500"
        />
      ) : null}
    </button>
  );
}

function PipelineCanvas({ onAssetSelect }: { onAssetSelect: (assetId: string) => void }) {
  const {
    pipelineAssets,
    routedAssetId,
    createDownstreamAsset,
    openNewAssetInGroup,
    runAssetById,
    deleteAssetById,
    goToCatalog,
    openPipelineConnections,
  } = useBuildContext();
  return (
    <AppLineageCanvas
      assets={pipelineAssets}
      selectedAssetId={routedAssetId}
      onAssetSelect={onAssetSelect}
      onRunAsset={runAssetById}
      onDeleteAsset={deleteAssetById}
      onGoToAsset={(assetId) => goToCatalog(assetId)}
      onAssetConnectionClick={() => openPipelineConnections()}
      goToLabel="Open in catalog"
      onCreateAsset={({ prefix }) => openNewAssetInGroup(prefix)}
      onCreateDownstream={(assetId) => {
        const source = pipelineAssets.find((asset) => asset.id === assetId);
        if (source) {
          createDownstreamAsset({ id: source.id, name: source.name });
        }
      }}
    />
  );
}

export function AppBuildCanvasView() {
  const { selectAsset } = useBuildContext();
  return <PipelineCanvas onAssetSelect={selectAsset} />;
}

export function AppBuildSplitView() {
  const { selectedAsset, selectAsset, editorMode } = useBuildContext();
  return (
    <PanelGroup orientation="horizontal" className="h-full min-h-0 min-w-0">
      <Panel defaultSize={50} minSize={28} className="min-w-0">
        <EditorWorkspace asset={selectedAsset} adhoc={editorMode === "adhoc"} />
      </Panel>
      <PanelResizeHandle className="w-px bg-border" />
      <Panel defaultSize={50} minSize={28} className="min-w-0">
        <PipelineCanvas onAssetSelect={selectAsset} />
      </Panel>
    </PanelGroup>
  );
}

export function AppBuildCodeView() {
  const { selectedAsset, editorMode } = useBuildContext();
  return <EditorWorkspace asset={selectedAsset} adhoc={editorMode === "adhoc"} />;
}

function EditorWorkspace({ asset, adhoc }: { asset: BuildAsset; adhoc: boolean }) {
  const {
    pipelineId,
    selectedAssetId,
    view,
    buildSearch,
    declaredDependencies,
    addDependency,
    goToAsset,
    openBottom,
    openInspector,
    materializeSelectedAsset,
    fullRefreshSelectedAsset,
    backfillSelectedAsset,
    inspectSelectedAsset,
    renderSelectedAsset,
    materializeLoading,
    inspectLoading,
    renderLoading,
    renderBlockedReason,
    executionBlocked,
    executionBlockedReason,
  } = useBuildContext();
  const isMobile = useIsMobile();
  const editorOnly = view === "code";
  const showActionLabels = editorOnly && !isMobile;
  if (adhoc) {
    return <AdhocEditor showActionLabels={showActionLabels} />;
  }

  // Real workspace assets get the live missing-dependency banner from
  // AppAssetEditor (driven by the asset's actual requirements.txt); this
  // mock affordance only covers the demo/sample assets that have no editor.
  const missingDependencies =
    !asset.workspaceAsset && asset.kind === "python"
      ? missingPythonDependencies(asset, declaredDependencies)
      : [];
  const actionLabel =
    asset.kind === "source"
      ? "Validate"
      : asset.kind === "sensor"
        ? "Check now"
        : asset.kind === "ingestr" || asset.kind === "load"
          ? "Run"
          : "Materialize";
  const filename =
    asset.path ?? `${asset.dir ? `${asset.dir}/` : ""}${asset.name}${kindMeta[asset.kind].ext}`;
  const renderableAsset = Boolean(
    asset.workspaceAsset && isRenderableAssetType(asset.workspaceAsset.type),
  );

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      <EditorFilenameHeader filename={filename}>
        <EditorActionButtons
          actionLabel={actionLabel}
          showLabels={showActionLabels}
          showInspect={asset.kind !== "source"}
          showRender={renderableAsset}
          onRun={materializeSelectedAsset}
          onFullRefresh={
            asset.workspaceAsset?.supports_full_refresh ? fullRefreshSelectedAsset : undefined
          }
          onBackfill={asset.staleness?.backfill_safe ? backfillSelectedAsset : undefined}
          onInspect={inspectSelectedAsset}
          onRender={renderSelectedAsset}
          runDisabled={materializeLoading || executionBlocked || !asset.workspaceAsset}
          runBlockedReason={executionBlocked ? executionBlockedReason : undefined}
          runLoading={materializeLoading}
          inspectDisabled={inspectLoading || !asset.workspaceAsset}
          inspectLoading={inspectLoading}
          renderDisabled={renderLoading || !asset.workspaceAsset}
          renderLoading={renderLoading}
          renderBlockedReason={renderBlockedReason}
        />
        {asset.workspaceAsset ? (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground xl:hidden"
            onClick={openInspector}
            title="Asset properties"
            aria-label="Asset properties"
          >
            <PanelRight className="size-3.5" />
            {showActionLabels ? <span className="ml-1">Properties</span> : null}
          </Button>
        ) : null}
        {editorOnly ? (
          <BuildViewButtonGroup
            pipelineId={pipelineId}
            selectedAssetId={selectedAssetId}
            currentView={view}
            search={buildSearch}
          />
        ) : null}
      </EditorFilenameHeader>
      {missingDependencies.length > 0 ? (
        <Button
          variant="outline"
          size="xs"
          className="absolute left-3 top-9 z-20 border-amber-300 bg-amber-50 text-amber-700 shadow-sm hover:bg-amber-100 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-300 dark:hover:bg-amber-500/20"
          onClick={() => openBottom("diagnostics")}
        >
          <AlertTriangle className="size-3" />
          {missingDependencies.length} not in deps
        </Button>
      ) : null}
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col">
          {asset.workspaceAsset?.parse_error ? (
            <div className="flex shrink-0 items-start gap-2 border-b border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
              <div className="min-w-0">
                <span className="font-medium">This asset could not be parsed.</span> Fix the file
                below to restore it.
                <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap font-mono text-[11px] opacity-80">
                  {asset.workspaceAsset.parse_error}
                </pre>
              </div>
            </div>
          ) : null}
          {asset.workspaceAsset &&
          asset.pipelineId &&
          asset.workspaceAsset.type.toLowerCase() === "load" ? (
            <LoadParametersEditor
              asset={asset.workspaceAsset}
              pipelineId={asset.pipelineId}
              onGoToAsset={goToAsset}
            />
          ) : asset.workspaceAsset &&
            asset.pipelineId &&
            asset.workspaceAsset.type.toLowerCase() === "api" ? (
            <ApiParametersEditor
              key={asset.workspaceAsset.id}
              asset={asset.workspaceAsset}
              pipelineId={asset.pipelineId}
              onInspect={inspectSelectedAsset}
              onGoToAsset={goToAsset}
            />
          ) : asset.workspaceAsset &&
            asset.pipelineId &&
            (isSeedAssetType(asset.workspaceAsset.type) ||
              isSensorAssetType(asset.workspaceAsset.type)) ? (
            <SemanticParametersEditor
              key={asset.workspaceAsset.id}
              asset={asset.workspaceAsset}
              pipelineId={asset.pipelineId}
              onCheck={materializeSelectedAsset}
              onGoToAsset={goToAsset}
            />
          ) : asset.workspaceAsset && asset.pipelineId ? (
            <AppAssetEditor
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
    </div>
  );
}

function EditorFilenameHeader({ filename, children }: { filename: string; children?: ReactNode }) {
  return (
    <div className="flex h-10 min-w-0 shrink-0 items-center gap-2 overflow-hidden border-b bg-background/70 px-3">
      <span className="block min-w-0 flex-[1_1_0] truncate font-mono text-[11px] text-muted-foreground">
        {filename}
      </span>
      {children ? (
        <div className="ml-auto flex shrink-0 items-center gap-1.5">{children}</div>
      ) : null}
    </div>
  );
}

function EditorActionButtons({
  actionLabel,
  showLabels,
  showInspect,
  showRender,
  onRun,
  onInspect,
  onRender,
  onFullRefresh,
  onBackfill,
  runDisabled = false,
  runBlockedReason,
  runLoading = false,
  inspectDisabled = false,
  inspectLoading = false,
  renderDisabled = false,
  renderLoading = false,
  renderBlockedReason,
}: {
  actionLabel: string;
  showLabels: boolean;
  showInspect: boolean;
  showRender: boolean;
  onRun: () => void;
  onInspect: () => void;
  onRender: () => void;
  onFullRefresh?: () => void;
  onBackfill?: () => void;
  runDisabled?: boolean;
  runBlockedReason?: string;
  runLoading?: boolean;
  inspectDisabled?: boolean;
  inspectLoading?: boolean;
  renderDisabled?: boolean;
  renderLoading?: boolean;
  renderBlockedReason?: string;
}) {
  const runLabel = runLoading ? "Running..." : actionLabel;
  const inspectLabel = inspectLoading ? "Loading..." : "Inspect";
  const renderLabel = renderLoading ? "Rendering..." : "Render";
  return (
    <>
      {onFullRefresh || onBackfill ? (
        <ButtonGroup>
          <Button
            size={showLabels ? "sm" : "icon-sm"}
            onClick={onRun}
            disabled={runDisabled}
            aria-label={actionLabel}
            title={runBlockedReason ?? actionLabel}
          >
            <Hammer data-icon="inline-start" />
            {showLabels ? runLabel : <span className="sr-only">{runLabel}</span>}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                size="icon-sm"
                disabled={runDisabled}
                aria-label="Materialization options"
              >
                <ChevronDown />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Materialization</DropdownMenuLabel>
                {onFullRefresh ? (
                  <DropdownMenuItem onClick={onFullRefresh}>Full refresh</DropdownMenuItem>
                ) : null}
                {onBackfill ? (
                  <DropdownMenuItem onClick={onBackfill}>Backfill range…</DropdownMenuItem>
                ) : null}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </ButtonGroup>
      ) : (
        <Button
          size={showLabels ? "sm" : "icon-sm"}
          onClick={onRun}
          disabled={runDisabled}
          aria-label={actionLabel}
          title={runBlockedReason ?? actionLabel}
        >
          <Hammer data-icon="inline-start" />
          {showLabels ? runLabel : <span className="sr-only">{runLabel}</span>}
        </Button>
      )}
      {showRender ? (
        <Button
          variant="outline"
          size={showLabels ? "sm" : "icon-sm"}
          onClick={onRender}
          disabled={renderDisabled || Boolean(renderBlockedReason)}
          aria-busy={renderLoading}
          aria-label="Render saved asset"
          title={renderBlockedReason ?? "Render saved asset"}
        >
          <FileCode className="size-3.5" data-icon="inline-start" />
          {showLabels ? renderLabel : <span className="sr-only">{renderLabel}</span>}
        </Button>
      ) : null}
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
          {showActionLabels ? (
            adhocLoading ? (
              "Running..."
            ) : (
              "Run"
            )
          ) : (
            <span className="sr-only">Run</span>
          )}
        </Button>
        {view === "code" ? (
          <BuildViewButtonGroup
            pipelineId={pipelineId}
            selectedAssetId={selectedAssetId}
            currentView={view}
            search={buildSearch}
          />
        ) : null}
      </EditorFilenameHeader>
      {adhocContextAsset ? (
        <AppAdhocEditor
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
            <div
              key={index}
              className={cn(
                "flex min-h-5 items-center",
                missing ? "bg-amber-500/10 shadow-[inset_2px_0_0_#f59e0b]" : null,
              )}
            >
              <span
                className={cn(
                  "w-11 shrink-0 select-none pr-3 text-right",
                  missing ? "text-amber-400" : "text-zinc-500",
                )}
              >
                {index + 1}
              </span>
              <pre className="min-w-0 whitespace-pre">{line}</pre>
              {missing && dependency ? (
                <Button
                  variant="outline"
                  size="xs"
                  className="ml-3 h-5 border-amber-300 bg-amber-50 px-1.5 text-[10px] text-amber-700 hover:bg-amber-100 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-300 dark:hover:bg-amber-500/20"
                  onClick={() => onAddDependency?.(dependency)}
                >
                  <Package className="size-3" />
                  add {dependency}
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
  typeCheckReport,
  typeCheckLoading,
  typeCheckError,
  onRunTypeCheck,
  onSelectAsset,
  inspectResult,
  inspectLoading,
  renderResult,
  renderLoading,
  renderError,
  onRender,
  canLoadMoreInspectRows,
  onLoadMoreInspectRows,
  selectedMaterializeEntry,
  materializeOutputHtml,
  pipelineMaterializeLoading,
  adhocResult,
  adhocRenderedQuery,
  adhocLoading,
}: {
  activeTab: AppResultTab;
  onTabChange: (tab: AppResultTab) => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
  typeCheckReport?: PipelineTypeCheckReport | null;
  typeCheckLoading?: boolean;
  typeCheckError?: string | null;
  onRunTypeCheck?: () => void;
  onSelectAsset?: (assetId: string) => void;
  inspectResult: AssetInspectResponse | null;
  inspectLoading: boolean;
  renderResult: AssetRenderResult | null;
  renderLoading: boolean;
  renderError: string | null;
  onRender: () => void;
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
    <AppPanel className="flex h-full min-h-0 flex-col">
      <Tabs
        value={activeTab}
        onValueChange={(value) => {
          if (resultTabs.includes(value as AppResultTab)) onTabChange(value as AppResultTab);
        }}
        className="flex h-full min-h-0 flex-col"
      >
        <DelimitedCardHeader className="min-h-9 gap-1 bg-muted py-1">
          <ScrollArea
            className="min-w-0 flex-1"
            horizontalScrollBarClassName="hidden"
            viewportClassName="w-full"
          >
            <TabsList className={scrollableTabsListClass}>
              <TabsTrigger value="inspect" className={scrollableTabsTriggerClass}>
                <Table2 className="size-3.5" />
                Inspect
              </TabsTrigger>
              <TabsTrigger value="render" className={scrollableTabsTriggerClass}>
                <FileCode className="size-3.5" />
                Render
              </TabsTrigger>
              <TabsTrigger value="materialize" className={scrollableTabsTriggerClass}>
                <Hammer className="size-3.5" />
                Materialize
              </TabsTrigger>
              <TabsTrigger value="query" className={scrollableTabsTriggerClass}>
                <Terminal className="size-3.5" />
                Query
              </TabsTrigger>
              <TabsTrigger value="typecheck" className={scrollableTabsTriggerClass}>
                <Bell className="size-3.5" />
                Type check
                {typeCheckReport &&
                typeCheckReport.summary.errors + typeCheckReport.summary.warnings > 0 ? (
                  <span
                    className={cn(
                      "ml-1 rounded-full px-1 text-[10px] text-white",
                      typeCheckReport.summary.errors > 0 ? "bg-red-500" : "bg-amber-500",
                    )}
                  >
                    {typeCheckReport.summary.errors + typeCheckReport.summary.warnings}
                  </span>
                ) : null}
              </TabsTrigger>
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
          ) : inspectResult?.info ? (
            <div className="flex h-full min-h-0 items-center justify-center overflow-auto p-3">
              <InspectInfoCard message={inspectResult.info} testId="app-inspect-info" />
            </div>
          ) : inspectResult?.error ? (
            <div className="flex h-full min-h-0 items-center justify-center overflow-auto p-3">
              <InspectWarningCard message={inspectResult.error} testId="app-inspect-warning" />
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
        <TabsContent value="render" className="min-h-0 flex-1 overflow-hidden p-0">
          <AssetRenderView
            result={renderResult}
            loading={renderLoading}
            error={renderError}
            onRetry={onRender}
          />
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
              <InspectWarningCard message={adhocResult.error} testId="app-query-warning" />
            </div>
          ) : adhocResult ? (
            <>
              <RenderedQueryDisclosure query={adhocRenderedQuery} />
              <div className="min-h-0 flex-1">
                <AssetInspectView
                  columns={adhocResult.columns ?? []}
                  rows={(adhocResult.rows ?? []) as Record<string, unknown>[]}
                  warning={
                    adhocResult.truncated
                      ? "Result truncated; showing the first rows only."
                      : undefined
                  }
                  frameless
                />
              </div>
            </>
          ) : (
            <ResultsEmpty label="Run an ad hoc query to see results here." />
          )}
        </TabsContent>
        <TabsContent value="tests" className="min-h-0 flex-1 overflow-auto p-3">
          <UnitTests />
        </TabsContent>
        <TabsContent value="diagnostics" className="min-h-0 flex-1 overflow-auto p-0">
          <DiagnosticsList />
        </TabsContent>
        <TabsContent value="metadata" className="min-h-0 flex-1 overflow-auto p-0">
          <MetadataPanel />
        </TabsContent>
        <TabsContent value="typecheck" className="min-h-0 flex-1 overflow-auto p-0">
          <TypeCheckPanel
            report={typeCheckReport ?? null}
            loading={Boolean(typeCheckLoading)}
            error={typeCheckError ?? null}
            onRun={onRunTypeCheck}
            onSelectAsset={onSelectAsset}
          />
        </TabsContent>
      </Tabs>
    </AppPanel>
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
    if (await copyTextToClipboard(trimmed)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
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
          <ChevronRight
            className={cn("size-3 shrink-0 transition-transform", open ? "rotate-90" : null)}
          />
          <Terminal className="size-3 shrink-0" />
          <span className="shrink-0 font-semibold uppercase tracking-wide">Query</span>
          {!open ? (
            <span className="min-w-0 flex-1 truncate font-mono">
              {trimmed.replace(/\s+/g, " ")}
            </span>
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
      {open ? <SqlPreview query={trimmed} /> : null}
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
  error,
  onRun,
  onSelectAsset,
}: {
  report: PipelineTypeCheckReport | null;
  loading: boolean;
  error: string | null;
  onRun?: () => void;
  onSelectAsset?: (assetId: string) => void;
}) {
  if (loading && !report) {
    return <ResultsLoading label="Type checking pipeline…" />;
  }
  if (!report) {
    return (
      <div className="flex h-full min-h-0 flex-col items-center justify-center gap-3 bg-background p-3 text-xs text-muted-foreground">
        {error ? (
          <Alert variant="destructive" className="max-w-lg">
            <AlertTriangle />
            <AlertTitle>Type check failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : (
          <span>Type check assets for column and type errors.</span>
        )}
        <Button size="sm" variant="outline" onClick={onRun}>
          <Bell className="size-3.5" />
          {error ? "Retry type check" : "Run type check"}
        </Button>
      </div>
    );
  }

  const flagged = report.assets.filter((asset) => asset.findings.length > 0);
  const checkedAt = report.start_date ? new Date(report.start_date) : null;

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5 text-xs">
        <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
          <XCircle className="size-3.5" />
          {report.summary.errors}
        </span>
        <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400">
          <AlertTriangle className="size-3.5" />
          {report.summary.warnings}
        </span>
        <span className="text-muted-foreground">
          {report.summary.assets} asset{report.summary.assets === 1 ? "" : "s"} checked
        </span>
        {checkedAt ? (
          <span className="hidden text-muted-foreground/70 sm:inline">
            · window {checkedAt.toISOString().slice(0, 10)}
          </span>
        ) : null}
        <Button size="xs" variant="outline" className="ml-auto" onClick={onRun} disabled={loading}>
          {loading ? <Loader2 className="size-3 animate-spin" /> : <RotateCw className="size-3" />}
          Re-run
        </Button>
      </div>
      {error ? (
        <Alert variant="destructive" className="m-2 shrink-0 w-auto">
          <AlertTriangle />
          <AlertTitle>Latest type check failed</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {flagged.length === 0 ? (
          <div className="flex items-center gap-2 px-2 py-3 text-xs text-emerald-600 dark:text-emerald-400">
            <CheckCircle2 className="size-4" />
            No type errors found across {report.summary.assets} asset
            {report.summary.assets === 1 ? "" : "s"}.
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
                  {asset.status === "error" ? (
                    <XCircle className="size-3.5 shrink-0 text-red-500" />
                  ) : (
                    <AlertTriangle className="size-3.5 shrink-0 text-amber-500" />
                  )}
                  <span className="min-w-0 flex-1 truncate font-mono font-medium">
                    {asset.name}
                  </span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{asset.type}</span>
                </button>
                <ul className="divide-y">
                  {asset.findings.map((finding, index) => (
                    <li key={index} className="flex items-start gap-2 px-2.5 py-1.5 text-xs">
                      {finding.severity === "error" ? (
                        <XCircle className="mt-0.5 size-3.5 shrink-0 text-red-500" />
                      ) : (
                        <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
                      )}
                      <span className="min-w-0 flex-1">{finding.message}</span>
                      {finding.line ? (
                        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                          L{finding.line}:C{finding.column}
                        </span>
                      ) : null}
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

function Inspector({ asset }: { asset: BuildAsset }) {
  const workspaceAsset = asset.workspaceAsset;
  const editable = Boolean(workspaceAsset && asset.pipelineId);

  // Title: the asset's own (leaf) name; subtitle: its namespace and integration.
  // The file path is intentionally omitted — it just repeats those two.
  const { prefix, title } = assetNameParts(asset.name);
  const subtitle = [prefix, asset.type ?? asset.integration].filter(Boolean).join(" · ");

  return (
    <div data-testid="asset-inspector" className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2 pr-12">
        <Sliders className="size-4 shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <div className="truncate font-monaco text-[13px] font-medium">{title}</div>
          {subtitle ? (
            <p className="truncate text-[11px] text-muted-foreground">{subtitle}</p>
          ) : null}
        </div>
      </div>
      {editable && workspaceAsset && asset.pipelineId ? (
        <ErrorBoundary
          resetKey={workspaceAsset.content ?? ""}
          fallback={
            <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-center text-xs text-muted-foreground">
              These properties can&apos;t be shown right now — the asset file may have a syntax
              error. Fix it in the code editor to continue.
            </div>
          }
        >
          <AssetGuidedCards asset={workspaceAsset} pipelineId={asset.pipelineId} />
        </ErrorBoundary>
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-center text-xs text-muted-foreground">
          Properties become editable once this asset is saved to the pipeline.
        </div>
      )}
    </div>
  );
}

function UnitTests({ compact, onOpenResults }: { compact?: boolean; onOpenResults?: () => void }) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium">Unit tests</span>
        <Badge
          variant="outline"
          className="bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300"
        >
          2 passed
        </Badge>
        <Badge
          variant="outline"
          className="bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-300"
        >
          1 failed
        </Badge>
        <Button variant="outline" size="xs" className="ml-auto">
          <Plus className="size-3" />
          New
        </Button>
        <Button size="xs" onClick={onOpenResults}>
          <Play className="size-3" />
          Run all
        </Button>
      </div>
      {tests.map((test) => (
        <div key={test.id} className="rounded-lg border p-2 text-xs">
          <div className="flex items-center gap-2">
            <StatusPill status={test.status} />
            <span className="min-w-0 flex-1 truncate font-mono font-medium">{test.name}</span>
            {compact ? <MoreHorizontal className="size-3.5 text-muted-foreground" /> : null}
          </div>
          <div className="mt-1 flex flex-wrap gap-3 text-muted-foreground">
            <span>given: {test.given}</span>
            <span>expect: {test.expect}</span>
          </div>
          {"got" in test ? (
            <div className="mt-2 rounded bg-muted p-2 font-mono text-red-600">got: {test.got}</div>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function DiagnosticsList() {
  return (
    <div>
      <div className="sticky top-0 flex h-9 items-center gap-3 border-b bg-card px-3 text-xs">
        <span className="flex items-center gap-1 text-red-500">
          <XCircle className="size-3.5" />2
        </span>
        <span className="flex items-center gap-1 text-amber-500">
          <AlertTriangle className="size-3.5" />2
        </span>
        <span className="flex items-center gap-1 text-muted-foreground">
          <Activity className="size-3.5" />0
        </span>
      </div>
      {diagnostics.map((diagnostic) => (
        <div
          key={`${diagnostic.asset}-${diagnostic.message}`}
          className="flex items-start gap-2 border-b px-3 py-2 text-xs"
        >
          <SeverityIcon severity={diagnostic.severity} />
          <div className="min-w-0 flex-1">
            <span className="font-mono text-primary">{diagnostic.asset}</span>
            <span className="text-muted-foreground"> - {diagnostic.message}</span>
          </div>
          <Button variant="outline" size="xs">
            {diagnostic.action}
          </Button>
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
          <Badge variant="outline" className="bg-amber-50 text-amber-700">
            drift detected
          </Badge>
          <Button variant="link" size="xs" className="ml-auto h-auto p-0" onClick={onFull}>
            Full diff
          </Button>
        </div>
        {schemaRows.map((row) => (
          <div key={row.name} className="rounded-lg border p-2 text-xs">
            <div className="flex items-center justify-between">
              <span className="font-mono">{row.name}</span>
              <SchemaStatus status={row.status} />
            </div>
            <div className="mt-1 flex gap-3 font-mono text-muted-foreground">
              <span>decl: {row.declared}</span>
              <span>table: {row.actual}</span>
            </div>
          </div>
        ))}
      </div>
    );
  }

  return (
    <SimpleTable
      columns={["Column", "Declared", "In table", "Description", "Status"]}
      rows={schemaRows.map((row) => [
        row.name,
        row.declared,
        row.actual,
        row.description || "no description",
        <SchemaStatus key={row.name} status={row.status} />,
      ])}
    />
  );
}

function SchemaStatus({ status }: { status: string }) {
  if (status === "match")
    return (
      <Badge variant="outline" className="bg-emerald-50 text-emerald-700">
        <CheckCircle2 className="size-3" />
        in sync
      </Badge>
    );
  if (status === "drift")
    return (
      <Badge variant="outline" className="bg-amber-50 text-amber-700">
        <AlertTriangle className="size-3" />
        type drift
      </Badge>
    );
  if (status === "missing")
    return (
      <Badge variant="outline" className="bg-muted">
        <Circle className="size-3" />
        missing
      </Badge>
    );
  return (
    <Badge variant="outline" className="bg-sky-50 text-sky-700">
      <Plus className="size-3" />
      extra
    </Badge>
  );
}

// Asset kinds the creation dialog can produce, mapped to real backend create
// calls. Standalone: SQL/Python transforms, "HTTP API" (Bruin api asset) and
// "Load" (renart load asset). Downstream (created from a canvas node): SQL,
// Python (via the Bruin Python SDK) and Load, each depending on the source.
type AssetKindOption = {
  id: NewAssetKind;
  label: string;
  description: string;
  icon: ComponentType<{ className?: string }>;
};

const CREATABLE_ASSETS: AssetKindOption[] = [
  { id: "sql", label: "SQL", description: "Transform with a SELECT", icon: FileCode },
  { id: "python", label: "Python", description: "Custom Python transform", icon: Cpu },
  {
    id: "api",
    label: "HTTP API",
    description: "Pull records from an HTTP API endpoint",
    icon: Globe,
  },
  {
    id: "seed",
    label: "Seed",
    description: "Load a file into a table",
    icon: Sprout,
  },
  {
    id: "sensor",
    label: "Sensor",
    description: "Check an external readiness condition",
    icon: Radar,
  },
  { id: "load", label: "Load", description: "Replicate data between connections", icon: Download },
];

const DOWNSTREAM_ASSETS: AssetKindOption[] = [
  { id: "sql", label: "SQL", description: "select * from the upstream table", icon: FileCode },
  { id: "python", label: "Python", description: "Read the upstream table from Python", icon: Cpu },
  {
    id: "load",
    label: "Load",
    description: "Replicate downstream between connections",
    icon: Download,
  },
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

// suggestPrefixedAssetName seeds a unique name under an explicit prefix
// (from the canvas prefix-group the user right-clicked in).
function suggestPrefixedAssetName(
  kind: NewAssetKind,
  prefix: string,
  existing: Set<string>,
): string {
  const base = `${prefix}.my_${kind}_asset_`;
  let index = 1;
  while (existing.has(`${base}${index}`)) {
    index += 1;
  }
  return `${base}${index}`;
}

// Sentinel Select value for "no explicit connection" (an empty SelectItem value
// is disallowed); it maps back to an empty connection so the asset uses the
// pipeline default.
const AUTO_CONNECTION_VALUE = "__auto__";

function NewAssetDialog({
  open,
  onOpenChange,
  pipelineId,
  pipelineName,
  existingAssetNames,
  downstreamSource,
  namePrefix,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId?: string;
  pipelineName?: string;
  existingAssetNames: Set<string>;
  downstreamSource?: { id: string; name: string } | null;
  namePrefix?: string | null;
  onCreated?: (assetId: string) => void;
}) {
  const [kind, setKind] = useState<NewAssetKind>("sql");
  const [name, setName] = useState("");
  const [connection, setConnection] = useState("");
  const [sourceConnection, setSourceConnection] = useState("");
  const [sourceTable, setSourceTable] = useState("");
  const [destinationObject, setDestinationObject] = useState("");
  const [apiTemplate, setAPITemplate] = useState<APIAssetTemplateId>("openapi");
  const [semanticDraft, setSemanticDraft] = useState<SemanticAssetDraft>(() =>
    defaultSemanticAssetDraft("seed", [], {}),
  );
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const resetModeRef = useRef<string | null>(null);

  const workspace = useAtomValue(workspaceAtom);
  const environment = useAtomValue(selectedEnvironmentAtom);
  const { workspaceConfig } = useWorkspaceSettingsData();
  const semanticCapabilities = useMemo(
    () => workspace?.asset_capabilities ?? [],
    [workspace?.asset_capabilities],
  );
  const semanticConnections = useMemo(() => workspace?.connections ?? {}, [workspace?.connections]);
  const connectionNames = useMemo(
    () => Object.keys(workspace?.connections ?? {}).sort((a, b) => a.localeCompare(b)),
    [workspace?.connections],
  );
  const loadConnections = useMemo(
    () => loadConnectionsForEnvironment(workspaceConfig, environment),
    [workspaceConfig, environment],
  );
  const loadConnectionNames = useMemo(
    () => [
      ...loadConnections
        .map((candidate) => candidate.name)
        .filter((name) => name !== LOCAL_LOAD_CONNECTION),
      LOCAL_LOAD_CONNECTION,
    ],
    [loadConnections],
  );
  const targetLoadCategory = loadConnectionCategory(loadConnections, connection);
  const targetNeedsDestinationObject = loadTargetNeedsDestinationObject(targetLoadCategory);

  const isDownstream = Boolean(downstreamSource);
  const options = isDownstream ? DOWNSTREAM_ASSETS : CREATABLE_ASSETS;
  const selected = options.find((option) => option.id === kind) ?? options[0];

  // Seed a unique, prefixed name suggestion (the backend requires a prefix).
  const suggestedName = useMemo(() => {
    if (isDownstream && downstreamSource) {
      return suggestDownstreamName(downstreamSource.name, existingAssetNames);
    }
    if (namePrefix) {
      return suggestPrefixedAssetName(selected.id, namePrefix, existingAssetNames);
    }
    return buildSuggestedAssetName(selected.id, existingAssetNames, pipelineName);
  }, [isDownstream, downstreamSource, namePrefix, selected.id, existingAssetNames, pipelineName]);

  // Reset to a valid kind whenever the dialog (or its mode) opens.
  useEffect(() => {
    if (!open) {
      resetModeRef.current = null;
      return;
    }
    const resetMode = isDownstream ? "downstream" : "standalone";
    if (resetModeRef.current === resetMode) return;
    resetModeRef.current = resetMode;
    setKind("sql");
    setConnection("");
    setSourceConnection("");
    setSourceTable("");
    setDestinationObject("");
    setAPITemplate("openapi");
    setSemanticDraft(defaultSemanticAssetDraft("seed", semanticCapabilities, semanticConnections));
    setError("");
  }, [open, isDownstream, semanticCapabilities, semanticConnections]);
  useEffect(() => {
    if (open) {
      setName(suggestedName);
    }
  }, [open, suggestedName]);

  useEffect(() => {
    if (open && selected.id === "load" && !isDownstream && !sourceConnection) {
      setSourceConnection(loadConnectionNames[0] ?? "");
    }
  }, [isDownstream, loadConnectionNames, open, selected.id, sourceConnection]);

  const semanticKind: SemanticAssetKind | null =
    selected.id === "seed" || selected.id === "sensor" ? selected.id : null;
  useEffect(() => {
    if (
      open &&
      semanticKind &&
      !semanticCapabilities.some(
        (capability) =>
          capability.kind === semanticKind && capability.type === semanticDraft.assetType,
      )
    ) {
      setSemanticDraft(
        defaultSemanticAssetDraft(semanticKind, semanticCapabilities, semanticConnections),
      );
    }
  }, [open, semanticCapabilities, semanticConnections, semanticDraft.assetType, semanticKind]);

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
    const semanticResult = semanticKind
      ? buildSemanticAssetCreatePayload(semanticKind, semanticDraft, semanticCapabilities)
      : null;
    if (semanticResult?.error) {
      setError(semanticResult.error);
      return;
    }
    if (selected.id === "load" && !isDownstream) {
      if (!sourceConnection.trim()) {
        setError("A source connection is required for a Load asset.");
        return;
      }
      if (!sourceTable.trim()) {
        setError("A source table or object is required for a Load asset.");
        return;
      }
    }
    if (selected.id === "load" && targetNeedsDestinationObject && !destinationObject.trim()) {
      setError("This target connection requires a destination object or file path.");
      return;
    }

    let input: Parameters<typeof createAsset>[1] =
      isDownstream && downstreamSource
        ? selected.id === "sql"
          ? { name: trimmed, source_asset_id: downstreamSource.id }
          : { name: trimmed, source_asset_id: downstreamSource.id, type: selected.id }
        : buildCreateAssetInput(trimmed, selected.id, undefined, connection, apiTemplate);
    if (selected.id === "load") {
      input = {
        ...input,
        type: "load",
        connection,
        parameters: {
          ...(isDownstream
            ? {}
            : {
                source_connection: sourceConnection.trim(),
                source_table: sourceTable.trim(),
              }),
          ...(targetNeedsDestinationObject && destinationObject.trim()
            ? { destination_object: destinationObject.trim() }
            : {}),
        },
      };
    }
    let seedFile: File | undefined;
    if (semanticResult?.payload) {
      const { seedFile: payloadFile, ...semanticInput } = semanticResult.payload;
      seedFile = payloadFile;
      input = { ...input, ...semanticInput };
    }
    setCreating(true);
    setError("");
    try {
      const response = await createAsset(pipelineId, input, seedFile ? { seedFile } : undefined);
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
      <DialogContent className="flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="size-4 text-primary" />
            {isDownstream ? "New downstream asset" : "New asset"}
          </DialogTitle>
          <DialogDescription>
            {isDownstream && downstreamSource ? (
              <>
                Depends on <span className="font-mono">{downstreamSource.name}</span>.
              </>
            ) : (
              <>
                Create an asset in{" "}
                {pipelineName ? <span className="font-mono">{pipelineName}</span> : "this pipeline"}
                .
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="grid min-h-0 flex-1 gap-5 overflow-y-auto">
          <ToggleGroup
            type="single"
            variant="outline"
            value={selected.id}
            onValueChange={(nextKind) => {
              if (!nextKind) return;
              const next = nextKind as NewAssetKind;
              setKind(next);
              if (next === "seed" || next === "sensor") {
                setSemanticDraft(
                  defaultSemanticAssetDraft(next, semanticCapabilities, semanticConnections),
                );
              }
            }}
            className="grid w-full grid-cols-2 items-stretch gap-2 sm:grid-cols-3"
          >
            {options.map((option) => (
              <ToggleGroupItem
                key={option.id}
                value={option.id}
                aria-label={option.label}
                className="h-24 w-full min-w-0 flex-col items-start justify-start whitespace-normal p-3 text-left data-[state=on]:border-primary data-[state=on]:ring-1 data-[state=on]:ring-primary"
              >
                <option.icon className="text-primary" />
                <div className="font-medium">{option.label}</div>
                <div className="text-xs text-muted-foreground">{option.description}</div>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Field variant="plain">
            <FieldLabel htmlFor="new-asset-name">Asset name</FieldLabel>
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
            <FieldDescription>
              Use a <span className="font-mono">prefix.name</span> to group it under{" "}
              <span className="font-mono">assets/prefix/</span>.
            </FieldDescription>
          </Field>
          {semanticKind ? (
            <SemanticAssetCreateFields
              kind={semanticKind}
              capabilities={semanticCapabilities}
              connections={semanticConnections}
              value={semanticDraft}
              onChange={setSemanticDraft}
            />
          ) : null}
          {selected.id === "api" ? (
            <FieldGroup>
              <Field variant="plain">
                <FieldLabel htmlFor="new-api-template">API source</FieldLabel>
                <Select
                  value={apiTemplate}
                  onValueChange={(value) => setAPITemplate(value as APIAssetTemplateId)}
                >
                  <SelectTrigger id="new-api-template">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {API_ASSET_TEMPLATES.map((template) => (
                        <SelectItem key={template.id} value={template.id}>
                          {template.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  {API_ASSET_TEMPLATES.find((template) => template.id === apiTemplate)?.description}
                </FieldDescription>
              </Field>
            </FieldGroup>
          ) : null}
          {selected.id === "load" ? (
            <FieldGroup>
              {!isDownstream ? (
                <>
                  <Field variant="plain">
                    <FieldLabel htmlFor="new-load-source-connection">Source connection</FieldLabel>
                    <Select value={sourceConnection} onValueChange={setSourceConnection}>
                      <SelectTrigger id="new-load-source-connection">
                        <SelectValue placeholder="Choose a source" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {loadConnectionNames.map((connectionName) => (
                            <SelectItem key={connectionName} value={connectionName}>
                              {connectionName}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field variant="plain">
                    <FieldLabel htmlFor="new-load-source-table">
                      {isLocalLoadConnection(sourceConnection)
                        ? "Source file"
                        : "Source table or object"}
                    </FieldLabel>
                    {isLocalLoadConnection(sourceConnection) ? (
                      <FilePathPicker
                        id="new-load-source-table"
                        variant="field"
                        ariaLabel="Choose source file"
                        placeholder="data/orders.csv"
                        value={sourceTable}
                        onCommit={setSourceTable}
                      />
                    ) : (
                      <Input
                        id="new-load-source-table"
                        className="font-mono"
                        placeholder="public.orders"
                        value={sourceTable}
                        onChange={(event) => setSourceTable(event.target.value)}
                      />
                    )}
                  </Field>
                </>
              ) : null}
            </FieldGroup>
          ) : null}
          {selected.id === "api" || selected.id === "load" ? (
            <FieldGroup>
              <Field variant="plain">
                <FieldLabel htmlFor="new-asset-connection">Destination connection</FieldLabel>
                <Select
                  value={connection || AUTO_CONNECTION_VALUE}
                  onValueChange={(value) =>
                    setConnection(value === AUTO_CONNECTION_VALUE ? "" : value)
                  }
                >
                  <SelectTrigger id="new-asset-connection">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value={AUTO_CONNECTION_VALUE}>Auto (pipeline default)</SelectItem>
                      {(selected.id === "load" ? loadConnectionNames : connectionNames).map(
                        (connectionName) => (
                          <SelectItem key={connectionName} value={connectionName}>
                            {connectionName}
                          </SelectItem>
                        ),
                      )}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  {selected.id === "load"
                    ? "Database destinations use the asset name as their table."
                    : "Where fetched records are loaded. You can change this later."}
                </FieldDescription>
              </Field>
              {selected.id === "load" && targetNeedsDestinationObject ? (
                <Field variant="plain">
                  <FieldLabel htmlFor="new-load-destination-object">Destination object</FieldLabel>
                  <Input
                    id="new-load-destination-object"
                    className="font-mono"
                    placeholder={
                      isLocalLoadConnection(connection) ? "data/orders.csv" : "path/to/object"
                    }
                    value={destinationObject}
                    onChange={(event) => setDestinationObject(event.target.value)}
                  />
                </Field>
              ) : null}
            </FieldGroup>
          ) : null}
          {error ? (
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>Could not create asset</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={creating}>
            Cancel
          </Button>
          <Button onClick={() => void create()} disabled={creating || !pipelineId}>
            {creating ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <CheckCircle2 data-icon="inline-start" />
            )}
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// NewPipelineDialog creates a pipeline directory (pipeline.yml + assets/) at
// the given workspace-relative path; the workspace SSE update then lists it
// and the page navigates onto it.
function NewPipelineDialog({
  open,
  onOpenChange,
  existingPaths,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existingPaths: Set<string>;
  onCreated: (path: string) => void;
}) {
  const [path, setPath] = useState("");
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setPath("");
      setName("");
      setError("");
    }
  }, [open]);

  const create = async () => {
    const trimmedPath = path.trim().replace(/^\/+|\/+$/g, "");
    if (!trimmedPath) {
      setError("Pipeline directory is required.");
      return;
    }
    if (/\s/.test(trimmedPath) || trimmedPath.includes("..")) {
      setError("Use a relative directory path without spaces.");
      return;
    }
    if (
      [...existingPaths].some(
        (existing) => existing === trimmedPath || existing.startsWith(`${trimmedPath}/`),
      )
    ) {
      setError(`A pipeline already exists at "${trimmedPath}".`);
      return;
    }
    setCreating(true);
    setError("");
    try {
      await createPipeline({ path: trimmedPath, name: name.trim() || undefined });
      onOpenChange(false);
      onCreated(trimmedPath);
    } catch (caught) {
      setError(String(caught));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="size-4 text-primary" />
            New pipeline
          </DialogTitle>
          <DialogDescription>
            Creates a directory with a <span className="font-mono">pipeline.yml</span> and an empty{" "}
            <span className="font-mono">assets/</span> folder.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="new-pipeline-path">Directory</Label>
            <Input
              id="new-pipeline-path"
              className="font-mono"
              placeholder="marketing_pipeline"
              value={path}
              onChange={(event) => setPath(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !creating) {
                  void create();
                }
              }}
              autoFocus
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="new-pipeline-name">Display name (optional)</Label>
            <Input
              id="new-pipeline-name"
              placeholder="Marketing"
              value={name}
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !creating) {
                  void create();
                }
              }}
            />
          </div>
          {error ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300">
              {error}
            </div>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={creating}>
            Cancel
          </Button>
          <Button onClick={() => void create()} disabled={creating}>
            {creating ? <Spinner className="size-4" /> : <CheckCircle2 className="size-4" />}Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// NewFolderDialog asks for a folder (prefix) name and chains into the asset
// dialog: folders are asset-name prefixes (assets/<folder>/), so a folder
// appears once its first asset is created inside it.
function NewFolderDialog({
  open,
  onOpenChange,
  pipelineName,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineName?: string;
  onConfirm: (prefix: string) => void;
}) {
  const [folder, setFolder] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setFolder("");
      setError("");
    }
  }, [open]);

  const confirm = () => {
    const trimmed = folder.trim().replace(/^\.+|\.+$/g, "");
    if (!trimmed) {
      setError("Folder name is required.");
      return;
    }
    if (!/^[a-z0-9_]+(\.[a-z0-9_]+)*$/i.test(trimmed)) {
      setError("Use letters, digits and underscores; separate nested folders with dots.");
      return;
    }
    onConfirm(trimmed);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderPlus className="size-4 text-primary" />
            New folder
          </DialogTitle>
          <DialogDescription>
            Folders group assets under <span className="font-mono">assets/&lt;folder&gt;/</span>
            {pipelineName ? (
              <>
                {" "}
                in <span className="font-mono">{pipelineName}</span>
              </>
            ) : null}
            . The folder is created together with its first asset.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="new-folder-name">Folder name</Label>
          <Input
            id="new-folder-name"
            className="font-mono"
            placeholder="analytics"
            value={folder}
            onChange={(event) => setFolder(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                confirm();
              }
            }}
            autoFocus
          />
          {error ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300">
              {error}
            </div>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={confirm}>
            <FolderPlus className="size-4" />
            Choose first asset
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const pipelineSettingsSections = [
  { id: "general", label: "General" },
  { id: "schedule", label: "Schedule" },
  { id: "connections", label: "Connections" },
  { id: "notifications", label: "Notifications" },
  { id: "variables", label: "Variables" },
] as const;

export type PipelineSettingsSection = (typeof pipelineSettingsSections)[number]["id"];

type PipelineConfigDraft = UpdatePipelineConfigRequest;

// The full pipeline config lives in pipeline.yml; the backend exposes it through
// GET/PUT /api/pipelines/:id/config. This dialog edits every writable field and
// persists via the same endpoint — SSE then reconciles the workspace.
function PipelineSettingsDialog({
  open,
  onOpenChange,
  pipelineId,
  initialSection,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId: string;
  initialSection?: PipelineSettingsSection;
}) {
  const [section, setSection] = useState<PipelineSettingsSection>(initialSection ?? "general");
  const [draft, setDraft] = useState<PipelineConfigDraft | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [inferredDefaultConnections, setInferredDefaultConnections] = useState<
    PipelineConfigConnection[]
  >([]);

  // Re-fetch whenever the dialog opens so the form always reflects on-disk state
  // (a code edit or CLI run may have changed pipeline.yml since last time).
  useEffect(() => {
    if (!open) return;
    setSection(initialSection ?? "general");
    setError(null);
    setLoading(true);
    setInferredDefaultConnections([]);
    let cancelled = false;
    getPipelineConfig(pipelineId)
      .then((config) => {
        if (cancelled) return;
        setDraft(configResponseToDraft(config));
        setInferredDefaultConnections(config.inferred_default_connections ?? []);
      })
      .catch((cause) => {
        if (cancelled) return;
        setError(cause instanceof Error ? cause.message : "Failed to load pipeline settings.");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, pipelineId, initialSection]);

  const update = useCallback(
    <K extends keyof PipelineConfigDraft>(key: K, value: PipelineConfigDraft[K]) => {
      setDraft((current) => (current ? { ...current, [key]: value } : current));
    },
    [],
  );

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setError(null);
    try {
      const response = await updatePipelineConfig(pipelineId, draft);
      setDraft(configResponseToDraft(response));
      setInferredDefaultConnections(response.inferred_default_connections ?? []);
      onOpenChange(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to save pipeline settings.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            Pipeline settings{" "}
            <span className="font-mono text-xs text-muted-foreground">
              · {draft?.name || pipelineId}
            </span>
          </DialogTitle>
          <DialogDescription>
            Edit the pipeline configuration stored in{" "}
            <span className="font-mono">pipeline.yml</span>.
          </DialogDescription>
        </DialogHeader>
        <div className="grid min-h-80 gap-4 md:grid-cols-[180px_minmax(0,1fr)]">
          <div className="flex gap-2 overflow-x-auto md:block md:space-y-1">
            {pipelineSettingsSections.map((item) => (
              <Button
                key={item.id}
                variant={section === item.id ? "secondary" : "ghost"}
                className="justify-start"
                onClick={() => setSection(item.id)}
              >
                {item.label}
              </Button>
            ))}
          </div>
          <ScrollArea
            className="max-h-[26rem] min-h-0 rounded-lg border"
            data-testid="pipeline-settings-content"
          >
            <div className="flex flex-col gap-4 p-4">
              {loading || !draft ? (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" />
                  Loading settings…
                </div>
              ) : (
                <PipelineSettingsSectionBody
                  section={section}
                  draft={draft}
                  update={update}
                  inferredDefaultConnections={inferredDefaultConnections}
                />
              )}
            </div>
          </ScrollArea>
        </div>
        {error ? <p className="text-xs text-red-600">{error}</p> : null}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={saving || loading || !draft}>
            {saving ? (
              <>
                <Loader2 className="size-4 animate-spin" />
                Saving…
              </>
            ) : (
              "Save changes"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function configResponseToDraft(config: {
  name?: string;
  schedule?: string;
  start_date?: string;
  owner?: string;
  tags?: string[];
  domains?: string[];
  default_connections?: PipelineConfigConnection[];
  inferred_default_connections?: PipelineConfigConnection[];
  catchup?: boolean;
  metadata_push_bigquery?: boolean;
  retries?: number;
  concurrency?: number;
  max_active_steps?: number;
  notifications_slack?: PipelineConfigDraft["notifications_slack"];
  notifications_teams?: PipelineConfigDraft["notifications_teams"];
  defaults?: PipelineConfigDraft["defaults"];
  variables?: PipelineConfigVariable[];
}): PipelineConfigDraft {
  const notification = (value?: PipelineConfigDraft["notifications_slack"]) => ({
    enabled: value?.enabled ?? false,
    channel: value?.channel ?? "",
    connection: value?.connection ?? "",
    success: value?.success ?? false,
    failure: value?.failure ?? true,
  });
  return {
    name: config.name ?? "",
    schedule: config.schedule ?? "",
    start_date: config.start_date ?? "",
    owner: config.owner ?? "",
    tags: config.tags ?? [],
    domains: config.domains ?? [],
    default_connections: config.default_connections ?? [],
    catchup: config.catchup ?? false,
    metadata_push_bigquery: config.metadata_push_bigquery ?? false,
    retries: config.retries ?? 0,
    concurrency: config.concurrency ?? 0,
    max_active_steps: config.max_active_steps,
    notifications_slack: notification(config.notifications_slack),
    notifications_teams: notification(config.notifications_teams),
    defaults: config.defaults ?? {},
    variables: config.variables ?? [],
  };
}

function PipelineSettingsSectionBody({
  section,
  draft,
  update,
  inferredDefaultConnections,
}: {
  section: PipelineSettingsSection;
  draft: PipelineConfigDraft;
  update: <K extends keyof PipelineConfigDraft>(key: K, value: PipelineConfigDraft[K]) => void;
  inferredDefaultConnections: PipelineConfigConnection[];
}) {
  const environment = useAtomValue(selectedEnvironmentAtom);
  if (section === "general") {
    return (
      <>
        <SettingsTextField
          label="Pipeline name"
          value={draft.name}
          onChange={(value) => update("name", value)}
          placeholder="my_pipeline"
        />
        <SettingsTextField
          label="Owner"
          value={draft.owner}
          onChange={(value) => update("owner", value)}
          placeholder="team@acme.io"
        />
        <SettingsMultiValueField
          label="Tags"
          value={draft.tags}
          onChange={(value) => update("tags", value)}
          placeholder="Add tag"
        />
        <SettingsMultiValueField
          label="Domains"
          value={draft.domains}
          onChange={(value) => update("domains", value)}
          placeholder="Add domain"
        />
        <div className="grid grid-cols-2 gap-3">
          <SettingsNumberField
            label="Retries"
            value={draft.retries}
            onChange={(value) => update("retries", value ?? 0)}
          />
          <SettingsNumberField
            label="Concurrency"
            value={draft.concurrency}
            onChange={(value) => update("concurrency", value ?? 0)}
          />
        </div>
        <SettingsNumberField
          label="Max active steps"
          value={draft.max_active_steps}
          onChange={(value) => update("max_active_steps", value)}
          hint="Leave blank for no limit."
        />
        <SettingsToggleField
          label="Push metadata to BigQuery"
          description="Sync asset metadata to BigQuery after each run."
          checked={draft.metadata_push_bigquery}
          onChange={(value) => update("metadata_push_bigquery", value)}
        />
      </>
    );
  }
  if (section === "schedule") {
    return (
      <>
        <SettingsTextField
          label="Schedule"
          value={draft.schedule}
          onChange={(value) => update("schedule", value)}
          placeholder="@daily"
          hint="A cron expression or preset like @daily / @hourly."
        />
        <SettingsTextField
          label="Start date"
          value={draft.start_date}
          onChange={(value) => update("start_date", value)}
          placeholder="2024-01-01"
        />
        <SettingsToggleField
          label="Catchup"
          description="Backfill every schedule interval missed since the start date."
          checked={draft.catchup}
          onChange={(value) => update("catchup", value)}
        />
        <div className="grid gap-3 border-t pt-4">
          <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            Interval defaults
          </div>
          <SettingsNumberField
            label="Rerun cooldown (seconds)"
            value={draft.defaults.rerun_cooldown}
            onChange={(value) => update("defaults", { ...draft.defaults, rerun_cooldown: value })}
          />
          <div className="grid grid-cols-2 gap-3">
            <SettingsTextField
              label="Start offset"
              value={draft.defaults.start_offset_raw ?? ""}
              onChange={(value) =>
                update("defaults", { ...draft.defaults, start_offset_raw: value || undefined })
              }
              placeholder="-1d"
            />
            <SettingsTextField
              label="End offset"
              value={draft.defaults.end_offset_raw ?? ""}
              onChange={(value) =>
                update("defaults", { ...draft.defaults, end_offset_raw: value || undefined })
              }
              placeholder="0d"
            />
          </div>
        </div>
      </>
    );
  }
  if (section === "connections") {
    return (
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">
          Default connection per platform. Assets that don&apos;t name a connection use these.
        </p>
        <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
          Pipeline overrides
        </div>
        {draft.default_connections.length === 0 ? (
          <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">
            No overrides in pipeline.yml.
          </p>
        ) : (
          draft.default_connections.map((connection, index) => (
            <div key={index} className="flex items-end gap-2">
              <SettingsTextField
                className="flex-1"
                label={index === 0 ? "Platform" : undefined}
                value={connection.platform}
                onChange={(value) =>
                  update(
                    "default_connections",
                    replaceAt(draft.default_connections, index, { ...connection, platform: value }),
                  )
                }
                placeholder="gcp"
              />
              <SettingsTextField
                className="flex-1"
                label={index === 0 ? "Connection" : undefined}
                value={connection.name}
                onChange={(value) =>
                  update(
                    "default_connections",
                    replaceAt(draft.default_connections, index, { ...connection, name: value }),
                  )
                }
                placeholder="bq-prod"
              />
              <PipelineConnectionSettingsLink
                environment={environment}
                connection={connection.name}
              />
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove connection"
                onClick={() =>
                  update("default_connections", removeAt(draft.default_connections, index))
                }
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            update("default_connections", [
              ...draft.default_connections,
              { platform: "", name: "" },
            ])
          }
        >
          <Plus className="size-3.5" />
          Add connection
        </Button>
        {inferredDefaultConnections.length > 0 ? (
          <div className="space-y-2 border-t pt-3">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                Inferred defaults
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                Bruin derives these from asset types when no pipeline override exists.
              </p>
            </div>
            {inferredDefaultConnections.map((connection) => (
              <div
                key={`${connection.platform}:${connection.name}`}
                data-testid="inferred-default-connection"
                className="flex items-center gap-3 rounded-md border bg-muted/30 px-3 py-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                    Platform
                  </div>
                  <div className="truncate font-mono text-xs">{connection.platform}</div>
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                    Connection
                  </div>
                  <div className="truncate font-mono text-xs">{connection.name}</div>
                </div>
                <Badge variant="outline">Inferred</Badge>
                <PipelineConnectionSettingsLink
                  environment={environment}
                  connection={connection.name}
                />
              </div>
            ))}
          </div>
        ) : null}
      </div>
    );
  }
  if (section === "notifications") {
    return (
      <div className="space-y-5">
        <NotificationChannelFields
          title="Slack"
          value={draft.notifications_slack}
          onChange={(value) => update("notifications_slack", value)}
          channelPlaceholder="#data-alerts"
        />
        <NotificationChannelFields
          title="Microsoft Teams"
          value={draft.notifications_teams}
          onChange={(value) => update("notifications_teams", value)}
          channelPlaceholder="Data alerts"
        />
      </div>
    );
  }
  if (section === "variables") {
    return (
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">
          Pipeline variables available to assets via{" "}
          <span className="font-mono">{"{{ var.name }}"}</span>.
        </p>
        {draft.variables.map((variable, index) => (
          <div key={index} className="space-y-2 rounded-md border p-3">
            <div className="flex items-end gap-2">
              <SettingsTextField
                className="flex-1"
                label="Name"
                value={variable.name}
                onChange={(value) =>
                  update(
                    "variables",
                    replaceAt(draft.variables, index, { ...variable, name: value }),
                  )
                }
                placeholder="lookback_days"
              />
              <SettingsTextField
                className="w-28"
                label="Type"
                value={variable.type}
                onChange={(value) =>
                  update(
                    "variables",
                    replaceAt(draft.variables, index, { ...variable, type: value }),
                  )
                }
                placeholder="integer"
              />
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove variable"
                onClick={() => update("variables", removeAt(draft.variables, index))}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
            <SettingsTextField
              label="Default"
              value={variableValueToText(variable.default_value)}
              onChange={(value) =>
                update(
                  "variables",
                  replaceAt(draft.variables, index, { ...variable, default_value: value }),
                )
              }
              placeholder="30"
            />
            <SettingsTextField
              label="Description"
              value={variable.description ?? ""}
              onChange={(value) =>
                update(
                  "variables",
                  replaceAt(draft.variables, index, {
                    ...variable,
                    description: value || undefined,
                  }),
                )
              }
            />
          </div>
        ))}
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            update("variables", [
              ...draft.variables,
              { name: "", type: "string", default_value: "" },
            ])
          }
        >
          <Plus className="size-3.5" />
          Add variable
        </Button>
      </div>
    );
  }
  return null;
}

function NotificationChannelFields({
  title,
  value,
  onChange,
  channelPlaceholder,
}: {
  title: string;
  value: PipelineConfigDraft["notifications_slack"];
  onChange: (value: PipelineConfigDraft["notifications_slack"]) => void;
  channelPlaceholder: string;
}) {
  return (
    <div className="space-y-3">
      <SettingsToggleField
        label={title}
        description={`Send ${title} messages for this pipeline's runs.`}
        checked={value.enabled}
        onChange={(enabled) => onChange({ ...value, enabled })}
      />
      {value.enabled ? (
        <div className="space-y-3 border-l pl-3">
          <SettingsTextField
            label="Channel"
            value={value.channel ?? ""}
            onChange={(channel) => onChange({ ...value, channel })}
            placeholder={channelPlaceholder}
          />
          <SettingsTextField
            label="Connection"
            value={value.connection ?? ""}
            onChange={(connection) => onChange({ ...value, connection })}
            placeholder="slack-default"
            hint="Named connection that authenticates the message."
          />
          <div className="flex gap-4">
            <SettingsToggleField
              compact
              label="On success"
              checked={value.success}
              onChange={(success) => onChange({ ...value, success })}
            />
            <SettingsToggleField
              compact
              label="On failure"
              checked={value.failure}
              onChange={(failure) => onChange({ ...value, failure })}
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

function PipelineConnectionSettingsLink({
  environment,
  connection,
}: {
  environment?: string;
  connection: string;
}) {
  const name = connection.trim();
  if (!name) return null;
  return (
    <Button asChild variant="ghost" size="icon-sm">
      <Link
        to="/project/connections"
        search={{ environment: environment || undefined, connection: name }}
        aria-label={`Open ${name} in project connection settings`}
        title={`Open ${name} in project connection settings`}
      >
        <ExternalLink />
      </Link>
    </Button>
  );
}

function SettingsTextField({
  label,
  value,
  onChange,
  placeholder,
  hint,
  className,
}: {
  label?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  hint?: string;
  className?: string;
}) {
  return (
    <label className={cn("block space-y-1.5", className)}>
      {label ? <span className="text-xs font-medium text-muted-foreground">{label}</span> : null}
      <Input
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      {hint ? <span className="block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  );
}

function SettingsMultiValueField({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <MultiValueInput value={value} onChange={onChange} placeholder={placeholder} />
    </label>
  );
}

function SettingsNumberField({
  label,
  value,
  onChange,
  hint,
}: {
  label: string;
  value?: number;
  onChange: (value: number | undefined) => void;
  hint?: string;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <Input
        type="number"
        value={value ?? ""}
        onChange={(event) => {
          const raw = event.target.value.trim();
          onChange(raw === "" ? undefined : Number(raw));
        }}
      />
      {hint ? <span className="block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  );
}

function SettingsToggleField({
  label,
  description,
  checked,
  onChange,
  compact,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  compact?: boolean;
}) {
  if (compact) {
    return (
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={checked} onCheckedChange={onChange} />
        {label}
      </label>
    );
  }
  return (
    <div className="flex items-start justify-between gap-3">
      <span>
        <span className="block text-sm font-medium">{label}</span>
        {description ? <span className="text-xs text-muted-foreground">{description}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  );
}

function replaceAt<T>(list: T[], index: number, value: T): T[] {
  return list.map((item, itemIndex) => (itemIndex === index ? value : item));
}

function removeAt<T>(list: T[], index: number): T[] {
  return list.filter((_, itemIndex) => itemIndex !== index);
}

function variableValueToText(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  return String(value);
}

// DeployButton keeps deployment status compact; mutating always opens the
// reviewed deployment plan rather than snapshotting immediately.
function DeployButton({
  deployState,
  onReview,
}: {
  deployState: PipelineDeployState;
  onReview: () => void;
}) {
  const { status, loading, error, deploying, refresh, driftedFileCount } = deployState;
  if (!status) {
    if (!loading && !error) return null;
    if (error) {
      return (
        <Button variant="outline" size="sm" onClick={() => void refresh()} title={error}>
          <RefreshCw data-icon="inline-start" /> Retry deployment status
        </Button>
      );
    }
    return (
      <Button variant="ghost" size="sm" disabled title="Resolving deployment status">
        <Spinner data-icon="inline-start" />
        Deployment…
      </Button>
    );
  }

  if (status.has_snapshot && status.in_sync && status.executable) {
    const currentDeployment = deploymentLabel(status.ordinal, status.version_id);
    return (
      <Button variant="ghost" size="sm" disabled title={`${currentDeployment} is current`}>
        <Package className="size-3.5 text-emerald-600" /> Deployed
      </Button>
    );
  }

  const label = status.has_snapshot
    ? status.executable
      ? `Redeploy (${driftedFileCount} file${driftedFileCount === 1 ? "" : "s"} changed)`
      : "Repair deployment"
    : "Deploy";
  const title = status.has_snapshot
    ? status.executable
      ? `Working tree differs from ${deploymentLabel(status.ordinal, status.version_id, "deployment")}`
      : `The latest deployment is not executable: ${status.integrity_error ?? "integrity validation failed"}`
    : "No deployment exists yet; schedules require an exact deployment pin";
  return (
    <Button variant="outline" size="sm" onClick={onReview} disabled={deploying} title={title}>
      <Package
        className={cn(
          "size-3.5",
          status.has_snapshot
            ? status.executable
              ? "text-amber-600"
              : "text-destructive"
            : undefined,
        )}
      />
      {deploying ? "Deploying…" : label}
    </Button>
  );
}

type BuildStaleProgress = "pending" | "running" | "done" | "failed" | "skipped";

// BuildStaleDialog previews the stale set and hands the whole build to the
// server, which recomputes the plan (every stale asset; for partially-covered
// incrementals exactly the uncovered gap intervals), builds it in dependency
// order as one streamed run, and reports per-asset progress events back here.
function BuildStaleDialog({
  open,
  onOpenChange,
  staleAssets,
  onBuild,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  staleAssets: AssetStaleness[];
  onBuild: (
    onAssetEvent: (event: StreamAssetEvent) => void,
  ) => Promise<MaterializeStreamPayload | null>;
}) {
  const [progress, setProgress] = useState<Record<string, BuildStaleProgress>>({});
  const [building, setBuilding] = useState(false);
  const [buildError, setBuildError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setProgress({});
      setBuilding(false);
      setBuildError(null);
    }
  }, [open]);

  const buildAll = async () => {
    setBuilding(true);
    setProgress({});
    setBuildError(null);
    try {
      const result = await onBuild((event) => {
        if (!event.asset_name || !event.status) {
          return;
        }
        const mapped: BuildStaleProgress =
          event.status === "running"
            ? "running"
            : event.status === "succeeded"
              ? "done"
              : event.status === "skipped"
                ? "skipped"
                : "failed";
        const assetName = event.asset_name;
        setProgress((current) => ({ ...current, [assetName]: mapped }));
      });
      if (!result) {
        setBuildError("The build could not be started. Review the Materialize output for details.");
      } else if (result.status === "error") {
        setBuildError(
          result.error || "The build failed. Review the Materialize output for details.",
        );
      }
    } catch (cause) {
      setBuildError(cause instanceof Error ? cause.message : "The build could not be started.");
    } finally {
      setBuilding(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Hammer className="size-4 text-primary" />
            Build stale assets
          </DialogTitle>
          <DialogDescription>
            {staleAssets.length} asset{staleAssets.length === 1 ? "" : "s"} out of date for this
            environment and time range. The server builds them in dependency order as one run;
            partial incrementals rebuild only the uncovered gaps.
          </DialogDescription>
        </DialogHeader>
        {buildError ? (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Build needed failed</AlertTitle>
            <AlertDescription>{buildError}</AlertDescription>
          </Alert>
        ) : null}
        <div className="max-h-80 space-y-1 overflow-y-auto">
          {staleAssets.map((stale) => (
            <div
              key={stale.asset_id}
              className="flex items-center gap-2 rounded-md border px-2 py-1.5 text-xs"
            >
              <span className="min-w-0 flex-1 truncate font-mono">{stale.asset_name}</span>
              <StalenessBadge staleness={stale} />
              {stale.gaps?.length ? (
                <span className="text-[10px] text-muted-foreground">
                  {stale.gaps.length} gap{stale.gaps.length === 1 ? "" : "s"}
                </span>
              ) : null}
              {progress[stale.asset_name] === "running" ? (
                <span className="text-[10px] text-sky-600">building…</span>
              ) : null}
              {progress[stale.asset_name] === "done" ? (
                <Check className="size-3.5 text-emerald-600" />
              ) : null}
              {progress[stale.asset_name] === "skipped" ? (
                <span
                  className="text-[10px] text-muted-foreground"
                  title="Skipped: a stale upstream failed"
                >
                  skipped
                </span>
              ) : null}
              {progress[stale.asset_name] === "failed" ? (
                <XCircle className="size-3.5 text-red-600" />
              ) : null}
            </div>
          ))}
          {staleAssets.length === 0 ? (
            <p className="text-xs text-muted-foreground">Everything is fresh.</p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={building}>
            Close
          </Button>
          <Button onClick={buildAll} disabled={building || staleAssets.length === 0}>
            <Play className="size-4" />
            {building
              ? "Building…"
              : `Build ${staleAssets.length} asset${staleAssets.length === 1 ? "" : "s"}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SettingsIcon() {
  return <Sliders className="size-3.5" />;
}

function isRenderableAssetType(assetType: string) {
  const normalized = assetType.trim().toLowerCase();
  return (
    isSqlAssetType(normalized) ||
    isSeedAssetType(normalized) ||
    isSensorAssetType(normalized) ||
    normalized === "python" ||
    normalized === "load" ||
    normalized === "api" ||
    normalized === "ingestr"
  );
}
