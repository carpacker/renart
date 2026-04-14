"use client";

import { useAtomValue } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";

import {
  createAsset,
  createPipeline,
  deleteAsset,
  deletePipeline,
  updatePipeline,
  updateAsset,
} from "@/lib/api";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import { normalizeAssetName } from "@/lib/workspace-shell-helpers";

export type UIMessage = {
  type: "success" | "error";
  text: string;
};

type CreateAssetInput = {
  name?: string;
  type?: string;
  path?: string;
  content?: string;
  source_asset_id?: string;
};

type UpdateAssetInput = {
  name?: string;
  type?: string;
  content?: string;
  materialization_type?: string;
  meta?: Record<string, string>;
  upstreams?: string[];
};

export function useAssetActions(defaultPipelinePath = "my-pipeline") {
  const workspace = useAtomValue(workspaceAtom);
  const [createPipelineDialogOpen, setCreatePipelineDialogOpen] =
    useState(false);
  const [createPipelinePath, setCreatePipelinePath] =
    useState(defaultPipelinePath);
  const [createPipelineLoading, setCreatePipelineLoading] = useState(false);
  const [uiMessage, setUIMessage] = useState<UIMessage | null>(null);
  const uiMessageTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const pushUIMessage = useCallback((type: UIMessage["type"], text: string) => {
    if (uiMessageTimerRef.current) {
      clearTimeout(uiMessageTimerRef.current);
    }

    setUIMessage({ type, text });
    uiMessageTimerRef.current = setTimeout(() => {
      setUIMessage(null);
      uiMessageTimerRef.current = null;
    }, 4500);
  }, []);

  useEffect(() => {
    return () => {
      if (uiMessageTimerRef.current) {
        clearTimeout(uiMessageTimerRef.current);
      }
    };
  }, []);

  const getConflictingAssetName = useCallback(
    (name: string, options?: { excludeAssetId?: string }) => {
      const normalizedName = normalizeAssetName(name);
      if (!normalizedName) {
        return null;
      }

      for (const pipeline of workspace?.pipelines ?? []) {
        for (const asset of pipeline.assets ?? []) {
          if (asset.id === options?.excludeAssetId) {
            continue;
          }

          if (normalizeAssetName(asset.name) === normalizedName) {
            return asset.name;
          }
        }
      }

      return null;
    },
    [workspace?.pipelines]
  );

  const openCreatePipelineDialog = useCallback(() => {
    if (createPipelineLoading) {
      return;
    }

    setCreatePipelinePath((current) => current || defaultPipelinePath);
    setCreatePipelineDialogOpen(true);
  }, [createPipelineLoading, defaultPipelinePath]);

  const confirmCreatePipeline = useCallback(async () => {
    const path = createPipelinePath.trim();
    if (!path || createPipelineLoading) {
      if (!path) {
        pushUIMessage("error", "Pipeline path is required.");
      }
      return false;
    }

    setCreatePipelineLoading(true);
    try {
      await createPipeline({ path, name: path });
      setCreatePipelineDialogOpen(false);
      pushUIMessage("success", `Pipeline "${path}" created.`);
      return true;
    } catch (error) {
      pushUIMessage("error", `Failed to create pipeline: ${String(error)}`);
      return false;
    } finally {
      setCreatePipelineLoading(false);
    }
  }, [createPipelineLoading, createPipelinePath, pushUIMessage]);

  const runCreateAsset = useCallback(
    async (pipelineId: string, input: CreateAssetInput) => {
      const trimmedName = input.name?.trim();
      if (trimmedName) {
        const conflictingAssetName = getConflictingAssetName(trimmedName);
        if (conflictingAssetName) {
          pushUIMessage(
            "error",
            `Asset "${trimmedName}" already exists. Choose a different name.`
          );
          return null;
        }
      }

      try {
        return await createAsset(pipelineId, {
          ...input,
          name: trimmedName ?? input.name,
        });
      } catch (error) {
        pushUIMessage("error", `Failed to create asset: ${String(error)}`);
        return null;
      }
    },
    [getConflictingAssetName, pushUIMessage]
  );

  const runDeleteAsset = useCallback(
    async (pipelineId: string, assetId: string) => {
      try {
        await deleteAsset(pipelineId, assetId);
        return true;
      } catch (error) {
        pushUIMessage("error", `Failed to delete asset: ${String(error)}`);
        return false;
      }
    },
    [pushUIMessage]
  );

  const runDeletePipeline = useCallback(
    async (pipelineId: string) => {
      try {
        await deletePipeline(pipelineId);
        return true;
      } catch (error) {
        pushUIMessage("error", `Failed to delete pipeline: ${String(error)}`);
        return false;
      }
    },
    [pushUIMessage]
  );

  const runUpdatePipeline = useCallback(
    async (pipelineId: string, input: { name?: string; content?: string }) => {
      const trimmedName = input.name?.trim();

      try {
        await updatePipeline({
          id: pipelineId,
          name: trimmedName ?? input.name,
          content: input.content,
        });

        if (trimmedName) {
          pushUIMessage("success", `Pipeline "${trimmedName}" saved.`);
        }

        return true;
      } catch (error) {
        pushUIMessage("error", `Failed to update pipeline: ${String(error)}`);
        return false;
      }
    },
    [pushUIMessage]
  );

  const runUpdateAsset = useCallback(
    async (pipelineId: string, assetId: string, input: UpdateAssetInput) => {
      const trimmedName = input.name?.trim();
      if (trimmedName) {
        const conflictingAssetName = getConflictingAssetName(trimmedName, {
          excludeAssetId: assetId,
        });
        if (conflictingAssetName) {
          pushUIMessage(
            "error",
            `Asset "${trimmedName}" already exists. Choose a different name.`
          );
          return false;
        }
      }

      try {
        await updateAsset(pipelineId, assetId, {
          ...input,
          name: trimmedName ?? input.name,
        });
        return true;
      } catch (error) {
        pushUIMessage("error", `Failed to update asset: ${String(error)}`);
        return false;
      }
    },
    [getConflictingAssetName, pushUIMessage]
  );

  return {
    uiMessage,
    pushUIMessage,
    createPipelineDialogOpen,
    setCreatePipelineDialogOpen,
    createPipelinePath,
    setCreatePipelinePath,
    createPipelineLoading,
    openCreatePipelineDialog,
    confirmCreatePipeline,
    runCreateAsset,
    runDeleteAsset,
    runDeletePipeline,
    runUpdatePipeline,
    runUpdateAsset,
  };
}
