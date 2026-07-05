import { Link } from "@tanstack/react-router";
import {
  ArrowUp,
  Building2,
  Check,
  ChevronDown,
  Cloud,
  Folder,
  FolderOpen,
  FolderSearch,
  LoaderCircle,
  Settings,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { browseProjectDirs, listProjects, openProject, removeProject } from "@/lib/api-projects";
import type { BrowseDirsResponse, ProjectListResponse } from "@/lib/generated/api-types";
import { getPinnedProjectId, pinProject } from "@/lib/project-context";
import { cn } from "@/lib/utils";

// Switching projects pins the target to this tab and reloads onto the home
// route: the current URL's pipelines/assets don't exist in the other
// project, so carrying the route over would only land on dead links.
function switchToProject(projectId: string, defaultProjectId: string) {
  pinProject(projectId === defaultProjectId ? null : projectId);
  window.location.assign("/redesign");
}

export function ProjectSwitcher() {
  const { workspaceConfig } = useWorkspaceSettingsData();
  const [directory, setDirectory] = useState<ProjectListResponse | null>(null);
  const [browseOpen, setBrowseOpen] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setDirectory(await listProjects());
    } catch {
      // The dropdown degrades to just the current project name.
    }
  }, []);

  const currentProjectId = getPinnedProjectId() ?? directory?.default_project_id ?? workspaceConfig?.project_id;
  const currentName =
    directory?.projects.find((project) => project.id === currentProjectId)?.name ||
    workspaceConfig?.project_name ||
    "project";

  return (
    <>
      <DropdownMenu onOpenChange={(open) => open && void refresh()}>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" className="h-7 border-zinc-800 bg-zinc-950 px-2 text-zinc-200 hover:bg-zinc-800 hover:text-white">
            <Building2 className="size-3.5 text-zinc-400" />
            <span className="max-w-32 truncate font-medium sm:max-w-44">{currentName}</span>
            <ChevronDown className="size-3 text-zinc-500" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-72">
          <DropdownMenuLabel>Projects</DropdownMenuLabel>
          {(directory?.projects ?? []).map((project) => (
            <DropdownMenuItem
              key={project.id}
              // Missing directories stay interactive (not `disabled`) so the
              // remove button still receives clicks; selecting them is a no-op
              // that keeps the menu open.
              className={cn(!project.exists && "opacity-70")}
              onSelect={(event) => {
                if (!project.exists) {
                  event.preventDefault();
                  return;
                }
                if (project.id !== currentProjectId && directory) {
                  switchToProject(project.id, directory.default_project_id);
                }
              }}
            >
              <Building2 className="size-4" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{project.name}</span>
                <span className={cn("block truncate text-xs text-muted-foreground", !project.exists && "text-destructive")}>
                  {project.exists ? project.path : `${project.path} (missing)`}
                </span>
              </span>
              {project.id === currentProjectId ? <Check className="size-3.5" /> : null}
              {!project.exists ? (
                <button
                  type="button"
                  className="rounded p-1 hover:bg-muted"
                  title="Remove from list"
                  aria-label={`Remove ${project.name} from list`}
                  onClick={(event) => {
                    event.stopPropagation();
                    void removeProject(project.id).then(setDirectory).catch(() => {});
                  }}
                >
                  <Trash2 className="size-3.5 text-muted-foreground" />
                </button>
              ) : null}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setBrowseOpen(true)}>
            <FolderSearch className="size-4" />
            Open project...
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/redesign/project/general"><Settings className="size-4" />Project settings</Link>
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/redesign/account/workspaces"><Cloud className="size-4" />Connect cloud workspace</Link>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <OpenProjectDialog
        open={browseOpen}
        onOpenChange={setBrowseOpen}
        defaultProjectId={directory?.default_project_id}
      />
    </>
  );
}

function OpenProjectDialog({
  open,
  onOpenChange,
  defaultProjectId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultProjectId?: string;
}) {
  const [listing, setListing] = useState<BrowseDirsResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const browse = useCallback(async (path?: string) => {
    setError(null);
    try {
      setListing(await browseProjectDirs(path));
    } catch (browseError) {
      setError(browseError instanceof Error ? browseError.message : "Failed to list directory.");
    }
  }, []);

  useEffect(() => {
    if (open) {
      void browse();
    }
  }, [browse, open]);

  const openDirectory = async (path: string) => {
    setBusy(true);
    setError(null);
    try {
      const response = await openProject(path);
      pinProject(response.project.id === defaultProjectId ? null : response.project.id);
      window.location.assign("/redesign");
    } catch (openError) {
      setError(openError instanceof Error ? openError.message : "Failed to open project.");
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderOpen className="size-4 text-primary" />
            Open project
          </DialogTitle>
          <DialogDescription>
            Pick a directory. It becomes a project with its own connections, environments, and schedules.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon-sm"
              disabled={!listing?.parent}
              onClick={() => listing?.parent && void browse(listing.parent)}
              title="Parent directory"
            >
              <ArrowUp className="size-3.5" />
            </Button>
            <div className="min-w-0 flex-1 truncate rounded-md border bg-muted/40 px-2 py-1.5 font-mono text-xs">
              {listing?.path ?? "..."}
            </div>
          </div>
          <ScrollArea className="h-64 rounded-md border" viewportClassName="max-h-64">
            <div className="flex flex-col">
              {(listing?.entries ?? []).map((entry) => (
                <button
                  key={entry.path}
                  type="button"
                  className="flex items-center gap-2 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/50"
                  onDoubleClick={() => void browse(entry.path)}
                  onClick={() => void browse(entry.path)}
                >
                  <Folder className={cn("size-4 shrink-0", entry.is_project ? "text-primary" : "text-muted-foreground")} />
                  <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                  {entry.is_project ? <span className="text-xs text-primary">project</span> : null}
                </button>
              ))}
              {listing && listing.entries.length === 0 ? (
                <p className="px-3 py-4 text-sm text-muted-foreground">No subdirectories.</p>
              ) : null}
            </div>
          </ScrollArea>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={busy || !listing?.path} onClick={() => listing?.path && void openDirectory(listing.path)}>
            {busy ? <LoaderCircle data-icon="inline-start" className="animate-spin" /> : null}
            Open this directory
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
