"use client";

import { useAtomValue } from "jotai";
import {
  Dispatch,
  MutableRefObject,
  SetStateAction,
  useCallback,
  useEffect,
  useState,
} from "react";
import { Edge, MarkerType, Node, ReactFlowInstance } from "reactflow";

import { NewAssetKind, NewAssetNodeData } from "@/components/new-asset-node";
import { StoredNodePositions } from "@/hooks/use-persisted-node-positions";
import { useSeedFileDrop } from "@/hooks/use-seed-file-drop";
import {
  pipelineAtom,
  resolvedSelectedAssetAtom,
} from "@/lib/atoms/domains/workspace";

type NewAssetDraftState = {
  flowX: number;
  flowY: number;
  name: string;
  kind: NewAssetKind;
  sourceAssetId?: string;
};

type UseAssetCanvasInteractionsInput = {
  reactFlowInstance: ReactFlowInstance | null;
  canvasContainerRef: MutableRefObject<HTMLDivElement | null>;
  graphNodes: Node[];
  graphEdges: Edge[];
  connectedNodeIDs: Set<string>;
  storedNodePositions: StoredNodePositions;
  setStoredNodePositions: Dispatch<SetStateAction<StoredNodePositions>>;
  defaultAssetNamesByKind: Record<NewAssetKind, string>;
  setNodes: Dispatch<SetStateAction<Node[]>>;
  setEdges: Dispatch<SetStateAction<Edge[]>>;
  runCreateAsset: (
    pipelineId: string,
    input: {
      name?: string;
      type?: string;
      path?: string;
      content?: string;
      source_asset_id?: string;
      seed_file_name?: string;
      seed_file_content?: string;
    }
  ) => Promise<{ asset_id?: string } | null>;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
  inspectLoadingByAssetId?: Record<string, boolean>;
  onInspectAsset?: (assetId: string) => void;
  onMaterializeAsset?: (assetId: string) => void;
  onDeleteAsset?: (assetId: string) => void;
  isMobile?: boolean;
  openSelectedAssetEditor?: () => void;
  buildCreateAssetInput: (
    name: string,
    kind: NewAssetKind
  ) => { name: string; type: string; path?: string; content?: string };
  defaultSeedAssetType?: string;
};

const NEW_ASSET_NODE_ID = "__new_asset__";
const DOWNSTREAM_NODE_VERTICAL_GAP = 40;

type NodeWithMeasuredHeight = Node & {
  measured?: {
    height?: number;
  };
};

