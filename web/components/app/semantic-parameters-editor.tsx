"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAtomValue } from "jotai";
import { CircleAlert, CloudUpload, FileWarning, Upload } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupText,
  InputGroupTextarea,
} from "@/components/ui/input-group";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { refreshAssetColumnsFromDefinition } from "@/lib/api-asset-transactions";
import { replaceSeedAssetFile, updateAsset } from "@/lib/api-assets";
import {
  getAssetAuthoringCapability,
  isQuerySensorAssetType,
  isSeedAssetType,
  isSensorAssetType,
} from "@/lib/asset-types";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { WebAsset } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  clipboardSeedFileStem,
  clipboardSeedFormatLabel,
  detectClipboardSeedFormat,
  prepareClipboardSeed,
  type ClipboardSeedFormat,
} from "@/lib/seed-clipboard";

import { Comment, InlineSelect, InlineText, Key, Line } from "./asset-yaml-editor";
import { SensorQueryEditor } from "./sensor-query-editor";

const DEFAULT_SEED_FILE_TYPES = ["csv", "parquet", "json", "jsonl", "ndjson", "avro"];

/**
 * Main-pane editor for dedicated seed and sensor runtime parameters. Structured
 * variants follow the Load editor's compact YAML-like presentation, while query
 * sensors project parameters.query into the shared SQL Monaco surface.
 */
