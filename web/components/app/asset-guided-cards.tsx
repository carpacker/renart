"use client";

import { useEffect, useId, useMemo, useState } from "react";

import { useAtomValue } from "jotai";
import {
  Ban,
  Check,
  ChevronsUpDown,
  Database,
  KeyRound,
  Plus,
  RefreshCw,
  RotateCcw,
  Trash2,
  X,
} from "lucide-react";

import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import { selectedEnvironmentAtom } from "@/lib/atoms/workspace";

import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AssetReconcileItem,
  applyAssetTransaction,
  reconcileAssetColumns,
  refreshAssetColumnsFromDefinition,
  refreshAssetColumnsFromMaterializedOutput,
} from "@/lib/api-asset-transactions";
import { updateAsset, updateAssetColumns } from "@/lib/api-assets";
import { inferAPIAsset } from "@/lib/api-assets-columns";
import type { APIInferResult, MaterializationCapability } from "@/lib/generated/api-types";
import { classifyDependencies, columnStatus, parseAssetProvenance } from "@/lib/asset-provenance";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  NON_SQL_ASSET_TYPES,
  SEED_ASSET_TYPES,
  SQL_ASSET_TYPES,
  getAssetAuthoringCapability,
  getAssetColumnRefreshMode,
  groupAssetTypesByKind,
  isSeedAssetType,
  isSensorAssetType,
  isSqlAssetType,
} from "@/lib/asset-types";
import { useIngestrEnabled } from "@/lib/features";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { LOCAL_LOAD_CONNECTION, loadConnectionsForEnvironment } from "@/lib/load-assets";
import { cn } from "@/lib/utils";
import { WebAsset, WebColumn } from "@/lib/types";
import { MultiValueInput } from "./multi-value-input";

/**
 * Guided metadata cards for the app asset editor (§13–14 of the asset
 * editing concept). Renders focused, editable sections beside the SQL editor so
 * users edit asset intent without touching raw YAML; every edit flows through
 * the asset API, and the workspace SSE stream refreshes the asset prop.
 */
