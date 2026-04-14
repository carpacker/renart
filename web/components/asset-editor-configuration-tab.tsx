"use client";

import { Link } from "@tanstack/react-router";
import { AlertTriangle } from "lucide-react";
import { Controller, UseFormReturn } from "react-hook-form";

import { AssetTypeIcon } from "@/components/asset-type-icon";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AssetConfigForm } from "@/components/workspace-editor-pane";

const MATERIALIZATION_NONE_VALUE = "__none__";

export function AssetEditorConfigurationTab({
  activeConfigEnvironmentName,
  availableAssetTypes,
  form,
  onAssetTypeChange,
  onMaterializationTypeChange,
  requiredConnectionType,
  showMissingConnectionWarning,
}: {
  activeConfigEnvironmentName?: string | null;
  availableAssetTypes: string[];
  form: UseFormReturn<AssetConfigForm>;
  onAssetTypeChange: (assetType: string) => void;
  onMaterializationTypeChange: (materializationType: string) => void;
  requiredConnectionType: string | null;
  showMissingConnectionWarning: boolean;
}) {
  return (
    <>
      <div className="grid gap-1">
        <Label>Type</Label>
        <Controller
          control={form.control}
          name="type"
          render={({ field }) => (
            <AssetTypeSelect
              availableAssetTypes={availableAssetTypes}
              onChange={(nextValue) => {
                field.onChange(nextValue);
                onAssetTypeChange(nextValue);
              }}
              value={field.value}
            />
          )}
        />
        {showMissingConnectionWarning ? (
          <div className="mt-1 flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-900 dark:text-amber-100">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="min-w-0">
              <div>
                No connection of type <span className="font-medium">{requiredConnectionType}</span>{" "}
                is configured for environment{" "}
                <span className="font-medium">
                  {activeConfigEnvironmentName ?? "default"}
                </span>
                .
              </div>
              <Link
                to="/settings/connections"
                search={{
                  environment: activeConfigEnvironmentName ?? undefined,
                  connection: undefined,
                  connectionType: requiredConnectionType ?? undefined,
                  mode: "create",
                }}
                className="mt-1 inline-flex text-amber-700 underline decoration-amber-400 underline-offset-2 hover:text-amber-800 dark:text-amber-200 dark:hover:text-amber-100"
              >
                Create a new {requiredConnectionType} connection
              </Link>
            </div>
          </div>
        ) : null}
      </div>
      <div className="grid gap-1">
        <Label>Materialization</Label>
        <Controller
          control={form.control}
          name="materialization"
          render={({ field }) => (
            <Select
              onValueChange={(value) => {
                const materializationType =
                  value === MATERIALIZATION_NONE_VALUE ? "" : value;
                field.onChange(materializationType);
                onMaterializationTypeChange(materializationType);
              }}
              value={field.value?.trim() ? field.value : MATERIALIZATION_NONE_VALUE}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select materialization" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={MATERIALIZATION_NONE_VALUE}>none</SelectItem>
                <SelectItem value="table">table</SelectItem>
                <SelectItem value="view">view</SelectItem>
                <SelectItem value="incremental">incremental</SelectItem>
              </SelectContent>
            </Select>
          )}
        />
      </div>
    </>
  );
}

function AssetTypeSelect({
  value,
  onChange,
  availableAssetTypes,
}: {
  value: string;
  onChange: (value: string) => void;
  availableAssetTypes: string[];
}) {
  return (
    <Select value={value || undefined} onValueChange={onChange}>
      <SelectTrigger className="w-full">
        <SelectValue placeholder="Select asset type">
          {value ? (
            <span className="flex items-center gap-2">
              <AssetTypeIcon assetType={value} className="text-muted-foreground" />
              <span>{value}</span>
            </span>
          ) : null}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {availableAssetTypes.map((assetType) => (
          <SelectItem key={assetType} value={assetType}>
            <span className="flex items-center gap-2">
              <AssetTypeIcon
                assetType={assetType}
                className="text-muted-foreground"
              />
              <span>{assetType}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