export function SemanticParametersEditor({
  asset,
  pipelineId,
  onCheck,
  onGoToAsset,
}: {
  asset: WebAsset;
  pipelineId: string;
  onCheck?: () => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const isSeed = isSeedAssetType(asset.type);
  const isSensor = isSensorAssetType(asset.type);
  const capability = getAssetAuthoringCapability(asset.type, workspace?.asset_capabilities);
  const [parameters, setParameters] = useState<Record<string, string>>(
    () => asset.parameters ?? {},
  );
  const parametersRef = useRef(parameters);
  const pendingUpdatesRef = useRef(0);
  const updateQueueRef = useRef<Promise<unknown>>(Promise.resolve());
  const [error, setError] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadMessage, setUploadMessage] = useState("");

  useEffect(() => {
    if (pendingUpdatesRef.current > 0) return;
    const next = asset.parameters ?? {};
    parametersRef.current = next;
    setParameters(next);
  }, [asset.parameters]);

  const persistParameters = useCallback(
    (next: Record<string, string>) => {
      setError("");
      pendingUpdatesRef.current += 1;
      const request = updateQueueRef.current
        .catch(() => undefined)
        .then(() => updateAsset(pipelineId, asset.id, { parameters: next }));
      const tracked = request
        .catch((cause: unknown) => {
          setError(cause instanceof Error ? cause.message : "Could not update parameters");
          throw cause;
        })
        .finally(() => {
          pendingUpdatesRef.current = Math.max(0, pendingUpdatesRef.current - 1);
        });
      updateQueueRef.current = tracked.catch(() => undefined);
      return tracked;
    },
    [asset.id, pipelineId],
  );

  const saveParameter = (key: string, value: string) => {
    const next = { ...parametersRef.current };
    const trimmed = value.trim();
    if (trimmed) {
      next[key] = trimmed;
    } else {
      delete next[key];
    }
    if (next[key] === parametersRef.current[key]) return;

    parametersRef.current = next;
    setParameters(next);
    void persistParameters(next).catch(() => undefined);
  };

  const saveQuery = useCallback(
    async (query: string) => {
      const next = { ...parametersRef.current, query };
      parametersRef.current = next;
      setParameters(next);
      await persistParameters(next);
    },
    [persistParameters],
  );

  const required = useMemo(
    () => new Set(capability?.required_parameters ?? []),
    [capability?.required_parameters],
  );
  const variant = capability?.variant ?? asset.type.split(".sensor.")[1] ?? "";
  const usesQuery = required.has("query") || variant === "query";
  const usesTable = required.has("table") || variant === "table";
  const usesS3Key = required.has("bucket_name") || required.has("bucket_key") || variant === "key";
  const isQuerySensor = isQuerySensorAssetType(asset.type);
  const seedFileTypes = useMemo(
    () =>
      Array.from(
        new Set([parameters.file_type, ...(capability?.file_types ?? DEFAULT_SEED_FILE_TYPES)]),
      ).filter(Boolean),
    [capability?.file_types, parameters.file_type],
  );
  const explicitConnection = (asset.explicit_connection ?? "").trim();
  const connection =
    explicitConnection || (asset.connection ? `auto (${asset.connection})` : "auto");

  const uploadSeedFile = async (file: File) => {
    setUploading(true);
    setError("");
    setUploadMessage("");
    let uploaded = false;
    try {
      await updateQueueRef.current.catch(() => undefined);
      await replaceSeedAssetFile(asset.id, file);
      uploaded = true;
      await refreshAssetColumnsFromDefinition(asset.id);
      setUploadMessage(`${file.name} uploaded and columns refreshed.`);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "Could not upload the seed file";
      if (uploaded) {
        setUploadMessage(`${file.name} uploaded. Columns were not refreshed: ${message}`);
      } else {
        setError(message);
      }
    } finally {
      setUploading(false);
    }
  };

  if (!isSeed && !isSensor) return null;

  if (isQuerySensor) {
    return (
      <div
        className="flex min-h-0 flex-1 flex-col bg-background"
        data-testid="semantic-parameters-editor"
        data-asset-kind="sensor-query"
      >
        <SensorQueryEditor
          asset={asset}
          query={parameters.query ?? ""}
          onSave={saveQuery}
          onCheck={onCheck}
          onGoToAsset={onGoToAsset}
        />
        <div
          className="font-monaco shrink-0 border-t px-3 py-2 text-[13px] leading-6"
          data-testid="sensor-query-controls"
        >
          <Line>
            <Key>poke_interval</Key>
            <InlineText
              value={
                parameters.poke_interval ?? capability?.default_parameters?.poke_interval ?? "30"
              }
              placeholder="30"
              ariaLabel="Sensor check interval"
              onCommit={(value) => saveParameter("poke_interval", value)}
            />
          </Line>
          <Line>
            <Key>timeout</Key>
            <InlineText
              value={parameters.timeout ?? capability?.default_parameters?.timeout ?? "24h"}
              placeholder="24h"
              ariaLabel="Sensor timeout"
              onCommit={(value) => saveParameter("timeout", value)}
            />
          </Line>
          <Comment>Manual runs check once; scheduled runs repeat until ready or timed out.</Comment>
          {error ? (
            <Alert variant="destructive" className="mt-2">
              <CircleAlert />
              <AlertTitle>Could not update parameters</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div
      className="font-monaco min-h-0 flex-1 overflow-y-auto bg-background p-3 text-[13px] leading-6"
      data-testid="semantic-parameters-editor"
      data-asset-kind={isSeed ? "seed" : "sensor"}
    >
      {isSeed ? (
        <Comment>Seed assets load a local file or HTTP(S) URL through Sling.</Comment>
      ) : (
        <Comment>Sensor assets gate downstream work until their condition is ready.</Comment>
      )}
      <Comment>Edit the target connection and generic metadata in Properties.</Comment>
      <Line>
        <Key>type</Key>
        <span className="text-foreground">{asset.type}</span>
      </Line>
      <Line>
        <Key>connection</Key>
        <span className="truncate text-foreground">{connection}</span>
      </Line>
      <Line>
        <Key>parameters</Key>
      </Line>

      {isSeed ? (
        <>
          <Line depth={1}>
            <Key>path</Key>
            <InlineText
              value={parameters.path ?? ""}
              placeholder="./customers.csv or https://…"
              ariaLabel="Seed path"
              onCommit={(value) => saveParameter("path", value)}
            />
          </Line>
          <Line depth={1}>
            <Key>file_type</Key>
            <InlineSelect
              value={parameters.file_type ?? capability?.default_parameters?.file_type ?? "csv"}
              options={seedFileTypes.map((fileType) => ({ value: fileType, label: fileType }))}
              ariaLabel="Seed file format"
              onChange={(value) => saveParameter("file_type", value)}
            />
          </Line>
          <Line depth={1}>
            <Key>enforce_schema</Key>
            <InlineSelect
              value={
                parameters.enforce_schema ??
                capability?.default_parameters?.enforce_schema ??
                "true"
              }
              options={[
                { value: "true", label: "true" },
                { value: "false", label: "false" },
              ]}
              ariaLabel="Enforce seed schema"
              onChange={(value) => saveParameter("enforce_schema", value)}
            />
          </Line>
          <Separator className="my-3" />
          <SeedReplacementInput
            currentPath={parameters.path ?? ""}
            fileTypes={seedFileTypes}
            uploading={uploading}
            message={uploadMessage}
            onFile={(file) => void uploadSeedFile(file)}
          />
        </>
      ) : (
        <>
          {usesQuery ? (
            <Line depth={1} className="items-start">
              <Key>query</Key>
              <InlineParameterTextarea
                value={parameters.query ?? ""}
                placeholder="select count(*) > 0 from analytics.orders"
                ariaLabel="Ready condition query"
                onCommit={(value) => saveParameter("query", value)}
              />
            </Line>
          ) : null}
          {usesTable ? (
            <Line depth={1}>
              <Key>table</Key>
              <InlineText
                value={parameters.table ?? ""}
                placeholder="analytics.orders"
                ariaLabel="Sensor table"
                onCommit={(value) => saveParameter("table", value)}
              />
            </Line>
          ) : null}
          {usesS3Key ? (
            <>
              <Line depth={1}>
                <Key>bucket_name</Key>
                <InlineText
                  value={parameters.bucket_name ?? ""}
                  placeholder="raw-data"
                  ariaLabel="Sensor bucket"
                  onCommit={(value) => saveParameter("bucket_name", value)}
                />
              </Line>
              <Line depth={1}>
                <Key>bucket_key</Key>
                <InlineText
                  value={parameters.bucket_key ?? ""}
                  placeholder="daily/orders.csv"
                  ariaLabel="Sensor object key"
                  onCommit={(value) => saveParameter("bucket_key", value)}
                />
              </Line>
            </>
          ) : null}
          <Line depth={1}>
            <Key>poke_interval</Key>
            <InlineText
              value={
                parameters.poke_interval ?? capability?.default_parameters?.poke_interval ?? "30"
              }
              placeholder="30"
              ariaLabel="Sensor check interval"
              onCommit={(value) => saveParameter("poke_interval", value)}
            />
          </Line>
          <Line depth={1}>
            <Key>timeout</Key>
            <InlineText
              value={parameters.timeout ?? capability?.default_parameters?.timeout ?? "24h"}
              placeholder="24h"
              ariaLabel="Sensor timeout"
              onCommit={(value) => saveParameter("timeout", value)}
            />
          </Line>
        </>
      )}

      {isSensor ? (
        <>
          <Separator className="mt-3" />
          <div className="flex flex-col gap-0.5 pt-2">
            <Comment>
              Manual runs check once; scheduled runs repeat until ready or timed out.
            </Comment>
          </div>
        </>
      ) : null}
      {error ? (
        <Alert variant="destructive" className="mt-3">
          <CircleAlert />
          <AlertTitle>Could not update parameters</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}

function SeedReplacementInput({
  currentPath,
  fileTypes,
  uploading,
  message,
  onFile,
}: {
  currentPath: string;
  fileTypes: string[];
  uploading: boolean;
  message: string;
  onFile: (file: File) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [pendingMethod, setPendingMethod] = useState("");
  const [clipboardText, setClipboardText] = useState("");
  const [clipboardFormat, setClipboardFormat] = useState<ClipboardSeedFormat>("auto");
  const [clipboardError, setClipboardError] = useState("");
  const accept = fileTypes.map((fileType) => `.${fileType}`).join(",");
  const detectedClipboardFormat = clipboardText ? detectClipboardSeedFormat(clipboardText) : null;

  const reviewFile = (files: FileList | null, method: string) => {
    const file = files?.[0];
    if (!file) return;
    setPendingFile(file);
    setPendingMethod(method);
  };

  const saveClipboard = () => {
    setClipboardError("");
    try {
      const prepared = prepareClipboardSeed(
        clipboardText,
        clipboardFormat,
        clipboardSeedFileStem(currentPath),
      );
      setPendingFile(
        new File([prepared.content], prepared.fileName, {
          type: prepared.mimeType,
        }),
      );
      setPendingMethod(`pasted ${clipboardSeedFormatLabel(prepared.inputFormat)} data`);
    } catch (cause) {
      setClipboardError(cause instanceof Error ? cause.message : "Could not prepare pasted data.");
    }
  };

  return (
    <Field
      className="gap-1.5"
      data-disabled={uploading ? true : undefined}
      data-invalid={clipboardError ? true : undefined}
      data-testid="seed-replacement-input"
    >
      <FieldLabel htmlFor="seed-editor-data">Seed data</FieldLabel>
      <InputGroup
        data-testid="seed-file-drop-target"
        data-disabled={uploading ? true : undefined}
        className={cn("border-dashed transition-colors", dragging && "border-primary bg-accent")}
        onDragEnter={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragOver={(event) => {
          event.preventDefault();
          event.dataTransfer.dropEffect = "copy";
        }}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
            setDragging(false);
          }
        }}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          if (uploading) return;
          if (event.dataTransfer.files.length > 0) {
            reviewFile(event.dataTransfer.files, "the dropped file");
            return;
          }
          const text = event.dataTransfer.getData("text/plain");
          if (text) {
            setClipboardText(text);
            setClipboardError("");
          }
        }}
      >
        <InputGroupTextarea
          id="seed-editor-data"
          className="min-h-32 resize-y font-mono text-xs"
          value={clipboardText}
          disabled={uploading}
          aria-invalid={clipboardError ? true : undefined}
          onChange={(event) => {
            setClipboardText(event.target.value);
            setClipboardError("");
          }}
        />
        <InputGroupAddon align="block-start">
          <CloudUpload aria-hidden="true" />
          <InputGroupText>
            {dragging
              ? "Drop the seed file here"
              : currentPath
                ? `Paste data or drop a file to replace ${currentPath}`
                : "Paste data or drop a seed file"}
          </InputGroupText>
        </InputGroupAddon>
        <InputGroupAddon align="block-end" className="flex-wrap gap-1.5">
          <InputGroupText>Format</InputGroupText>
          <Select
            value={clipboardFormat}
            disabled={uploading}
            onValueChange={(value) => setClipboardFormat(value as ClipboardSeedFormat)}
          >
            <SelectTrigger aria-label="Pasted format" size="sm" className="w-40">
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
          <InputGroupButton
            variant="outline"
            disabled={uploading}
            onClick={() => inputRef.current?.click()}
          >
            {uploading ? <Spinner data-icon="inline-start" /> : <Upload data-icon="inline-start" />}
            {uploading ? "Uploading" : "Choose file"}
          </InputGroupButton>
          <InputGroupButton
            className="ml-auto"
            variant="default"
            disabled={uploading || !clipboardText.trim()}
            onClick={saveClipboard}
          >
            Save
          </InputGroupButton>
        </InputGroupAddon>
      </InputGroup>
      <input
        ref={inputRef}
        className="sr-only"
        type="file"
        accept={accept}
        aria-label="Upload seed file"
        disabled={uploading}
        onChange={(event) => {
          reviewFile(event.target.files, "the selected file");
          event.target.value = "";
        }}
      />
      {clipboardError ? (
        <FieldError>{clipboardError}</FieldError>
      ) : (
        <FieldDescription>
          Paste text and save, or drop or choose a file. You will confirm before replacing the
          current seed.
        </FieldDescription>
      )}
      {message ? <Comment>{message}</Comment> : null}

      <AlertDialog
        open={pendingFile !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingFile(null);
            setPendingMethod("");
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <FileWarning />
            </AlertDialogMedia>
            <AlertDialogTitle>Replace the seed file?</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingMethod ? `Using ${pendingMethod} will replace ` : "This will replace "}
              {currentPath || "the current seed content"}. The replacement will also refresh the
              inferred columns.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (pendingFile) onFile(pendingFile);
                setPendingFile(null);
                setPendingMethod("");
              }}
            >
              Replace file
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Field>
  );
}

function InlineParameterTextarea({
  value,
  placeholder,
  ariaLabel,
  onCommit,
}: {
  value: string;
  placeholder?: string;
  ariaLabel: string;
  onCommit: (value: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);

  return (
    <Textarea
      className="font-monaco min-h-28 min-w-0 flex-1 resize-y text-[13px] leading-5"
      value={draft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={() => onCommit(draft)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
          event.currentTarget.blur();
        } else if (event.key === "Escape") {
          setDraft(value);
          event.currentTarget.blur();
        }
      }}
    />
  );
}
