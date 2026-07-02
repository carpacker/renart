"use client";

import { useEffect, useMemo, useState } from "react";

import { useAtomValue } from "jotai";
import { Boxes, Check, Database, Loader2, Plus, X } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { applyAssetTransaction, reconcileAssetColumns, refreshAssetColumnsFromDefinition } from "@/lib/api-asset-transactions";
import { updateAsset } from "@/lib/api-assets";
import { updateAssetColumns } from "@/lib/api-assets-columns";
import { getSQLTableColumns } from "@/lib/api-sql-discovery";
import { classifyDependencies, parseAssetProvenance } from "@/lib/asset-provenance";
import { NON_SQL_ASSET_TYPES, SQL_ASSET_TYPES } from "@/lib/asset-types";
import { selectedEnvironmentAtom, workspaceAtom } from "@/lib/atoms/workspace";
import { WebAsset, WebColumn } from "@/lib/types";

import {
  COLUMN_CHECK_NAMES,
  MATERIALIZATION_OPTIONS,
  VALUE_CHECKS,
  checkValueFor,
  currentMaterializationOption,
  formatCheckValue,
} from "./asset-guided-cards";

/**
 * An interactive, YAML-shaped view of an asset's configurable metadata — an
 * alternative to the focused cards (§15, structured rather than a raw textarea).
 * Every value is an inline widget: text where free-form, a dropdown where the
 * value set is constrained, and YAML-list rows with an add affordance for
 * collections (tags, dependencies, checks). Labels and empty states render as
 * `#` comments. It drives the same asset API + transactions as the cards, so the
 * two stay in sync through the workspace SSE stream.
 */
export function AssetYamlEditor({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const isSql = useMemo(
    () => asset.path?.toLowerCase().endsWith(".sql") ?? asset.type.toLowerCase().includes("sql"),
    [asset.path, asset.type]
  );

  return (
    <ScrollArea className="min-h-0 flex-1 bg-background">
      <div className="font-monaco p-3 text-[13px] leading-6">
        <IdentitySection asset={asset} pipelineId={pipelineId} />
        <MaterializationSection asset={asset} pipelineId={pipelineId} isSql={isSql} />
        <DependsSection asset={asset} />
        <ColumnsSection asset={asset} isSql={isSql} />
      </div>
    </ScrollArea>
  );
}

// --- YAML primitives ---

export function Line({ depth = 0, children, className }: { depth?: number; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex items-center gap-1.5", className)} style={{ paddingLeft: depth * 14 }}>
      {children}
    </div>
  );
}

export function Key({ children }: { children: React.ReactNode }) {
  return <span className="shrink-0 text-sky-700 dark:text-sky-300">{children}:</span>;
}

function Dash() {
  return <span className="shrink-0 text-muted-foreground">-</span>;
}

export function Comment({ depth = 0, children }: { depth?: number; children: React.ReactNode }) {
  return (
    <div className="flex" style={{ paddingLeft: depth * 14 }}>
      <span className="italic text-emerald-700/80 dark:text-emerald-400/70"># {children}</span>
    </div>
  );
}

// InlineText reads as a plain YAML value but edits on commit (blur / Enter).
export function InlineText({
  value,
  placeholder,
  onCommit,
}: {
  value: string;
  placeholder?: string;
  onCommit: (next: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  return (
    <input
      className="font-monaco min-w-0 flex-1 rounded-sm bg-transparent px-1 text-foreground outline-none ring-offset-background placeholder:text-muted-foreground/60 hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring"
      value={draft}
      placeholder={placeholder}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={() => onCommit(draft)}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.currentTarget.blur();
        } else if (event.key === "Escape") {
          setDraft(value);
          event.currentTarget.blur();
        }
      }}
    />
  );
}

