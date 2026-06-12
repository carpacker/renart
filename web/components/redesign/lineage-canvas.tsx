import { useEffect, useMemo, useState } from "react";
import { Background, Controls, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps, type NodeTypes } from "reactflow";
import "reactflow/dist/style.css";

import { computeRedesignLineageLayout, type RedesignLineageLayoutEdge } from "@/lib/redesign-lineage-layout";

import type { RedesignAsset } from "./redesign-data";
import { AssetNode } from "./redesign-primitives";

export type RedesignLineageCanvasAsset = RedesignAsset & {
  displayName?: string;
  prefix?: string;
  pipelineId?: string;
  isMaterialized?: boolean;
  upstreams?: string[];
};

type AssetNodeData = {
  asset: RedesignLineageCanvasAsset;
  selected: boolean;
  dimmed: boolean;
  onSelect?: (assetId: string) => void;
};

type PrefixGroupNodeData = {
  label: string;
  count: number;
  width: number;
  height: number;
};

export function assetNameParts(name: string) {
  const parts = name.split(".").filter(Boolean);
  if (parts.length <= 1) {
    return { title: name };
  }
  return {
    prefix: parts.slice(0, -1).join("."),
    title: parts[parts.length - 1],
  };
}

export function assetGroupName(asset: RedesignLineageCanvasAsset) {
  return asset.prefix || asset.group || "root";
}

export function assetDisplayName(asset: RedesignLineageCanvasAsset) {
  return asset.displayName || assetNameParts(asset.name).title;
}

function PrefixGroupFlowNode({ data }: NodeProps<PrefixGroupNodeData>) {
  return (
    <div
      className="pointer-events-none relative rounded-2xl border bg-background/50"
      style={{ width: data.width, height: data.height }}
    >
      <div className="absolute left-3 top-2.5 flex items-center gap-2">
        <span className="font-mono text-xs font-semibold">{data.label}</span>
        <span className="rounded-full bg-primary/10 px-1.5 text-[10px] text-primary">{data.count}</span>
      </div>
    </div>
  );
}

function AssetFlowNode({ data }: NodeProps<AssetNodeData>) {
  const displayAsset = {
    ...data.asset,
    name: assetDisplayName(data.asset),
  };

  return (
    <button
      type="button"
      className="text-left transition-opacity"
      style={{ opacity: data.dimmed ? 0.18 : 1 }}
      onClick={() => data.onSelect?.(data.asset.id)}
    >
      <Handle className="asset-node-hidden-handle" type="target" position={Position.Left} />
      <AssetNode asset={displayAsset} selected={data.selected} />
      <Handle className="asset-node-hidden-handle" type="source" position={Position.Right} />
    </button>
  );
}

const nodeTypes = {
  prefixGroup: PrefixGroupFlowNode,
  lineageAsset: AssetFlowNode,
} satisfies NodeTypes;

function derivedEdges(assets: RedesignLineageCanvasAsset[], links?: RedesignLineageLayoutEdge[]) {
  if (links) return links;
  const assetByName = new Map<string, RedesignLineageCanvasAsset>();
  const assetById = new Map<string, RedesignLineageCanvasAsset>();
  assets.forEach((asset) => {
    assetByName.set(asset.name, asset);
    assetById.set(asset.id, asset);
  });
  return assets.flatMap((asset) =>
    (asset.upstreams ?? [])
      .map((upstream) => assetByName.get(upstream) ?? assetById.get(upstream))
      .filter((source): source is RedesignLineageCanvasAsset => Boolean(source))
      .map((source) => ({ source: source.id, target: asset.id }))
  );
}

function lineageFor(assetId: string, edges: RedesignLineageLayoutEdge[]) {
  const preds = new Map<string, string[]>();
  const succs = new Map<string, string[]>();
  edges.forEach((edge) => {
    preds.set(edge.target, [...(preds.get(edge.target) ?? []), edge.source]);
    succs.set(edge.source, [...(succs.get(edge.source) ?? []), edge.target]);
  });

  const walk = (start: string, adjacency: Map<string, string[]>) => {
    const seen = new Set<string>();
    const stack = [start];
    while (stack.length) {
      const id = stack.pop();
      if (!id) continue;
      for (const next of adjacency.get(id) ?? []) {
        if (seen.has(next)) continue;
        seen.add(next);
        stack.push(next);
      }
    }
    return seen;
  };

  const upstream = walk(assetId, preds);
  const downstream = walk(assetId, succs);
  return { upstream, downstream, all: new Set([assetId, ...upstream, ...downstream]) };
}

