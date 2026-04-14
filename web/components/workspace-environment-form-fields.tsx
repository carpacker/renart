"use client";

import { Save, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { EnvironmentMode } from "@/hooks/use-workspace-environment-form";
import { WorkspaceConfigEnvironment } from "@/lib/types";

type EnvironmentFormState = {
  name: string;
  schemaPrefix: string;
  setAsDefault: boolean;
  cloneSourceName: string;
};

export function WorkspaceEnvironmentFormFields({
  activeEnvironmentExists,
  busy,
  environmentForm,
  environments,
  mode,
  onCloneSourceChange,
  onDelete,
  onNameChange,
  onSave,
  onSchemaPrefixChange,
  onSetAsDefaultChange,
}: {
  activeEnvironmentExists: boolean;
  busy: boolean;
  environmentForm: EnvironmentFormState;
  environments: WorkspaceConfigEnvironment[];
  mode: EnvironmentMode;
  onCloneSourceChange: (value: string) => void;
  onDelete: () => void;
  onNameChange: (value: string) => void;
  onSave: () => void;
  onSchemaPrefixChange: (value: string) => void;
  onSetAsDefaultChange: (value: boolean) => void;
}) {
  return (
    <div className="rounded-lg border bg-card/60 p-3 sm:p-4">
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="font-medium">
          {mode === "create"
            ? "Create Environment"
            : mode === "clone"
              ? "Clone Environment"
              : "Edit Environment"}
        </div>
        {mode === "edit" && activeEnvironmentExists ? (
          <Button
            size="sm"
            type="button"
            variant="destructive"
            onClick={onDelete}
            disabled={busy}
            className="w-full sm:w-auto"
          >
            <Trash2 className="mr-1 inline size-3" />
            Delete
          </Button>
        ) : null}
      </div>

      <div className="grid gap-3">
        {mode === "clone" ? (
          <div className="grid gap-1">
            <Label>Source environment</Label>
            <Select value={environmentForm.cloneSourceName} onValueChange={onCloneSourceChange}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Select source environment" />
              </SelectTrigger>
              <SelectContent>
                {environments.map((environment) => (
                  <SelectItem key={environment.name} value={environment.name}>
                    {environment.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}

        <div className="grid gap-1">
          <Label>Name</Label>
          <Input
            value={environmentForm.name}
            onChange={(event) => onNameChange(event.target.value)}
            placeholder="staging"
          />
        </div>
        <div className="grid gap-1">
          <Label>Schema prefix</Label>
          <Input
            value={environmentForm.schemaPrefix}
            onChange={(event) => onSchemaPrefixChange(event.target.value)}
            placeholder="staging_"
          />
        </div>
        <div className="flex flex-col gap-3 rounded-md border bg-background/70 px-3 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium">Use as default environment</div>
            <div className="text-xs text-muted-foreground">
              This becomes the active environment across the workspace.
            </div>
          </div>
          <Switch
            checked={environmentForm.setAsDefault}
            onCheckedChange={onSetAsDefaultChange}
          />
        </div>
        <Button
          className="w-full sm:w-auto"
          type="button"
          onClick={onSave}
          disabled={busy || !environmentForm.name.trim()}
        >
          <Save className="mr-1 inline size-3" />
          {mode === "create"
            ? "Create Environment"
            : mode === "clone"
              ? "Clone Environment"
              : "Save Environment"}
        </Button>
      </div>
    </div>
  );
}