export function InlineSelect({
  value,
  options,
  onChange,
  placeholder,
}: {
  value: string;
  options: { value: string; label: string }[];
  onChange: (next: string) => void;
  placeholder?: string;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="font-monaco h-6 w-auto gap-1 border-none bg-muted/40 px-1.5 text-xs hover:bg-muted/70 focus:ring-1">
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value} className="font-monaco text-xs">
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// AddItem renders a `- ` list row whose value is a small input committed on
// Enter or via the add button.
function AddItem({ depth, placeholder, onAdd }: { depth: number; placeholder: string; onAdd: (value: string) => void }) {
  const [value, setValue] = useState("");
  const commit = () => {
    const trimmed = value.trim();
    if (!trimmed) return;
    onAdd(trimmed);
    setValue("");
  };
  return (
    <Line depth={depth}>
      <Dash />
      <input
        className="font-monaco min-w-0 flex-1 rounded-sm bg-transparent px-1 text-foreground outline-none placeholder:text-muted-foreground/60 hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring"
        value={value}
        placeholder={placeholder}
        onChange={(event) => setValue(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter") commit();
        }}
      />
      <button
        type="button"
        className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40"
        aria-label="Add"
        disabled={!value.trim()}
        onClick={commit}
      >
        <Plus className="size-3" />
      </button>
    </Line>
  );
}

export function RemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
      aria-label={label}
      onClick={onClick}
    >
      <X className="size-3" />
    </button>
  );
}

// --- Sections ---

