"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import { useAtomValue } from "jotai";
import {
  ArrowUpRight,
  Boxes,
  Check,
  Database,
  FileText,
  Folder,
  HardDrive,
  Loader2,
  Plug,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { refreshAssetColumnsFromDefinition } from "@/lib/api-asset-transactions";
import { updateAsset } from "@/lib/api-assets";
import { getOnboardingPathSuggestions } from "@/lib/api-onboarding";
import { discoverLoadStreams, LoadDiscoveryStream } from "@/lib/api-load";
import { selectedEnvironmentAtom, workspaceAtom } from "@/lib/atoms/workspace";
import { IngestrSuggestion, WebAsset, WorkspaceConfigConnection } from "@/lib/types";
import { cn } from "@/lib/utils";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import {
  LOCAL_LOAD_CONNECTION_OPTION,
  isLocalLoadConnection,
  loadConnectionCategory,
  loadConnectionsForEnvironment,
  loadTargetNeedsDestinationObject,
} from "@/lib/load-assets";

import { Comment, Key, Line } from "./asset-yaml-editor";

const CATEGORY_LABELS: Record<string, string> = {
  database: "Databases",
  storage: "Storage",
  file: "Files",
};

const CATEGORY_ORDER = ["database", "storage", "file"];

function categoryIcon(category: string | undefined) {
  if (category === "storage") return HardDrive;
  if (category === "file") return HardDrive;
  return Database;
}

/**
 * The main-pane editor for a Load asset. Load assets carry their whole
 * source intent under flat `parameters`; its target connection lives in the
 * shared metadata editor and database targets always use the asset name.
 * Generic metadata (name, columns, dependencies, …) stays in the Properties
 * sidebar; the upstream dependency and columns are inferred from the source.
 */
export function LoadParametersEditor({
  asset,
  pipelineId,
  onGoToAsset,
}: {
  asset: WebAsset;
  pipelineId: string;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
}) {
  const environment = useAtomValue(selectedEnvironmentAtom);
  const workspace = useAtomValue(workspaceAtom);
  const { workspaceConfig } = useWorkspaceSettingsData();
  const params = asset.parameters ?? {};

  const configuredConnections = useMemo(
    () => loadConnectionsForEnvironment(workspaceConfig, environment),
    [workspaceConfig, environment],
  );
  const connections = useMemo(
    () => [LOCAL_LOAD_CONNECTION_OPTION, ...configuredConnections],
    [configuredConnections],
  );
  const targetCategory = loadConnectionCategory(configuredConnections, asset.connection);
  const targetNeedsObject = loadTargetNeedsDestinationObject(targetCategory);

  const sourceAsset = useMemo(() => {
    const pipeline = workspace?.pipelines.find((candidate) => candidate.id === pipelineId);
    if (!pipeline) return null;
    const source = (params.source_table ?? "").trim().toLowerCase();
    const direct = source
      ? pipeline.assets.find(
          (candidate) =>
            candidate.name.trim().toLowerCase() === source ||
            candidate.id.trim().toLowerCase() === source,
        )
      : undefined;
    if (direct) return direct;
    if ((asset.upstreams ?? []).length !== 1) return null;
    const upstream = asset.upstreams[0].trim().toLowerCase();
    return (
      pipeline.assets.find(
        (candidate) =>
          candidate.name.trim().toLowerCase() === upstream ||
          candidate.id.trim().toLowerCase() === upstream,
      ) ?? null
    );
  }, [asset.upstreams, params.source_table, pipelineId, workspace?.pipelines]);

  const setParam = (key: string, value: string) => {
    const next: Record<string, string> = { ...params };
    const trimmed = value.trim();
    if (trimmed) {
      next[key] = trimmed;
    } else {
      delete next[key];
    }
    void updateAsset(pipelineId, asset.id, { parameters: next });
  };

  return (
    <div className="font-monaco min-h-0 flex-1 overflow-y-auto bg-background p-3 text-[13px] leading-6">
      <Comment>Load assets replicate data between two configured connections.</Comment>
      <Comment>
        Edit the target connection in Properties; materialization controls the load mode.
      </Comment>
      <Line>
        <Key>type</Key>
        <span className="text-foreground">load</span>
      </Line>
      <Line>
        <Key>connection</Key>
        <span className="truncate text-foreground">
          {(asset.explicit_connection ?? "").trim() ||
            (asset.connection ? `auto (${asset.connection})` : "auto")}
        </span>
      </Line>
      <Line>
        <Key>parameters</Key>
      </Line>

      <Line depth={1}>
        <Key>source_connection</Key>
        <ConnectionValue
          value={params.source_connection ?? ""}
          connections={connections}
          onPick={(name) => setParam("source_connection", name)}
        />
      </Line>
      <Line depth={1}>
        <Key>source_table</Key>
        {isLocalLoadConnection(params.source_connection) ? (
          <PathValue
            value={params.source_table ?? ""}
            placeholder="path/to/source.csv"
            onCommit={(value) => setParam("source_table", value)}
          />
        ) : (
          <StreamValue
            value={params.source_table ?? ""}
            connection={params.source_connection ?? ""}
            environment={environment}
            placeholder="schema.table"
            onCommit={(value) => setParam("source_table", value)}
          />
        )}
        {sourceAsset && onGoToAsset ? (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={`Go to ${sourceAsset.name}`}
            title={`Go to ${sourceAsset.name}`}
            onClick={() => onGoToAsset(pipelineId, sourceAsset.id)}
          >
            <ArrowUpRight />
          </Button>
        ) : null}
      </Line>

      {targetNeedsObject ? (
        <Line depth={1}>
          <Key>destination_object</Key>
          {isLocalLoadConnection(asset.connection) ? (
            <PathValue
              value={params.destination_object ?? ""}
              placeholder="path/to/destination.csv"
              onCommit={(value) => setParam("destination_object", value)}
            />
          ) : (
            <StreamValue
              value={params.destination_object ?? ""}
              connection={asset.connection ?? ""}
              environment={environment}
              placeholder="path/to/object"
              onCommit={(value) => setParam("destination_object", value)}
            />
          )}
        </Line>
      ) : (
        <Line depth={1}>
          <Key>destination_table</Key>
          <span className="truncate text-foreground">{asset.name}</span>
        </Line>
      )}

      <Separator className="mt-3" />
      <div className="pt-2">
        <Comment>The upstream dependency is inferred from the source on save.</Comment>
        <InferColumnsButton asset={asset} />
      </div>
    </div>
  );
}

// ConnectionValue is a combobox showing the chosen bruin connection; the picker
// is grouped by Load category (database / storage / file).
function ConnectionValue({
  value,
  connections,
  onPick,
}: {
  value: string;
  connections: WorkspaceConfigConnection[];
  onPick: (name: string) => void;
}) {
  const [open, setOpen] = useState(false);

  const grouped = useMemo(() => {
    const byCategory = new Map<string, WorkspaceConfigConnection[]>();
    for (const connection of connections) {
      const category = connection.load_category ?? "database";
      const list = byCategory.get(category) ?? [];
      list.push(connection);
      byCategory.set(category, list);
    }
    return CATEGORY_ORDER.filter((category) => byCategory.has(category)).map((category) => ({
      category,
      items: (byCategory.get(category) ?? []).sort((a, b) => a.name.localeCompare(b.name)),
    }));
  }, [connections]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "font-monaco flex min-w-0 flex-1 items-center gap-1 rounded-sm px-1 text-left outline-none hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring",
            value ? "text-foreground" : "text-muted-foreground/60",
          )}
        >
          <Plug className="size-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{value || "pick a connection…"}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <Command>
          <CommandInput placeholder="Search connections…" className="text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-xs">
              No database, storage, or file connections configured.
            </CommandEmpty>
            {grouped.map((group) => (
              <CommandGroup
                key={group.category}
                heading={CATEGORY_LABELS[group.category] ?? group.category}
              >
                {group.items.map((connection) => {
                  const Icon = categoryIcon(connection.load_category);
                  const selected = connection.name === value;
                  return (
                    <CommandItem
                      key={connection.name}
                      value={connection.name}
                      onSelect={() => {
                        onPick(connection.name);
                        setOpen(false);
                      }}
                      className="text-xs"
                    >
                      <Icon className="mr-2 size-3 text-muted-foreground" />
                      <span className="flex-1 truncate">{connection.name}</span>
                      {selected ? (
                        <Check className="size-3 text-muted-foreground" />
                      ) : (
                        <span className="text-[10px] text-muted-foreground">{connection.type}</span>
                      )}
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// StreamValue is a free-text-or-discover combobox: it accepts an arbitrary
// object name and, when a connection is set, lists discovered streams via
// `sling conns discover` (with the typed text used as a --pattern).
function StreamValue({
  value,
  connection,
  environment,
  placeholder,
  onCommit,
}: {
  value: string;
  connection: string;
  environment: string | undefined;
  placeholder?: string;
  onCommit: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [streams, setStreams] = useState<LoadDiscoveryStream[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const requestRef = useRef(0);

  useEffect(() => {
    if (!open || !connection) return;
    const token = ++requestRef.current;
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    discoverLoadStreams({ connection, environment, signal: controller.signal })
      .then((result) => {
        if (token !== requestRef.current) return;
        if (result.status === "error") {
          setError(result.error || "Discovery failed.");
          setStreams([]);
        } else {
          setStreams(result.streams ?? []);
        }
      })
      .catch((cause: unknown) => {
        if (token !== requestRef.current) return;
        if (cause instanceof DOMException && cause.name === "AbortError") return;
        setError(cause instanceof Error ? cause.message : "Discovery failed.");
      })
      .finally(() => {
        if (token === requestRef.current) setLoading(false);
      });
    return () => controller.abort();
  }, [open, connection, environment]);

  const commitCustom = () => {
    const trimmed = query.trim();
    if (trimmed) {
      onCommit(trimmed);
      setOpen(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "font-monaco min-w-0 flex-1 rounded-sm px-1 text-left outline-none hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring",
            value ? "text-foreground" : "text-muted-foreground/60",
          )}
        >
          <span className="truncate">{value || placeholder || "object…"}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={connection ? "Search or type an object…" : "Type an object name…"}
            className="text-xs"
            value={query}
            onValueChange={setQuery}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commitCustom();
              }
            }}
          />
          <CommandList>
            {loading ? (
              <div className="flex items-center gap-2 px-3 py-3 text-xs text-muted-foreground">
                <Loader2 className="size-3 animate-spin" /> discovering…
              </div>
            ) : null}
            {error ? <div className="px-3 py-3 text-xs text-amber-600">{error}</div> : null}
            {!loading && !error ? (
              <CommandEmpty className="py-3 text-xs">
                {connection ? "No streams found." : "Pick a connection to discover streams."}
              </CommandEmpty>
            ) : null}
            {query.trim() ? (
              <CommandGroup heading="Custom">
                <CommandItem
                  value={`__custom__${query}`}
                  onSelect={commitCustom}
                  className="text-xs"
                >
                  <span className="flex-1 truncate">
                    Use “<span className="text-foreground">{query.trim()}</span>”
                  </span>
                </CommandItem>
              </CommandGroup>
            ) : null}
            {streams.length > 0 ? (
              <CommandGroup heading="Discovered">
                {streams
                  .filter((stream) =>
                    query.trim()
                      ? stream.name.toLowerCase().includes(query.trim().toLowerCase())
                      : true,
                  )
                  .map((stream) => (
                    <CommandItem
                      key={stream.name}
                      value={stream.name}
                      onSelect={() => {
                        onCommit(stream.name);
                        setOpen(false);
                      }}
                      className="text-xs"
                    >
                      <Boxes className="mr-2 size-3 text-muted-foreground" />
                      <span className="flex-1 truncate">{stream.name}</span>
                      {stream.name === value ? (
                        <Check className="size-3 text-muted-foreground" />
                      ) : null}
                    </CommandItem>
                  ))}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// PathValue is a file-path combobox with filesystem autocomplete (reusing the
// onboarding path-suggestions endpoint). Selecting a directory drills in;
// selecting a file — or pressing Enter — commits the path.
function PathValue({
  value,
  placeholder,
  onCommit,
}: {
  value: string;
  placeholder?: string;
  onCommit: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState(value);
  const [suggestions, setSuggestions] = useState<IngestrSuggestion[]>([]);
  const [loading, setLoading] = useState(false);
  const requestRef = useRef(0);

  useEffect(() => {
    if (!open) return;
    const token = ++requestRef.current;
    setLoading(true);
    getOnboardingPathSuggestions(query)
      .then((result) => {
        if (token === requestRef.current) setSuggestions(result.suggestions ?? []);
      })
      .catch(() => {
        if (token === requestRef.current) setSuggestions([]);
      })
      .finally(() => {
        if (token === requestRef.current) setLoading(false);
      });
  }, [open, query]);

  const commit = (next: string) => {
    const trimmed = next.trim();
    if (trimmed) {
      onCommit(trimmed);
      setOpen(false);
    }
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setQuery(value);
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "font-monaco flex min-w-0 flex-1 items-center gap-1 rounded-sm px-1 text-left outline-none hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring",
            value ? "text-foreground" : "text-muted-foreground/60",
          )}
        >
          <FileText className="size-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{value || placeholder || "path…"}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-0">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="Type a path…"
            className="text-xs"
            value={query}
            onValueChange={setQuery}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commit(query);
              }
            }}
          />
          <CommandList>
            {loading ? (
              <div className="flex items-center gap-2 px-3 py-3 text-xs text-muted-foreground">
                <Loader2 className="size-3 animate-spin" /> listing…
              </div>
            ) : null}
            {!loading ? (
              <CommandEmpty className="py-3 text-xs">No matching paths.</CommandEmpty>
            ) : null}
            {query.trim() ? (
              <CommandGroup heading="Use path">
                <CommandItem
                  value={`__use__${query}`}
                  onSelect={() => commit(query)}
                  className="text-xs"
                >
                  <span className="flex-1 truncate">
                    Use “<span className="text-foreground">{query.trim()}</span>”
                  </span>
                </CommandItem>
              </CommandGroup>
            ) : null}
            {suggestions.length > 0 ? (
              <CommandGroup heading="Paths">
                {suggestions.map((suggestion) => {
                  const isDirectory = suggestion.kind === "directory";
                  return (
                    <CommandItem
                      key={suggestion.value}
                      value={suggestion.value}
                      onSelect={() => {
                        if (isDirectory) {
                          setQuery(suggestion.value);
                        } else {
                          commit(suggestion.value);
                        }
                      }}
                      className="text-xs"
                    >
                      {isDirectory ? (
                        <Folder className="mr-2 size-3 text-muted-foreground" />
                      ) : (
                        <FileText className="mr-2 size-3 text-muted-foreground" />
                      )}
                      <span className="flex-1 truncate">{suggestion.value}</span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// InferColumnsButton mirrors the source asset's declared columns into this asset
// (definition-driven, the same model SQL assets use).
function InferColumnsButton({ asset }: { asset: WebAsset }) {
  const [state, setState] = useState<"idle" | "loading" | "error">("idle");
  const [message, setMessage] = useState<string | null>(null);

  const run = async () => {
    setState("loading");
    setMessage(null);
    try {
      await refreshAssetColumnsFromDefinition(asset.id);
      setState("idle");
    } catch (cause) {
      setState("error");
      setMessage(cause instanceof Error ? cause.message : "Could not infer columns.");
    }
  };

  return (
    <div className="mt-0.5">
      <button
        type="button"
        onClick={() => void run()}
        disabled={state === "loading"}
        className="font-monaco flex items-center gap-1 rounded-sm px-1 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
      >
        {state === "loading" ? (
          <Loader2 className="size-3 animate-spin" />
        ) : (
          <Database className="size-3" />
        )}
        infer columns from source
      </button>
      {message ? <Comment>{message}</Comment> : null}
    </div>
  );
}
