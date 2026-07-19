"use client";

import { useState } from "react";

import { ClipboardPaste } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AssetAuthoringCapability } from "@/lib/types";
import {
  clipboardSeedFormatLabel,
  detectClipboardSeedFormat,
  prepareClipboardSeed,
  type ClipboardSeedFormat,
} from "@/lib/seed-clipboard";

import { FilePathPicker } from "./file-path-picker";

export type SemanticAssetKind = "seed" | "sensor";
export type SeedSourceMode = "upload" | "clipboard" | "path" | "url";

export type SemanticAssetDraft = {
  assetType: string;
  connection: string;
  seedSource: SeedSourceMode;
  seedFile: File | null;
  seedClipboardText: string;
  seedClipboardFormat: ClipboardSeedFormat;
  seedPath: string;
  seedFileType: string;
  enforceSchema: boolean;
  query: string;
  table: string;
  bucketName: string;
  bucketKey: string;
  pokeInterval: string;
  timeout: string;
};

export type SemanticAssetCreatePayload = {
  type: string;
  connection?: string;
  parameters: Record<string, string>;
  seedFile?: File;
};

const AUTO_CONNECTION_VALUE = "__auto__";
const INFER_FILE_TYPE_VALUE = "__infer__";

export function defaultSemanticAssetDraft(
  kind: SemanticAssetKind,
  capabilities: AssetAuthoringCapability[],
  connections: Record<string, string>,
): SemanticAssetDraft {
  const candidates = capabilities.filter((capability) => capability.kind === kind);
  const configuredTypes = new Set(Object.values(connections));
  const preferredType = kind === "seed" ? "duckdb.seed" : "duckdb.sensor.query";
  const capability =
    candidates.find(
      (candidate) =>
        candidate.type === preferredType &&
        candidate.connection_types.some((type) => configuredTypes.has(type)),
    ) ??
    candidates.find((candidate) =>
      candidate.connection_types.some((type) => configuredTypes.has(type)),
    ) ??
    candidates.find((candidate) => candidate.type === preferredType) ??
    candidates[0];

  return {
    assetType: capability?.type ?? "",
    connection: "",
    seedSource: "upload",
    seedFile: null,
    seedClipboardText: "",
    seedClipboardFormat: "auto",
    seedPath: "",
    seedFileType: "",
    enforceSchema: (capability?.default_parameters?.enforce_schema ?? "true") !== "false",
    query: "",
    table: "",
    bucketName: "",
    bucketKey: "",
    pokeInterval: capability?.default_parameters?.poke_interval ?? "30",
    timeout: capability?.default_parameters?.timeout ?? "24h",
  };
}

export function buildSemanticAssetCreatePayload(
  kind: SemanticAssetKind,
  draft: SemanticAssetDraft,
  capabilities: AssetAuthoringCapability[],
  assetName = "",
): { payload?: SemanticAssetCreatePayload; error?: string } {
  const capability = capabilities.find(
    (candidate) => candidate.kind === kind && candidate.type === draft.assetType,
  );
  if (!capability) {
    return { error: `Choose a supported ${kind} type.` };
  }

  if (kind === "seed") {
    if (draft.seedSource === "upload" && !draft.seedFile) {
      return { error: "Choose a seed file to upload." };
    }
    if (draft.seedSource === "clipboard" && !draft.seedClipboardText.trim()) {
      return { error: "Paste seed data from the clipboard." };
    }
    if (
      draft.seedSource !== "upload" &&
      draft.seedSource !== "clipboard" &&
      !draft.seedPath.trim()
    ) {
      return {
        error:
          draft.seedSource === "url"
            ? "Enter the seed file URL."
            : "Choose a seed file from the workspace.",
      };
    }
    let clipboardFile: File | undefined;
    let clipboardFileType = "";
    if (draft.seedSource === "clipboard") {
      try {
        const prepared = prepareClipboardSeed(
          draft.seedClipboardText,
          draft.seedClipboardFormat,
          assetName.split(".").at(-1),
        );
        clipboardFile = new File([prepared.content], prepared.fileName, {
          type: prepared.mimeType,
        });
        clipboardFileType = prepared.fileType;
      } catch (cause) {
        return { error: cause instanceof Error ? cause.message : "Could not prepare pasted data." };
      }
    }
    return {
      payload: {
        type: capability.type,
        connection: draft.connection || undefined,
        parameters: {
          ...(draft.seedSource === "upload" || draft.seedSource === "clipboard"
            ? {}
            : draft.seedSource === "path"
              ? { workspace_path: draft.seedPath.trim() }
              : { path: draft.seedPath.trim() }),
          ...(clipboardFileType
            ? { file_type: clipboardFileType }
            : draft.seedFileType
              ? { file_type: draft.seedFileType }
              : {}),
          enforce_schema: String(draft.enforceSchema),
        },
        ...(draft.seedSource === "upload" && draft.seedFile ? { seedFile: draft.seedFile } : {}),
        ...(clipboardFile ? { seedFile: clipboardFile } : {}),
      },
    };
  }

  const parameterValues: Record<string, string> = {
    query: draft.query.trim(),
    table: draft.table.trim(),
    bucket_name: draft.bucketName.trim(),
    bucket_key: draft.bucketKey.trim(),
  };
  const missing = (capability.required_parameters ?? []).find(
    (parameter) => !parameterValues[parameter],
  );
  if (missing) {
    return { error: `${sensorParameterLabel(missing)} is required.` };
  }
  return {
    payload: {
      type: capability.type,
      connection: draft.connection || undefined,
      parameters: {
        ...Object.fromEntries(
          Object.entries(parameterValues).filter(([, value]) => Boolean(value)),
        ),
        poke_interval: draft.pokeInterval.trim() || "30",
        timeout: draft.timeout.trim() || "24h",
      },
    },
  };
}

