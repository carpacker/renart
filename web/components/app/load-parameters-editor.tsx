"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import { useAtomValue } from "jotai";
import { ArrowUpRight, Boxes, Check, Database, HardDrive, Loader2, Plug } from "lucide-react";

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
import { updateAsset } from "@/lib/api-assets";
import { discoverLoadStreams, LoadDiscoveryStream } from "@/lib/api-load";
import { selectedEnvironmentAtom, workspaceAtom } from "@/lib/atoms/workspace";
import { assetCreationRole } from "@/lib/asset-creation-profile";
import { useAssetCreationProfile } from "@/hooks/use-asset-creation-profile";
import { AssetCreationConnection, WebAsset } from "@/lib/types";
import { cn } from "@/lib/utils";
import { isLocalLoadConnection, loadTargetNeedsDestinationObject } from "@/lib/load-assets";

import { Comment, Key, Line } from "./asset-yaml-editor";
import { FilePathPicker } from "./file-path-picker";

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
 * sidebar; dependencies and columns are inferred from the source.
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
  const { profile } = useAssetCreationProfile(pipelineId);
  const params = asset.parameters ?? {};

  const sourceRole = assetCreationRole(profile, "load", "source");
  const destinationRole = assetCreationRole(profile, "load", "destination");
  const connections = useMemo(() => {
    const available = [...(sourceRole?.connections ?? [])];
    const current = (params.source_connection ?? "").trim();
    if (current && !available.some((connection) => connection.name === current)) {
      available.push({
        name: current,
        connection_type: workspace?.connections?.[current] ?? "unknown",
        candidates: [],
      });
    }
    return available;
  }, [params.source_connection, sourceRole?.connections, workspace?.connections]);
  const targetCategory = destinationRole?.connections.find(
    (connection) => connection.name === asset.connection,
  )?.category;
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
          <FilePathPicker
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
            <FilePathPicker
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
    </div>
  );
}

// ConnectionValue is a combobox showing the chosen configured connection; the picker
// is grouped by Load category (database / storage / file).
function ConnectionValue({
  value,
  connections,
  onPick,
}: {
  value: string;
  connections: AssetCreationConnection[];
  onPick: (name: string) => void;
}) {
  const [open, setOpen] = useState(false);

  const grouped = useMemo(() => {
    const byCategory = new Map<string, AssetCreationConnection[]>();
    for (const connection of connections) {
      const category = connection.category ?? "database";
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
                  const Icon = categoryIcon(connection.category);
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
                        <span className="text-[10px] text-muted-foreground">
                          {connection.connection_type}
                        </span>
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
