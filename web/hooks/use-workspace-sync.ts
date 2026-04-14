"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useEffect } from "react";

import {
  workspaceAtom,
  workspaceSyncSourceAtom,
} from "@/lib/atoms/domains/workspace";
import { getWorkspace } from "@/lib/api";
import { WebAsset, WorkspaceEvent, WorkspaceState } from "@/lib/types";

function mergeWorkspaceWithPreservedContent(
  current: WorkspaceState | null,
  incoming: WorkspaceState,
  changedAssetIds: string[] = []
): WorkspaceState {
  if (!current) {
    return incoming;
  }

  const changedAssetIdSet = new Set(changedAssetIds);
  const currentAssetById = new Map<string, WebAsset>();
  for (const pipeline of current.pipelines ?? []) {
    for (const asset of pipeline.assets ?? []) {
      if (asset.id) {
        currentAssetById.set(asset.id, asset);
      }
    }
  }

  const nextPipelines = (incoming.pipelines ?? []).map((pipeline) => ({
    ...pipeline,
    assets: (pipeline.assets ?? []).map((asset) => {
      const currentAsset = currentAssetById.get(asset.id);
      if (!currentAsset) {
        return asset;
      }

      const isChangedAsset = changedAssetIdSet.has(asset.id);

      return {
        ...currentAsset,
        ...asset,
        content: asset.content || currentAsset.content,
        meta: asset.meta ?? (isChangedAsset ? asset.meta : currentAsset.meta),
        columns:
          asset.columns ??
          (isChangedAsset ? asset.columns : currentAsset.columns),
      };
    }),
  }));

  return {
    ...incoming,
    pipelines: nextPipelines,
  };
}

export function useWorkspaceSync() {
  const workspace = useAtomValue(workspaceAtom);
  const setWorkspace = useSetAtom(workspaceAtom);
  const setWorkspaceSyncSource = useSetAtom(workspaceSyncSourceAtom);

  useEffect(() => {
    let mounted = true;

    getWorkspace()
      .then((data) => {
        if (mounted) {
          setWorkspace(data);
          setWorkspaceSyncSource({
            method: "workspace-load",
            recordedAt: new Date().toISOString(),
            revision: data.revision,
          });
        }
      })
      .catch(() => undefined);

    const source = new EventSource("/api/events");
    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as WorkspaceEvent;

        setWorkspaceSyncSource({
          method: "workspace-event",
          recordedAt: new Date().toISOString(),
          revision: payload.workspace?.revision,
          eventType: payload.type,
          eventPath: payload.path,
          lite: payload.lite,
          changedAssetIds: payload.changed_asset_ids,
        });

        setWorkspace((current) => {
          const currentRevision = current?.revision ?? -1;
          const incomingRevision =
            payload.workspace?.revision ?? currentRevision + 1;

          if (incomingRevision <= currentRevision) {
            return current;
          }

          if (payload.lite) {
            return mergeWorkspaceWithPreservedContent(
              current,
              payload.workspace,
              payload.changed_asset_ids ?? []
            );
          }

          return payload.workspace;
        });
      } catch {
        return;
      }
    };

    return () => {
      mounted = false;
      source.close();
    };
  }, [setWorkspace, setWorkspaceSyncSource]);

  return workspace as WorkspaceState | null;
}
