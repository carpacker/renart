import { Link } from "@tanstack/react-router";
import {
  Building2,
  Check,
  ChevronDown,
  Cloud,
  FolderPlus,
  FolderSearch,
  Monitor,
  Moon,
  Settings,
  Sun,
  Trash2,
} from "lucide-react";
import { useCallback, useState } from "react";

import { DirectoryPickerDialog } from "@/components/app/directory-picker-dialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { type WorkspaceThemePreference, useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { listProjects, openProject, removeProject } from "@/lib/api-projects";
import { appFeatureFlags } from "@/lib/app-feature-flags";
import type { ProjectListResponse } from "@/lib/generated/api-types";
import { getPinnedProjectId, pinProject } from "@/lib/project-context";
import { cn } from "@/lib/utils";

// Switching projects pins the target to this tab and reloads onto the home
// route: the current URL's pipelines/assets don't exist in the other
// project, so carrying the route over would only land on dead links.
function switchToProject(projectId: string, defaultProjectId: string) {
  pinProject(projectId === defaultProjectId ? null : projectId);
  window.location.assign("/");
}

export function ProjectSwitcher() {
  const { workspaceConfig } = useWorkspaceSettingsData();
  const { themePreference, setTheme } = useWorkspaceTheme();
  const [directory, setDirectory] = useState<ProjectListResponse | null>(null);
  const [browseOpen, setBrowseOpen] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setDirectory(await listProjects());
    } catch {
      // The dropdown degrades to just the current project name.
    }
  }, []);

  const currentProjectId =
    getPinnedProjectId() ?? directory?.default_project_id ?? workspaceConfig?.project_id;
  const currentName =
    directory?.projects.find((project) => project.id === currentProjectId)?.name ||
    workspaceConfig?.project_name ||
    "project";

  return (
    <>
      <DropdownMenu onOpenChange={(open) => open && void refresh()}>
        <DropdownMenuTrigger asChild>
          <Button
            data-testid="project-switcher-trigger"
            variant="outline"
            size="sm"
            className="h-7 border-zinc-800 bg-zinc-950 px-2 text-zinc-200 hover:bg-zinc-800 hover:text-white"
          >
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
                <span
                  className={cn(
                    "block truncate text-xs text-muted-foreground",
                    !project.exists && "text-destructive",
                  )}
                >
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
                    void removeProject(project.id)
                      .then(setDirectory)
                      .catch(() => {});
                  }}
                >
                  <Trash2 className="size-3.5 text-muted-foreground" />
                </button>
              ) : null}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem asChild>
            <Link to="/welcome" search={{ new: true }}>
              <FolderPlus className="size-4" />
              New project...
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => setBrowseOpen(true)}>
            <FolderSearch className="size-4" />
            Open project...
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/project/general">
              <Settings className="size-4" />
              Project settings
            </Link>
          </DropdownMenuItem>
          {appFeatureFlags.cloudWorkspaces ? (
            <DropdownMenuItem asChild>
              <Link to="/account/workspaces">
                <Cloud className="size-4" />
                Connect cloud workspace
              </Link>
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuSeparator />
          <DropdownMenuLabel>Appearance</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={themePreference}
            onValueChange={(value) => setTheme(value as WorkspaceThemePreference)}
          >
            <DropdownMenuRadioItem value="light">
              <Sun />
              Light
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="dark">
              <Moon />
              Dark
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="system">
              <Monitor />
              System
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
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
  const openDirectory = async (path: string) => {
    const response = await openProject(path);
    pinProject(response.project.id === defaultProjectId ? null : response.project.id);
    window.location.assign("/");
  };

  return (
    <DirectoryPickerDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Open project"
      description="Pick a directory. It becomes a project with its own connections, environments, and schedules."
      confirmLabel="Open this directory"
      showProjectMarkers
      onSelect={openDirectory}
    />
  );
}
