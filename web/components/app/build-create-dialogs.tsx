import { useAtomValue } from "jotai";
import {
  AlertTriangle,
  CheckCircle2,
  Cpu,
  Download,
  FileCode,
  FolderPlus,
  Globe,
  Plus,
  Radar,
  Sprout,
} from "lucide-react";
import { type ComponentType, useEffect, useMemo, useRef, useState } from "react";

import type { NewAssetKind } from "@/components/new-asset-node";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { createAsset } from "@/lib/api-assets";
import { API_ASSET_TEMPLATES, type APIAssetTemplateId } from "@/lib/api-asset-templates";
import { createPipeline } from "@/lib/api-pipelines";
import { selectedEnvironmentAtom, workspaceAtom } from "@/lib/atoms/domains/workspace";
import {
  LOCAL_LOAD_CONNECTION,
  isLocalLoadConnection,
  loadConnectionCategory,
  loadConnectionsForEnvironment,
  loadTargetNeedsDestinationObject,
} from "@/lib/load-assets";
import { cn } from "@/lib/utils";
import { buildCreateAssetInput, buildSuggestedAssetName } from "@/lib/workspace-shell-helpers";

import { FilePathPicker } from "./file-path-picker";
import {
  SemanticAssetCreateFields,
  buildSemanticAssetCreatePayload,
  defaultSemanticAssetDraft,
  type SemanticAssetDraft,
  type SemanticAssetKind,
} from "./semantic-asset-create-fields";

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

export function NewAssetDialog({
  open,
  onOpenChange,
  pipelineId,
  pipelineName,
  existingAssetNames,
  downstreamSource,
  namePrefix,
  initialExecutableContent,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId?: string;
  pipelineName?: string;
  existingAssetNames: Set<string>;
  downstreamSource?: { id: string; name: string } | null;
  namePrefix?: string | null;
  initialExecutableContent?: string | null;
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
  const [kindPickerExpanded, setKindPickerExpanded] = useState(true);
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
    setKindPickerExpanded(true);
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
      ? buildSemanticAssetCreatePayload(semanticKind, semanticDraft, semanticCapabilities, trimmed)
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
    if (selected.id === "sql" && initialExecutableContent?.trim()) {
      input = { ...input, executable_content: initialExecutableContent };
    }
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
      <DialogContent className="flex max-h-[90dvh] min-w-0 flex-col overflow-hidden sm:max-w-3xl">
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
        <ScrollArea className="min-h-0 min-w-0 flex-1" viewportClassName="p-1">
          <div className="grid min-w-0 gap-5">
            <div className="min-w-0">
              <div
                className={cn(
                  "grid min-w-0 transition-[grid-template-rows,opacity] duration-300 ease-out motion-reduce:transition-none",
                  kindPickerExpanded
                    ? "grid-rows-[1fr] opacity-100"
                    : "pointer-events-none grid-rows-[0fr] opacity-0",
                )}
                aria-hidden={!kindPickerExpanded}
                inert={!kindPickerExpanded}
              >
                <div className="min-h-0 min-w-0 overflow-hidden p-1">
                  <ToggleGroup
                    type="single"
                    variant="outline"
                    value={selected.id}
                    onValueChange={(nextKind) => {
                      setKindPickerExpanded(false);
                      if (!nextKind) return;
                      const next = nextKind as NewAssetKind;
                      setKind(next);
                      if (next === "seed" || next === "sensor") {
                        setSemanticDraft(
                          defaultSemanticAssetDraft(
                            next,
                            semanticCapabilities,
                            semanticConnections,
                          ),
                        );
                      }
                    }}
                    className="grid w-full min-w-0 grid-cols-2 items-stretch gap-2 sm:grid-cols-3"
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
                </div>
              </div>
              <div
                className={cn(
                  "grid min-w-0 transition-[grid-template-rows,opacity] duration-300 ease-out motion-reduce:transition-none",
                  kindPickerExpanded
                    ? "pointer-events-none grid-rows-[0fr] opacity-0"
                    : "grid-rows-[1fr] opacity-100",
                )}
                aria-hidden={kindPickerExpanded}
                inert={kindPickerExpanded}
              >
                <div className="min-h-0 min-w-0 overflow-hidden p-1">
                  <div className="flex min-w-0 items-center gap-2 rounded-md border bg-muted/20 px-2.5 py-2">
                    <selected.icon className="size-4 shrink-0 text-primary" />
                    <div className="min-w-0 flex-1">
                      <div className="text-xs font-medium">{selected.label}</div>
                      <div className="truncate text-[11px] text-muted-foreground">
                        {selected.description}
                      </div>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="xs"
                      className="shrink-0"
                      onClick={() => setKindPickerExpanded(true)}
                    >
                      Change type
                    </Button>
                  </div>
                </div>
              </div>
            </div>
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
                    {
                      API_ASSET_TEMPLATES.find((template) => template.id === apiTemplate)
                        ?.description
                    }
                  </FieldDescription>
                </Field>
              </FieldGroup>
            ) : null}
            {selected.id === "load" ? (
              <FieldGroup>
                {!isDownstream ? (
                  <>
                    <Field variant="plain">
                      <FieldLabel htmlFor="new-load-source-connection">
                        Source connection
                      </FieldLabel>
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
            {selected.id === "sql" || selected.id === "api" || selected.id === "load" ? (
              <FieldGroup>
                <Field variant="plain">
                  <FieldLabel htmlFor="new-asset-connection">
                    {selected.id === "sql" ? "Target connection" : "Destination connection"}
                  </FieldLabel>
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
                        <SelectItem value={AUTO_CONNECTION_VALUE}>
                          Auto (pipeline default)
                        </SelectItem>
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
                      : selected.id === "sql"
                        ? "Where the query is materialized. Auto uses the pipeline default."
                        : "Where fetched records are loaded. You can change this later."}
                  </FieldDescription>
                </Field>
                {selected.id === "load" && targetNeedsDestinationObject ? (
                  <Field variant="plain">
                    <FieldLabel htmlFor="new-load-destination-object">
                      Destination object
                    </FieldLabel>
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
        </ScrollArea>
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
export function NewPipelineDialog({
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
export function NewFolderDialog({
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
