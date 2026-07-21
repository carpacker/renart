"use client";

import { useState } from "react";
import { CheckCircle2, LoaderCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
  useComboboxAnchor,
} from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ConnectionMode } from "@/hooks/use-workspace-connection-form";
import { WorkspaceConfigConnectionType, WorkspaceConfigEnvironment } from "@/lib/types";

type ConnectionFormState = {
  environmentName: string;
  name: string;
  type: string;
  values: Record<string, string | number | boolean | string[]>;
};

export function WorkspaceConnectionFormFields({
  busy,
  canValidate,
  connectionForm,
  connectionTypes,
  environments,
  mode,
  selectedConnectionType,
  selectedEnvironment,
  environmentDisabled = false,
  typeDisabled = false,
  showEnvironmentSelector = true,
  validateBusy,
  validateMessage,
  validateTone,
  showActions = true,
  onEnvironmentChange,
  onFieldValueChange,
  onNameChange,
  onSave,
  onTypeChange,
  onValidate,
}: {
  busy: boolean;
  canValidate: boolean;
  connectionForm: ConnectionFormState;
  connectionTypes: WorkspaceConfigConnectionType[];
  environments: WorkspaceConfigEnvironment[];
  mode: ConnectionMode;
  selectedConnectionType: WorkspaceConfigConnectionType | null;
  selectedEnvironment?: string | null;
  environmentDisabled?: boolean;
  typeDisabled?: boolean;
  showEnvironmentSelector?: boolean;
  validateBusy: boolean;
  validateMessage: string | null;
  validateTone: "error" | "success" | null;
  showActions?: boolean;
  onEnvironmentChange: (value: string) => void;
  onFieldValueChange: (fieldName: string, value: string | number | boolean | string[]) => void;
  onNameChange: (value: string) => void;
  onSave: () => void;
  onTypeChange: (value: string) => void;
  onValidate: () => void;
}) {
  return (
    <div className="grid gap-4">
      {showEnvironmentSelector ? (
        <div className="grid gap-1.5">
          <Label htmlFor="workspace-connection-environment">Environment</Label>
          <Select
            value={connectionForm.environmentName || selectedEnvironment || undefined}
            onValueChange={onEnvironmentChange}
            disabled={environmentDisabled}
          >
            <SelectTrigger id="workspace-connection-environment" className="w-full">
              <SelectValue placeholder="Select environment" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {environments.map((environment) => (
                  <SelectItem key={environment.name} value={environment.name}>
                    {environment.name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <Label htmlFor="workspace-connection-name">Name</Label>
          <Input
            id="workspace-connection-name"
            value={connectionForm.name}
            onChange={(event) => onNameChange(event.target.value)}
            placeholder="postgres-default"
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="workspace-connection-type">Type</Label>
          <Select
            value={connectionForm.type || undefined}
            onValueChange={onTypeChange}
            disabled={typeDisabled}
          >
            <SelectTrigger id="workspace-connection-type" className="w-full">
              <SelectValue placeholder="Select connection type" />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {connectionTypes.map((connectionType) => (
                  <SelectItem key={connectionType.type_name} value={connectionType.type_name}>
                    {connectionType.type_name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="grid gap-3">
        <Label>Configured Values</Label>
        <div className="overflow-hidden rounded-lg border">
          {selectedConnectionType?.fields.map((field) => {
            const fieldValue = connectionForm.values[field.name];
            if (field.type === "bool") {
              return (
                <div
                  key={field.name}
                  className="flex items-center justify-between gap-4 border-t px-4 py-3 first:border-t-0"
                >
                  <div>
                    <div className="font-medium">{field.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {field.is_required ? "Required" : "Optional"}
                    </div>
                  </div>
                  <Switch
                    checked={Boolean(fieldValue)}
                    onCheckedChange={(checked) => onFieldValueChange(field.name, checked)}
                  />
                </div>
              );
            }

            if (field.type === "string_array") {
              const values = Array.isArray(fieldValue) ? fieldValue : [];
              return (
                <div
                  key={field.name}
                  className="grid border-t first:border-t-0 sm:grid-cols-[160px_minmax(0,1fr)]"
                >
                  <div
                    className="bg-muted/30 px-4 py-2 text-xs text-muted-foreground"
                    style={{ fontFamily: '"Geist Mono", ui-monospace, SFMono-Regular, monospace' }}
                  >
                    {field.name}
                  </div>
                  <div className="px-4 py-1.5 transition-colors focus-within:bg-emerald-500/10 dark:focus-within:bg-emerald-500/15">
                    <StringArrayCombobox
                      value={values}
                      suggestions={field.default_value?.split(",") ?? []}
                      placeholder="Add values..."
                      onChange={(nextValues) => onFieldValueChange(field.name, nextValues)}
                    />
                  </div>
                </div>
              );
            }

            return (
              <div
                key={field.name}
                className="grid border-t first:border-t-0 sm:grid-cols-[160px_minmax(0,1fr)]"
              >
                <div
                  className="bg-muted/30 px-4 py-2 text-xs text-muted-foreground"
                  style={{ fontFamily: '"Geist Mono", ui-monospace, SFMono-Regular, monospace' }}
                >
                  {field.name}
                </div>
                <div className="px-4 py-1.5 transition-colors focus-within:bg-emerald-500/10 dark:focus-within:bg-emerald-500/15">
                  <Input
                    aria-label={field.name}
                    type={field.type === "int" ? "number" : secretInputType(field.name)}
                    value={
                      fieldValue === undefined || fieldValue === null
                        ? ""
                        : Array.isArray(fieldValue)
                          ? fieldValue.join(", ")
                          : String(fieldValue)
                    }
                    onChange={(event) =>
                      onFieldValueChange(
                        field.name,
                        field.type === "string_array"
                          ? event.target.value
                              .split(",")
                              .map((item) => item.trim())
                              .filter(Boolean)
                          : field.type === "int"
                            ? event.target.value
                            : event.target.value,
                      )
                    }
                    placeholder={
                      field.default_value || (field.is_required ? "Required" : "Optional")
                    }
                    className="h-6 border-0 bg-transparent px-0 text-xs shadow-none focus-visible:ring-0"
                    style={{ fontFamily: '"Geist Mono", ui-monospace, SFMono-Regular, monospace' }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      </div>

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
          <div className="mt-1 whitespace-pre-wrap text-xs sm:text-sm">{validateMessage}</div>
        </div>
      ) : null}

      {showActions ? (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
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
            Verify Connection
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
            {mode === "create" ? "Create Connection" : "Save Changes"}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function StringArrayCombobox({
  value,
  suggestions,
  placeholder,
  onChange,
}: {
  value: string[];
  suggestions: string[];
  placeholder: string;
  onChange: (value: string[]) => void;
}) {
  const anchor = useComboboxAnchor();
  const [draft, setDraft] = useState("");
  const normalizedValue = compactUnique(value);
  const draftAsItem = draft.trim();
  const items = compactUnique([
    ...normalizedValue,
    ...suggestions,
    ...(draftAsItem ? [draftAsItem] : []),
  ]);

  const commitDraft = () => {
    const additions = splitCommaSeparated(draft);
    if (additions.length === 0) {
      return;
    }
    onChange(compactUnique([...normalizedValue, ...additions]));
    setDraft("");
  };

  const toggleValue = (item: string) => {
    const key = item.toLowerCase();
    if (normalizedValue.some((current) => current.toLowerCase() === key)) {
      onChange(normalizedValue.filter((current) => current.toLowerCase() !== key));
      setDraft("");
      return;
    }
    onChange(compactUnique([...normalizedValue, item]));
    setDraft("");
  };

  return (
    <Combobox
      multiple
      autoHighlight
      items={items}
      value={normalizedValue}
      onValueChange={(nextValue) =>
        onChange(Array.isArray(nextValue) ? compactUnique(nextValue as string[]) : [])
      }
    >
      <ComboboxChips
        ref={anchor}
        className="min-h-7 w-full border-0 bg-transparent px-0 py-0 shadow-none focus-within:ring-0"
      >
        <ComboboxValue>
          {(values) => (
            <>
              {(values as string[]).map((item) => (
                <ComboboxChip key={item}>{item}</ComboboxChip>
              ))}
              <ComboboxChipsInput
                value={draft}
                placeholder={placeholder}
                onChange={(event) => setDraft(event.target.value)}
                onBlur={commitDraft}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === ",") {
                    event.preventDefault();
                    commitDraft();
                  }
                }}
              />
            </>
          )}
        </ComboboxValue>
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>No values yet.</ComboboxEmpty>
        <ComboboxList>
          {(item) => (
            <ComboboxItem key={item} value={item} onClick={() => toggleValue(item)}>
              {item}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function splitCommaSeparated(value: string) {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function compactUnique(values: string[]) {
  const result: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed) {
      continue;
    }
    const key = trimmed.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(trimmed);
  }
  return result;
}

function secretInputType(name: string) {
  return isSecretField(name) ? "password" : "text";
}

function isSecretField(name: string) {
  const lower = name.toLowerCase();
  return ["password", "secret", "token", "api_key", "private_key", "access_key"].some((part) =>
    lower.includes(part),
  );
}
