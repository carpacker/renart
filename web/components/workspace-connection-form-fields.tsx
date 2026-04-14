"use client";

import { CheckCircle2, LoaderCircle, Save, Trash2 } from "lucide-react";

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
import { ConnectionMode } from "@/hooks/use-workspace-connection-form";
import {
  WorkspaceConfigConnectionType,
  WorkspaceConfigEnvironment,
} from "@/lib/types";

type ConnectionFormState = {
  environmentName: string;
  name: string;
  type: string;
  values: Record<string, string | number | boolean>;
};

export function WorkspaceConnectionFormFields({
  activeConnectionExists,
  busy,
  canValidate,
  connectionForm,
  connectionTypes,
  environments,
  mode,
  selectedConnectionType,
  selectedEnvironment,
  validateBusy,
  validateMessage,
  validateTone,
  onDelete,
  onEnvironmentChange,
  onFieldValueChange,
  onNameChange,
  onSave,
  onTypeChange,
  onValidate,
}: {
  activeConnectionExists: boolean;
  busy: boolean;
  canValidate: boolean;
  connectionForm: ConnectionFormState;
  connectionTypes: WorkspaceConfigConnectionType[];
  environments: WorkspaceConfigEnvironment[];
  mode: ConnectionMode;
  selectedConnectionType: WorkspaceConfigConnectionType | null;
  selectedEnvironment?: string | null;
  validateBusy: boolean;
  validateMessage: string | null;
  validateTone: "error" | "success" | null;
  onDelete: () => void;
  onEnvironmentChange: (value: string) => void;
  onFieldValueChange: (
    fieldName: string,
    value: string | number | boolean
  ) => void;
  onNameChange: (value: string) => void;
  onSave: () => void;
  onTypeChange: (value: string) => void;
  onValidate: () => void;
}) {
  return (
    <div className="grid gap-3">
      <div className="grid gap-1">
        <Label>Environment</Label>
        <Select
          value={connectionForm.environmentName || selectedEnvironment || undefined}
          onValueChange={onEnvironmentChange}
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select environment" />
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

      <div className="rounded-lg border bg-card/60 p-3 sm:p-4">
        <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div className="font-medium">
            {mode === "create" ? "Create Connection" : "Edit Connection"}
          </div>
          {mode === "edit" && activeConnectionExists ? (
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
          <div className="grid gap-1">
            <Label>Name</Label>
            <Input
              value={connectionForm.name}
              onChange={(event) => onNameChange(event.target.value)}
              placeholder="MY_CONNECTION"
            />
          </div>
          <div className="grid gap-1">
            <Label>Type</Label>
            <Select value={connectionForm.type || undefined} onValueChange={onTypeChange}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Select connection type" />
              </SelectTrigger>
              <SelectContent>
                {connectionTypes.map((connectionType) => (
                  <SelectItem key={connectionType.type_name} value={connectionType.type_name}>
                    {connectionType.type_name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {selectedConnectionType?.fields.map((field) => {
            const fieldValue = connectionForm.values[field.name];
            if (field.type === "bool") {
              return (
                <div
                  key={field.name}
                  className="flex flex-col gap-3 rounded-md border bg-background/70 px-3 py-3 text-sm sm:flex-row sm:items-center sm:justify-between"
                >
                  <div>
                    <div className="font-medium">{field.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {field.is_required
                        ? "Required boolean field"
                        : "Optional boolean field"}
                    </div>
                  </div>
                  <Switch
                    checked={Boolean(fieldValue)}
                    onCheckedChange={(checked) =>
                      onFieldValueChange(field.name, checked)
                    }
                  />
                </div>
              );
            }

            return (
              <div key={field.name} className="grid gap-1">
                <Label>{field.name}</Label>
                <Input
                  type={field.type === "int" ? "number" : secretInputType(field.name)}
                  value={
                    fieldValue === undefined || fieldValue === null
                      ? ""
                      : String(fieldValue)
                  }
                  onChange={(event) =>
                    onFieldValueChange(
                      field.name,
                      field.type === "int" ? event.target.value : event.target.value
                    )
                  }
                  placeholder={
                    field.default_value || (field.is_required ? "Required" : "Optional")
                  }
                />
              </div>
            );
          })}

          {validateMessage ? (
            <div
              className={`rounded-md border px-3 py-2 text-sm ${
                validateTone === "error"
                  ? "border-destructive/40 bg-destructive/10 text-destructive"
                  : "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
              }`}
            >
              <div className="font-medium">
                {validateTone === "error"
                  ? "Connection validation failed"
                  : "Connection validation succeeded"}
              </div>
              <div className="mt-1 whitespace-pre-wrap text-xs sm:text-sm">
                {validateMessage}
              </div>
            </div>
          ) : null}

          <div className="flex flex-col gap-2 sm:flex-row">
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={onValidate}
              disabled={busy || validateBusy || !canValidate}
            >
              {validateBusy ? (
                <LoaderCircle className="mr-1 inline size-3 animate-spin" />
              ) : (
                <CheckCircle2 className="mr-1 inline size-3" />
              )}
              Validate Connection
            </Button>
            <Button
              className="w-full sm:w-auto"
              type="button"
              onClick={onSave}
              disabled={
                busy ||
                !connectionForm.environmentName ||
                !connectionForm.name.trim() ||
                !connectionForm.type
              }
            >
              <Save className="mr-1 inline size-3" />
              {mode === "create" ? "Create Connection" : "Save Connection"}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function secretInputType(name: string) {
  return isSecretField(name) ? "password" : "text";
}

function isSecretField(name: string) {
  const lower = name.toLowerCase();
  return ["password", "secret", "token", "api_key", "private_key", "access_key"].some(
    (part) => lower.includes(part)
  );
}
