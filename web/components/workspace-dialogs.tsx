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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type WorkspaceDialogsProps = {
  deletePipelineDialogOpen: boolean;
  deletePipelineLoading: boolean;
  selectedPipelineName?: string;
  canDeletePipeline: boolean;
  onDeletePipelineDialogOpenChange: (open: boolean) => void;
  onConfirmDeletePipeline: () => void;
  onCancelDeletePipeline: () => void;

  deleteDialogOpen: boolean;
  deleteLoading: boolean;
  selectedAssetName?: string;
  canDeleteAsset: boolean;
  onDeleteDialogOpenChange: (open: boolean) => void;
  onConfirmDeleteAsset: () => void;
  onCancelDeleteAsset: () => void;

  createPipelineDialogOpen: boolean;
  createPipelineLoading: boolean;
  createPipelinePath: string;
  onCreatePipelineDialogOpenChange: (open: boolean) => void;
  onCreatePipelinePathChange: (value: string) => void;
  onConfirmCreatePipeline: () => Promise<boolean>;
};

export function WorkspaceDialogs({
  deletePipelineDialogOpen,
  deletePipelineLoading,
  selectedPipelineName,
  canDeletePipeline,
  onDeletePipelineDialogOpenChange,
  onConfirmDeletePipeline,
  onCancelDeletePipeline,
  deleteDialogOpen,
  deleteLoading,
  selectedAssetName,
  canDeleteAsset,
  onDeleteDialogOpenChange,
  onConfirmDeleteAsset,
  onCancelDeleteAsset,
  createPipelineDialogOpen,
  createPipelineLoading,
  createPipelinePath,
  onCreatePipelineDialogOpenChange,
  onCreatePipelinePathChange,
  onConfirmCreatePipeline,
}: WorkspaceDialogsProps) {
  return (
    <>
      <Dialog
        open={deletePipelineDialogOpen}
        onOpenChange={onDeletePipelineDialogOpenChange}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete pipeline?</DialogTitle>
            <DialogDescription>
              This will permanently delete{" "}
              {selectedPipelineName
                ? `"${selectedPipelineName}"`
                : "this pipeline"}{" "}
              and all of its assets.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={deletePipelineLoading}
              onClick={onCancelDeletePipeline}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deletePipelineLoading || !canDeletePipeline}
              onClick={onConfirmDeletePipeline}
            >
              {deletePipelineLoading ? "Deleting..." : "Delete Pipeline"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteDialogOpen} onOpenChange={onDeleteDialogOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete asset?</DialogTitle>
            <DialogDescription>
              This will permanently delete{" "}
              {selectedAssetName ? `"${selectedAssetName}"` : "this asset"} from
              the pipeline.
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

      <Dialog
        open={createPipelineDialogOpen}
        onOpenChange={onCreatePipelineDialogOpenChange}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create pipeline</DialogTitle>
            <DialogDescription>
              Enter the pipeline folder path relative to the workspace root.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-1">
            <Label htmlFor="create-pipeline-path">Pipeline path</Label>
            <Input
              id="create-pipeline-path"
              value={createPipelinePath}
              onChange={(event) =>
                onCreatePipelinePathChange(event.target.value)
              }
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void onConfirmCreatePipeline();
                }
              }}
              placeholder="my-pipeline"
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={createPipelineLoading}
              onClick={() => onCreatePipelineDialogOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={createPipelineLoading}
              onClick={() => {
                void onConfirmCreatePipeline();
              }}
            >
              {createPipelineLoading ? "Creating..." : "Create Pipeline"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
