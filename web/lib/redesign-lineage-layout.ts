export type RedesignLineageLayoutId = "strict" | "bands" | "force";

export type RedesignLineageLayoutNode = {
  id: string;
  layer: string;
  name: string;
  width?: number;
  height?: number;
};

export type RedesignLineageLayoutEdge = {
  source: string;
  target: string;
};

export type RedesignLineageLayoutRecommendation = {
  id: RedesignLineageLayoutId;
  reason: string;
  scores: Record<RedesignLineageLayoutId, number> | null;
};

type Graph = {
  nodes: Required<RedesignLineageLayoutNode>[];
  edges: Array<RedesignLineageLayoutEdge & { key: string }>;
};

type Analysis = {
  preds: Map<string, string[]>;
  succs: Map<string, string[]>;
  nodeById: Map<string, Required<RedesignLineageLayoutNode>>;
  layerOrder: string[];
  layerIndex: Map<string, number>;
  layerLinearizable: boolean;
  backEdges: Graph["edges"];
  skipEdges: Graph["edges"];
  intraEdges: Graph["edges"];
  nodeRank: Map<string, number>;
  cyclicNodes: string[];
};

export type RedesignLineageLayoutResult = {
  layoutId: RedesignLineageLayoutId;
  recommendation: RedesignLineageLayoutRecommendation | null;
  positions: Map<string, { x: number; y: number }>;
  analysis: Pick<Analysis, "layerOrder" | "layerLinearizable" | "backEdges" | "skipEdges" | "intraEdges" | "cyclicNodes">;
};

const DEFAULT_NODE_WIDTH = 232;
const DEFAULT_NODE_HEIGHT = 96;
const ROW_GAP = 124;
const COL_GAP = 90;
const LEFT_PAD = 40;
const TOP_PAD = 96;

function graphFor(nodes: RedesignLineageLayoutNode[], edges: RedesignLineageLayoutEdge[]): Graph {
  const nodeIds = new Set(nodes.map((node) => node.id));
  const edgeSet = new Set<string>();
  return {
    nodes: nodes.map((node) => ({
      ...node,
      layer: node.layer || "default",
      name: node.name || node.id,
      width: node.width ?? DEFAULT_NODE_WIDTH,
      height: node.height ?? DEFAULT_NODE_HEIGHT,
    })),
    edges: edges
      .filter((edge) => edge.source !== edge.target && nodeIds.has(edge.source) && nodeIds.has(edge.target))
      .map((edge) => ({ ...edge, key: `${edge.source}->${edge.target}` }))
      .filter((edge) => {
        if (edgeSet.has(edge.key)) return false;
        edgeSet.add(edge.key);
        return true;
      }),
  };
}

function buildAdjacency(graph: Graph) {
  const preds = new Map<string, string[]>();
  const succs = new Map<string, string[]>();
  graph.nodes.forEach((node) => {
    preds.set(node.id, []);
    succs.set(node.id, []);
  });
  graph.edges.forEach((edge) => {
    succs.get(edge.source)?.push(edge.target);
    preds.get(edge.target)?.push(edge.source);
  });
  return { preds, succs };
}

