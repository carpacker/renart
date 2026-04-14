"use client";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type WorkspaceAssetDialogsProps = {
  deleteDialogOpen: boolean;
  deleteLoading: boolean;
  selectedAssetName?: string;
  canDeleteAsset: boolean;
  onDeleteDialogOpenChange: (open: boolean) => void;
  onConfirmDeleteAsset: () => void;
  onCancelDeleteAsset: () => void;
};

export function WorkspaceAssetDialogs({
  deleteDialogOpen,
  deleteLoading,
  selectedAssetName,
  canDeleteAsset,
  onDeleteDialogOpenChange,
  onConfirmDeleteAsset,
  onCancelDeleteAsset,
}: WorkspaceAssetDialogsProps) {
  return (
    <Dialog open={deleteDialogOpen} onOpenChange={onDeleteDialogOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete asset?</DialogTitle>
          <DialogDescription>
            This will permanently delete {selectedAssetName ? `"${selectedAssetName}"` : "this asset"} from the pipeline.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={deleteLoading}
            onClick={onCancelDeleteAsset}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={deleteLoading || !canDeleteAsset}
            onClick={onConfirmDeleteAsset}
          >
            {deleteLoading ? "Deleting..." : "Delete"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}