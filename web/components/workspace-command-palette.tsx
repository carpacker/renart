"use client";

import { useNavigate } from "@tanstack/react-router";
import { useCommandState } from "cmdk";
import {
  Cable,
  ChevronRight,
  FolderTree,
  Search,
  Settings2,
  Workflow,
} from "lucide-react";
import {
  useEffect,
  useMemo,
  useState,
} from "react";

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command";
import { Button } from "@/components/ui/button";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { WorkspaceState } from "@/lib/types";

type WorkspaceCommandPaletteProps = {
  workspace: WorkspaceState;
  activePipeline: string | null;
  selectedAsset: string | null;
  currentView: "workspace" | "environments" | "connections";
};

type PaletteItem = {
  id: string;
  kind: "asset" | "pipeline" | "connection" | "environment";
  title: string;
  subtitle?: string;
  keywords: string[];
  perform: () => void;
};

type PalettePage = "pipelines" | "assets" | "environments" | "connections";

type PaletteSection = {
  id: PalettePage;
  title: string;
  description: string;
  icon: typeof Workflow;
  items: PaletteItem[];
};

function toKeywords(...values: Array<string | undefined | null>) {
  return values
    .flatMap((value) => (value ? value.split(/[^A-Za-z0-9_.-]+/) : []))
    .map((value) => value.trim())
    .filter(Boolean);
}

function PaletteSearchSubItem({
  item,
  activePipeline,
  selectedAsset,
}: {
  item: PaletteItem;
  activePipeline: string | null;
  selectedAsset: string | null;
}) {
  const search = useCommandState((state) => state.search);

  if (!search.trim()) {
    return null;
  }

  return (
    <CommandItem
      value={`${item.title} ${item.subtitle ?? ""}`}
      keywords={item.keywords}
      onSelect={item.perform}
      className="ml-6"
    >
      <ChevronRight className="size-4 text-muted-foreground" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">{item.title}</span>
        {item.subtitle ? (
          <span className="truncate text-xs text-muted-foreground">
            {item.subtitle}
          </span>
        ) : null}
      </div>
      {(item.kind === "asset" && item.id === `asset:${selectedAsset}`) ||
      (item.kind === "pipeline" && item.id === `pipeline:${activePipeline}`) ? (
        <CommandShortcut>Current</CommandShortcut>
      ) : null}
    </CommandItem>
  );
}

