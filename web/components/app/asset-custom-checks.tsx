"use client";

import type { Monaco } from "@monaco-editor/react";
import { useAtomValue } from "jotai";
import type * as MonacoNS from "monaco-editor";
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";

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
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useSQLLSP } from "@/hooks/use-sql-lsp";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { applyAssetTransaction } from "@/lib/api-asset-transactions";
import { selectedAssetSchemaTablesAtom } from "@/lib/atoms/domains/suggestions";
import { loadMonacoEditorModule } from "@/lib/load-monaco-editor";
import { defineBruinMonacoThemes } from "@/lib/monaco-theme";
import type { WebAsset, WebCustomCheck } from "@/lib/types";
import { cn } from "@/lib/utils";

const MonacoEditor = lazy(async () => {
  const module = await loadMonacoEditorModule();
  return { default: module.default };
});

type CheckDraft = {
  name: string;
  description: string;
  query: string;
  evaluation: "row_count" | "scalar";
  expected: string;
  blocking: boolean;
  retries?: number;
};

function draftFor(check: WebCustomCheck | undefined, assetName: string): CheckDraft {
  return {
    name: check?.name ?? "",
    description: check?.description ?? "",
    query: check?.query ?? `select *\nfrom ${assetName}\nwhere false`,
    evaluation: check ? (check.count === undefined ? "scalar" : "row_count") : "row_count",
    expected: String(check?.count ?? check?.value ?? 0),
    blocking: check?.blocking ?? true,
    retries: check?.retries,
  };
}

export function AssetCustomChecks({
  asset,
  focusedCheck,
}: {
  asset: WebAsset;
  focusedCheck?: { name: string; token: number } | null;
}) {
  const checks = asset.custom_checks ?? [];
  const [editing, setEditing] = useState<WebCustomCheck | null | undefined>(undefined);
  const [highlightedName, setHighlightedName] = useState("");
  const checkElements = useRef(new Map<string, HTMLDivElement>());

  useEffect(() => {
    const name = focusedCheck?.name.trim().toLowerCase();
    if (!name) return;
    const element = checkElements.current.get(name);
    if (!element) return;
    element.scrollIntoView({ behavior: "smooth", block: "center" });
    setHighlightedName(name);
    const timeout = window.setTimeout(() => setHighlightedName(""), 2200);
    return () => window.clearTimeout(timeout);
  }, [focusedCheck?.name, focusedCheck?.token]);

  const remove = (name: string) => {
    void applyAssetTransaction(asset.id, {
      type: "custom_check.remove",
      custom_check_name: name,
    });
  };

  return (
    <div data-testid="asset-custom-checks" className="space-y-2.5">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[11px] font-medium text-foreground">Custom SQL checks</p>
        <Button variant="outline" size="xs" onClick={() => setEditing(null)}>
          <Plus className="size-3" />
          Add
        </Button>
      </div>
      {checks.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">
          Run a SQL assertion after this asset is materialized.
        </p>
      ) : (
        <div className="space-y-2">
          {checks.map((check) => {
            const key = check.name.trim().toLowerCase();
            return (
              <div
                key={check.name}
                ref={(element) => {
                  if (element) checkElements.current.set(key, element);
                  else checkElements.current.delete(key);
                }}
                data-custom-check={check.name}
                data-highlighted={highlightedName === key ? "true" : undefined}
                className={cn(
                  "rounded-md border bg-muted/20 p-2.5 transition-[border-color,box-shadow,background-color] duration-500",
                  highlightedName === key &&
                    "border-destructive/70 bg-destructive/5 ring-2 ring-destructive/20",
                )}
              >
                <div className="flex min-w-0 items-start gap-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-xs font-medium">{check.name}</p>
                    <p className="mt-0.5 text-[10px] text-muted-foreground">
                      {check.count === undefined
                        ? `Scalar result = ${check.value}`
                        : `Rows returned = ${check.count}`}
                      {" · "}
                      {check.blocking === false ? "Non-blocking" : "Blocking"}
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Edit custom check ${check.name}`}
                    onClick={() => setEditing(check)}
                  >
                    <Pencil className="size-3" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Remove custom check ${check.name}`}
                    onClick={() => remove(check.name)}
                  >
                    <Trash2 className="size-3" />
                  </Button>
                </div>
                {check.description ? (
                  <p className="mt-1.5 line-clamp-2 text-[11px] text-muted-foreground">
                    {check.description}
                  </p>
                ) : null}
                <pre className="mt-2 max-h-16 overflow-hidden whitespace-pre-wrap rounded bg-background/70 px-2 py-1.5 font-mono text-[10px] text-muted-foreground">
                  {check.query}
                </pre>
              </div>
            );
          })}
        </div>
      )}
      <CustomCheckDialog
        asset={asset}
        check={editing}
        open={editing !== undefined}
        onOpenChange={(open) => {
          if (!open) setEditing(undefined);
        }}
      />
    </div>
  );
}