export function useAssetCanvasInteractions({
  reactFlowInstance,
  canvasContainerRef,
  graphNodes,
  graphEdges,
  connectedNodeIDs,
  storedNodePositions,
  setStoredNodePositions,
  defaultAssetNamesByKind,
  setNodes,
  setEdges,
  runCreateAsset,
  navigateSelection,
  inspectLoadingByAssetId,
  onInspectAsset,
  onMaterializeAsset,
  onDeleteAsset,
  isMobile = false,
  openSelectedAssetEditor,
  buildCreateAssetInput,
  defaultSeedAssetType = "duckdb.seed",
}: UseAssetCanvasInteractionsInput) {
  const pipeline = useAtomValue(pipelineAtom);
  const selectedAssetId = useAtomValue(resolvedSelectedAssetAtom);
  const pipelineId = pipeline?.id ?? null;
  const [newAssetDraft, setNewAssetDraft] = useState<NewAssetDraftState | null>(
    null
  );
  const {
    handleCanvasDragOver,
    handleCanvasDragLeave,
    handleCanvasDrop,
    seedDropActive,
  } = useSeedFileDrop({
    pipeline,
    pipelineId,
    reactFlowInstance,
    defaultSeedAssetType,
    runCreateAsset,
    setStoredNodePositions,
    navigateSelection,
  });

  useEffect(() => {
    if (!newAssetDraft) {
      return;
    }

    const handleWindowPointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        setNewAssetDraft(null);
        return;
      }

      if (target.closest('[data-new-asset-node="true"]')) {
        return;
      }

      if (target.closest(".react-flow")) {
        return;
      }

      setNewAssetDraft(null);
    };

    window.addEventListener("pointerdown", handleWindowPointerDown, true);
    return () => {
      window.removeEventListener("pointerdown", handleWindowPointerDown, true);
    };
  }, [newAssetDraft]);

  const openNewAssetInput = useCallback(
    (clientX: number, clientY: number) => {
      const container = canvasContainerRef.current;
      if (!container) {
        return;
      }

      const rect = container.getBoundingClientRect();
      const x = Math.max(12, Math.min(clientX - rect.left, rect.width - 260));
      const y = Math.max(12, Math.min(clientY - rect.top, rect.height - 130));
      const flowPosition = reactFlowInstance?.screenToFlowPosition({
        x: clientX,
        y: clientY,
      });

      setNewAssetDraft({
        flowX: flowPosition?.x ?? x,
        flowY: flowPosition?.y ?? y,
        name: defaultAssetNamesByKind.sql,
        kind: "sql",
      });
    },
    [canvasContainerRef, defaultAssetNamesByKind.sql, reactFlowInstance]
  );

  const handlePaneClick = useCallback(
    (event: React.MouseEvent) => {
      if (newAssetDraft) {
        setNewAssetDraft(null);
        return;
      }

      if (!pipelineId) {
        return;
      }

      openNewAssetInput(event.clientX, event.clientY);
    },
    [newAssetDraft, openNewAssetInput, pipelineId]
  );

  const handlePaneContextMenu = useCallback(
    (event: React.MouseEvent) => {
      event.preventDefault();
      if (newAssetDraft) {
        setNewAssetDraft(null);
        return;
      }

      if (!pipelineId) {
        return;
      }

      openNewAssetInput(event.clientX, event.clientY);
    },
    [newAssetDraft, openNewAssetInput, pipelineId]
  );

  const submitNewAsset = useCallback(
    (nameValue?: string) => {
      if (!pipelineId || !newAssetDraft) {
        return;
      }

      const name = (nameValue ?? newAssetDraft.name).trim();
      if (!name) {
        setNewAssetDraft(null);
        return;
      }

      const draftPosition = { x: newAssetDraft.flowX, y: newAssetDraft.flowY };
      const createInput = newAssetDraft.sourceAssetId
        ? { name, source_asset_id: newAssetDraft.sourceAssetId }
        : buildCreateAssetInput(name, newAssetDraft.kind);
      void runCreateAsset(pipelineId, createInput).then((response) => {
        if (response?.asset_id) {
          setStoredNodePositions((previous) => ({
            ...previous,
            [response.asset_id as string]: draftPosition,
          }));
          navigateSelection(pipelineId, response.asset_id);
        }
      });
      setNewAssetDraft(null);
    },
    [
      buildCreateAssetInput,
      navigateSelection,
      newAssetDraft,
      pipelineId,
      runCreateAsset,
      setStoredNodePositions,
    ]
  );

  const handleCreateDownstreamAsset = useCallback(
    (sourceAssetId: string) => {
      if (!pipelineId) {
        return;
      }

      const sourceNode = graphNodes.find((node) => node.id === sourceAssetId);
      const renderedSourceNode = reactFlowInstance?.getNode(sourceAssetId);
      const sourcePosition = storedNodePositions[sourceAssetId] ??
        sourceNode?.position ?? { x: 32, y: 32 };
      const renderedSourceNodeWithMeasurement = renderedSourceNode as
        | NodeWithMeasuredHeight
        | undefined;
      const sourceNodeWithMeasurement = sourceNode as
        | NodeWithMeasuredHeight
        | undefined;
      const sourceHeight =
        renderedSourceNodeWithMeasurement?.measured?.height ??
        renderedSourceNode?.height ??
        sourceNodeWithMeasurement?.measured?.height ??
        sourceNode?.height ??
        180;

      const sourceAsset = pipeline?.assets.find((asset) => asset.id === sourceAssetId);
      setNewAssetDraft({
        flowX: sourcePosition.x,
        flowY: sourcePosition.y + sourceHeight + DOWNSTREAM_NODE_VERTICAL_GAP,
        name: buildSuggestedDownstreamAssetName(
          sourceAsset?.name ?? "asset",
          new Set(pipeline?.assets.map((asset) => asset.name) ?? [])
        ),
        kind: "sql",
        sourceAssetId,
      });
    },
    [
      graphNodes,
      pipeline?.assets,
      pipelineId,
      reactFlowInstance,
      storedNodePositions,
    ]
  );

  useEffect(() => {
    const mappedNodes = graphNodes.map((node) => ({
      ...node,
      data:
        node.type === "assetNode"
          ? {
              ...(node.data as Record<string, unknown>),
              onCreateDownstreamAsset: () =>
                handleCreateDownstreamAsset(node.id),
              onInspect: onInspectAsset
                ? () => onInspectAsset(node.id)
                : undefined,
              onMaterialize: onMaterializeAsset
                ? () => onMaterializeAsset(node.id)
                : undefined,
              onDelete: onDeleteAsset
                ? () => onDeleteAsset(node.id)
                : undefined,
              inspectLoading: inspectLoadingByAssetId?.[node.id] ?? false,
              materializeLoading: Boolean((node.data as { materializeLoading?: boolean }).materializeLoading),
            }
          : node.data,
      position: storedNodePositions[node.id] ?? node.position,
      selected: selectedAssetId ? node.id === selectedAssetId : false,
    }));

    if (newAssetDraft) {
      const draftData: NewAssetNodeData = {
        name: newAssetDraft.name,
        kind: newAssetDraft.kind,
        createLabel: newAssetDraft.sourceAssetId ? "Create child" : undefined,
        kindLocked: Boolean(newAssetDraft.sourceAssetId),
        onKindChange: (kind) => {
          if (newAssetDraft.sourceAssetId) {
            return newAssetDraft.name;
          }
          const nextName = defaultAssetNamesByKind[kind];
          setNewAssetDraft((previous) =>
            previous ? { ...previous, kind, name: nextName } : previous
          );
          return nextName;
        },
        onCreate: (name) => submitNewAsset(name),
        onCancel: () => setNewAssetDraft(null),
      };

      mappedNodes.push({
        id: NEW_ASSET_NODE_ID,
        type: "newAssetNode",
        data: draftData,
        position: { x: newAssetDraft.flowX, y: newAssetDraft.flowY },
        selected: false,
        draggable: true,
        selectable: false,
        focusable: false,
      });
    }

    setNodes((currentNodes) => mergeStableNodes(currentNodes, mappedNodes));
    const mappedEdges = newAssetDraft?.sourceAssetId
      ? [
          ...graphEdges,
          {
            id: `${newAssetDraft.sourceAssetId}->${NEW_ASSET_NODE_ID}`,
            source: newAssetDraft.sourceAssetId,
            target: NEW_ASSET_NODE_ID,
            animated: true,
            className: "asset-edge-active",
            markerEnd: {
              type: MarkerType.ArrowClosed,
              color: "var(--primary)",
              width: 18,
              height: 18,
            },
          },
        ]
      : graphEdges;
    setEdges((currentEdges) => mergeStableEdges(currentEdges, mappedEdges));
  }, [
    connectedNodeIDs,
    defaultAssetNamesByKind,
    graphEdges,
    graphNodes,
    handleCreateDownstreamAsset,
    inspectLoadingByAssetId,
    newAssetDraft,
    onDeleteAsset,
    onInspectAsset,
    onMaterializeAsset,
    selectedAssetId,
    setEdges,
    setNodes,
    storedNodePositions,
    submitNewAsset,
  ]);

  const handleNodeDragStop = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      if (node.id === NEW_ASSET_NODE_ID) {
        setNewAssetDraft((previous) =>
          previous
            ? { ...previous, flowX: node.position.x, flowY: node.position.y }
            : previous
        );
        return;
      }

      setStoredNodePositions((previous) => ({
        ...previous,
        [node.id]: node.position,
      }));
    },
    [setStoredNodePositions]
  );

  const handleNodeClick = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      if (node.id === NEW_ASSET_NODE_ID) {
        return;
      }
      if (isMobile && selectedAssetId === node.id) {
        openSelectedAssetEditor?.();
        return;
      }

      if (pipelineId) {
        navigateSelection(pipelineId, node.id);
      }
    },
    [isMobile, navigateSelection, openSelectedAssetEditor, pipelineId, selectedAssetId]
  );

  return {
    handlePaneClick,
    handlePaneContextMenu,
    handleNodeDragStop,
    handleNodeClick,
    handleCanvasDragOver,
    handleCanvasDragLeave,
    handleCanvasDrop,
    seedDropActive,
  };
}