export function AssetGuidedCards({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const supportsColumns = getAssetColumnRefreshMode(asset.type, asset.parameters) !== "none";
  return (
    <ScrollArea className="min-h-0 w-full flex-1">
      <div className="divide-y px-3">
        <IdentityCard asset={asset} pipelineId={pipelineId} />
        <MaterializationCard asset={asset} pipelineId={pipelineId} />
        <DependenciesCard asset={asset} />
        {supportsColumns ? <ColumnsCard asset={asset} /> : null}
        {supportsColumns ? <QualityChecksCard asset={asset} /> : null}
      </div>
    </ScrollArea>
  );
}

/**
 * A borderless section: an eyebrow title and its controls, separated from the
 * next section by a hairline divider (the parent's divide-y) rather than a card,
 * so a stack of sections reads as one calm form instead of nested boxes.
 */
function GuidedCard({
  title,
  action,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-2.5 py-4">
      <div className="flex min-h-5 items-center justify-between gap-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
          {title}
        </h3>
        {action}
      </div>
      {children}
    </section>
  );
}

// --- Identity card (§14.1) ---

// Sentinel Select value for "no explicit connection" — an empty SelectItem value
// is disallowed, and clearing the field lets the asset fall back to the
// pipeline's default connection.
const AUTO_CONNECTION_VALUE = "__auto__";

/**
 * Target connection picker for API, Python, and Load assets. The connection is a top-level
 * `connection:` key (not part of the request `parameters` spec), so it belongs in
 * the guided editor — especially once raw editing is scoped to `parameters`.
 * Leaving it on "Auto" omits the key and lets the asset use the pipeline default.
 */
function ConnectionField({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const fieldId = `${useId()}-connection`;
  const workspace = useAtomValue(workspaceAtom);
  const environment = useAtomValue(selectedEnvironmentAtom);
  const { workspaceConfig } = useWorkspaceSettingsData();
  const current = (asset.explicit_connection ?? "").trim();
  const effective = (asset.connection ?? "").trim();
  const isLoad = asset.type.trim().toLowerCase() === "load";
  const capability = getAssetAuthoringCapability(asset.type, workspace?.asset_capabilities);
  const connectionNames = useMemo(() => {
    const names = isLoad
      ? loadConnectionsForEnvironment(workspaceConfig, environment).map(
          (connection) => connection.name,
        )
      : capability
        ? Object.entries(workspace?.connections ?? {})
            .filter(([, type]) => capability.connection_types.includes(type))
            .map(([name]) => name)
        : Object.keys(workspace?.connections ?? {});
    if (isLoad && !names.includes(LOCAL_LOAD_CONNECTION)) {
      names.push(LOCAL_LOAD_CONNECTION);
    }
    // Keep an explicitly-set connection selectable even if it isn't (yet) in the
    // active environment's connection list.
    if (current && !names.includes(current)) {
      names.push(current);
    }
    return names.sort((a, b) => a.localeCompare(b));
  }, [workspace?.connections, workspaceConfig, environment, current, isLoad, capability]);

  return (
    <FieldRow label="Connection" htmlFor={fieldId}>
      <Select
        value={current || AUTO_CONNECTION_VALUE}
        onValueChange={(value) => {
          const next = value === AUTO_CONNECTION_VALUE ? "" : value;
          if (next !== current) void updateAsset(pipelineId, asset.id, { connection: next });
        }}
      >
        <SelectTrigger id={fieldId} className="h-8">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value={AUTO_CONNECTION_VALUE}>
              {effective ? `Auto (${effective})` : "Auto (pipeline default)"}
            </SelectItem>
            {connectionNames.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </FieldRow>
  );
}

function IdentityCard({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const fieldIdPrefix = `${useId()}-identity`;
  const fieldId = (name: string) => `${fieldIdPrefix}-${name}`;
  const ingestrEnabled = useIngestrEnabled();
  const workspace = useAtomValue(workspaceAtom);
  const normalizedType = asset.type.trim().toLowerCase();
  const hasTargetConnection =
    normalizedType === "api" ||
    normalizedType === "load" ||
    normalizedType.includes("python") ||
    isSeedAssetType(normalizedType) ||
    isSensorAssetType(normalizedType);
  const assetTypeGroups = useMemo(
    () =>
      groupAssetTypesByKind(
        Array.from(
          new Set([
            ...SQL_ASSET_TYPES,
            ...SEED_ASSET_TYPES,
            ...NON_SQL_ASSET_TYPES,
            ...(workspace?.asset_capabilities ?? []).map((capability) => capability.type),
            asset.type,
          ]),
        ).filter(
          // A broken/half-typed YAML asset can parse to an empty type; a Select
          // item must never have an empty value, so drop it.
          (type) => Boolean(type) && (ingestrEnabled || type !== "ingestr" || type === asset.type),
        ),
      ),
    [asset.type, ingestrEnabled, workspace?.asset_capabilities],
  );

  const updateMetaDescription = (description: string) => {
    const nextMeta = { ...(asset.meta ?? {}) };
    if (description.trim()) {
      nextMeta.description = description.trim();
    } else {
      delete nextMeta.description;
    }
    void updateAsset(pipelineId, asset.id, { meta: nextMeta });
  };

  return (
    <GuidedCard title="Identity">
      <FieldGroup>
        <FieldRow label="Name" htmlFor={fieldId("name")}>
          <CommitInput
            id={fieldId("name")}
            mono
            value={asset.name}
            placeholder="analytics.orders"
            onCommit={(name) => {
              if (name.trim() && name.trim() !== asset.name) {
                void updateAsset(pipelineId, asset.id, { name: name.trim() });
              }
            }}
          />
        </FieldRow>
        <FieldRow label="Type" htmlFor={fieldId("type")}>
          <Select
            value={asset.type}
            onValueChange={(type) => {
              if (type && type !== asset.type) void updateAsset(pipelineId, asset.id, { type });
            }}
          >
            <SelectTrigger id={fieldId("type")} className="h-8">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectLabel>SQL assets</SelectLabel>
                {assetTypeGroups.sql.map((type) => (
                  <SelectItem key={type} value={type}>
                    {type}
                  </SelectItem>
                ))}
              </SelectGroup>
              <SelectGroup>
                <SelectLabel>Non-SQL assets</SelectLabel>
                {assetTypeGroups.nonSql.map((type) => (
                  <SelectItem key={type} value={type}>
                    {type}
                  </SelectItem>
                ))}
              </SelectGroup>
              {assetTypeGroups.seed.length > 0 ? (
                <SelectGroup>
                  <SelectLabel>Seeds</SelectLabel>
                  {assetTypeGroups.seed.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type}
                    </SelectItem>
                  ))}
                </SelectGroup>
              ) : null}
              {assetTypeGroups.sensor.length > 0 ? (
                <SelectGroup>
                  <SelectLabel>Sensors</SelectLabel>
                  {assetTypeGroups.sensor.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type}
                    </SelectItem>
                  ))}
                </SelectGroup>
              ) : null}
            </SelectContent>
          </Select>
        </FieldRow>
        {hasTargetConnection ? <ConnectionField asset={asset} pipelineId={pipelineId} /> : null}
        <FieldRow label="Owner" htmlFor={fieldId("owner")}>
          <CommitInput
            id={fieldId("owner")}
            value={asset.owner ?? ""}
            placeholder="team@company.com"
            onCommit={(owner) => {
              if (owner !== (asset.owner ?? "")) void updateAsset(pipelineId, asset.id, { owner });
            }}
          />
        </FieldRow>
        <FieldRow label="Description" htmlFor={fieldId("description")}>
          <CommitInput
            id={fieldId("description")}
            value={asset.meta?.description ?? ""}
            placeholder="What this asset produces"
            onCommit={(description) => {
              if (description !== (asset.meta?.description ?? "")) {
                updateMetaDescription(description);
              }
            }}
          />
        </FieldRow>
        <FieldRow label="Tags" htmlFor={fieldId("tags")}>
          <MultiValueInput
            id={fieldId("tags")}
            value={asset.tags ?? []}
            placeholder="Add tag"
            onChange={(tags) => {
              if (tags.join("\n") !== (asset.tags ?? []).join("\n")) {
                void updateAsset(pipelineId, asset.id, { tags });
              }
            }}
          />
        </FieldRow>
      </FieldGroup>
    </GuidedCard>
  );
}

