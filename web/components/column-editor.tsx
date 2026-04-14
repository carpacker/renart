"use client";

import { useSetAtom } from "jotai";
import { Plus, Sparkles, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { registerAssetColumnsAtom } from "@/lib/atoms/domains/suggestions";
import { inferAssetColumns, updateAssetColumns } from "@/lib/api";
import { WebColumn, WebColumnCheck } from "@/lib/types";

type Props = {
  assetId: string;
  initialColumns: WebColumn[];
  onSaved?: (columns: WebColumn[]) => void;
};

export function ColumnEditor({ assetId, initialColumns, onSaved }: Props) {
  const registerAssetColumns = useSetAtom(registerAssetColumnsAtom);
  const [columns, setColumns] = useState<WebColumn[]>(() => initialColumns ?? []);
  const [saving, setSaving] = useState(false);
  const [inferring, setInferring] = useState(false);

  const summary = useMemo(() => {
    const checkCount = columns.reduce(
      (acc, column) => acc + (column.checks?.length ?? 0),
      0,
    );

    return `${columns.length} columns · ${checkCount} checks`;
  }, [columns]);

  const addColumn = () => {
    setColumns((prev) => [
      ...prev,
      {
        name: "",
        type: "",
        description: "",
        nullable: true,
        checks: [],
      },
    ]);
  };

  const removeColumn = (index: number) => {
    setColumns((prev) => prev.filter((_, idx) => idx !== index));
  };

  const updateColumn = (index: number, next: Partial<WebColumn>) => {
    setColumns((prev) =>
      prev.map((column, idx) => (idx === index ? { ...column, ...next } : column)),
    );
  };

  const addCheck = (columnIndex: number) => {
    const checks = [...(columns[columnIndex]?.checks ?? [])];
    checks.push({ name: "" });
    updateColumn(columnIndex, { checks });
  };

  const updateCheck = (
    columnIndex: number,
    checkIndex: number,
    next: Partial<WebColumnCheck>,
  ) => {
    const checks = [...(columns[columnIndex]?.checks ?? [])];
    checks[checkIndex] = { ...checks[checkIndex], ...next };
    updateColumn(columnIndex, { checks });
  };

  const removeCheck = (columnIndex: number, checkIndex: number) => {
    const checks = [...(columns[columnIndex]?.checks ?? [])].filter(
      (_, idx) => idx !== checkIndex,
    );
    updateColumn(columnIndex, { checks });
  };

  const handleInfer = () => {
    setInferring(true);
    void inferAssetColumns(assetId)
      .then((result) => {
        registerAssetColumns({
          assetId,
          method: "asset-column-inference",
          columns: (result.columns ?? []).map((column) => ({
            name: column.name,
            type: column.type,
            description: column.description,
            primaryKey: column.primary_key,
          })),
        });

        if (result.columns.length > 0) {
          setColumns((prev) => mergeInferredColumns(prev, result.columns));
        }
      })
      .finally(() => setInferring(false));
  };

  const handleSave = () => {
    setSaving(true);
    const sanitized = sanitizeColumns(columns);

    void updateAssetColumns(assetId, sanitized)
      .then(() => {
        onSaved?.(sanitized);
      })
      .finally(() => setSaving(false));
  };

  return (
    <div className="flex h-full min-h-0 flex-col border-l">
      <div className="border-b px-4 py-3">
        <div className="text-sm font-semibold">Column Editor</div>
        <div className="text-xs text-muted-foreground">{summary}</div>
        <div className="mt-3 flex gap-2">
          <Button onClick={addColumn} size="sm" type="button" variant="outline">
            <Plus className="mr-1 size-3" />
            Add Column
          </Button>
          <Button
            disabled={inferring}
            onClick={handleInfer}
            size="sm"
            type="button"
            variant="outline"
          >
            <Sparkles className="mr-1 size-3" />
            {inferring ? "Inferring..." : "Infer Types"}
          </Button>
          <Button disabled={saving} onClick={handleSave} size="sm" type="button">
            {saving ? "Saving..." : "Save Columns"}
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        <div className="grid gap-4">
          {columns.map((column, index) => (
            <div className="rounded-lg border bg-muted/20 p-3" key={`${index}-${column.name}`}>
              <div className="mb-3 flex items-center justify-between">
                <div className="text-xs font-medium text-muted-foreground">
                  Column #{index + 1}
                </div>
                <Button
                  onClick={() => removeColumn(index)}
                  size="icon-sm"
                  type="button"
                  variant="ghost"
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>

              <div className="grid gap-3">
                <div className="flex items-start justify-between gap-2">
                  <InlineEditableField
                    className="text-base font-semibold text-foreground"
                    emptyLabel="Click to set column name"
                    label="Name"
                    onChange={(value) => updateColumn(index, { name: value })}
                    value={column.name ?? ""}
                  />

                  <InlineEditableField
                    className="rounded-md bg-primary/10 px-2 py-1 text-xs font-medium text-primary"
                    emptyLabel="type"
                    label="Type"
                    onChange={(value) => updateColumn(index, { type: value })}
                    value={column.type ?? ""}
                  />
                </div>

                <InlineEditableArea
                  emptyLabel="Click to add description"
                  label="Description"
                  onChange={(value) => updateColumn(index, { description: value })}
                  value={column.description ?? ""}
                />

                <div className="flex flex-wrap gap-2">
                  <TogglePill
                    active={column.primary_key ?? false}
                    label="Primary Key"
                    onToggle={() =>
                      updateColumn(index, { primary_key: !(column.primary_key ?? false) })
                    }
                  />
                  <TogglePill
                    active={column.nullable ?? true}
                    label="Nullable"
                    onToggle={() => updateColumn(index, { nullable: !(column.nullable ?? true) })}
                  />
                  <TogglePill
                    active={column.update_on_merge ?? false}
                    label="Update on Merge"
                    onToggle={() =>
                      updateColumn(index, {
                        update_on_merge: !(column.update_on_merge ?? false),
                      })
                    }
                  />
                </div>

                <div className="grid gap-2 rounded border bg-background p-2">
                  <div className="flex items-center justify-between">
                    <Label>Checks</Label>
                    <Button
                      onClick={() => addCheck(index)}
                      size="xs"
                      type="button"
                      variant="outline"
                    >
                      Add Check
                    </Button>
                  </div>

                  {(column.checks ?? []).length === 0 && (
                    <div className="text-xs text-muted-foreground">No checks defined.</div>
                  )}

                  {(column.checks ?? []).map((check, checkIndex) => (
                    <div className="rounded border bg-muted/30 p-2" key={`${index}-${checkIndex}`}>
                      <div className="flex items-start justify-between gap-2">
                        <div className="grid min-w-0 flex-1 gap-1">
                          <InlineEditableField
                            className="text-sm font-medium"
                            emptyLabel="Check name"
                            label="Check name"
                            onChange={(value) =>
                              updateCheck(index, checkIndex, { name: value })
                            }
                            value={check.name ?? ""}
                          />
                          <InlineEditableField
                            className="font-mono text-xs text-muted-foreground"
                            emptyLabel="JSON value"
                            label="Check value"
                            onChange={(value) =>
                              updateCheck(index, checkIndex, {
                                value: parseMaybeJSON(value),
                              })
                            }
                            value={stringifyMaybeJSON(check.value)}
                          />
                        </div>

                        <div className="flex items-center gap-1">
                          <TogglePill
                            active={check.blocking ?? true}
                            label="Blocking"
                            onToggle={() =>
                              updateCheck(index, checkIndex, {
                                blocking: !(check.blocking ?? true),
                              })
                            }
                          />
                          <Button
                            onClick={() => removeCheck(index, checkIndex)}
                            size="icon-sm"
                            type="button"
                            variant="ghost"
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function InlineEditableField({
  value,
  label,
  emptyLabel,
  onChange,
  className,
}: {
  value: string;
  label: string;
  emptyLabel: string;
  onChange: (value: string) => void;
  className?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);

  if (editing) {
    return (
      <Input
        autoFocus
        aria-label={label}
        className="h-8"
        onBlur={() => {
          setEditing(false);
          onChange(draft.trim());
        }}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            setEditing(false);
            onChange(draft.trim());
          }

          if (event.key === "Escape") {
            setEditing(false);
            setDraft(value);
          }
        }}
        value={draft}
      />
    );
  }

  return (
    <button
      className={`w-fit rounded px-1 py-0.5 text-left transition-colors hover:bg-muted/80 ${className ?? ""}`}
      onClick={() => {
        setDraft(value);
        setEditing(true);
      }}
      title={`Edit ${label}`}
      type="button"
    >
      {value || <span className="text-muted-foreground">{emptyLabel}</span>}
    </button>
  );
}

function InlineEditableArea({
  value,
  label,
  emptyLabel,
  onChange,
}: {
  value: string;
  label: string;
  emptyLabel: string;
  onChange: (value: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);

  if (editing) {
    return (
      <Textarea
        autoFocus
        aria-label={label}
        className="min-h-16"
        onBlur={() => {
          setEditing(false);
          onChange(draft.trim());
        }}
        onChange={(event) => setDraft(event.target.value)}
        value={draft}
      />
    );
  }

  return (
    <button
      className="w-full rounded border border-dashed px-2 py-2 text-left text-sm transition-colors hover:bg-muted/70"
      onClick={() => {
        setDraft(value);
        setEditing(true);
      }}
      title={`Edit ${label}`}
      type="button"
    >
      {value || <span className="text-muted-foreground">{emptyLabel}</span>}
    </button>
  );
}

function TogglePill({
  label,
  active,
  onToggle,
}: {
  label: string;
  active: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      className={`rounded-md border px-2 py-1 text-xs transition-colors ${
        active
          ? "border-primary/40 bg-primary/10 text-primary"
          : "border-border bg-background text-muted-foreground"
      }`}
      onClick={onToggle}
      type="button"
    >
      {label}
    </button>
  );
}

function stringifyMaybeJSON(value: unknown) {
  if (value === undefined) {
    return "";
  }

  if (typeof value === "string") {
    return value;
  }

  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function parseMaybeJSON(value: string): unknown {
  const trimmed = value.trim();
  if (trimmed === "") {
    return undefined;
  }

  try {
    return JSON.parse(trimmed);
  } catch {
    return value;
  }
}

function sanitizeColumns(columns: WebColumn[]) {
  return columns
    .map((column) => ({
      ...column,
      name: (column.name ?? "").trim(),
      type: (column.type ?? "").trim(),
      description: (column.description ?? "").trim(),
      checks: (column.checks ?? []).filter((check) => (check.name ?? "").trim() !== ""),
    }))
    .filter((column) => column.name !== "");
}

function mergeInferredColumns(existing: WebColumn[], inferred: WebColumn[]) {
  const byName = new Map(existing.map((column) => [column.name.toLowerCase(), column]));
  const inferredNames = new Set(inferred.map((column) => column.name.toLowerCase()));

  const merged = inferred.map((inferredColumn) => {
    const current = byName.get(inferredColumn.name.toLowerCase());
    if (!current) {
      return inferredColumn;
    }

    return {
      ...current,
      type: inferredColumn.type || current.type,
    };
  });

  for (const current of existing) {
    if (!inferredNames.has(current.name.toLowerCase())) {
      merged.push(current);
    }
  }

  return merged;
}