function CustomCheckDialog({
  asset,
  check,
  open,
  onOpenChange,
}: {
  asset: WebAsset;
  check: WebCustomCheck | null | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const schemaTables = useAtomValue(selectedAssetSchemaTablesAtom);
  const [draft, setDraft] = useState<CheckDraft>(() =>
    draftFor(check ?? undefined, asset.name),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [monacoInstance, setMonacoInstance] = useState<Monaco | null>(null);
  const [editorInstance, setEditorInstance] =
    useState<MonacoNS.editor.IStandaloneCodeEditor | null>(null);

  useSQLLSP(
    monacoInstance,
    editorInstance,
    asset,
    draft.query,
    schemaTables,
    undefined,
    undefined,
    {
      documentContext: "custom_check",
      allowNonSQLDocument: true,
    },
  );

  const handleBeforeMount = useCallback((monaco: Monaco) => {
    defineBruinMonacoThemes(monaco);
  }, []);
  const handleMount = useCallback(
    (editor: MonacoNS.editor.IStandaloneCodeEditor, monaco: Monaco) => {
      defineBruinMonacoThemes(monaco);
      setEditorInstance(editor);
      setMonacoInstance(monaco);
    },
    [],
  );

  useEffect(() => {
    if (!open) return;
    setDraft(draftFor(check ?? undefined, asset.name));
    setError("");
  }, [asset.name, check, open]);

  const expected = Number(draft.expected);
  const validExpected = Number.isSafeInteger(expected);
  const canSave = Boolean(draft.name.trim() && draft.query.trim() && validExpected && !saving);

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    setError("");
    const customCheck: WebCustomCheck = {
      name: draft.name.trim(),
      description: draft.description.trim() || undefined,
      value: draft.evaluation === "scalar" ? expected : 0,
      count: draft.evaluation === "row_count" ? expected : undefined,
      blocking: draft.blocking,
      query: draft.query.trim(),
      retries: draft.retries,
    };
    try {
      await applyAssetTransaction(asset.id, {
        type: "custom_check.upsert",
        custom_check_name: check?.name,
        custom_check: customCheck,
      });
      onOpenChange(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "The custom check could not be saved.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100vh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle>{check ? "Edit custom check" : "Add custom check"}</DialogTitle>
          <DialogDescription>
            Run a SQL assertion after <span className="font-mono">{asset.name}</span> is
            materialized.
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1">
          <FieldGroup className="gap-4 px-5 py-4">
            <Field>
              <FieldLabel htmlFor="custom-check-name">Name</FieldLabel>
              <Input
                id="custom-check-name"
                value={draft.name}
                placeholder="No duplicate rows"
                onChange={(event) =>
                  setDraft((current) => ({ ...current, name: event.target.value }))
                }
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="custom-check-description">Description</FieldLabel>
              <Input
                id="custom-check-description"
                value={draft.description}
                placeholder="What this assertion protects"
                onChange={(event) =>
                  setDraft((current) => ({ ...current, description: event.target.value }))
                }
              />
            </Field>
            <Field>
              <FieldLabel>SQL query</FieldLabel>
              <div className="h-56 overflow-hidden rounded-md border bg-background">
                <Suspense
                  fallback={
                    <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
                      Loading SQL editor...
                    </div>
                  }
                >
                  <MonacoEditor
                    aria-label="Custom check SQL"
                    language="sql"
                    path={`inmemory://renart/custom-check/${asset.id}/${encodeURIComponent(check?.name ?? "new")}.sql`}
                    value={draft.query}
                    theme={monacoTheme}
                    beforeMount={handleBeforeMount}
                    onMount={handleMount}
                    onChange={(query) =>
                      setDraft((current) => ({ ...current, query: query ?? "" }))
                    }
                    options={{
                      automaticLayout: true,
                      fontSize: 12,
                      lineNumbersMinChars: 3,
                      minimap: { enabled: false },
                      padding: { top: 8, bottom: 8 },
                      scrollBeyondLastLine: false,
                      wordWrap: "on",
                    }}
                  />
                </Suspense>
              </div>
              <FieldDescription>
                Jinja variables and the materialized asset name are available at runtime.
              </FieldDescription>
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel>Result</FieldLabel>
                <Select
                  value={draft.evaluation}
                  onValueChange={(evaluation: "row_count" | "scalar") =>
                    setDraft((current) => ({ ...current, evaluation }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="row_count">Count returned rows</SelectItem>
                    <SelectItem value="scalar">Use scalar result</SelectItem>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  {draft.evaluation === "row_count"
                    ? "Renart counts the rows returned by the query."
                    : "The query must return one integer value."}
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="custom-check-expected">Expected value</FieldLabel>
                <Input
                  id="custom-check-expected"
                  type="number"
                  step="1"
                  value={draft.expected}
                  onChange={(event) =>
                    setDraft((current) => ({ ...current, expected: event.target.value }))
                  }
                />
                {!validExpected ? (
                  <FieldDescription className="text-destructive">
                    Enter a whole number.
                  </FieldDescription>
                ) : null}
              </Field>
            </div>
            <Field orientation="horizontal" className="justify-between">
              <div>
                <FieldLabel htmlFor="custom-check-blocking">Block downstream assets</FieldLabel>
                <FieldDescription>Stop dependent work when this assertion fails.</FieldDescription>
              </div>
              <Switch
                id="custom-check-blocking"
                checked={draft.blocking}
                onCheckedChange={(blocking) => setDraft((current) => ({ ...current, blocking }))}
              />
            </Field>
            {error ? <p className="text-xs text-destructive">{error}</p> : null}
          </FieldGroup>
        </ScrollArea>
        <DialogFooter className="border-t px-5 py-3">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={!canSave} onClick={() => void save()}>
            {saving ? "Saving..." : "Save check"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
