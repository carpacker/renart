"use client";

import { RefObject, useEffect, useRef } from "react";
import { Edge, Node, ReactFlowInstance } from "reactflow";

type UseGraphViewportFocusInput = {
  reactFlowInstance: ReactFlowInstance | null;
  activePipelineId: string | null;
  recomputeVersion: number;
  graphNodes: Node[];
  graphEdges: Edge[];
  selectedAssetId: string | null;
  storedNodePositions: Record<string, { x: number; y: number }>;
  canvasContainerRef: RefObject<HTMLDivElement | null>;
};

export function useGraphViewportFocus({
  reactFlowInstance,
  activePipelineId,
  recomputeVersion,
  graphNodes,
  graphEdges,
  selectedAssetId,
  storedNodePositions,
  canvasContainerRef,
}: UseGraphViewportFocusInput) {
  const lastFitKeyRef = useRef<string | null>(null);
  const lastFocusedAssetRef = useRef<string | null>(null);

  useEffect(() => {
    if (!reactFlowInstance || graphNodes.length === 0) {
      return;
    }

    const fitKey = `${activePipelineId ?? "__no_pipeline__"}:${recomputeVersion}`;
    if (lastFitKeyRef.current === fitKey) {
      return;
    }

    lastFitKeyRef.current = fitKey;

    const raf = window.requestAnimationFrame(() => {
      reactFlowInstance.fitView({ padding: 0.2, duration: 180 });
    });

    return () => window.cancelAnimationFrame(raf);
  }, [
    activePipelineId,
    graphEdges,
    graphNodes.length,
    reactFlowInstance,
    recomputeVersion,
  ]);

  useEffect(() => {
    lastFocusedAssetRef.current = null;
  }, [activePipelineId]);

  useEffect(() => {
    if (!reactFlowInstance || !selectedAssetId) {
      return;
    }

    if (lastFocusedAssetRef.current === selectedAssetId) {
      return;
    }

    const raf = window.requestAnimationFrame(() => {
      const selectedNode = reactFlowInstance.getNode(selectedAssetId);
      if (!selectedNode) {
        return;
      }

      const container = canvasContainerRef.current;
      if (!container) {
        return;
      }

      lastFocusedAssetRef.current = selectedAssetId;

      const viewport = reactFlowInstance.getViewport();
      const zoom = viewport.zoom;
      const nodeX = selectedNode.positionAbsolute?.x ?? selectedNode.position.x;
      const nodeY = selectedNode.positionAbsolute?.y ?? selectedNode.position.y;
      const nodeWidth = selectedNode.width ?? 240;
      const nodeHeight = selectedNode.height ?? 120;
      const nodeLeft = nodeX * zoom + viewport.x;
      const nodeTop = nodeY * zoom + viewport.y;
      const nodeRight = nodeLeft + nodeWidth * zoom;
      const nodeBottom = nodeTop + nodeHeight * zoom;
      const visibilityMargin = 24;

      const isInView =
        nodeLeft >= visibilityMargin &&
        nodeTop >= visibilityMargin &&
        nodeRight <= container.clientWidth - visibilityMargin &&
        nodeBottom <= container.clientHeight - visibilityMargin;

      if (isInView) {
        return;
      }

      const nodeCenterX = nodeX + nodeWidth / 2;
      const nodeCenterY = nodeY + nodeHeight / 2;
      const nextX = container.clientWidth / 2 - nodeCenterX * zoom;
      const nextY = container.clientHeight / 2 - nodeCenterY * zoom;
      reactFlowInstance.setViewport(
        { x: nextX, y: nextY, zoom },
        { duration: 220 }
      );
    });

    return () => window.cancelAnimationFrame(raf);
  }, [
    activePipelineId,
    canvasContainerRef,
    graphNodes,
    reactFlowInstance,
    selectedAssetId,
    storedNodePositions,
  ]);
}