export function SemanticAssetCreateFields({
  kind,
  capabilities,
  connections,
  value,
  onChange,
}: {
  kind: SemanticAssetKind;
  capabilities: AssetAuthoringCapability[];
  connections: Record<string, string>;
  value: SemanticAssetDraft;
  onChange: (value: SemanticAssetDraft) => void;
}) {
  const candidates = capabilities.filter((capability) => capability.kind === kind);
  const selected = candidates.find((capability) => capability.type === value.assetType);
  const configuredTypes = new Set(Object.values(connections));
  const configured = candidates.filter((capability) =>
    capability.connection_types.some((type) => configuredTypes.has(type)),
  );
  const other = candidates.filter((capability) => !configured.includes(capability));
  const compatibleConnections = selected
    ? Object.entries(connections)
        .filter(([, type]) => selected.connection_types.includes(type))
        .map(([name]) => name)
        .sort((left, right) => left.localeCompare(right))
    : [];

  const set = (patch: Partial<SemanticAssetDraft>) => onChange({ ...value, ...patch });
  const [clipboardError, setClipboardError] = useState("");
  const detectedClipboardFormat = value.seedClipboardText
    ? detectClipboardSeedFormat(value.seedClipboardText)
    : null;

  return (
    <FieldGroup className="min-w-0">
      <Field variant="plain">
        <FieldLabel htmlFor="new-semantic-asset-type">
          {kind === "seed" ? "Seed type" : "Sensor type"}
        </FieldLabel>
        <Select
          value={value.assetType}
          onValueChange={(assetType) => {
            const nextCapability = candidates.find((capability) => capability.type === assetType);
            const connectionCompatible = nextCapability
              ? nextCapability.connection_types.includes(connections[value.connection])
              : false;
            set({ assetType, ...(connectionCompatible ? {} : { connection: "" }) });
          }}
        >
          <SelectTrigger id="new-semantic-asset-type">
            <SelectValue placeholder={`Choose a ${kind} type`} />
          </SelectTrigger>
          <SelectContent>
            {configured.length > 0 ? (
              <SelectGroup>
                <SelectLabel>Configured platforms</SelectLabel>
                {configured.map((capability) => (
                  <SelectItem key={capability.type} value={capability.type}>
                    {semanticAssetTypeLabel(capability)}
                  </SelectItem>
                ))}
              </SelectGroup>
            ) : null}
            {other.length > 0 ? (
              <SelectGroup>
                <SelectLabel>Other platforms</SelectLabel>
                {other.map((capability) => (
                  <SelectItem key={capability.type} value={capability.type}>
                    {semanticAssetTypeLabel(capability)}
                  </SelectItem>
                ))}
              </SelectGroup>
            ) : null}
          </SelectContent>
        </Select>
        <FieldDescription>
          Renart only lists asset types supported by the embedded Bruin runtime.
        </FieldDescription>
      </Field>

      <Field variant="plain">
        <FieldLabel htmlFor="new-semantic-connection">Connection</FieldLabel>
        <Select
          value={value.connection || AUTO_CONNECTION_VALUE}
          onValueChange={(connection) =>
            set({ connection: connection === AUTO_CONNECTION_VALUE ? "" : connection })
          }
        >
          <SelectTrigger id="new-semantic-connection">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value={AUTO_CONNECTION_VALUE}>Auto (pipeline default)</SelectItem>
              {compatibleConnections.map((connection) => (
                <SelectItem key={connection} value={connection}>
                  {connection}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <FieldDescription>
          {compatibleConnections.length > 0
            ? "Only connections compatible with the selected asset type are shown."
            : "No compatible explicit connection is configured; Auto uses the pipeline default."}
        </FieldDescription>
      </Field>

      {kind === "seed" && selected ? (
        <>
          <Field variant="plain">
            <FieldLabel>Seed source</FieldLabel>
            <ToggleGroup
              type="single"
              variant="outline"
              value={value.seedSource}
              onValueChange={(seedSource) => {
                if (seedSource) set({ seedSource: seedSource as SeedSourceMode });
              }}
              className="grid w-full min-w-0 grid-cols-2 sm:grid-cols-4"
            >
              <ToggleGroupItem value="upload" className="w-full">
                Upload
              </ToggleGroupItem>
              <ToggleGroupItem value="clipboard" className="w-full">
                Paste
              </ToggleGroupItem>
              <ToggleGroupItem value="path" className="w-full">
                Workspace
              </ToggleGroupItem>
              <ToggleGroupItem value="url" className="w-full">
                URL
              </ToggleGroupItem>
            </ToggleGroup>
          </Field>
          {value.seedSource === "upload" ? (
            <Field variant="plain">
              <FieldLabel htmlFor="new-seed-file">File</FieldLabel>
              <Input
                id="new-seed-file"
                type="file"
                accept={(selected.file_types ?? []).map((type) => `.${type}`).join(",")}
                onChange={(event) => set({ seedFile: event.target.files?.[0] ?? null })}
              />
              <FieldDescription>
                The file is copied beside the asset definition and removed with the asset.
              </FieldDescription>
            </Field>
          ) : value.seedSource === "clipboard" ? (
            <>
              <Field variant="plain">
                <div className="flex items-center justify-between gap-2">
                  <FieldLabel htmlFor="new-seed-clipboard">Pasted data</FieldLabel>
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    onClick={async () => {
                      setClipboardError("");
                      try {
                        const seedClipboardText = await navigator.clipboard.readText();
                        if (!seedClipboardText.trim()) {
                          setClipboardError("The clipboard is empty.");
                          return;
                        }
                        set({ seedClipboardText, seedClipboardFormat: "auto" });
                      } catch {
                        setClipboardError(
                          "Clipboard access was blocked. Paste directly into the text area instead.",
                        );
                      }
                    }}
                  >
                    <ClipboardPaste data-icon="inline-start" />
                    Paste from clipboard
                  </Button>
                </div>
                <Textarea
                  id="new-seed-clipboard"
                  className="min-h-28 resize-y font-mono text-xs"
                  value={value.seedClipboardText}
                  onChange={(event) => {
                    setClipboardError("");
                    set({ seedClipboardText: event.target.value });
                  }}
                />
                {clipboardError ? (
                  <FieldDescription className="text-destructive">{clipboardError}</FieldDescription>
                ) : (
                  <FieldDescription>
                    The pasted data is copied beside the asset definition as a seed file.
                  </FieldDescription>
                )}
              </Field>
              <Field variant="plain">
                <FieldLabel htmlFor="new-seed-clipboard-format">Pasted format</FieldLabel>
                <Select
                  value={value.seedClipboardFormat}
                  onValueChange={(seedClipboardFormat) =>
                    set({ seedClipboardFormat: seedClipboardFormat as ClipboardSeedFormat })
                  }
                >
                  <SelectTrigger id="new-seed-clipboard-format">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="auto">
                        Auto
                        {detectedClipboardFormat
                          ? ` (${clipboardSeedFormatLabel(detectedClipboardFormat)})`
                          : ""}
                      </SelectItem>
                      <SelectItem value="csv">CSV</SelectItem>
                      <SelectItem value="tsv">TSV (convert to CSV)</SelectItem>
                      <SelectItem value="json">JSON</SelectItem>
                      <SelectItem value="jsonl">JSON Lines</SelectItem>
                      <SelectItem value="text">Plain text (one column)</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            </>
          ) : (
            <Field variant="plain">
              <FieldLabel htmlFor="new-seed-path">
                {value.seedSource === "url" ? "File URL" : "Relative path"}
              </FieldLabel>
              {value.seedSource === "url" ? (
                <Input
                  id="new-seed-path"
                  className="font-mono"
                  type="url"
                  placeholder="https://example.com/customers.csv"
                  value={value.seedPath}
                  onChange={(event) => set({ seedPath: event.target.value })}
                />
              ) : (
                <FilePathPicker
                  id="new-seed-path"
                  variant="field"
                  ariaLabel="Choose workspace seed file"
                  placeholder="Choose a workspace file…"
                  value={value.seedPath}
                  onCommit={(seedPath) => set({ seedPath })}
                  workspaceOnly
                  fileExtensions={selected.file_types}
                />
              )}
              <FieldDescription>
                {value.seedSource === "url"
                  ? "Sling fetches this HTTP or HTTPS URL when the seed runs."
                  : "Choose a file already in this workspace. Renart stores its path relative to the new asset definition."}
              </FieldDescription>
            </Field>
          )}
          {value.seedSource !== "clipboard" ? (
            <Field variant="plain">
              <FieldLabel htmlFor="new-seed-file-type">File format</FieldLabel>
              <Select
                value={value.seedFileType || INFER_FILE_TYPE_VALUE}
                onValueChange={(fileType) =>
                  set({ seedFileType: fileType === INFER_FILE_TYPE_VALUE ? "" : fileType })
                }
              >
                <SelectTrigger id="new-seed-file-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value={INFER_FILE_TYPE_VALUE}>Infer from extension</SelectItem>
                    {(selected.file_types ?? []).map((fileType) => (
                      <SelectItem key={fileType} value={fileType}>
                        {fileType}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          <Field orientation="horizontal" variant="plain" className="min-w-0">
            <FieldContent className="min-w-0">
              <FieldLabel htmlFor="new-seed-enforce-schema">Enforce schema</FieldLabel>
              <FieldDescription>Reject rows that do not match declared columns.</FieldDescription>
            </FieldContent>
            <Switch
              className="shrink-0"
              id="new-seed-enforce-schema"
              checked={value.enforceSchema}
              onCheckedChange={(enforceSchema) => set({ enforceSchema })}
            />
          </Field>
        </>
      ) : null}

      {kind === "sensor" && selected ? (
        <>
          {(selected.required_parameters ?? []).includes("query") ? (
            <Field variant="plain">
              <FieldLabel htmlFor="new-sensor-query">Ready condition query</FieldLabel>
              <Textarea
                id="new-sensor-query"
                className="min-h-24 font-mono"
                placeholder="select count(*) > 0 from analytics.orders"
                value={value.query}
                onChange={(event) => set({ query: event.target.value })}
              />
              <FieldDescription>The query must return a truthy first value.</FieldDescription>
            </Field>
          ) : null}
          {(selected.required_parameters ?? []).includes("table") ? (
            <Field variant="plain">
              <FieldLabel htmlFor="new-sensor-table">Table</FieldLabel>
              <Input
                id="new-sensor-table"
                className="font-mono"
                placeholder="analytics.orders"
                value={value.table}
                onChange={(event) => set({ table: event.target.value })}
              />
            </Field>
          ) : null}
          {(selected.required_parameters ?? []).includes("bucket_name") ? (
            <FieldGroup className="sm:grid-cols-2">
              <Field variant="plain">
                <FieldLabel htmlFor="new-sensor-bucket">Bucket</FieldLabel>
                <Input
                  id="new-sensor-bucket"
                  className="font-mono"
                  placeholder="raw-data"
                  value={value.bucketName}
                  onChange={(event) => set({ bucketName: event.target.value })}
                />
              </Field>
              <Field variant="plain">
                <FieldLabel htmlFor="new-sensor-key">Object key</FieldLabel>
                <Input
                  id="new-sensor-key"
                  className="font-mono"
                  placeholder="daily/orders.csv"
                  value={value.bucketKey}
                  onChange={(event) => set({ bucketKey: event.target.value })}
                />
              </Field>
            </FieldGroup>
          ) : null}
          <FieldGroup className="sm:grid-cols-2">
            <Field variant="plain">
              <FieldLabel htmlFor="new-sensor-poke-interval">Check every</FieldLabel>
              <Input
                id="new-sensor-poke-interval"
                type="number"
                min={1}
                inputMode="numeric"
                value={value.pokeInterval}
                onChange={(event) => set({ pokeInterval: event.target.value })}
              />
              <FieldDescription>Seconds between checks in scheduled runs.</FieldDescription>
            </Field>
            <Field variant="plain">
              <FieldLabel htmlFor="new-sensor-timeout">Timeout</FieldLabel>
              <Input
                id="new-sensor-timeout"
                className="font-mono"
                placeholder="24h"
                value={value.timeout}
                onChange={(event) => set({ timeout: event.target.value })}
              />
              <FieldDescription>Positive duration, for example 30m or 24h.</FieldDescription>
            </Field>
          </FieldGroup>
        </>
      ) : null}
    </FieldGroup>
  );
}

function semanticAssetTypeLabel(capability: AssetAuthoringCapability) {
  const [provider] = capability.type.split(".");
  const providerLabel =
    {
      bq: "BigQuery",
      ms: "SQL Server",
      my: "MySQL",
      pg: "Postgres",
      rs: "Redshift",
      sf: "Snowflake",
      s3: "Amazon S3",
    }[provider] ?? provider.charAt(0).toUpperCase() + provider.slice(1);
  return capability.kind === "seed"
    ? `${providerLabel} · ${capability.type}`
    : `${providerLabel} ${capability.variant} · ${capability.type}`;
}

function sensorParameterLabel(parameter: string) {
  return (
    {
      query: "Ready condition query",
      table: "Table",
      bucket_name: "Bucket",
      bucket_key: "Object key",
    }[parameter] ?? parameter
  );
}