function analyze(graph: Graph): Analysis {
  const { preds, succs } = buildAdjacency(graph);
  const nodeById = new Map(graph.nodes.map((node) => [node.id, node]));
  const layersSeen: string[] = [];
  graph.nodes.forEach((node) => {
    if (!layersSeen.includes(node.layer)) layersSeen.push(node.layer);
  });

  const layerEdgeSet = new Set<string>();
  graph.edges.forEach((edge) => {
    const sourceLayer = nodeById.get(edge.source)?.layer;
    const targetLayer = nodeById.get(edge.target)?.layer;
    if (sourceLayer && targetLayer && sourceLayer !== targetLayer) {
      layerEdgeSet.add(`${sourceLayer}->${targetLayer}`);
    }
  });

  const layerEdges = [...layerEdgeSet].map((key) => key.split("->"));
  const layerIndeg = new Map(layersSeen.map((layer) => [layer, 0]));
  const layerSucc = new Map(layersSeen.map((layer) => [layer, [] as string[]]));
  layerEdges.forEach(([source, target]) => {
    layerIndeg.set(target, (layerIndeg.get(target) ?? 0) + 1);
    layerSucc.get(source)?.push(target);
  });

  const appearanceIndex = new Map(layersSeen.map((layer, index) => [layer, index]));
  const indegWork = new Map(layerIndeg);
  const queue = layersSeen.filter((layer) => indegWork.get(layer) === 0);
  const layerOrder: string[] = [];
  while (queue.length) {
    queue.sort((a, b) => (appearanceIndex.get(a) ?? 0) - (appearanceIndex.get(b) ?? 0));
    const layer = queue.shift();
    if (!layer) continue;
    layerOrder.push(layer);
    for (const next of layerSucc.get(layer) ?? []) {
      indegWork.set(next, (indegWork.get(next) ?? 0) - 1);
      if (indegWork.get(next) === 0) queue.push(next);
    }
  }
  const layerLinearizable = layerOrder.length === layersSeen.length;
  if (!layerLinearizable) {
    layersSeen.forEach((layer) => {
      if (!layerOrder.includes(layer)) layerOrder.push(layer);
    });
  }
  const layerIndex = new Map(layerOrder.map((layer, index) => [layer, index]));

  const backEdges: Graph["edges"] = [];
  const skipEdges: Graph["edges"] = [];
  const intraEdges: Graph["edges"] = [];
  graph.edges.forEach((edge) => {
    const sourceLayer = nodeById.get(edge.source)?.layer ?? "default";
    const targetLayer = nodeById.get(edge.target)?.layer ?? "default";
    const sourceIndex = layerIndex.get(sourceLayer) ?? 0;
    const targetIndex = layerIndex.get(targetLayer) ?? 0;
    if (targetIndex < sourceIndex) backEdges.push(edge);
    else if (targetIndex === sourceIndex) intraEdges.push(edge);
    else if (targetIndex - sourceIndex > 1) skipEdges.push(edge);
  });

  const indeg = new Map(graph.nodes.map((node) => [node.id, preds.get(node.id)?.length ?? 0]));
  const nodeRank = new Map<string, number>();
  const rankQueue = graph.nodes.filter((node) => indeg.get(node.id) === 0).map((node) => node.id);
  rankQueue.forEach((id) => nodeRank.set(id, 0));
  while (rankQueue.length) {
    const id = rankQueue.shift();
    if (!id) continue;
    for (const target of succs.get(id) ?? []) {
      nodeRank.set(target, Math.max(nodeRank.get(target) ?? 0, (nodeRank.get(id) ?? 0) + 1));
      indeg.set(target, (indeg.get(target) ?? 0) - 1);
      if (indeg.get(target) === 0) rankQueue.push(target);
    }
  }
  const cyclicNodes = graph.nodes.filter((node) => !nodeRank.has(node.id)).map((node) => node.id);
  if (cyclicNodes.length) {
    const fallbackRank = Math.max(0, ...nodeRank.values()) + 1;
    cyclicNodes.forEach((id) => nodeRank.set(id, fallbackRank));
  }

  return { preds, succs, nodeById, layerOrder, layerIndex, layerLinearizable, backEdges, skipEdges, intraEdges, nodeRank, cyclicNodes };
}

function recommendLayout(graph: Graph, analysis: Analysis): RedesignLineageLayoutRecommendation | null {
  const nodeCount = graph.nodes.length;
  if (!nodeCount || !graph.edges.length) return null;

  if (analysis.cyclicNodes.length) {
    return {
      id: "force",
      reason: "The asset graph contains a cycle, so an organic layout is the safest way to expose the structure while the DAG is fixed.",
      scores: null,
    };
  }

  const edgeCount = graph.edges.length;
  const skipRatio = analysis.skipEdges.length / edgeCount;
  const intraRatio = analysis.intraEdges.length / edgeCount;
  const backRatio = analysis.backEdges.length / edgeCount;
  const layers = analysis.layerOrder.length;
  const density = edgeCount / nodeCount;
  const clean = analysis.skipEdges.length + analysis.intraEdges.length + analysis.backEdges.length === 0;

  const scores: Record<RedesignLineageLayoutId, number> = {
    strict: 1.0 + (layers >= 3 && layers <= 6 ? 0.15 : 0) - skipRatio * 1.4 - intraRatio * 0.9 - backRatio * 5.0,
    bands: 0.80 + (!analysis.layerLinearizable ? 0.6 : 0) + intraRatio * 0.5 + skipRatio * 0.2 + (layers >= 3 ? 0.05 : -0.35) - (analysis.layerLinearizable && clean ? 0.2 : 0),
    force: 0.2 + (density > 2.4 ? 0.45 : 0) + (density > 3.4 ? 0.4 : 0),
  };
  const id = Object.entries(scores).sort((a, b) => b[1] - a[1])[0]?.[0] as RedesignLineageLayoutId;
  const reasons: Record<RedesignLineageLayoutId, string> = {
    strict: clean
      ? "Layers are linearizable and every edge moves one layer forward, so strict columns match the model."
      : "The flow mostly respects the layer order, so strict columns preserve the prefix model clearly.",
    bands: !analysis.layerLinearizable
      ? "The layer graph is cyclic, but layer bands preserve prefixes while the horizontal axis follows dependency depth."
      : "Intra-layer or skip dependencies make bands more readable than a strict prefix grid.",
    force: "The graph is dense enough that cluster structure matters more than rank, so the organic layout is clearer.",
  };
  return { id, reason: reasons[id], scores };
}

