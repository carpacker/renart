import { ArrowUp, CircleAlert, Folder, FolderOpen, FolderPlus, LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useId, useState } from "react";

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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { browseProjectDirs, createProjectDirectory } from "@/lib/api-projects";
import type { BrowseDirsResponse } from "@/lib/generated/api-types";
import { cn } from "@/lib/utils";

export function DirectoryPickerDialog({
  open,
  onOpenChange,
  initialPath,
  browsePurpose,
  title,
  description,
  confirmLabel,
  allowCreate = false,
  showProjectMarkers = false,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialPath?: string;
  browsePurpose?: "create";
  title: string;
  description: string;
  confirmLabel: string;
  allowCreate?: boolean;
  showProjectMarkers?: boolean;
  onSelect: (path: string) => void | Promise<void>;
}) {
  const newFolderInputId = useId();
  const [listing, setListing] = useState<BrowseDirsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [creating, setCreating] = useState(false);
  const [showNewFolder, setShowNewFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [error, setError] = useState<string | null>(null);

  const browse = useCallback(
    async (path?: string) => {
      setLoading(true);
      setError(null);
      try {
        setListing(await browseProjectDirs(path, path ? undefined : browsePurpose));
      } catch (browseError) {
        setError(
          browseError instanceof Error ? browseError.message : "Failed to list the directory.",
        );
      } finally {
        setLoading(false);
      }
    },
    [browsePurpose],
  );

  useEffect(() => {
    if (!open) return;
    setListing(null);
    setError(null);
    setShowNewFolder(false);
    setNewFolderName("");
    void browse(initialPath?.trim() || undefined);
  }, [browse, initialPath, open]);

  const createFolder = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!listing?.path || !newFolderName.trim()) return;

    setCreating(true);
    setError(null);
    try {
      const response = await createProjectDirectory(listing.path, newFolderName.trim());
      setNewFolderName("");
      setShowNewFolder(false);
      await browse(response.path);
    } catch (createError) {
      setError(
        createError instanceof Error ? createError.message : "Failed to create the directory.",
      );
    } finally {
      setCreating(false);
    }
  };

  const selectDirectory = async () => {
    if (!listing?.path) return;
    setSelecting(true);
    setError(null);
    try {
      await onSelect(listing.path);
      onOpenChange(false);
    } catch (selectError) {
      setError(selectError instanceof Error ? selectError.message : "Failed to use the directory.");
    } finally {
      setSelecting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderOpen className="size-4 text-primary" />
            {title}
          </DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <Button
              variant="outline"
              size="icon-sm"
              disabled={loading || !listing?.parent}
              onClick={() => listing?.parent && void browse(listing.parent)}
              title="Parent directory"
              aria-label="Parent directory"
            >
              <ArrowUp />
            </Button>
            <div
              className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-2 py-1.5 font-mono text-xs"
              title={listing?.path}
            >
              {listing?.path ?? "Loading directory..."}
            </div>
            {allowCreate ? (
              <Button
                variant="outline"
                size="sm"
                disabled={loading || !listing?.path}
                onClick={() => {
                  setShowNewFolder((visible) => !visible);
                  setError(null);
                }}
              >
                <FolderPlus data-icon="inline-start" />
                {showNewFolder ? "Cancel" : "New folder"}
              </Button>
            ) : null}
          </div>

          {showNewFolder ? (
            <form onSubmit={(event) => void createFolder(event)}>
              <Field>
                <FieldLabel htmlFor={newFolderInputId}>New folder name</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id={newFolderInputId}
                    value={newFolderName}
                    onChange={(event) => setNewFolderName(event.target.value)}
                    placeholder="data-projects"
                    autoFocus
                    disabled={creating}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton type="submit" disabled={creating || !newFolderName.trim()}>
                      {creating ? (
                        <LoaderCircle data-icon="inline-start" className="animate-spin" />
                      ) : (
                        <FolderPlus data-icon="inline-start" />
                      )}
                      Create
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
                <FieldDescription>
                  The folder is created inside the directory shown above.
                </FieldDescription>
              </Field>
            </form>
          ) : null}

          <ScrollArea className="h-64 rounded-md border" viewportClassName="max-h-64">
            <div className="flex flex-col" aria-busy={loading}>
              {loading && !listing ? (
                <div className="flex flex-col gap-2 p-3">
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-4/5" />
                  <Skeleton className="h-8 w-3/5" />
                </div>
              ) : (
                (listing?.entries ?? []).map((entry) => (
                  <button
                    key={entry.path}
                    type="button"
                    className="flex items-center gap-2 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/50"
                    onClick={() => void browse(entry.path)}
                  >
                    <Folder
                      className={cn(
                        "size-4 shrink-0",
                        showProjectMarkers && entry.is_project
                          ? "text-primary"
                          : "text-muted-foreground",
                      )}
                    />
                    <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                    {showProjectMarkers && entry.is_project ? (
                      <span className="text-xs text-primary">project</span>
                    ) : null}
                  </button>
                ))
              )}
              {!loading && listing && listing.entries.length === 0 ? (
                <p className="px-3 py-4 text-sm text-muted-foreground">No subdirectories.</p>
              ) : null}
            </div>
          </ScrollArea>

          {error ? (
            <Alert variant="destructive">
              <CircleAlert />
              <AlertTitle>Directory unavailable</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={loading || selecting || !listing?.path}
            onClick={() => void selectDirectory()}
          >
            {selecting ? <LoaderCircle data-icon="inline-start" className="animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