function IdentitySection({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const assetTypes = useMemo(
    () => Array.from(new Set([...SQL_ASSET_TYPES, ...NON_SQL_ASSET_TYPES, asset.type])).sort(),
    [asset.type]
  );
  const tags = asset.tags ?? [];

  const setTags = (next: string[]) => {
    void updateAsset(pipelineId, asset.id, { tags: next });
  };

  return (
    <>
      <Line>
        <Key>name</Key>
        <InlineText
          value={asset.name}
          placeholder="analytics.orders"
          onCommit={(name) => {
            if (name.trim() && name.trim() !== asset.name) void updateAsset(pipelineId, asset.id, { name: name.trim() });
          }}
        />
      </Line>
      <Line>
        <Key>type</Key>
        <InlineSelect
          value={asset.type}
          options={assetTypes.map((type) => ({ value: type, label: type }))}
          onChange={(type) => {
            if (type && type !== asset.type) void updateAsset(pipelineId, asset.id, { type });
          }}
        />
      </Line>
      <Line>
        <Key>owner</Key>
        <InlineText
          value={asset.owner ?? ""}
          placeholder="team@company.com"
          onCommit={(owner) => {
            if (owner !== (asset.owner ?? "")) void updateAsset(pipelineId, asset.id, { owner });
          }}
        />
      </Line>
      <Line>
        <Key>tags</Key>
      </Line>
      {tags.map((tag) => (
        <Line key={tag} depth={1}>
          <Dash />
          <span className="flex-1 text-foreground">{tag}</span>
          <RemoveButton label={`Remove tag ${tag}`} onClick={() => setTags(tags.filter((t) => t !== tag))} />
        </Line>
      ))}
      <AddItem
        depth={1}
        placeholder="add tag"
        onAdd={(tag) => {
          if (!tags.includes(tag)) setTags([...tags, tag]);
        }}
      />
    </>
  );
}

function MaterializationSection({ asset, pipelineId, isSql }: { asset: WebAsset; pipelineId: string; isSql: boolean }) {
  const selected = currentMaterializationOption(asset);
  // Only SQL assets can materialize as a view; everything else (api/sling/ingestr)
  // shares the same table/append/merge/incremental strategies.
  const options = isSql ? MATERIALIZATION_OPTIONS : MATERIALIZATION_OPTIONS.filter((option) => option.value !== "view");
  return (
    <>
      <Line className="mt-1">
        <Key>materialization</Key>
      </Line>
      <Line depth={1}>
        <Key>type</Key>
        <InlineSelect
          value={selected.value}
          options={options.map((option) => ({ value: option.value, label: option.label }))}
          onChange={(value) => {
            const option = MATERIALIZATION_OPTIONS.find((o) => o.value === value);
            if (!option) return;
            void updateAsset(pipelineId, asset.id, {
              materialization_type: option.type,
              materialization_strategy: option.strategy,
            });
          }}
        />
      </Line>
      {selected.value === "incremental" ? (
        <Line depth={1}>
          <Key>incremental_key</Key>
          <InlineText
            value={asset.incremental_key ?? ""}
            placeholder="loaded_at"
            onCommit={(key) => {
              if (key !== (asset.incremental_key ?? "")) void updateAsset(pipelineId, asset.id, { incremental_key: key });
            }}
          />
        </Line>
      ) : null}
    </>
  );
}

function DependsSection({ asset }: { asset: WebAsset }) {
  const { inferred, manual, ignored } = useMemo(() => classifyDependencies(asset), [asset]);
  const apply = (tx: Parameters<typeof applyAssetTransaction>[1]) => {
    void applyAssetTransaction(asset.id, tx);
  };
  const presentNames = useMemo(
    () => new Set([...inferred, ...manual].map((dep) => dep.name.toLowerCase())),
    [inferred, manual]
  );
  const hasAny = inferred.length > 0 || manual.length > 0;

  return (
    <>
      <Line className="mt-1">
        <Key>depends</Key>
      </Line>
      {!hasAny ? <Comment depth={1}>none yet — add a dependency below or pick from existing assets</Comment> : null}
      {inferred.length > 0 ? <Comment depth={1}>inferred from SQL</Comment> : null}
      {inferred.map((dep) => (
        <Line key={dep.key} depth={1}>
          <Dash />
          <span className="flex-1 text-foreground">{dep.name}</span>
          <button
            type="button"
            className="shrink-0 rounded-sm px-1 text-[10px] text-muted-foreground hover:bg-muted hover:text-foreground"
            onClick={() => apply({ type: "dependency.inferred.ignore", dependency_key: dep.key })}
          >
            ignore
          </button>
        </Line>
      ))}
      {manual.length > 0 ? <Comment depth={1}>manual</Comment> : null}
      {manual.map((dep) => (
        <Line key={dep.key} depth={1}>
          <Dash />
          <span className="flex-1 text-foreground">
            {dep.name}
            {dep.mode === "symbolic" ? <span className="ml-1 text-muted-foreground">(symbolic)</span> : null}
          </span>
          <RemoveButton label={`Remove dependency ${dep.name}`} onClick={() => apply({ type: "dependency.manual.remove", dependency_key: dep.key })} />
        </Line>
      ))}
      {ignored.length > 0 ? <Comment depth={1}>ignored — restore to let inference manage them again</Comment> : null}
      {ignored.map((dep) => (
        <div key={dep.key} className="flex items-center gap-1.5" style={{ paddingLeft: 14 }}>
          <span className="italic text-emerald-700/80 dark:text-emerald-400/70"># - {dep.value}</span>
          <button
            type="button"
            className="shrink-0 rounded-sm px-1 text-[10px] text-muted-foreground hover:bg-muted hover:text-foreground"
            onClick={() => apply({ type: "dependency.inferred.restore", dependency_key: dep.key })}
          >
            restore
          </button>
        </div>
      ))}
      <AddItem depth={1} placeholder="add dependency (asset name)" onAdd={(name) => apply({ type: "dependency.manual.add", dependency: { asset: name } })} />
      <AssetDependencyPicker
        assetId={asset.id}
        present={presentNames}
        onPick={(name) => apply({ type: "dependency.manual.add", dependency: { asset: name } })}
      />
    </>
  );
}

// Proposes the workspace's existing assets as dependency candidates (for SQL and
// non-SQL assets alike) — picking one attaches it as a manual dependency.
function AssetDependencyPicker({
  assetId,
  present,
  onPick,
}: {
  assetId: string;
  present: Set<string>;
  onPick: (name: string) => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const [open, setOpen] = useState(false);

  const candidates = useMemo(() => {
    const seen = new Set<string>();
    const out: { name: string; type: string }[] = [];
    for (const pipeline of workspace?.pipelines ?? []) {
      for (const candidate of pipeline.assets) {
        if (candidate.id === assetId) continue;
        const lower = candidate.name.toLowerCase();
        if (seen.has(lower)) continue;
        seen.add(lower);
        out.push({ name: candidate.name, type: candidate.type });
      }
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }, [workspace, assetId]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="font-monaco ml-[14px] mt-0.5 flex items-center gap-1 rounded-sm px-1 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <Boxes className="size-3" />
          pick from existing assets…
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <Command>
          <CommandInput placeholder="Search assets…" className="text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-xs">No assets found.</CommandEmpty>
            <CommandGroup>
              {candidates.map((candidate) => {
                const added = present.has(candidate.name.toLowerCase());
                return (
                  <CommandItem
                    key={candidate.name}
                    value={candidate.name}
                    disabled={added}
                    onSelect={() => {
                      onPick(candidate.name);
                      setOpen(false);
                    }}
                    className="text-xs"
                  >
                    <Boxes className="mr-2 size-3 text-muted-foreground" />
                    <span className="flex-1 truncate">{candidate.name}</span>
                    {added ? (
                      <Check className="size-3 text-muted-foreground" />
                    ) : (
                      <span className="text-[10px] text-muted-foreground">{candidate.type}</span>
                    )}
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function ColumnsSection({ asset, isSql }: { asset: WebAsset; isSql: boolean }) {
  const columns = asset.columns ?? [];
  const apply = (tx: Parameters<typeof applyAssetTransaction>[1]) => {
    void applyAssetTransaction(asset.id, tx);
  };
  const setColumnType = (name: string, type: string) => {
    const next: WebColumn[] = columns.map((column) => (column.name === name ? { ...column, type } : column));
    void updateAssetColumns(asset.id, next);
  };

  const existingNames = useMemo(() => new Set(columns.map((column) => column.name.toLowerCase())), [columns]);
  // Columns the user has dropped/ignored — shown commented-out so they can be restored.
  const dropped = useMemo(() => {
    const present = new Set(columns.map((column) => column.name.toLowerCase()));
    return [...parseAssetProvenance(asset.meta).colDrop].filter((name) => !present.has(name)).sort();
  }, [asset.meta, columns]);

  return (
    <>
      <Line className="mt-1">
        <Key>columns</Key>
      </Line>
      {columns.length === 0 ? <Comment depth={1}>none yet — add one below{isSql ? " or refresh from the SQL definition" : " or import from the warehouse"}</Comment> : null}
      {columns.map((column) => (
        <ColumnEntry
          key={column.name}
          column={column}
          onSetType={setColumnType}
          apply={apply}
          onRemove={() => apply({ type: "column.inferred.drop", column: column.name })}
        />
      ))}
      {dropped.length > 0 ? <Comment depth={1}>ignored — restore to bring back</Comment> : null}
      {dropped.map((name) => (
        <div key={name} className="flex items-center gap-1.5" style={{ paddingLeft: 14 }}>
          <span className="italic text-emerald-700/80 dark:text-emerald-400/70"># - name: {name}</span>
          <button
            type="button"
            className="shrink-0 rounded-sm px-1 text-[10px] text-muted-foreground hover:bg-muted hover:text-foreground"
            onClick={() => apply({ type: "column.inferred.restore", column: name })}
          >
            restore
          </button>
        </div>
      ))}
      <AddItem
        depth={1}
        placeholder="add column"
        onAdd={(name) => {
          if (!existingNames.has(name.toLowerCase())) apply({ type: "column.manual.add", column_def: { name } });
        }}
      />
      {!isSql ? <ImportColumnsButton asset={asset} /> : null}
    </>
  );
}

// Non-SQL assets have no SELECT to infer columns from, so this reads the column
// schema of the asset's table straight from the warehouse and reconciles it in.
function ImportColumnsButton({ asset }: { asset: WebAsset }) {
  const environment = useAtomValue(selectedEnvironmentAtom);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const connection = asset.connection;
  const table = asset.materialized_as || asset.name;
  const isAPIAsset = asset.type.toLowerCase() === "api";

  if (!connection && !isAPIAsset) {
    return <Comment depth={1}>no connection set — can&apos;t import columns from the warehouse</Comment>;
  }

  const run = () => {
    setLoading(true);
    setError(null);
    if (isAPIAsset) {
      refreshAssetColumnsFromDefinition(asset.id)
        .catch((err) => setError(err instanceof Error ? err.message : "Failed to infer columns from OpenAPI"))
        .finally(() => setLoading(false));
      return;
    }
    if (!connection) {
      setError("No connection set for warehouse column import");
      setLoading(false);
      return;
    }
    getSQLTableColumns({ connection, table, environment })
      .then(async (response) => {
        if (response.error) {
          setError(response.error);
          return;
        }
        const inferred: WebColumn[] = (response.columns ?? []).map((column) => ({ name: column.name, type: column.type }));
        if (inferred.length === 0) {
          setError(`No columns found for ${table}`);
          return;
        }
        await reconcileAssetColumns(asset.id, inferred);
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to import columns"))
      .finally(() => setLoading(false));
  };

  return (
    <>
      <Line depth={1}>
        <button
          type="button"
          disabled={loading}
          onClick={run}
          className="font-monaco flex items-center gap-1 rounded-sm px-1 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
        >
          {loading ? <Loader2 className="size-3 animate-spin" /> : <Database className="size-3" />}
          {isAPIAsset ? "infer columns from OpenAPI" : "import columns from warehouse"}
        </button>
      </Line>
      {error ? <Comment depth={1}>{error}</Comment> : null}
    </>
  );
}

function ColumnEntry({
  column,
  onSetType,
  apply,
  onRemove,
}: {
  column: WebColumn;
  onSetType: (name: string, type: string) => void;
  apply: (tx: Parameters<typeof applyAssetTransaction>[1]) => void;
  onRemove: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [checkName, setCheckName] = useState<string>(COLUMN_CHECK_NAMES[0]);
  const [checkValue, setCheckValue] = useState("");
  const checks = column.checks ?? [];

  const addCheck = () => {
    const value = checkValueFor(checkName, checkValue);
    const check: { name: string; value?: unknown } = { name: checkName };
    if (value !== undefined) check.value = value;
    apply({ type: "column.check.add", column: column.name, check });
    setCheckValue("");
    setAdding(false);
  };

  return (
    <>
      <Line depth={1}>
        <Dash />
        <Key>name</Key>
        <span className="flex-1 text-foreground">{column.name}</span>
        <RemoveButton label={`Remove column ${column.name}`} onClick={onRemove} />
      </Line>
      <Line depth={3}>
        <Key>type</Key>
        <InlineText value={column.type ?? ""} placeholder="VARCHAR" onCommit={(type) => { if (type !== (column.type ?? "")) onSetType(column.name, type); }} />
      </Line>
      <Line depth={3}>
        <Key>checks</Key>
      </Line>
      {checks.map((check, index) => (
        <Line key={`${check.name}-${index}`} depth={4}>
          <Dash />
          <span className="flex-1 text-foreground">{check.name}{formatCheckValue(check.value)}</span>
          <RemoveButton label={`Remove ${check.name} from ${column.name}`} onClick={() => apply({ type: "column.check.remove", column: column.name, check: { name: check.name } })} />
        </Line>
      ))}
      {adding ? (
        <Line depth={4}>
          <Dash />
          <InlineSelect
            value={checkName}
            options={COLUMN_CHECK_NAMES.map((name) => ({ value: name, label: name }))}
            onChange={setCheckName}
          />
          {VALUE_CHECKS.has(checkName) ? (
            <input
              autoFocus
              className="font-monaco w-24 min-w-0 rounded-sm bg-transparent px-1 text-foreground outline-none placeholder:text-muted-foreground/60 hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring"
              value={checkValue}
              placeholder={checkName === "accepted_values" ? "a, b, c" : checkName === "pattern" ? "regex" : "number"}
              onChange={(event) => setCheckValue(event.target.value)}
              onKeyDown={(event) => { if (event.key === "Enter") addCheck(); if (event.key === "Escape") setAdding(false); }}
            />
          ) : null}
          <button
            type="button"
            className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label={`Confirm check on ${column.name}`}
            onClick={addCheck}
          >
            <Check className="size-3" />
          </button>
          <RemoveButton label="Cancel add check" onClick={() => { setAdding(false); setCheckValue(""); }} />
        </Line>
      ) : (
        <Line depth={4}>
          <button
            type="button"
            className="font-monaco flex items-center gap-1 rounded-sm px-1 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
            onClick={() => setAdding(true)}
          >
            <Plus className="size-3" />
            add check…
          </button>
        </Line>
      )}
    </>
  );
}