function orderWithinRanks(ranks: string[][], analysis: Analysis) {
  const position = new Map<string, number>();
  ranks.forEach((rank, rankIndex) => rank.forEach((id, index) => position.set(id, rankIndex * 1000 + index)));
  const sortRank = (rank: string[], neighborsOf: (id: string) => string[]) => {
    const scored = rank.map((id) => {
      const neighbors = neighborsOf(id).map((neighbor) => position.get(neighbor)).filter((value): value is number => value !== undefined);
      return { id, score: neighbors.length ? neighbors.reduce((sum, value) => sum + value, 0) / neighbors.length : position.get(id) ?? 0 };
    });
    scored.sort((a, b) => a.score - b.score || a.id.localeCompare(b.id));
    scored.forEach((item, index) => position.set(item.id, index));
    return scored.map((item) => item.id);
  };

  for (let iteration = 0; iteration < 4; iteration++) {
    for (let rank = 1; rank < ranks.length; rank++) ranks[rank] = sortRank(ranks[rank], (id) => analysis.preds.get(id) ?? []);
    for (let rank = ranks.length - 2; rank >= 0; rank--) ranks[rank] = sortRank(ranks[rank], (id) => analysis.succs.get(id) ?? []);
  }
  return ranks;
}

function placeColumns(ranks: string[][], graph: Graph) {
  const positions = new Map<string, { x: number; y: number }>();
  const maxRows = Math.max(1, ...ranks.map((rank) => rank.length));
  let x = LEFT_PAD;
  ranks.forEach((rank) => {
    const maxWidth = Math.max(DEFAULT_NODE_WIDTH, ...rank.map((id) => graph.nodes.find((node) => node.id === id)?.width ?? DEFAULT_NODE_WIDTH));
    const startY = TOP_PAD + ((maxRows - rank.length) * ROW_GAP) / 2;
    rank.forEach((id, index) => {
      const node = graph.nodes.find((candidate) => candidate.id === id);
      positions.set(id, { x: x + (maxWidth - (node?.width ?? DEFAULT_NODE_WIDTH)) / 2, y: startY + index * ROW_GAP });
    });
    x += maxWidth + COL_GAP;
  });
  return positions;
}

function layoutStrict(graph: Graph, analysis: Analysis) {
  const ranks = analysis.layerOrder.map(() => [] as string[]);
  graph.nodes.forEach((node) => ranks[analysis.layerIndex.get(node.layer) ?? 0].push(node.id));
  return placeColumns(orderWithinRanks(ranks, analysis), graph);
}

function layoutBands(graph: Graph, analysis: Analysis) {
  const maxRank = Math.max(0, ...graph.nodes.map((node) => analysis.nodeRank.get(node.id) ?? 0));
  const columnWidths = Array.from({ length: maxRank + 1 }, (_, rank) => {
    const inRank = graph.nodes.filter((node) => analysis.nodeRank.get(node.id) === rank);
    return Math.max(DEFAULT_NODE_WIDTH, ...inRank.map((node) => node.width));
  });
  const columnX: number[] = [];
  let x = LEFT_PAD;
  columnWidths.forEach((width) => {
    columnX.push(x);
    x += width + COL_GAP;
  });

  const positions = new Map<string, { x: number; y: number }>();
  let y = 54;
  analysis.layerOrder.forEach((layer) => {
    const inLayer = graph.nodes.filter((node) => node.layer === layer);
    if (!inLayer.length) return;
    const byRank = new Map<number, Required<RedesignLineageLayoutNode>[]>();
    inLayer.forEach((node) => {
      const rank = analysis.nodeRank.get(node.id) ?? 0;
      byRank.set(rank, [...(byRank.get(rank) ?? []), node]);
    });
    byRank.forEach((nodes) => nodes.sort((a, b) => a.id.localeCompare(b.id)));
    const rows = Math.max(1, ...[...byRank.values()].map((nodes) => nodes.length));
    byRank.forEach((nodes, rank) => {
      nodes.forEach((node, index) => {
        positions.set(node.id, { x: columnX[rank] + (columnWidths[rank] - node.width) / 2, y: y + 42 + index * ROW_GAP });
      });
    });
    y += 42 + rows * ROW_GAP + 42;
  });
  return positions;
}

