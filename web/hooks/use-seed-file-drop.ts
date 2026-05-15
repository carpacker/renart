"use client";

import { useCallback, useState } from "react";
import type { Dispatch, DragEvent, SetStateAction } from "react";
import type { ReactFlowInstance } from "reactflow";

import type { StoredNodePositions } from "@/hooks/use-persisted-node-positions";
import type { WebPipeline } from "@/lib/types";

type CreateAssetInput = {
  name?: string;
  type?: string;
  path?: string;
  content?: string;
  source_asset_id?: string;
  seed_file_name?: string;
  seed_file_content?: string;
};

type UseSeedFileDropInput = {
  pipeline: WebPipeline | null | undefined;
  pipelineId: string | null;
  reactFlowInstance: ReactFlowInstance | null;
  defaultSeedAssetType?: string;
  runCreateAsset: (
    pipelineId: string,
    input: CreateAssetInput
  ) => Promise<{ asset_id?: string } | null>;
  setStoredNodePositions: Dispatch<SetStateAction<StoredNodePositions>>;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
};

export function useSeedFileDrop({
  pipeline,
  pipelineId,
  reactFlowInstance,
  defaultSeedAssetType = "duckdb.seed",
  runCreateAsset,
  setStoredNodePositions,
  navigateSelection,
}: UseSeedFileDropInput) {
  const [seedDropActive, setSeedDropActive] = useState(false);

  const handleCanvasDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (!event.dataTransfer || !hasCSVFile(event.dataTransfer)) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    setSeedDropActive(true);
  }, []);

  const handleCanvasDragLeave = useCallback((event: DragEvent<HTMLDivElement>) => {
    const relatedTarget = event.relatedTarget;
    if (relatedTarget instanceof globalThis.Node && event.currentTarget.contains(relatedTarget)) {
      return;
    }

    setSeedDropActive(false);
  }, []);

  const handleCanvasDrop = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      if (!pipelineId || !pipeline || !event.dataTransfer || !hasCSVFile(event.dataTransfer)) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      setSeedDropActive(false);

      const file = Array.from(event.dataTransfer.files).find(isCSVFile);
      if (!file) {
        return;
      }

      const flowPosition = reactFlowInstance?.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });
      const draftPosition = flowPosition ?? { x: 32, y: 32 };
      const prefix = slugifySeedPart(pipeline.name || "analytics");
      const existingNames = new Set((pipeline.assets ?? []).map((asset) => asset.name));
      const leaf = uniqueSeedLeaf(file.name, existingNames, prefix);
      const seedFileName = `${leaf}.csv`;
      const assetName = `${prefix}.${leaf}`;

      void file.text().then((seedFileContent) => {
        void runCreateAsset(pipelineId, {
          name: assetName,
          type: defaultSeedAssetType,
          path: `assets/${prefix}/${leaf}.asset.yml`,
          content: buildSeedAssetContent(assetName, defaultSeedAssetType, seedFileName),
          seed_file_name: seedFileName,
          seed_file_content: seedFileContent,
        }).then((response) => {
          if (response?.asset_id) {
            setStoredNodePositions((previous) => ({
              ...previous,
              [response.asset_id as string]: draftPosition,
            }));
            navigateSelection(pipelineId, response.asset_id);
          }
        });
      });
    },
    [
      defaultSeedAssetType,
      navigateSelection,
      pipeline,
      pipelineId,
      reactFlowInstance,
      runCreateAsset,
      setStoredNodePositions,
    ]
  );

  return {
    handleCanvasDragOver,
    handleCanvasDragLeave,
    handleCanvasDrop,
    seedDropActive,
  };
}

function hasCSVFile(dataTransfer: DataTransfer) {
  return (
    Array.from(dataTransfer.items ?? []).some((item) => {
      if (item.kind !== "file") {
        return false;
      }
      const file = item.getAsFile();
      return file ? isCSVFile(file) : item.type === "text/csv";
    }) || Array.from(dataTransfer.files ?? []).some(isCSVFile)
  );
}

function isCSVFile(file: File) {
  return file.name.toLowerCase().endsWith(".csv") || file.type === "text/csv";
}

function uniqueSeedLeaf(fileName: string, existingNames: Set<string>, prefix: string) {
  const baseName = fileName.replace(/\.[^.]+$/, "");
  const baseLeaf = slugifySeedPart(baseName || "seed");
  let leaf = baseLeaf;
  let index = 2;
  while (existingNames.has(`${prefix}.${leaf}`)) {
    leaf = `${baseLeaf}_${index}`;
    index += 1;
  }
  return leaf;
}

function slugifySeedPart(value: string) {
  return (
    value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "") || "seed"
  );
}

function buildSeedAssetContent(assetName: string, assetType: string, seedFileName: string) {
  return `name: ${assetName}\ntype: ${assetType}\n\nparameters:\n  path: ./${seedFileName}\n`;
}
