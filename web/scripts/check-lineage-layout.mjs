// Run from web/ with: node scripts/check-lineage-layout.mjs
// The script compiles lib/redesign-lineage-layout.ts into a temporary ESM directory, imports it,
// and checks deterministic strict-layout crossing reduction on synthetic graphs.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const root = process.cwd();
const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "renart-lineage-layout-"));
fs.writeFileSync(path.join(tempDir, "package.json"), JSON.stringify({ type: "module" }));

try {
  execFileSync(
    "corepack",
    [
      "pnpm",
      "exec",
      "tsc",
      "lib/redesign-lineage-layout.ts",
      "--outDir",
      tempDir,
      "--module",
      "ES2022",
      "--target",
      "ES2022",
      "--moduleResolution",
      "bundler",
      "--skipLibCheck",
      "--strict",
      "--esModuleInterop",
      "--noEmit",
      "false",
      "--declaration",
      "false",
    ],
    { cwd: root, stdio: "inherit" },
  );

  const compiledPath = path.join(tempDir, "redesign-lineage-layout.js");
  const { computeRedesignLineageLayout } = await import(pathToFileURL(compiledPath).href);

  const cases = [chainCase(), diamondCase(), bipartiteShuffleCase(), skipEdgesAndCycleCase()];

  for (const testCase of cases) {
    const first = computeRedesignLineageLayout({ ...testCase, layoutId: "strict" });
    const second = computeRedesignLineageLayout({ ...testCase, layoutId: "strict" });
    const firstPositions = serializePositions(first.positions);
    const secondPositions = serializePositions(second.positions);
    assert.equal(firstPositions, secondPositions, `${testCase.name} should be deterministic`);

    const naive = countLayeredCrossings(
      testCase.nodes,
      testCase.edges,
      first.analysis.layerOrder,
      null,
    );
    const optimized = countLayeredCrossings(
      testCase.nodes,
      testCase.edges,
      first.analysis.layerOrder,
      first.positions,
    );
    assert.ok(
      optimized <= naive,
      `${testCase.name} optimized crossings (${optimized}) should not exceed naive crossings (${naive})`,
    );
    console.log(`${testCase.name}: naive=${naive} optimized=${optimized}`);
  }
} finally {
  fs.rmSync(tempDir, { recursive: true, force: true });
}

function serializePositions(positions) {
  return JSON.stringify(
    [...positions.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([id, position]) => [id, position.x, position.y]),
  );
}

function chainCase() {
  const nodes = ["a", "b", "c", "d"].map((id, index) => node(id, `l${index}`));
  const edges = [
    { source: "a", target: "b" },
    { source: "b", target: "c" },
    { source: "c", target: "d" },
  ];
  return { name: "chain", nodes, edges };
}

function diamondCase() {
  const nodes = [
    node("root", "source"),
    node("right", "middle"),
    node("left", "middle"),
    node("sink", "mart"),
  ];
  const edges = [
    { source: "root", target: "left" },
    { source: "root", target: "right" },
    { source: "left", target: "sink" },
    { source: "right", target: "sink" },
  ];
  return { name: "diamond", nodes, edges };
}

function bipartiteShuffleCase() {
  const nodes = [
    ...["a0", "a1", "a2", "a3", "a4", "a5"].map((id) => node(id, "left")),
    ...["b5", "b4", "b3", "b2", "b1", "b0"].map((id) => node(id, "right")),
  ];
  const edges = [
    { source: "a0", target: "b0" },
    { source: "a1", target: "b1" },
    { source: "a2", target: "b2" },
    { source: "a3", target: "b3" },
    { source: "a4", target: "b4" },
    { source: "a5", target: "b5" },
  ];
  return { name: "bipartite shuffle", nodes, edges };
}

function skipEdgesAndCycleCase() {
  const nodes = [
    node("s0", "source"),
    node("s1", "source"),
    node("m0", "staging"),
    node("m1", "staging"),
    node("x0", "transform"),
    node("x1", "transform"),
    node("out0", "mart"),
    node("out1", "mart"),
  ];
  const edges = [
    { source: "s0", target: "m1" },
    { source: "s1", target: "m0" },
    { source: "m0", target: "x1" },
    { source: "m1", target: "x0" },
    { source: "x0", target: "out1" },
    { source: "x1", target: "out0" },
    { source: "s0", target: "out0" },
    { source: "s1", target: "out1" },
    { source: "x1", target: "m0" },
  ];
  return { name: "skip edges plus cycle", nodes, edges };
}

function node(id, layer) {
  return { id, layer, name: id };
}

function countLayeredCrossings(nodes, edges, layerOrder, positions) {
  const layerIndex = new Map(layerOrder.map((layer, index) => [layer, index]));
  const nodeById = new Map(
    nodes.map((candidate, index) => [candidate.id, { ...candidate, index }]),
  );
  const ranks = layerOrder.map(() => []);
  nodes.forEach((candidate) => {
    ranks[layerIndex.get(candidate.layer) ?? 0].push(candidate.id);
  });
  ranks.forEach((rank) => {
    rank.sort((a, b) => {
      if (positions)
        return (positions.get(a)?.y ?? 0) - (positions.get(b)?.y ?? 0) || a.localeCompare(b);
      return (nodeById.get(a)?.index ?? 0) - (nodeById.get(b)?.index ?? 0) || a.localeCompare(b);
    });
  });

  const order = new Map();
  ranks.forEach((rank, rankIndex) =>
    rank.forEach((id, index) => order.set(id, { rank: rankIndex, index })),
  );
  const virtualRanks = ranks.map((rank) => rank.slice());
  const segments = [];
  edges
    .slice()
    .sort((a, b) => `${a.source}->${a.target}`.localeCompare(`${b.source}->${b.target}`))
    .forEach((edge) => {
      const source = order.get(edge.source);
      const target = order.get(edge.target);
      if (!source || !target || target.rank <= source.rank) return;
      let previous = edge.source;
      for (let rank = source.rank + 1; rank < target.rank; rank++) {
        const virtualId = `virtual:${edge.source}->${edge.target}:${rank}`;
        virtualRanks[rank].push(virtualId);
        previous = virtualId;
        segments.push({
          source:
            rank === source.rank + 1
              ? edge.source
              : `virtual:${edge.source}->${edge.target}:${rank - 1}`,
          target: virtualId,
        });
      }
      segments.push({ source: previous, target: edge.target });
    });

  const virtualOrder = new Map();
  virtualRanks.forEach((rank, rankIndex) =>
    rank.forEach((id, index) => virtualOrder.set(id, { rank: rankIndex, index })),
  );
  const byRank = Array.from({ length: Math.max(0, virtualRanks.length - 1) }, () => []);
  segments.forEach((edge) => {
    const source = virtualOrder.get(edge.source);
    const target = virtualOrder.get(edge.target);
    if (source && target && target.rank === source.rank + 1)
      byRank[source.rank].push([source.index, target.index]);
  });
  return byRank.reduce((sum, bilayer) => sum + countBilayer(bilayer), 0);
}

function countBilayer(edges) {
  const targets = edges.sort((a, b) => a[0] - b[0] || a[1] - b[1]).map((edge) => edge[1]);
  let crossings = 0;
  for (let i = 0; i < targets.length; i++) {
    for (let j = i + 1; j < targets.length; j++) {
      if (targets[i] > targets[j]) crossings += 1;
    }
  }
  return crossings;
}