function layoutForce(graph: Graph, analysis: Analysis) {
  const centers = graph.nodes.map((node, index) => ({
    id: node.id,
    x: LEFT_PAD + (analysis.layerIndex.get(node.layer) ?? 0) * 290 + (index % 3) * 42 + node.width / 2,
    y: TOP_PAD + (index % 7) * ROW_GAP + node.height / 2,
    vx: 0,
    vy: 0,
  }));
  const byId = new Map(centers.map((node) => [node.id, node]));

  for (let tick = 0; tick < 260; tick++) {
    for (let i = 0; i < centers.length; i++) {
      for (let j = i + 1; j < centers.length; j++) {
        const a = centers[i];
        const b = centers[j];
        const dx = b.x - a.x || 0.01;
        const dy = b.y - a.y || 0.01;
        const distSq = Math.max(dx * dx + dy * dy, 100);
        const force = 52000 / distSq;
        const dist = Math.sqrt(distSq);
        const fx = (dx / dist) * force;
        const fy = (dy / dist) * force;
        a.vx -= fx;
        a.vy -= fy;
        b.vx += fx;
        b.vy += fy;
      }
    }
    graph.edges.forEach((edge) => {
      const source = byId.get(edge.source);
      const target = byId.get(edge.target);
      if (!source || !target) return;
      const dx = target.x - source.x || 0.01;
      const dy = target.y - source.y || 0.01;
      const dist = Math.max(Math.sqrt(dx * dx + dy * dy), 1);
      const force = (dist - 280) * 0.015;
      const fx = (dx / dist) * force;
      const fy = (dy / dist) * force;
      source.vx += fx;
      source.vy += fy;
      target.vx -= fx;
      target.vy -= fy;
    });
    centers.forEach((node) => {
      const source = analysis.nodeById.get(node.id);
      const targetX = LEFT_PAD + (analysis.layerIndex.get(source?.layer ?? "default") ?? 0) * 290 + DEFAULT_NODE_WIDTH / 2;
      node.vx += (targetX - node.x) * 0.018;
      node.vy += (TOP_PAD + 220 - node.y) * 0.004;
      node.x += node.vx;
      node.y += node.vy;
      node.vx *= 0.72;
      node.vy *= 0.72;
    });
  }

  const positions = new Map<string, { x: number; y: number }>();
  centers.forEach((center) => {
    const node = analysis.nodeById.get(center.id);
    if (node) positions.set(center.id, { x: center.x - node.width / 2, y: center.y - node.height / 2 });
  });
  normalizePositions(positions);
  return positions;
}

function normalizePositions(positions: Map<string, { x: number; y: number }>) {
  if (!positions.size) return;
  const minX = Math.min(...[...positions.values()].map((position) => position.x));
  const minY = Math.min(...[...positions.values()].map((position) => position.y));
  positions.forEach((position, id) => {
    positions.set(id, { x: position.x - minX + LEFT_PAD, y: position.y - minY + TOP_PAD });
  });
}

function computeLayout(layoutId: RedesignLineageLayoutId, graph: Graph, analysis: Analysis) {
  if (!graph.nodes.length) return new Map<string, { x: number; y: number }>();
  if (layoutId === "bands") return layoutBands(graph, analysis);
  if (layoutId === "force") return layoutForce(graph, analysis);
  return layoutStrict(graph, analysis);
}

export function computeRedesignLineageLayout({
  nodes,
  edges,
  layoutId,
}: {
  nodes: RedesignLineageLayoutNode[];
  edges: RedesignLineageLayoutEdge[];
  layoutId?: RedesignLineageLayoutId;
}): RedesignLineageLayoutResult {
  const graph = graphFor(nodes, edges);
  const analysis = analyze(graph);
  const recommendation = recommendLayout(graph, analysis);
  const effectiveLayout = layoutId ?? recommendation?.id ?? "strict";
  return {
    layoutId: effectiveLayout,
    recommendation,
    positions: computeLayout(effectiveLayout, graph, analysis),
    analysis,
  };
}