// --- Materialization card (§14.2) ---

export type MaterializationOption = {
  value: string;
  label: string;
  type: string;
  strategy: string;
  capability?: MaterializationCapability;
  custom?: boolean;
};

export const MATERIALIZATION_OPTIONS: MaterializationOption[] = [
  { value: "none", label: "None (run only)", type: "", strategy: "" },
  { value: "view", label: "View", type: "view", strategy: "" },
  {
    value: "create+replace",
    label: "Table (replace)",
    type: "table",
    strategy: "create+replace",
  },
  {
    value: "truncate+insert",
    label: "Table (truncate)",
    type: "table",
    strategy: "truncate+insert",
  },
  { value: "append", label: "Append rows", type: "table", strategy: "append" },
  { value: "merge", label: "Merge by key", type: "table", strategy: "merge" },
  {
    value: "delete+insert",
    label: "Replace matching keys",
    type: "table",
    strategy: "delete+insert",
  },
  {
    value: "time_interval",
    label: "Incremental (time interval)",
    type: "table",
    strategy: "time_interval",
  },
];

function materializationOptionForCapability(
  capability: MaterializationCapability,
): MaterializationOption {
  const known = MATERIALIZATION_OPTIONS.find((option) => option.value === capability.mode);
  return {
    value: capability.mode,
    label: known?.label ?? capability.mode,
    type: capability.type,
    strategy: capability.strategy,
    capability,
  };
}

function currentMaterializationMode(asset: WebAsset) {
  const type = (asset.materialization_type ?? "").toLowerCase();
  const strategy = (asset.materialization_strategy ?? "").toLowerCase();
  if (!type) {
    return (asset.materialization_capabilities ?? []).some((item) => item.mode === "none")
      ? "none"
      : "create+replace";
  }
  if (type === "view") return "view";
  if (type === "table" && !strategy) return "create+replace";
  if (["create+replace", "create_replace", "full-refresh", "full_refresh"].includes(strategy)) {
    return "create+replace";
  }
  if (["truncate+insert", "truncate_insert", "truncate"].includes(strategy)) {
    return "truncate+insert";
  }
  if (strategy === "delete_insert") return "delete+insert";
  return strategy;
}

export function currentMaterializationOption(asset: WebAsset): MaterializationOption {
  const mode = currentMaterializationMode(asset);
  const capability = (asset.materialization_capabilities ?? []).find((item) => item.mode === mode);
  if (capability) return materializationOptionForCapability(capability);

  const type = (asset.materialization_type ?? "").trim();
  const strategy = (asset.materialization_strategy ?? "").trim();
  const detail = strategy || type || mode;
  return {
    value: `custom:${type}:${strategy || mode}`,
    label: `Custom (${detail})`,
    type,
    strategy,
    custom: true,
  };
}

export function materializationEditorState(asset: WebAsset) {
  const options = (asset.materialization_capabilities ?? []).map(
    materializationOptionForCapability,
  );
  const selected = currentMaterializationOption(asset);
  const hasDeclaredMaterialization = Boolean(
    (asset.materialization_type ?? "").trim() || (asset.materialization_strategy ?? "").trim(),
  );
  if (selected.custom && (options.length > 0 || hasDeclaredMaterialization)) {
    options.unshift(selected);
  }
  return {
    selected,
    selectedValue: selected.value,
    options,
    hasEditor: options.length > 0,
  };
}

