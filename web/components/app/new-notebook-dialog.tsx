"use client";

import { useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { BookOpen, CheckCircle2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { createNotebook } from "@/lib/api-notebooks";

export function NewNotebookDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);
  const [title, setTitle] = useState("Exploration");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      return;
    }
    setTitle("Exploration");
    setError("");
    window.setTimeout(() => inputRef.current?.select(), 0);
  }, [open]);

  const create = async () => {
    const trimmed = title.trim();
    setCreating(true);
    setError("");
    try {
      const created = await createNotebook({ title: trimmed || "Untitled" });
      onOpenChange(false);
      await navigate({ to: "/notebooks/$notebookId", params: { notebookId: created.id } });
    } catch (caught) {
      setError(String(caught));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!creating) {
        onOpenChange(nextOpen);
      }
    }}>
      <DialogContent>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (!creating) {
              void create();
            }
          }}
        >
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><BookOpen className="size-4 text-primary" />New notebook</DialogTitle>
            <DialogDescription>Create an exploratory SQL notebook in this workspace.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="new-notebook-title">Title</Label>
            <Input
              ref={inputRef}
              id="new-notebook-title"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              onFocus={(event) => event.currentTarget.select()}
              autoFocus
              disabled={creating}
            />
          </div>
          {error ? (
            <div className="mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-500/25 dark:bg-red-500/10 dark:text-red-300">{error}</div>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={creating}>Cancel</Button>
            <Button type="submit" disabled={creating}>
              {creating ? <Spinner className="size-4" /> : <CheckCircle2 className="size-4" />}Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