export function WorkspaceCommandPalette({
  workspace,
  activePipeline,
  selectedAsset,
  currentView,
}: WorkspaceCommandPaletteProps) {
  const navigate = useNavigate();
  const { normalizedConfigEnvironments } = useWorkspaceSettingsData();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [pages, setPages] = useState<PalettePage[]>([]);
  const [shortcutLabel, setShortcutLabel] = useState("Ctrl K");
  const page = pages[pages.length - 1];

  useEffect(() => {
    const down = (event: KeyboardEvent) => {
      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable ||
          target.closest("[role='dialog'] input, textarea") ||
          target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA")
      ) {
        if (!(event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey))) {
          return;
        }
      }

      if (event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        setOpen((current) => !current);
      }
    };

    document.addEventListener("keydown", down);
    return () => document.removeEventListener("keydown", down);
  }, []);

  useEffect(() => {
    if (typeof navigator === "undefined") {
      return;
    }

    setShortcutLabel(
      navigator.platform.toLowerCase().includes("mac") ? "⌘K" : "Ctrl K"
    );
  }, []);

  useEffect(() => {
    if (open) {
      return;
    }

    setSearch("");
    setPages([]);
  }, [open]);

  const sections = useMemo<PaletteSection[]>(() => {
    const pipelineItems: PaletteItem[] = [];
    const assetItems: PaletteItem[] = [];
    const environmentItems: PaletteItem[] = [];
    const connectionItems: PaletteItem[] = [];

    for (const pipeline of workspace.pipelines) {
      const assets = pipeline.assets ?? [];

      pipelineItems.push({
        id: `pipeline:${pipeline.id}`,
        kind: "pipeline",
        title: pipeline.name,
        subtitle: pipeline.path,
        keywords: toKeywords(pipeline.name, pipeline.path, pipeline.id),
        perform: () => {
          void navigate({
            to: "/",
            search: {
              pipeline: pipeline.id,
              asset: assets[0]?.id ?? undefined,
            },
          });
          setOpen(false);
        },
      });

      for (const asset of assets) {
        assetItems.push({
          id: `asset:${asset.id}`,
          kind: "asset",
          title: asset.name,
          subtitle: pipeline.name,
          keywords: toKeywords(
            asset.name,
            asset.path,
            asset.type,
            pipeline.name,
            pipeline.path,
            asset.connection,
            asset.materialization_type
          ),
          perform: () => {
            void navigate({
              to: "/",
              search: {
                pipeline: pipeline.id,
                asset: asset.id,
              },
            });
            setOpen(false);
          },
        });
      }
    }

    for (const environment of normalizedConfigEnvironments) {
      const connections = environment.connections ?? [];

      environmentItems.push({
        id: `environment:${environment.name}`,
        kind: "environment",
        title: environment.name,
        subtitle: environment.schema_prefix || "Environment",
        keywords: toKeywords(
          environment.name,
          environment.schema_prefix,
          ...connections.map((connection) => connection.name)
        ),
        perform: () => {
          void navigate({
            to: "/settings/environments",
            search: {
              environment: environment.name,
              mode: "edit",
            },
          });
          setOpen(false);
        },
      });

      for (const connection of connections) {
        connectionItems.push({
          id: `connection:${environment.name}:${connection.name}`,
          kind: "connection",
          title: connection.name,
          subtitle: `${environment.name} · ${connection.type}`,
          keywords: toKeywords(
            connection.name,
            connection.type,
            environment.name,
            ...Object.keys(connection.values ?? {})
          ),
          perform: () => {
            void navigate({
              to: "/settings/connections",
              search: {
                environment: environment.name,
                connection: connection.name,
                connectionType: connection.type,
                mode: "edit",
              },
            });
            setOpen(false);
          },
        });
      }
    }

    return [
      {
        id: "pipelines",
        title: "Go to pipeline...",
        description: "Jump to a pipeline and open its first asset",
        icon: Workflow,
        items: pipelineItems,
      },
      {
        id: "assets",
        title: "Go to asset...",
        description: "Open an asset directly in the editor",
        icon: FolderTree,
        items: assetItems,
      },
      {
        id: "environments",
        title: "Go to environment...",
        description: "Open an environment in settings",
        icon: Settings2,
        items: environmentItems,
      },
      {
        id: "connections",
        title: "Go to connection...",
        description: "Open a connection in settings",
        icon: Cable,
        items: connectionItems,
      },
    ];
  }, [navigate, normalizedConfigEnvironments, workspace.pipelines]);

  const currentSection = sections.find((section) => section.id === page) ?? null;

  const rootPlaceholder =
    currentView === "workspace"
      ? "Search workspace commands..."
      : "Search settings commands...";
  const inputPlaceholder = currentSection
    ? `Search ${currentSection.title.replace("...", "").toLowerCase()}`
    : rootPlaceholder;

  return (
    <>
      <Button
        type="button"
        variant="outline"
        aria-label="Open search"
        className="ml-4 h-8 min-w-0 max-w-72 justify-between gap-2 rounded-lg border-dashed px-3 text-muted-foreground"
        onClick={() => setOpen(true)}
      >
        <span className="flex min-w-0 items-center gap-2 truncate">
          <Search className="size-4 shrink-0" />
          <span className="truncate text-sm">
            Search {currentView === "workspace" ? "workspace" : "settings"}
          </span>
        </span>
        <span className="hidden text-xs sm:inline">{shortcutLabel}</span>
      </Button>

      <CommandDialog
        open={open}
        onOpenChange={setOpen}
        title="Search workspace"
        description="Search assets, pipelines, connections, environments, and future commands."
        className="max-w-2xl"
      >
        <Command
          loop
          onKeyDown={(event) => {
            if (
              pages.length > 0 &&
              (event.key === "Escape" ||
                (event.key === "Backspace" && !search.trim()))
            ) {
              event.preventDefault();
              setPages((current) => current.slice(0, -1));
            }
          }}
        >
          <CommandInput
            value={search}
            onValueChange={setSearch}
            placeholder={inputPlaceholder}
          />
          <CommandList>
            <CommandEmpty>No results found.</CommandEmpty>
            {currentSection ? (
              <CommandGroup heading={currentSection.title.replace("...", "") }>
                {currentSection.items.map((item) => (
                  <CommandItem
                    key={item.id}
                    value={`${item.title} ${item.subtitle ?? ""}`}
                    keywords={item.keywords}
                    onSelect={item.perform}
                  >
                    <ChevronRight className="size-4 text-muted-foreground" />
                    <div className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate">{item.title}</span>
                      {item.subtitle ? (
                        <span className="truncate text-xs text-muted-foreground">
                          {item.subtitle}
                        </span>
                      ) : null}
                    </div>
                    {(item.kind === "asset" && item.id === `asset:${selectedAsset}`) ||
                    (item.kind === "pipeline" && item.id === `pipeline:${activePipeline}`) ? (
                      <CommandShortcut>Current</CommandShortcut>
                    ) : null}
                  </CommandItem>
                ))}
              </CommandGroup>
            ) : (
              <>
                <CommandGroup heading="Jump to">
                  {sections.map((section) => {
                    const Icon = section.icon;

                    return (
                      <div key={section.id}>
                        <CommandItem
                          value={`${section.title} ${section.description}`}
                          keywords={section.items.flatMap((item) => item.keywords)}
                          onSelect={() => {
                            setSearch("");
                            setPages((current) => [...current, section.id]);
                          }}
                        >
                          <Icon className="size-4 text-muted-foreground" />
                          <div className="flex min-w-0 flex-1 flex-col">
                            <span className="truncate">{section.title}</span>
                            <span className="truncate text-xs text-muted-foreground">
                              {section.description}
                            </span>
                          </div>
                          <CommandShortcut>{section.items.length}</CommandShortcut>
                        </CommandItem>
                        {section.items.map((item) => (
                          <PaletteSearchSubItem
                            key={item.id}
                            item={item}
                            activePipeline={activePipeline}
                            selectedAsset={selectedAsset}
                          />
                        ))}
                      </div>
                    );
                  })}
                </CommandGroup>
                <CommandSeparator />
                <CommandGroup heading="Tips">
                  <CommandItem
                    value="Use backspace to go back"
                    onSelect={() => undefined}
                    disabled
                  >
                    <Search className="size-4 text-muted-foreground" />
                    <div className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate">Type to reveal matching items</span>
                      <span className="truncate text-xs text-muted-foreground">
                        Press Backspace or Escape to leave nested pages
                      </span>
                    </div>
                  </CommandItem>
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </CommandDialog>
    </>
  );
}
