"use client";

import { useEffect, useMemo, useState } from "react";

import { Ban, Plus, RefreshCw, RotateCcw, Trash2, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AssetReconcileItem,
  applyAssetTransaction,
  refreshAssetColumnsFromDefinition,
} from "@/lib/api-asset-transactions";
import { updateAsset, updateAssetColumns } from "@/lib/api-assets";
import {
  classifyDependencies,
  columnStatus,
  parseAssetProvenance,
} from "@/lib/asset-provenance";
import { ScrollArea } from "@/components/ui/scroll-area";
import { NON_SQL_ASSET_TYPES, SQL_ASSET_TYPES } from "@/lib/asset-types";
import { cn } from "@/lib/utils";
import { WebAsset, WebColumn } from "@/lib/types";

/**
 * Guided metadata cards for the redesign asset editor (§13–14 of the asset
 * editing concept). Renders focused, editable sections beside the SQL editor so
 * users edit asset intent without touching raw YAML; every edit flows through
 * the asset API, and the workspace SSE stream refreshes the asset prop.
 */
export function AssetGuidedCards({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const isSql = useMemo(
    () => asset.path?.toLowerCase().endsWith(".sql") ?? asset.type.toLowerCase().includes("sql"),
    [asset.path, asset.type]
  );

  return (
    <ScrollArea className="min-h-0 w-full flex-1">
      <div className="space-y-3 p-3">
        <IdentityCard asset={asset} pipelineId={pipelineId} />
        <MaterializationCard asset={asset} pipelineId={pipelineId} isSql={isSql} />
        <DependenciesCard asset={asset} />
        <ColumnsCard asset={asset} />
        <QualityChecksCard asset={asset} />
      </div>
    </ScrollArea>
  );
}

/**
 * A single flat card. One border, an eyebrow title, and space for its
 * controls — no nested boxes, so a stack of cards reads calmly.
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
    <section className="space-y-2.5 rounded-lg border bg-card p-3">
      <div className="flex min-h-5 items-center justify-between gap-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

// --- Identity card (§14.1) ---

function IdentityCard({ asset, pipelineId }: { asset: WebAsset; pipelineId: string }) {
  const assetTypes = useMemo(
    () => Array.from(new Set([...SQL_ASSET_TYPES, ...NON_SQL_ASSET_TYPES, asset.type])).sort(),
    [asset.type]
  );

  return (
    <GuidedCard title="Identity">
      <FieldRow label="Name">
        <CommitInput
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
      <FieldRow label="Type">
        <Select
          value={asset.type}
          onValueChange={(type) => {
            if (type && type !== asset.type) void updateAsset(pipelineId, asset.id, { type });
          }}
        >
          <SelectTrigger className="h-8">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {assetTypes.map((type) => (
              <SelectItem key={type} value={type}>
                {type}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FieldRow>
      <FieldRow label="Owner">
        <CommitInput
          value={asset.owner ?? ""}
          placeholder="team@company.com"
          onCommit={(owner) => {
            if (owner !== (asset.owner ?? "")) void updateAsset(pipelineId, asset.id, { owner });
          }}
        />
      </FieldRow>
      <FieldRow label="Tags">
        <CommitInput
          value={(asset.tags ?? []).join(", ")}
          placeholder="finance, daily"
          onCommit={(raw) => {
            const tags = raw.split(",").map((t) => t.trim()).filter(Boolean);
            if (tags.join(",") !== (asset.tags ?? []).join(",")) {
              void updateAsset(pipelineId, asset.id, { tags });
            }
          }}
        />
      </FieldRow>
    </GuidedCard>
  );
}

// --- Materialization card (§14.2) ---

export type MaterializationOption = { value: string; label: string; type: string; strategy: string };

export const MATERIALIZATION_OPTIONS: MaterializationOption[] = [
  { value: "none", label: "None (run only)", type: "", strategy: "" },
  { value: "view", label: "View", type: "view", strategy: "" },
  { value: "table", label: "Table (replace)", type: "table", strategy: "create+replace" },
  { value: "append", label: "Append rows", type: "table", strategy: "append" },
  { value: "merge", label: "Merge by key", type: "table", strategy: "merge" },
  { value: "incremental", label: "Incremental (time interval)", type: "table", strategy: "time_interval" },
];

export function currentMaterializationOption(asset: WebAsset): MaterializationOption {
  const type = (asset.materialization_type ?? "").toLowerCase();
  const strategy = (asset.materialization_strategy ?? "").toLowerCase();
  if (!type) return MATERIALIZATION_OPTIONS[0];
  if (type === "view") return MATERIALIZATION_OPTIONS[1];
  const byStrategy = MATERIALIZATION_OPTIONS.find((o) => o.type === "table" && o.strategy === strategy);
  return byStrategy ?? MATERIALIZATION_OPTIONS[2];
}

function MaterializationCard({ asset, pipelineId, isSql }: { asset: WebAsset; pipelineId: string; isSql: boolean }) {
  const selected = currentMaterializationOption(asset);
  // Only SQL assets can materialize as a view.
  const options = isSql ? MATERIALIZATION_OPTIONS : MATERIALIZATION_OPTIONS.filter((option) => option.value !== "view");

  return (
    <GuidedCard title="Materialization">
      <FieldRow label="Write behavior">
        <Select
          value={selected.value}
          onValueChange={(value) => {
            const option = MATERIALIZATION_OPTIONS.find((o) => o.value === value);
            if (!option) return;
            void updateAsset(pipelineId, asset.id, {
              materialization_type: option.type,
              materialization_strategy: option.strategy,
            });
          }}
        >
          <SelectTrigger className="h-8">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FieldRow>
      {selected.value === "incremental" ? (
        <FieldRow label="Incremental key">
          <CommitInput
            mono
            value={asset.incremental_key ?? ""}
            placeholder="loaded_at"
            onCommit={(key) => {
              if (key !== (asset.incremental_key ?? "")) {
                void updateAsset(pipelineId, asset.id, { incremental_key: key });
              }
            }}
          />
        </FieldRow>
      ) : null}
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
    apply({ type: "dependency.manual.add", dependency: { asset: adding.trim() } });
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
              onAction={() => apply({ type: "dependency.inferred.ignore", dependency_key: dep.key })}
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
              onAction={() => apply({ type: "dependency.manual.remove", dependency_key: dep.key })}
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
              onAction={() => apply({ type: "dependency.inferred.restore", dependency_key: dep.key })}
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
      <div className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground/70">{label}</div>
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
      <span className={cn("min-w-0 flex-1 truncate font-monaco", muted && "text-muted-foreground line-through")}>
        {name}
      </span>
      {badge ? <span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">{badge}</span> : null}
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
  const provenance = useMemo(() => parseAssetProvenance(asset.meta), [asset.meta]);
  const columns = asset.columns ?? [];
  const [reconcileItems, setReconcileItems] = useState<AssetReconcileItem[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refreshFromDefinition = async () => {
    setRefreshing(true);
    setError(null);
    try {
      const result = await refreshAssetColumnsFromDefinition(asset.id);
      setReconcileItems(result.reconcile_items ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to refresh columns");
    } finally {
      setRefreshing(false);
    }
  };

  const setDescription = (column: string, description: string) => {
    void applyAssetTransaction(asset.id, { type: "column.description.set", column, description });
  };

  const dropColumn = (column: string) => {
    void applyAssetTransaction(asset.id, { type: "column.inferred.drop", column });
    setReconcileItems((items) => items.filter((i) => i.column.toLowerCase() !== column.toLowerCase()));
  };

  const keepAsManual = (column: string, def: WebColumn) => {
    void applyAssetTransaction(asset.id, { type: "column.manual.add", column_def: def });
    setReconcileItems((items) => items.filter((i) => i.column.toLowerCase() !== column.toLowerCase()));
  };

  const commitType = (column: WebColumn, nextType: string) => {
    if (nextType === (column.type ?? "")) return;
    const nextColumns = columns.map((c) =>
      c.name.toLowerCase() === column.name.toLowerCase() ? { ...c, type: nextType } : c
    );
    void updateAssetColumns(asset.id, nextColumns).then(() =>
      applyAssetTransaction(asset.id, { type: "column.field.own", column: column.name, field: "type" })
    );
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
          title="Derive columns from the SQL definition and upstream assets"
        >
          <RefreshCw className={cn("size-3", refreshing && "animate-spin")} />
          Refresh
        </Button>
      }
    >
      {error ? <p className="text-[11px] text-destructive">{error}</p> : null}

      {reconcileItems.map((item) => {
        const def = columns.find((c) => c.name.toLowerCase() === item.column.toLowerCase());
        return (
          <div key={item.column} className="rounded-md border border-amber-300 bg-amber-50 p-2 text-[11px] dark:border-amber-500/40 dark:bg-amber-950/30">
            <div className="font-monaco font-medium text-amber-900 dark:text-amber-200">{item.column}</div>
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
        <p className="text-[11px] text-muted-foreground">No columns. Refresh to infer them from the definition.</p>
      ) : (
        <div className="divide-y rounded-md border">
          {columns.map((column) => (
            <ColumnRow
              key={column.name}
              column={column}
              status={columnStatus(column.name, provenance)}
              onCommitType={(type) => commitType(column, type)}
              onCommitDescription={(description) => setDescription(column.name, description)}
              onDrop={() => dropColumn(column.name)}
            />
          ))}
        </div>
      )}
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
    return raw.split(",").map((part) => part.trim()).filter(Boolean);
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
                {check.name}{formatCheckValue(check.value)}
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground"
                  aria-label={`Remove ${check.name} from ${col.name}`}
                  onClick={() => apply({ type: "column.check.remove", column: col.name, check: { name: check.name } })}
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
            <SelectTrigger className="h-7 text-xs"><SelectValue placeholder="Column" /></SelectTrigger>
            <SelectContent>
              {columns.map((col) => (
                <SelectItem key={col.name} value={col.name}>{col.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={checkName} onValueChange={setCheckName}>
            <SelectTrigger className="h-7 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {COLUMN_CHECK_NAMES.map((name) => (
                <SelectItem key={name} value={name}>{name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {VALUE_CHECKS.has(checkName) ? (
          <Input
            className="h-7 text-xs"
            placeholder={checkName === "accepted_values" ? "a, b, c" : checkName === "pattern" ? "regex" : "number"}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            onKeyDown={(event) => { if (event.key === "Enter") addCheck(); }}
          />
        ) : null}
        <Button variant="outline" size="xs" disabled={!column} onClick={addCheck}>
          <Plus className="size-3" />Add check
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
  onDrop,
}: {
  column: WebColumn;
  status: ReturnType<typeof columnStatus>;
  onCommitType: (type: string) => void;
  onCommitDescription: (description: string) => void;
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
      <span className={cn("rounded px-1 text-[10px]", styles[status] ?? styles.inferred)}>{status}</span>
    </span>
  );
}

// --- shared field primitives ---

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[5.5rem_1fr] items-center gap-2">
      <Label className="text-[11px] text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

/**
 * An input that holds local edits and commits on blur or Enter, so saves don't
 * fire on every keystroke.
 */
function CommitInput({
  value,
  placeholder,
  onCommit,
  mono,
  className,
}: {
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