export function RedesignLineageCanvas({
  assets,
  links,
  selectedAssetId,
  onAssetSelect,
}: {
  assets: RedesignLineageCanvasAsset[];
  links?: RedesignLineageLayoutEdge[];
  selectedAssetId?: string;
  onAssetSelect?: (assetId: string) => void;
}) {
  const [lineageAssetId, setLineageAssetId] = useState<string | null>(null);

  useEffect(() => {
    setLineageAssetId((current) => current && current !== selectedAssetId ? null : current);
  }, [selectedAssetId]);

  const { nodes, edges } = useMemo(() => {
    const graphEdges = derivedEdges(assets, links);
    const lineage = lineageAssetId ? lineageFor(lineageAssetId, graphEdges) : null;
    const visuallySelectedAssetId = selectedAssetId ?? lineageAssetId ?? undefined;
    const layout = computeRedesignLineageLayout({
      nodes: assets.map((asset) => ({
        id: asset.id,
        layer: assetGroupName(asset),
        name: assetDisplayName(asset),
      })),
      edges: graphEdges,
    });

    const assetsByGroup = assets.reduce<Record<string, RedesignLineageCanvasAsset[]>>((groups, asset) => {
      const group = assetGroupName(asset);
      groups[group] = [...(groups[group] ?? []), asset];
      return groups;
    }, {});

    const groupNodes: Node<PrefixGroupNodeData>[] = Object.entries(assetsByGroup).map(([group, groupAssets]) => {
      const positionedAssets = groupAssets
        .map((asset) => ({ asset, position: layout.positions.get(asset.id) }))
        .filter((item): item is { asset: RedesignLineageCanvasAsset; position: { x: number; y: number } } => Boolean(item.position));
      const minX = Math.min(...positionedAssets.map(({ position }) => position.x)) - 16;
      const minY = Math.min(...positionedAssets.map(({ position }) => position.y)) - 42;
      const maxX = Math.max(...positionedAssets.map(({ position }) => position.x)) + 248;
      const maxY = Math.max(...positionedAssets.map(({ position }) => position.y)) + 112;
      return {
        id: `prefix-group-${group}`,
        type: "prefixGroup",
        position: { x: minX, y: minY },
        data: {
          label: group,
          count: groupAssets.length,
          width: maxX - minX,
          height: maxY - minY,
        },
        draggable: false,
        selectable: false,
        connectable: false,
        zIndex: 0,
      };
    });

    const assetNodes: Node<AssetNodeData>[] = assets.map((asset) => ({
      id: asset.id,
      type: "lineageAsset",
      position: layout.positions.get(asset.id) ?? { x: asset.x, y: asset.y },
      data: {
        asset,
        selected: asset.id === visuallySelectedAssetId,
        dimmed: Boolean(lineage && !lineage.all.has(asset.id)),
        onSelect: (assetId) => {
          if (!onAssetSelect) {
            setLineageAssetId((current) => current === assetId ? null : assetId);
            return;
          }
          if (assetId === selectedAssetId) {
            setLineageAssetId((current) => current === assetId ? null : assetId);
            return;
          }
          setLineageAssetId(null);
          onAssetSelect(assetId);
        },
      },
      zIndex: 2,
    }));

    const edges: Edge[] = graphEdges.map((edge) => {
      const active = Boolean(lineage && lineage.all.has(edge.source) && lineage.all.has(edge.target));
      const dimmed = Boolean(lineage && !active);
      return {
        id: `${edge.source}-${edge.target}`,
        source: edge.source,
        target: edge.target,
        type: "default",
        className: active ? "asset-edge-active" : "asset-edge",
        animated: active,
        style: { stroke: active ? undefined : "#a1a1aa", strokeWidth: active ? undefined : 1.5, opacity: dimmed ? 0.12 : 1 },
      };
    });

    return { nodes: [...groupNodes, ...assetNodes], edges };
  }, [assets, lineageAssetId, links, onAssetSelect, selectedAssetId]);

  return (
    <div className="h-full min-h-0 bg-zinc-100">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        deleteKeyCode={null}
        panActivationKeyCode={null}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={22} size={1} color="rgba(0,0,0,0.08)" />
        <Controls position="bottom-left" />
      </ReactFlow>
    </div>
  );
}
