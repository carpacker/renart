"use client";

import { useAtomValue } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";

import { getAssetCreationProfile } from "@/lib/api-assets";
import { selectedEnvironmentAtom } from "@/lib/atoms/domains/workspace";
import type { AssetCreationProfile } from "@/lib/types";

export function useAssetCreationProfile(pipelineId: string | undefined, enabled = true) {
  const environment = useAtomValue(selectedEnvironmentAtom);
  const [profile, setProfile] = useState<AssetCreationProfile | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const requestRef = useRef(0);

  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      if (!pipelineId || !enabled) return null;
      const request = ++requestRef.current;
      setLoading(true);
      setError("");
      try {
        const next = await getAssetCreationProfile(pipelineId, environment ?? "", signal);
        if (request === requestRef.current && !signal?.aborted) setProfile(next);
        return next;
      } catch (cause) {
        if (request === requestRef.current && !signal?.aborted) {
          setError(
            cause instanceof Error ? cause.message : "Could not load compatible connections.",
          );
        }
        return null;
      } finally {
        if (request === requestRef.current && !signal?.aborted) setLoading(false);
      }
    },
    [enabled, environment, pipelineId],
  );

  useEffect(() => {
    if (!pipelineId || !enabled) {
      requestRef.current += 1;
      setProfile(null);
      setLoading(false);
      setError("");
      return;
    }
    setProfile(null);
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [enabled, pipelineId, refresh]);

  return { profile, loading, error, refresh };
}
