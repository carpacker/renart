"use client";

import { Link } from "@tanstack/react-router";
import {
  Cable,
  ChevronRight,
  ChevronsLeft,
  FolderPlus,
  Moon,
  Pencil,
  Play,
  Settings2,
  Sun,
  Trash2,
  Workflow,
} from "lucide-react";
import { CSSProperties, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { WorkspaceState } from "@/lib/types";

type Props = {
  workspace: WorkspaceState | null;
  currentView: "workspace" | "environments" | "connections";
  connectionsEnvironment?: string | null;
  activePipeline: string | null;
  selectedAsset: string | null;
  highlighted?: boolean;
  highlightStyle?: CSSProperties;
  theme: "light" | "dark";
  onToggleTheme: () => void;
  onCreatePipeline: () => void;
  onDeletePipeline: (pipelineId: string) => void;
  onRenamePipeline: (pipelineId: string) => void;
  canDeletePipeline: boolean;
  deletePipelineLoading: boolean;
  renamePipelineLoading: boolean;
  onRunPipeline: (pipelineId: string) => void;
  canRunPipeline: boolean;
  runPipelineLoading: boolean;
  onOnboardingMountChange?: (element: HTMLDivElement | null) => void;
};

export function WorkspaceSidebar({
  workspace,
  currentView,
  connectionsEnvironment,
  activePipeline,
  selectedAsset,
  highlighted = false,
  highlightStyle,
  theme,
  onToggleTheme,
  onCreatePipeline,
  onDeletePipeline,
  onRenamePipeline,
  canDeletePipeline,
  deletePipelineLoading,
  renamePipelineLoading,
  onRunPipeline,
  canRunPipeline,
  runPipelineLoading,
  onOnboardingMountChange,
}: Props) {
  const { isMobile, state, openMobile, setOpenMobile, toggleSidebar } = useSidebar();
  const [expandedPipelineIds, setExpandedPipelineIds] = useState<Set<string>>(
    () => new Set(activePipeline ? [activePipeline] : [])
  );

  const shouldAutoCloseOnNavigate = isMobile && openMobile;

  const closeSidebarAfterNavigation = () => {
    if (shouldAutoCloseOnNavigate) {
      setOpenMobile(false);
    }
  };

  const handleCreatePipeline = () => {
    closeSidebarAfterNavigation();
    onCreatePipeline();
  };

  useEffect(() => {
    if (!activePipeline) {
      return;
    }

    setExpandedPipelineIds((previous) => {
      if (previous.has(activePipeline)) {
        return previous;
      }

      const next = new Set(previous);
      next.add(activePipeline);
      return next;
    });
  }, [activePipeline]);

  return (
    <Sidebar
      className={`h-full border-r transition-transform md:shadow-none ${
        highlighted ? "ring-2 ring-primary/70 ring-inset" : ""
      }`}
      collapsible="offcanvas"
      style={highlightStyle}
    >
      <SidebarHeader className="border-b px-3 py-3 sm:px-4">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex items-center gap-2 text-sm font-semibold">
            <Workflow className="size-4" />
            <div className="min-w-0">
              <div className="truncate">Pipelines</div>
              <div className="text-xs font-normal text-muted-foreground">
                Browse assets and workspace settings
              </div>
            </div>
          </div>
          <div className="flex items-center gap-1">
            {!isMobile && state === "expanded" ? (
              <Button
                size="icon-sm"
                type="button"
                variant="ghost"
                onClick={toggleSidebar}
                className="shrink-0"
              >
                <ChevronsLeft className="size-3.5" />
              </Button>
            ) : null}
            <Button
              size="icon-sm"
              type="button"
              variant="outline"
              onClick={onToggleTheme}
              className="shrink-0"
            >
              {theme === "dark" ? (
                <Sun className="size-3.5" />
              ) : (
                <Moon className="size-3.5" />
              )}
            </Button>
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Views</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton
                  asChild
                  isActive={currentView === "workspace"}
                >
                  <Link
                    to="/"
                    search={{
                      pipeline: activePipeline ?? undefined,
                      asset: selectedAsset ?? undefined,
                    }}
                    activeOptions={{ exact: true, includeSearch: false }}
                    onClick={closeSidebarAfterNavigation}
                  >
                    <Workflow className="size-4" />
                    <span>Workspace</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton onClick={handleCreatePipeline}>
                  <FolderPlus className="size-4" />
                  <span>New Pipeline</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  asChild
                  isActive={currentView === "environments"}
                >
                  <Link
                    to="/settings/environments"
                    search={{ environment: undefined, mode: undefined }}
                    activeOptions={{ exact: true, includeSearch: false }}
                    onClick={closeSidebarAfterNavigation}
                  >
                    <Settings2 className="size-4" />
                    <span>Environments</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  asChild
                  isActive={currentView === "connections"}
                >
                  <Link
                    to="/settings/connections"
                    search={{
                      environment: connectionsEnvironment ?? undefined,
                      connection: undefined,
                      connectionType: undefined,
                      mode: undefined,
                    }}
                    activeOptions={{ exact: true, includeSearch: false }}
                    onClick={closeSidebarAfterNavigation}
                  >
                    <Cable className="size-4" />
                    <span>Connections</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>Workspace</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {workspace?.pipelines.map((item) => {
                const isActive = item.id === activePipeline;
                const isExpanded = expandedPipelineIds.has(item.id);
                const assets = item.assets ?? [];

                return (
                  <SidebarMenuItem key={item.id}>
                    <ContextMenu>
                      <ContextMenuTrigger asChild>
                        <div>
                          <SidebarMenuButton asChild isActive={isActive}>
                            <Link
                              to="/"
                              search={{
                                pipeline: item.id,
                                asset: assets[0]?.id ?? undefined,
                              }}
                              activeOptions={{ exact: true, includeSearch: false }}
                              onClick={closeSidebarAfterNavigation}
                            >
                              <span>{item.name}</span>
                            </Link>
                          </SidebarMenuButton>
                          <SidebarMenuAction
                            aria-label={isExpanded ? "Collapse pipeline" : "Expand pipeline"}
                            onClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                              setExpandedPipelineIds((previous) => {
                                const next = new Set(previous);
                                if (next.has(item.id)) {
                                  next.delete(item.id);
                                } else {
                                  next.add(item.id);
                                }
                                return next;
                              });
                            }}
                            showOnHover
                            type="button"
                          >
                            <ChevronRight
                              className={`size-3 transition-transform ${
                                isExpanded ? "rotate-90" : ""
                              }`}
                            />
                          </SidebarMenuAction>
                        </div>
                      </ContextMenuTrigger>
                      <ContextMenuContent>
                        <ContextMenuItem disabled>{item.name}</ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem
                          disabled={renamePipelineLoading}
                          onClick={() => onRenamePipeline(item.id)}
                        >
                          <Pencil />
                          Rename Pipeline
                        </ContextMenuItem>
                        <ContextMenuItem
                          disabled={!canRunPipeline || runPipelineLoading}
                          onClick={() => onRunPipeline(item.id)}
                        >
                          <Play
                            className={
                              runPipelineLoading && activePipeline === item.id
                                ? "animate-pulse"
                                : ""
                            }
                          />
                          Run Pipeline
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem
                          variant="destructive"
                          disabled={!canDeletePipeline || deletePipelineLoading}
                          onClick={() => onDeletePipeline(item.id)}
                        >
                          <Trash2 />
                          Delete Pipeline
                        </ContextMenuItem>
                      </ContextMenuContent>
                    </ContextMenu>

                    {isExpanded && (
                      <SidebarMenuSub>
                        {assets.map((asset) => (
                          <SidebarMenuSubItem key={asset.id}>
                            <SidebarMenuSubButton
                              asChild
                              isActive={asset.id === selectedAsset}
                              size="sm"
                            >
                              <Link
                                to="/"
                                search={{ pipeline: item.id, asset: asset.id }}
                                activeOptions={{
                                  exact: true,
                                  includeSearch: false,
                                }}
                                onClick={closeSidebarAfterNavigation}
                              >
                                <span>{asset.name}</span>
                              </Link>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        ))}
                      </SidebarMenuSub>
                    )}
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        {onOnboardingMountChange && (
          <SidebarGroup>
            <SidebarGroupLabel>Onboarding</SidebarGroupLabel>
            <SidebarGroupContent>
              <div ref={onOnboardingMountChange} />
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>
    </Sidebar>
  );
}
