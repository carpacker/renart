"use client";

import { CSSProperties, KeyboardEvent, useEffect, useState } from "react";
import {
  ArrowDownFromLine,
  ArrowDownToLine,
  ArrowDownWideNarrow,
  Check,
  ChevronDown,
  Eye,
  Hammer,
  Pencil,
  Trash2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { MaterializeScope } from "@/lib/materialize-scope";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

type AssetEditorHeaderProps = {
  assetName?: string | null;
  assetPath?: string | null;
  helpMode: boolean;
  actionHighlighted: boolean;
  highlightStyle?: CSSProperties;
  hasAsset: boolean;
  assetRenameLoading?: boolean;
  materializeLoading: boolean;
  inspectLoading: boolean;
  deleteLoading: boolean;
  onRenameAsset: (assetName: string) => Promise<boolean> | boolean;
  onMaterialize: (scope?: MaterializeScope) => void;
  onInspect: () => void;
  onDelete: () => void;
};

export function AssetEditorHeader({
  assetName,
  assetPath,
  helpMode,
  actionHighlighted,
  highlightStyle,
  hasAsset,
  assetRenameLoading = false,
  materializeLoading,
  inspectLoading,
  deleteLoading,
  onRenameAsset,
  onMaterialize,
  onInspect,
  onDelete,
}: AssetEditorHeaderProps) {
  const [assetDraft, setAssetDraft] = useState(assetName ?? "");
  const [renameMode, setRenameMode] = useState(false);

  useEffect(() => {
    setAssetDraft(assetName ?? "");
    setRenameMode(false);
  }, [assetName]);

  const handleSaveAssetName = async () => {
    const trimmedName = assetDraft.trim();
    if (!trimmedName) {
      setAssetDraft(assetName ?? "");
      setRenameMode(false);
      return;
    }

    const renamed = await onRenameAsset(trimmedName);
    if (renamed) {
      setRenameMode(false);
    }
  };

  const handleAssetInputKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      void handleSaveAssetName();
      return;
    }

    if (event.key === "Escape") {
      event.preventDefault();
      setAssetDraft(assetName ?? "");
      setRenameMode(false);
    }
  };

  return (
    <div className="border-b px-4 py-3">
      <div className="mb-2 flex min-w-0 items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          {renameMode ? (
            <div className="flex items-center gap-2">
              <Input
                autoFocus
                className="h-8 max-w-sm"
                disabled={assetRenameLoading}
                onChange={(event) => setAssetDraft(event.target.value)}
                onKeyDown={handleAssetInputKeyDown}
                value={assetDraft}
              />
              <Button
                size="sm"
                type="button"
                disabled={assetRenameLoading || !assetDraft.trim()}
                onClick={() => void handleSaveAssetName()}
              >
                <Check className="mr-1 inline size-3" />
                {assetRenameLoading ? "Saving..." : "Save"}
              </Button>
            </div>
          ) : (
            <div className="flex min-w-0 items-center gap-2">
              <div className="truncate text-sm font-semibold">
                <span data-testid="editor-asset-name">
                {assetName ?? "No asset selected"}
                </span>
              </div>
              <Button
                aria-label="Rename asset"
                size="icon-sm"
                type="button"
                variant="ghost"
                disabled={!hasAsset}
                onClick={() => setRenameMode(true)}
              >
                <Pencil className="size-3.5" />
              </Button>
            </div>
          )}
        </div>
      </div>
      <div className="text-xs opacity-70" data-testid="editor-asset-path">
        {assetPath ?? "No asset selected"}
      </div>
      <div
        className={`mt-2 flex flex-wrap items-center gap-2 ${
          helpMode && actionHighlighted
            ? "rounded-md ring-2 ring-primary/70 ring-offset-2"
            : ""
        }`}
        style={helpMode && actionHighlighted ? highlightStyle : undefined}
      >
        <ButtonGroup>
          <Button
            size="sm"
            variant="outline"
            disabled={!hasAsset || materializeLoading}
            onClick={() => onMaterialize("asset")}
            type="button"
          >
            <Hammer className="mr-1 inline size-3" />
            {materializeLoading ? "Running..." : "Materialize"}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                size="icon-sm"
                variant="outline"
                disabled={!hasAsset || materializeLoading}
                type="button"
                aria-label="Materialize options"
              >
                <ChevronDown className="size-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Materialize</DropdownMenuLabel>
                <DropdownMenuItem onClick={() => onMaterialize("asset")}>
                  <Hammer className="size-4" />
                  Asset only
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem onClick={() => onMaterialize("asset_with_upstreams")}>
                <ArrowDownToLine className="size-4" />
                With upstream
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onMaterialize("asset_with_downstreams")}>
                <ArrowDownFromLine className="size-4" />
                  With downstream
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onMaterialize("asset_with_upstreams_and_downstreams")}>
                <ArrowDownWideNarrow className="size-4" />
                  With both
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </ButtonGroup>
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                size="sm"
                variant="outline"
                disabled={!hasAsset || inspectLoading}
                onClick={onInspect}
                type="button"
              >
                <Eye className="mr-1 inline size-3" />
                {inspectLoading ? "Loading..." : "Inspect Data"}
              </Button>
            </TooltipTrigger>
            <TooltipContent>Shortcut: ⌘ + ↵</TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <Button
          size="sm"
          variant="destructive"
          disabled={!hasAsset || deleteLoading}
          onClick={onDelete}
          type="button"
        >
          <Trash2 className="mr-1 inline size-3" />
          Delete Asset
        </Button>
      </div>
    </div>
  );
}
