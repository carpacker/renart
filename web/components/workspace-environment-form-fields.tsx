"use client";

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
  environmentForm,
  environments,
  mode,
  onCloneSourceChange,
  onNameChange,
  onSchemaPrefixChange,
  onSetAsDefaultChange,
}: {
  environmentForm: EnvironmentFormState;
  environments: WorkspaceConfigEnvironment[];
  mode: EnvironmentMode;
  onCloneSourceChange: (value: string) => void;
  onNameChange: (value: string) => void;
  onSchemaPrefixChange: (value: string) => void;
  onSetAsDefaultChange: (value: boolean) => void;
}) {
  return (
    <div className="grid gap-4">
      {mode === "clone" ? (
        <div className="grid gap-1.5">
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

      <div className="grid gap-1.5">
        <Label>Environment name</Label>
        <Input
          value={environmentForm.name}
          onChange={(event) => onNameChange(event.target.value)}
          placeholder="production"
        />
      </div>
      <div className="grid gap-1.5">
        <Label>Schema prefix</Label>
        <Input
          value={environmentForm.schemaPrefix}
          onChange={(event) => onSchemaPrefixChange(event.target.value)}
          placeholder="prod_"
        />
        <p className="text-xs text-muted-foreground">
          Applied as a prefix to schema names in this environment.
        </p>
      </div>
      <div className="flex flex-col gap-3 rounded-md border bg-muted/20 px-4 py-4 text-sm sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="font-medium">Default environment</div>
          <div className="text-xs text-muted-foreground">
            Makes this the active environment across the workspace.
          </div>
        </div>
        <Switch checked={environmentForm.setAsDefault} onCheckedChange={onSetAsDefaultChange} />
      </div>
    </div>
  );
}