function mergeStableNodes(currentNodes: Node[], nextNodes: Node[]) {
  if (currentNodes.length === 0) {
    return nextNodes;
  }

  const currentById = new Map(currentNodes.map((node) => [node.id, node]));
  let changed = currentNodes.length !== nextNodes.length;
  const merged = nextNodes.map((next) => {
    const current = currentById.get(next.id);
    if (!current) {
      changed = true;
      return next;
    }

    const same =
      current.type === next.type &&
      current.selected === next.selected &&
      current.draggable === next.draggable &&
      current.selectable === next.selectable &&
      current.position.x === next.position.x &&
      current.position.y === next.position.y &&
      shallowEqual(current.data as Record<string, unknown>, next.data as Record<string, unknown>);
    if (same) {
      return current;
    }

    changed = true;
    return {
      ...current,
      ...next,
      measured: (current as NodeWithMeasuredHeight).measured,
      height: current.height,
      width: current.width,
    };
  });

  return changed ? merged : currentNodes;
}

function buildSuggestedDownstreamAssetName(sourceAssetName: string, existingNames: Set<string>) {
  const trimmed = sourceAssetName.trim() || "asset";
  const lastDot = trimmed.lastIndexOf(".");
  const prefix = lastDot >= 0 ? trimmed.slice(0, lastDot) : "";
  const leaf = lastDot >= 0 ? trimmed.slice(lastDot + 1) : trimmed;
  const baseLeaf = `${slugifyAssetLeaf(leaf)}_child`;

  for (let index = 1; index < 1000; index += 1) {
    const candidateLeaf = `${baseLeaf}_${index}`;
    const candidate = prefix ? `${prefix}.${candidateLeaf}` : candidateLeaf;
    if (!hasAssetName(existingNames, candidate)) {
      return candidate;
    }
  }

  return prefix ? `${prefix}.${baseLeaf}_1` : `${baseLeaf}_1`;
}

