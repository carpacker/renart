"use client";

import { useNavigate } from "@tanstack/react-router";
import { AlertTriangle, BookPlus, CheckCircle2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import {
  createNotebook,
  createNotebookCell,
  renameNotebookCell,
  updateNotebookCell,
} from "@/lib/api-notebooks";
import type { WebAsset, WebNotebook } from "@/lib/types";

const NEW_NOTEBOOK = "__new_notebook__";

function nextCellName(notebook?: WebNotebook) {
  const names = new Set((notebook?.cells ?? []).map((cell) => cell.name.toLowerCase()));
  if (!names.has("query")) return "query";
  let index = 2;
  while (names.has(`query_${index}`)) index += 1;
  return `query_${index}`;
}

function nextNotebookTitle(notebooks: WebNotebook[]) {
  const titles = new Set(notebooks.map((notebook) => notebook.title.toLowerCase()));
  const base = "Ad-hoc exploration";
  if (!titles.has(base.toLowerCase())) return base;
  let index = 2;
  while (titles.has(`${base} ${index}`.toLowerCase())) index += 1;
  return `${base} ${index}`;
}

function requireCellID(cell: WebAsset | undefined) {
  const cellID = cell?.cell_id?.trim();
  if (!cellID) throw new Error("The notebook cell was created without a durable ID.");
  return cellID;
}

export function AdhocToNotebookDialog({
  open,
  onOpenChange,
  notebooks,
  query,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  notebooks: WebNotebook[];
  query: string;
}) {
  const navigate = useNavigate();
  const [target, setTarget] = useState(NEW_NOTEBOOK);
  const [title, setTitle] = useState("");
  const [cellName, setCellName] = useState("query");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const selectedNotebook = useMemo(
    () => notebooks.find((notebook) => notebook.id === target),
    [notebooks, target],
  );

  useEffect(() => {
    if (!open) return;
    const initialTarget = notebooks[0]?.id ?? NEW_NOTEBOOK;
    setTarget(initialTarget);
    setTitle(nextNotebookTitle(notebooks));
    setCellName(nextCellName(notebooks[0]));
    setError("");
  }, [notebooks, open]);

  const changeTarget = (nextTarget: string) => {
    setTarget(nextTarget);
    setCellName(nextCellName(notebooks.find((notebook) => notebook.id === nextTarget)));
    setError("");
  };

  const convert = async () => {
    const trimmedName = cellName.trim();
    if (!/^\w+$/.test(trimmedName)) {
      setError("Cell names may only contain letters, digits, and underscores.");
      return;
    }
    if (!query.trim()) {
      setError("Enter a SQL query before converting it.");
      return;
    }

    setCreating(true);
    setError("");
    try {
      let notebook: WebNotebook;
      let cell: WebAsset | undefined;
      if (target === NEW_NOTEBOOK) {
        notebook = await createNotebook({ title: title.trim() || "Ad-hoc exploration" });
        cell = notebook.cells[0];
        const cellID = requireCellID(cell);
        if (cell?.name !== trimmedName) {
          notebook = await renameNotebookCell(notebook.id, cellID, trimmedName);
          cell = notebook.cells.find((candidate) => candidate.cell_id === cellID);
        }
      } else {
        if (!selectedNotebook) throw new Error("Choose a notebook for the new cell.");
        notebook = await createNotebookCell(selectedNotebook.id, { name: trimmedName });
        cell = notebook.cells.find(
          (candidate) => candidate.name.toLowerCase() === trimmedName.toLowerCase(),
        );
      }

      const cellID = requireCellID(cell);
      await updateNotebookCell(notebook.id, cellID, query, cell?.content_revision);
      onOpenChange(false);
      await navigate({ to: "/notebooks/$notebookId", params: { notebookId: notebook.id } });
    } catch (caught) {
      setError(String(caught));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!creating) onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <BookPlus className="size-4 text-primary" />
            Convert to notebook cell
          </DialogTitle>
          <DialogDescription>
            Copy this SQL draft into a filesystem-backed notebook cell. The ad-hoc draft stays
            available in Build.
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field variant="plain">
            <FieldLabel htmlFor="adhoc-notebook-target">Notebook</FieldLabel>
            <Select value={target} onValueChange={changeTarget}>
              <SelectTrigger id="adhoc-notebook-target">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {notebooks.map((notebook) => (
                    <SelectItem key={notebook.id} value={notebook.id}>
                      {notebook.title}
                    </SelectItem>
                  ))}
                  <SelectItem value={NEW_NOTEBOOK}>New notebook…</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          {target === NEW_NOTEBOOK ? (
            <Field variant="plain">
              <FieldLabel htmlFor="adhoc-notebook-title">Notebook title</FieldLabel>
              <Input
                id="adhoc-notebook-title"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                disabled={creating}
              />
            </Field>
          ) : null}
          <Field variant="plain">
            <FieldLabel htmlFor="adhoc-notebook-cell-name">Cell name</FieldLabel>
            <Input
              id="adhoc-notebook-cell-name"
              className="font-mono"
              value={cellName}
              onChange={(event) => setCellName(event.target.value)}
              disabled={creating}
            />
            <FieldDescription>Use letters, digits, and underscores.</FieldDescription>
          </Field>
        </FieldGroup>

        {error ? (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Could not create notebook cell</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={creating}>
            Cancel
          </Button>
          <Button onClick={() => void convert()} disabled={creating || !query.trim()}>
            {creating ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <CheckCircle2 data-icon="inline-start" />
            )}
            Create cell
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
