"use client";

import { useEffect, useState } from "react";

export type StoredNodePositions = Record<string, { x: number; y: number }>;

const NODE_POSITIONS_STORAGE_KEY = "bruin-web-node-positions-v1";

export function usePersistedNodePositions(): [
  StoredNodePositions,
  React.Dispatch<React.SetStateAction<StoredNodePositions>>,
] {
  const [storedNodePositions, setStoredNodePositions] =
    useState<StoredNodePositions>(() => {
      if (typeof window === "undefined") {
        return {};
      }

      try {
        const raw = window.localStorage.getItem(NODE_POSITIONS_STORAGE_KEY);
        if (!raw) {
          return {};
        }

        const parsed = JSON.parse(raw) as StoredNodePositions;
        return parsed && typeof parsed === "object" ? parsed : {};
      } catch {
        return {};
      }
    });

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    try {
      window.localStorage.setItem(
        NODE_POSITIONS_STORAGE_KEY,
        JSON.stringify(storedNodePositions)
      );
    } catch {
      // noop: local storage is best-effort only
    }
  }, [storedNodePositions]);

  return [storedNodePositions, setStoredNodePositions];
}