function hasAssetName(existingNames: Set<string>, candidate: string) {
  const normalizedCandidate = candidate.trim().toLowerCase();
  for (const name of existingNames) {
    if (name.trim().toLowerCase() === normalizedCandidate) {
      return true;
    }
  }
  return false;
}

function slugifyAssetLeaf(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9\s_-]/g, "")
    .replace(/[\s_-]+/g, "_")
    .replace(/^_+|_+$/g, "") || "asset";
}

function mergeStableEdges(currentEdges: Edge[], nextEdges: Edge[]) {
  if (currentEdges.length === 0) {
    return nextEdges;
  }

  const currentById = new Map(currentEdges.map((edge) => [edge.id, edge]));
  let changed = currentEdges.length !== nextEdges.length;
  const merged = nextEdges.map((next) => {
    const current = currentById.get(next.id);
    if (!current) {
      changed = true;
      return next;
    }

    const same =
      current.source === next.source &&
      current.target === next.target &&
      current.type === next.type &&
      current.animated === next.animated &&
      current.className === next.className &&
      shallowEqual(current.markerEnd as Record<string, unknown>, next.markerEnd as Record<string, unknown>) &&
      shallowEqual(current.data as Record<string, unknown>, next.data as Record<string, unknown>);
    if (same) {
      return current;
    }

    changed = true;
    return { ...current, ...next };
  });

  return changed ? merged : currentEdges;
}

function shallowEqual(left?: Record<string, unknown>, right?: Record<string, unknown>) {
  if (left === right) {
    return true;
  }
  const leftKeys = Object.keys(left ?? {});
  const rightKeys = Object.keys(right ?? {});
  if (leftKeys.length !== rightKeys.length) {
    return false;
  }
  return leftKeys.every((key) => {
    const leftValue = left?.[key];
    const rightValue = right?.[key];
    if (typeof leftValue === "function" && typeof rightValue === "function") {
      return true;
    }
    return leftValue === rightValue;
  });
}
