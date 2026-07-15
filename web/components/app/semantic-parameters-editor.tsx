"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import { useAtomValue } from "jotai";
import { CircleAlert } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { updateAsset } from "@/lib/api-assets";
import { getAssetAuthoringCapability, isSeedAssetType, isSensorAssetType } from "@/lib/asset-types";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { WebAsset } from "@/lib/types";

import { Comment, InlineSelect, InlineText, Key, Line } from "./asset-yaml-editor";

const DEFAULT_SEED_FILE_TYPES = ["csv", "parquet", "json", "jsonl", "ndjson", "avro"];

/**
 * Main-pane editor for the dedicated seed and sensor runtime parameters. It
 * follows the Load editor's compact YAML-like presentation while keeping
 * generic metadata in Properties and avoiding a raw Monaco surface for assets
 * whose executable intent is fully structured.
 */
export function SemanticParametersEditor({
  asset,
  pipelineId,
}: {
  asset: WebAsset;
  pipelineId: string;
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

  useEffect(() => {
    if (pendingUpdatesRef.current > 0) return;
    const next = asset.parameters ?? {};
    parametersRef.current = next;
    setParameters(next);
  }, [asset.parameters]);

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
    setError("");
    pendingUpdatesRef.current += 1;
    updateQueueRef.current = updateQueueRef.current
      .catch(() => undefined)
      .then(() => updateAsset(pipelineId, asset.id, { parameters: next }))
      .catch((cause: unknown) => {
        setError(cause instanceof Error ? cause.message : "Could not update parameters");
      })
      .finally(() => {
        pendingUpdatesRef.current = Math.max(0, pendingUpdatesRef.current - 1);
      });
  };

  const required = useMemo(
    () => new Set(capability?.required_parameters ?? []),
    [capability?.required_parameters],
  );
  const variant = capability?.variant ?? asset.type.split(".sensor.")[1] ?? "";
  const usesQuery = required.has("query") || variant === "query";
  const usesTable = required.has("table") || variant === "table";
  const usesS3Key = required.has("bucket_name") || required.has("bucket_key") || variant === "key";
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

  if (!isSeed && !isSensor) return null;

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

      <Separator className="mt-3" />
      <div className="flex flex-col gap-0.5 pt-2">
        {isSensor ? (
          <Comment>Manual runs check once; scheduled runs repeat until ready or timed out.</Comment>
        ) : (
          <Comment>Columns and checks are configured in Properties.</Comment>
        )}
      </div>
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
