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

type WorkspacePipelineDialogsProps = {
  deletePipelineDialogOpen: boolean;
  deletePipelineLoading: boolean;
  renamePipelineDialogOpen: boolean;
  renamePipelineLoading: boolean;
  renamePipelineName: string;
  selectedPipelineName?: string;
  canDeletePipeline: boolean;
  onDeletePipelineDialogOpenChange: (open: boolean) => void;
  onRenamePipelineDialogOpenChange: (open: boolean) => void;
  onRenamePipelineNameChange: (value: string) => void;
  onConfirmRenamePipeline: () => Promise<boolean>;
  onConfirmDeletePipeline: () => void;
  onCancelDeletePipeline: () => void;
  createPipelineDialogOpen: boolean;
  createPipelineLoading: boolean;
  createPipelinePath: string;
  onCreatePipelineDialogOpenChange: (open: boolean) => void;
  onCreatePipelinePathChange: (value: string) => void;
  onConfirmCreatePipeline: () => Promise<boolean>;
};

export function WorkspacePipelineDialogs({
  deletePipelineDialogOpen,
  deletePipelineLoading,
  renamePipelineDialogOpen,
  renamePipelineLoading,
  renamePipelineName,
  selectedPipelineName,
  canDeletePipeline,
  onDeletePipelineDialogOpenChange,
  onRenamePipelineDialogOpenChange,
  onRenamePipelineNameChange,
  onConfirmRenamePipeline,
  onConfirmDeletePipeline,
  onCancelDeletePipeline,
  createPipelineDialogOpen,
  createPipelineLoading,
  createPipelinePath,
  onCreatePipelineDialogOpenChange,
  onCreatePipelinePathChange,
  onConfirmCreatePipeline,
}: WorkspacePipelineDialogsProps) {
  return (
    <>
      <Dialog
        open={renamePipelineDialogOpen}
        onOpenChange={onRenamePipelineDialogOpenChange}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename pipeline</DialogTitle>
            <DialogDescription>
              Update the display name stored in `pipeline.yml`.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-1">
            <Label htmlFor="rename-pipeline-name">Pipeline name</Label>
            <Input
              id="rename-pipeline-name"
              value={renamePipelineName}
              onChange={(event) => onRenamePipelineNameChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void onConfirmRenamePipeline();
                }
              }}
              placeholder="my-pipeline"
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={renamePipelineLoading}
              onClick={() => onRenamePipelineDialogOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={renamePipelineLoading || !renamePipelineName.trim()}
              onClick={() => {
                void onConfirmRenamePipeline();
              }}
            >
              {renamePipelineLoading ? "Saving..." : "Save"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={deletePipelineDialogOpen}
        onOpenChange={onDeletePipelineDialogOpenChange}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete pipeline?</DialogTitle>
            <DialogDescription>
              This will permanently delete {selectedPipelineName ? `"${selectedPipelineName}"` : "this pipeline"} and all of its assets.
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
              onChange={(event) => onCreatePipelinePathChange(event.target.value)}
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