export function inferMaterializationTimeGranularity(asset: WebAsset, incrementalKey?: string) {
  const key = (incrementalKey ?? asset.incremental_key ?? "").trim().toLowerCase();
  const columnType = (asset.columns ?? [])
    .find((column) => column.name.trim().toLowerCase() === key)
    ?.type?.trim()
    .toLowerCase();
  return columnType?.replace(/\(.*/, "").trim() === "date" ? "date" : "timestamp";
}

export function materializationSelectionInput(asset: WebAsset, option: MaterializationOption) {
  const timeGranularity =
    asset.time_granularity ||
    ((asset.incremental_key ?? "").trim()
      ? inferMaterializationTimeGranularity(asset, asset.incremental_key)
      : "");
  return {
    materialization_type: option.type,
    materialization_strategy: option.strategy,
    ...(option.capability?.requires_time_granularity && timeGranularity
      ? { time_granularity: timeGranularity }
      : {}),
  };
}

export function ColumnCombobox({
  id,
  columns,
  value,
  placeholder,
  className,
  onChange,
}: {
  id?: string;
  columns: WebColumn[];
  value: string;
  placeholder: string;
  className?: string;
  onChange: (value: string) => void;
}) {
  const items = columns.map((column) => column.name).filter(Boolean);
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn("w-full min-w-0 justify-between font-monaco font-normal", className)}
        >
          <span className="truncate">
            {value || (items.length === 0 ? "Add or infer columns first" : placeholder)}
          </span>
          <ChevronsUpDown data-icon="inline-end" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[var(--radix-popover-trigger-width)] p-0">
        <Command>
          <CommandInput placeholder="Search columns…" />
          <CommandList>
            <CommandEmpty>
              {items.length === 0
                ? "No declared columns. Add or infer columns first."
                : "No matching column."}
            </CommandEmpty>
            <CommandGroup>
              {value ? (
                <CommandItem
                  value="__clear_update_key__"
                  onSelect={() => {
                    onChange("");
                    setOpen(false);
                  }}
                >
                  No update key
                </CommandItem>
              ) : null}
              {items.map((item) => (
                <CommandItem
                  key={item}
                  value={item}
                  onSelect={() => {
                    onChange(item);
                    setOpen(false);
                  }}
                >
                  <Check className={cn(value === item ? "opacity-100" : "opacity-0")} />
                  {item}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function MaterializationCard({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const fieldIdPrefix = `${useId()}-materialization`;
  const fieldId = (name: string) => `${fieldIdPrefix}-${name}`;
  const { selected, selectedValue, options, hasEditor } = materializationEditorState(asset);
  const primaryKeys = (asset.columns ?? [])
    .filter((column) => column.primary_key)
    .map((column) => column.name);
  const [error, setError] = useState("");

  const save = (input: Parameters<typeof updateAsset>[2]) => {
    setError("");
    void updateAsset(pipelineId, asset.id, input).catch((cause) => {
      setError(cause instanceof Error ? cause.message : "Could not update materialization");
    });
  };

  if (!hasEditor) return null;

  return (
    <GuidedCard title="Materialization">
      <FieldGroup>
        <FieldRow label="Write behavior" htmlFor={fieldId("write-behavior")}>
          <Select
            value={selectedValue}
            onValueChange={(value) => {
              const option = options.find((item) => item.value === value);
              if (!option || option.custom) return;
              save(materializationSelectionInput(asset, option));
            }}
          >
            <SelectTrigger id={fieldId("write-behavior")} className="h-8">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {options.map((option) => (
                  <SelectItem key={option.value} value={option.value} disabled={option.custom}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </FieldRow>
        {selected.capability?.requires_incremental_key ||
        selected.capability?.supports_incremental_key ? (
          <FieldRow
            htmlFor={fieldId("incremental-key")}
            label={
              selected.capability?.requires_incremental_key
                ? "Incremental key"
                : "Update key (optional)"
            }
          >
            <ColumnCombobox
              id={fieldId("incremental-key")}
              columns={asset.columns ?? []}
              value={asset.incremental_key ?? ""}
              placeholder={
                selected.capability?.requires_incremental_key ? "loaded_at" : "updated_at"
              }
              onChange={(key) => {
                if (key !== (asset.incremental_key ?? "")) {
                  save({
                    incremental_key: key,
                    ...(selected.capability?.requires_time_granularity && !asset.time_granularity
                      ? { time_granularity: inferMaterializationTimeGranularity(asset, key) }
                      : {}),
                  });
                }
              }}
            />
          </FieldRow>
        ) : null}
        {selected.capability?.requires_time_granularity ? (
          <FieldRow label="Time granularity" htmlFor={fieldId("time-granularity")}>
            <Select
              value={asset.time_granularity ?? ""}
              onValueChange={(timeGranularity) => save({ time_granularity: timeGranularity })}
            >
              <SelectTrigger id={fieldId("time-granularity")} className="h-8">
                <SelectValue placeholder="Select date or timestamp" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="timestamp">Timestamp</SelectItem>
                  <SelectItem value="date">Date</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </FieldRow>
        ) : null}
        {selected.capability?.supports_partition_by ? (
          <FieldRow label="Partition by" htmlFor={fieldId("partition-by")}>
            <CommitInput
              id={fieldId("partition-by")}
              mono
              value={asset.partition_by ?? ""}
              placeholder="event_date"
              onCommit={(partitionBy) => {
                if (partitionBy !== (asset.partition_by ?? "")) {
                  save({ partition_by: partitionBy });
                }
              }}
            />
          </FieldRow>
        ) : null}
        {selected.capability?.supports_cluster_by ? (
          <FieldRow label="Cluster by" htmlFor={fieldId("cluster-by")}>
            <MultiValueInput
              id={fieldId("cluster-by")}
              value={asset.cluster_by ?? []}
              placeholder="Add column or expression"
              onChange={(clusterBy) => {
                if (clusterBy.join("\n") !== (asset.cluster_by ?? []).join("\n")) {
                  save({ cluster_by: clusterBy });
                }
              }}
            />
          </FieldRow>
        ) : null}
        {selected.capability?.requires_primary_key ? (
          <p
            className={cn(
              "text-[11px]",
              primaryKeys.length === 0 ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {primaryKeys.length === 0
              ? `${selected.value === "merge" ? "Merge" : "This mode"} needs at least one primary-key column. Set one with the key control in Columns.`
              : `Primary key${primaryKeys.length === 1 ? "" : "s"}: ${primaryKeys.join(", ")}`}
          </p>
        ) : null}
        {error ? <p className="text-[11px] text-destructive">{error}</p> : null}
      </FieldGroup>
    </GuidedCard>
  );
}

// --- Dependencies card (§14.3) ---

function DependenciesCard({ asset }: { asset: WebAsset }) {
  const { inferred, manual, ignored } = useMemo(() => classifyDependencies(asset), [asset]);
  const [adding, setAdding] = useState("");

  const apply = (tx: Parameters<typeof applyAssetTransaction>[1]) => {
    void applyAssetTransaction(asset.id, tx);
  };

  const addDependency = () => {
    if (!adding.trim()) return;
    apply({
      type: "dependency.manual.add",
      dependency: { asset: adding.trim() },
    });
    setAdding("");
  };

  const isEmpty = inferred.length === 0 && manual.length === 0 && ignored.length === 0;

  return (
    <GuidedCard title="Dependencies">
      {isEmpty ? <p className="text-[11px] text-muted-foreground">No dependencies yet.</p> : null}

      {inferred.length > 0 ? (
        <DepSection label="Inferred from SQL">
          {inferred.map((dep) => (
            <DepRow
              key={dep.key}
              name={dep.name}
              actionLabel="Ignore"
              actionIcon={<Ban className="size-3" />}
              onAction={() =>
                apply({
                  type: "dependency.inferred.ignore",
                  dependency_key: dep.key,
                })
              }
            />
          ))}
        </DepSection>
      ) : null}

      {manual.length > 0 ? (
        <DepSection label="Manual">
          {manual.map((dep) => (
            <DepRow
              key={dep.key}
              name={dep.name}
              badge={dep.mode === "symbolic" ? "symbolic" : undefined}
              actionLabel="Remove"
              actionIcon={<Trash2 className="size-3" />}
              onAction={() =>
                apply({
                  type: "dependency.manual.remove",
                  dependency_key: dep.key,
                })
              }
            />
          ))}
        </DepSection>
      ) : null}

      {ignored.length > 0 ? (
        <DepSection label="Ignored">
          {ignored.map((dep) => (
            <DepRow
              key={dep.key}
              name={dep.value}
              muted
              actionLabel="Restore"
              actionIcon={<RotateCcw className="size-3" />}
              onAction={() =>
                apply({
                  type: "dependency.inferred.restore",
                  dependency_key: dep.key,
                })
              }
            />
          ))}
        </DepSection>
      ) : null}

      <div className="flex items-center gap-1.5">
        <Input
          className="h-7 text-xs"
          placeholder="Add dependency (asset name)"
          value={adding}
          onChange={(e) => setAdding(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") addDependency();
          }}
        />
        <Button variant="outline" size="xs" disabled={!adding.trim()} onClick={addDependency}>
          <Plus className="size-3" />
        </Button>
      </div>
    </GuidedCard>
  );
}

function DepSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-0.5">
      <div className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground/70">
        {label}
      </div>
      <div className="divide-y rounded-md border">{children}</div>
    </div>
  );
}

function DepRow({
  name,
  badge,
  muted,
  actionLabel,
  actionIcon,
  onAction,
}: {
  name: string;
  badge?: string;
  muted?: boolean;
  actionLabel: string;
  actionIcon: React.ReactNode;
  onAction: () => void;
}) {
  return (
    <div className="group flex items-center gap-1.5 px-2 py-1 text-xs">
      <span
        className={cn(
          "min-w-0 flex-1 truncate font-monaco",
          muted && "text-muted-foreground line-through",
        )}
      >
        {name}
      </span>
      {badge ? (
        <span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">{badge}</span>
      ) : null}
      <Button
        variant="ghost"
        size="xs"
        className="size-6 shrink-0 p-0 text-muted-foreground opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
        title={actionLabel}
        aria-label={actionLabel}
        onClick={onAction}
      >
        {actionIcon}
      </Button>
    </div>
  );
}

// --- Columns card (§14.4) ---

function ColumnsCard({ asset }: { asset: WebAsset }) {
  const environment = useAtomValue(selectedEnvironmentAtom);
  const refreshMode = getAssetColumnRefreshMode(asset.type, asset.parameters);
  const isAPI = refreshMode === "api";
  const importsMaterializedOutput = refreshMode === "materialized";
  const isSQLMerge =
    isSqlAssetType(asset.type) && asset.materialization_strategy?.toLowerCase() === "merge";
  const provenance = useMemo(() => parseAssetProvenance(asset.meta), [asset.meta]);
  const columns = asset.columns ?? [];
  // Columns the user has ignored (renart_col_drop) that aren't currently present.
  const ignored = useMemo(() => {
    const present = new Set(columns.map((column) => column.name.toLowerCase()));
    return [...provenance.colDrop].filter((name) => !present.has(name)).sort();
  }, [provenance, columns]);
  const [reconcileItems, setReconcileItems] = useState<AssetReconcileItem[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [apiSample, setAPISample] = useState<APIInferResult | null>(null);

  const refreshFromDefinition = async () => {
    setRefreshing(true);
    setError(null);
    try {
      if (isAPI) {
        setAPISample(await inferAPIAsset(asset.id));
        return;
      }
      const result = importsMaterializedOutput
        ? await refreshAssetColumnsFromMaterializedOutput(asset, environment)
        : await refreshAssetColumnsFromDefinition(asset.id);
      setReconcileItems(result.reconcile_items ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to refresh columns");
    } finally {
      setRefreshing(false);
    }
  };

  const setDescription = (column: string, description: string) => {
    void applyAssetTransaction(asset.id, {
      type: "column.description.set",
      column,
      description,
    });
  };

  const dropColumn = (column: string) => {
    void applyAssetTransaction(asset.id, {
      type: "column.inferred.drop",
      column,
    });
    setReconcileItems((items) =>
      items.filter((i) => i.column.toLowerCase() !== column.toLowerCase()),
    );
  };

  const keepAsManual = (column: string, def: WebColumn) => {
    void applyAssetTransaction(asset.id, {
      type: "column.manual.add",
      column_def: def,
    });
    setReconcileItems((items) =>
      items.filter((i) => i.column.toLowerCase() !== column.toLowerCase()),
    );
  };

  const commitType = (column: WebColumn, nextType: string) => {
    if (nextType === (column.type ?? "")) return;
    const nextColumns = columns.map((c) =>
      c.name.toLowerCase() === column.name.toLowerCase() ? { ...c, type: nextType } : c,
    );
    void updateAssetColumns(asset.id, nextColumns).then(() =>
      applyAssetTransaction(asset.id, {
        type: "column.field.own",
        column: column.name,
        field: "type",
      }),
    );
  };

  // primary_key counts as user metadata (columnHasUserMetadata on the server),
  // so a plain columns update survives refresh-from-definition merges.
  const togglePrimaryKey = (column: WebColumn) => {
    const nextColumns = columns.map((c) =>
      c.name.toLowerCase() === column.name.toLowerCase()
        ? { ...c, primary_key: !c.primary_key }
        : c,
    );
    void updateAssetColumns(asset.id, nextColumns);
  };

  const toggleUpdateOnMerge = (column: WebColumn) => {
    const nextColumns = columns.map((c) =>
      c.name.toLowerCase() === column.name.toLowerCase()
        ? { ...c, update_on_merge: !c.update_on_merge }
        : c,
    );
    void updateAssetColumns(asset.id, nextColumns);
  };

  const commitMergeSQL = (column: WebColumn, mergeSQL: string) => {
    if (mergeSQL === (column.merge_sql ?? "")) return;
    const nextColumns = columns.map((c) =>
      c.name.toLowerCase() === column.name.toLowerCase() ? { ...c, merge_sql: mergeSQL } : c,
    );
    void updateAssetColumns(asset.id, nextColumns);
  };

  return (
    <GuidedCard
      title="Columns"
      action={
        <Button
          variant="outline"
          size="xs"
          disabled={refreshing}
          onClick={refreshFromDefinition}
          title={
            isAPI
              ? "Fetch one response page and infer its record shape"
              : importsMaterializedOutput
                ? "Import columns from the materialized output"
                : isSeedAssetType(asset.type)
                  ? "Infer columns from the seed file with Sling"
                  : "Derive columns from the definition and upstream assets"
          }
        >
          {refreshing ? (
            <Spinner data-icon="inline-start" />
          ) : isAPI || importsMaterializedOutput ? (
            <Database data-icon="inline-start" />
          ) : (
            <RefreshCw data-icon="inline-start" />
          )}
          {isAPI ? "Test response" : importsMaterializedOutput ? "Import" : "Refresh"}
        </Button>
      }
    >
      {error ? <p className="text-[11px] text-destructive">{error}</p> : null}
      {apiSample ? (
        <div className="flex flex-col gap-1.5 rounded-md border bg-muted/30 p-2 text-[11px]">
          <p>
            Found {apiSample.records_count} records and {apiSample.columns.length} columns.
          </p>
          {apiSample.warnings.map((warning) => (
            <p key={warning} className="text-destructive">
              {warning}
            </p>
          ))}
          {apiSample.columns.length > 0 ? (
            <Button
              variant="outline"
              size="xs"
              className="self-start"
              onClick={() => {
                void reconcileAssetColumns(asset.id, apiSample.columns).then((result) => {
                  setReconcileItems(result.reconcile_items ?? []);
                  setAPISample(null);
                });
              }}
            >
              Apply inferred columns
            </Button>
          ) : null}
        </div>
      ) : null}

      {reconcileItems.map((item) => {
        const def = columns.find((c) => c.name.toLowerCase() === item.column.toLowerCase());
        return (
          <div
            key={item.column}
            className="rounded-md border border-amber-300 bg-amber-50 p-2 text-[11px] dark:border-amber-500/40 dark:bg-amber-950/30"
          >
            <div className="font-monaco font-medium text-amber-900 dark:text-amber-200">
              {item.column}
            </div>
            <div className="mb-1.5 text-amber-700 dark:text-amber-300">{item.detail}</div>
            <div className="flex gap-1.5">
              {def ? (
                <Button variant="outline" size="xs" onClick={() => keepAsManual(item.column, def)}>
                  Keep
                </Button>
              ) : null}
              <Button variant="outline" size="xs" onClick={() => dropColumn(item.column)}>
                Remove
              </Button>
            </div>
          </div>
        );
      })}

      {columns.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">
          No columns. Refresh to infer them from the definition.
        </p>
      ) : (
        <div className="divide-y rounded-md border">
          {columns.map((column) => (
            <ColumnRow
              key={column.name}
              column={column}
              status={columnStatus(column.name, provenance)}
              onCommitType={(type) => commitType(column, type)}
              onCommitDescription={(description) => setDescription(column.name, description)}
              onTogglePrimaryKey={() => togglePrimaryKey(column)}
              showMergeFields={isSQLMerge}
              onToggleUpdateOnMerge={() => toggleUpdateOnMerge(column)}
              onCommitMergeSQL={(mergeSQL) => commitMergeSQL(column, mergeSQL)}
              onDrop={() => dropColumn(column.name)}
            />
          ))}
        </div>
      )}

      {ignored.length > 0 ? (
        <DepSection label="Ignored">
          {ignored.map((name) => (
            <DepRow
              key={name}
              name={name}
              muted
              actionLabel="Restore"
              actionIcon={<RotateCcw className="size-3" />}
              onAction={() =>
                applyAssetTransaction(asset.id, {
                  type: "column.inferred.restore",
                  column: name,
                })
              }
            />
          ))}
        </DepSection>
      ) : null}
    </GuidedCard>
  );
}

// --- Quality checks card (§14.5) ---

// The standard Bruin column checks. Value-bearing ones take an argument
// (accepted_values a list, min/max a number, pattern a regex); the rest are
// presence assertions.
export const COLUMN_CHECK_NAMES = [
  "not_null",
  "unique",
  "positive",
  "non_negative",
  "negative",
  "accepted_values",
  "pattern",
  "min",
  "max",
] as const;
export const VALUE_CHECKS = new Set(["accepted_values", "pattern", "min", "max"]);

export function checkValueFor(checkName: string, raw: string): unknown {
  if (checkName === "accepted_values") {
    return raw
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean);
  }
  if (checkName === "min" || checkName === "max") {
    const parsed = Number(raw);
    return Number.isNaN(parsed) ? undefined : parsed;
  }
  if (checkName === "pattern") {
    return raw.trim() || undefined;
  }
  return undefined;
}

export function formatCheckValue(value: unknown): string {
  if (value === undefined || value === null || value === "") return "";
  if (Array.isArray(value)) return `: ${value.join(", ")}`;
  return `: ${String(value)}`;
}

function QualityChecksCard({ asset }: { asset: WebAsset }) {
  const columns = asset.columns ?? [];
  const columnsWithChecks = columns.filter((column) => (column.checks?.length ?? 0) > 0);
  const [column, setColumn] = useState("");
  const [checkName, setCheckName] = useState<string>(COLUMN_CHECK_NAMES[0]);
  const [value, setValue] = useState("");

  const apply = (tx: Parameters<typeof applyAssetTransaction>[1]) => {
    void applyAssetTransaction(asset.id, tx);
  };

  const addCheck = () => {
    if (!column) return;
    const checkValue = checkValueFor(checkName, value);
    const check: { name: string; value?: unknown } = { name: checkName };
    if (checkValue !== undefined) check.value = checkValue;
    apply({ type: "column.check.add", column, check });
    setValue("");
  };

  if (columns.length === 0) {
    return (
      <GuidedCard title="Quality checks">
        <p className="text-[11px] text-muted-foreground">Add columns first to attach checks.</p>
      </GuidedCard>
    );
  }

  return (
    <GuidedCard title="Quality checks">
      {columnsWithChecks.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">No checks yet.</p>
      ) : null}
      {columnsWithChecks.map((col) => (
        <div key={col.name} className="space-y-1">
          <div className="font-monaco text-[11px] text-foreground">{col.name}</div>
          <div className="flex flex-wrap gap-1">
            {(col.checks ?? []).map((check, index) => (
              <span
                key={`${check.name}-${index}`}
                className="inline-flex items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-[10px]"
              >
                {check.name}
                {formatCheckValue(check.value)}
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground"
                  aria-label={`Remove ${check.name} from ${col.name}`}
                  onClick={() =>
                    apply({
                      type: "column.check.remove",
                      column: col.name,
                      check: { name: check.name },
                    })
                  }
                >
                  <X className="size-2.5" />
                </button>
              </span>
            ))}
          </div>
        </div>
      ))}
      <div className="space-y-1.5">
        <div className="flex items-center gap-1.5">
          <Select value={column} onValueChange={setColumn}>
            <SelectTrigger className="h-7 text-xs">
              <SelectValue placeholder="Column" />
            </SelectTrigger>
            <SelectContent>
              {columns
                .filter((col) => col.name)
                .map((col) => (
                  <SelectItem key={col.name} value={col.name}>
                    {col.name}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
          <Select value={checkName} onValueChange={setCheckName}>
            <SelectTrigger className="h-7 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {COLUMN_CHECK_NAMES.map((name) => (
                <SelectItem key={name} value={name}>
                  {name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {VALUE_CHECKS.has(checkName) ? (
          <Input
            className="h-7 text-xs"
            placeholder={
              checkName === "accepted_values"
                ? "a, b, c"
                : checkName === "pattern"
                  ? "regex"
                  : "number"
            }
            value={value}
            onChange={(event) => setValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") addCheck();
            }}
          />
        ) : null}
        <Button variant="outline" size="xs" disabled={!column} onClick={addCheck}>
          <Plus className="size-3" />
          Add check
        </Button>
      </div>
    </GuidedCard>
  );
}

function ColumnRow({
  column,
  status,
  onCommitType,
  onCommitDescription,
  onTogglePrimaryKey,
  showMergeFields,
  onToggleUpdateOnMerge,
  onCommitMergeSQL,
  onDrop,
}: {
  column: WebColumn;
  status: ReturnType<typeof columnStatus>;
  onCommitType: (type: string) => void;
  onCommitDescription: (description: string) => void;
  onTogglePrimaryKey: () => void;
  showMergeFields: boolean;
  onToggleUpdateOnMerge: () => void;
  onCommitMergeSQL: (mergeSQL: string) => void;
  onDrop: () => void;
}) {
  return (
    <div className="group px-2.5 py-2">
      <div className="flex items-center gap-1.5">
        <span className="min-w-0 flex-1 truncate font-monaco text-xs">{column.name}</span>
        <ColumnStatusBadge status={status} primaryKey={column.primary_key} />
        <Button
          variant="ghost"
          size="xs"
          className={cn(
            "size-6 shrink-0 p-0",
            column.primary_key
              ? "text-amber-600 dark:text-amber-400"
              : "text-muted-foreground opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
          )}
          title={column.primary_key ? "Unset primary key" : "Set as primary key"}
          aria-label={`${column.primary_key ? "Unset" : "Set"} ${column.name} as primary key`}
          onClick={onTogglePrimaryKey}
        >
          <KeyRound className="size-3" />
        </Button>
        <Button
          variant="ghost"
          size="xs"
          className="size-6 shrink-0 p-0 text-muted-foreground opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
          title="Remove column"
          aria-label={`Remove ${column.name}`}
          onClick={onDrop}
        >
          <Trash2 className="size-3" />
        </Button>
      </div>
      <div className="mt-1 flex items-center gap-1.5">
        <CommitInput
          mono
          value={column.type ?? ""}
          placeholder="type"
          onCommit={onCommitType}
          className="h-7 w-28 shrink-0"
        />
        <CommitInput
          value={column.description ?? ""}
          placeholder="describe this column"
          onCommit={onCommitDescription}
          className="h-7 flex-1"
        />
      </div>
      {showMergeFields ? (
        <div className="mt-1.5 flex min-w-0 items-center gap-1.5">
          <Button
            variant={column.update_on_merge ? "secondary" : "outline"}
            size="xs"
            title="Update this column when a primary-key match is found"
            aria-label={`${column.update_on_merge ? "Do not update" : "Update"} ${column.name} on merge`}
            aria-pressed={Boolean(column.update_on_merge)}
            onClick={onToggleUpdateOnMerge}
          >
            <RefreshCw data-icon="inline-start" />
            Update on merge
          </Button>
          <CommitInput
            mono
            value={column.merge_sql ?? ""}
            placeholder="merge SQL (optional)"
            onCommit={onCommitMergeSQL}
            className="h-7 min-w-0 flex-1"
          />
        </div>
      ) : null}
    </div>
  );
}

function ColumnStatusBadge({
  status,
  primaryKey,
}: {
  status: ReturnType<typeof columnStatus>;
  primaryKey?: boolean;
}) {
  const styles: Record<string, string> = {
    inferred: "bg-muted text-muted-foreground",
    manual: "bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
    "type-owned": "bg-purple-100 text-purple-700 dark:bg-purple-950 dark:text-purple-300",
  };
  return (
    <span className="flex items-center gap-1">
      {primaryKey ? (
        <span className="rounded bg-amber-100 px-1 text-[10px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
          pk
        </span>
      ) : null}
      <span className={cn("rounded px-1 text-[10px]", styles[status] ?? styles.inferred)}>
        {status}
      </span>
    </span>
  );
}

// --- shared field primitives ---

function FieldRow({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor?: string;
  children: React.ReactNode;
}) {
  return (
    <Field>
      <FieldLabel htmlFor={htmlFor}>{label}</FieldLabel>
      {children}
    </Field>
  );
}

/**
 * An input that holds local edits and commits on blur or Enter, so saves don't
 * fire on every keystroke.
 */
function CommitInput({
  id,
  value,
  placeholder,
  onCommit,
  mono,
  className,
}: {
  id?: string;
  value: string;
  placeholder?: string;
  onCommit: (value: string) => void;
  mono?: boolean;
  className?: string;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);

  return (
    <Input
      id={id}
      className={cn("h-8 text-xs", mono && "font-monaco", className)}
      value={draft}
      placeholder={placeholder}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => onCommit(draft)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.currentTarget.blur();
        }
      }}
    />
  );
}
