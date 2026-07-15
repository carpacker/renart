import { expect, test } from "@playwright/test";

import {
  computeAppLineageLayout,
  initialCenteredViewportX,
  type AppLineageLayoutEdge,
  type AppLineageLayoutNode,
} from "../../../lib/app-lineage-layout";

function node(id: string): AppLineageLayoutNode {
  const [layer, ...nameParts] = id.split(".");
  return { id, layer, name: nameParts.join(".") || id };
}

function edge(source: string, target: string): AppLineageLayoutEdge {
  return { source, target };
}

test.describe("app lineage layout engine", () => {
  test("centers a fitting graph in the initial viewport", () => {
    const x = initialCenteredViewportX({
      viewportWidth: 1000,
      graphMinX: 100,
      graphMaxX: 500,
    });

    expect(x).toBe(200);
  });

  test("leaves an overflowing graph at its existing horizontal position", () => {
    expect(
      initialCenteredViewportX({
        viewportWidth: 1000,
        graphMinX: 0,
        graphMaxX: 953,
      }),
    ).toBeNull();
  });

  test("recommends strict layers for cleanly layered DAGs", () => {
    const layout = computeAppLineageLayout({
      nodes: [node("raw.orders"), node("staging.orders"), node("core.orders")],
      edges: [edge("raw.orders", "staging.orders"), edge("staging.orders", "core.orders")],
    });

    expect(layout.recommendation?.id).toBe("strict");
    expect(layout.layoutId).toBe("strict");
    expect(layout.analysis.layerLinearizable).toBe(true);
    expect(layout.analysis.skipEdges).toHaveLength(0);
    expect(layout.analysis.intraEdges).toHaveLength(0);
    expect(layout.analysis.backEdges).toHaveLength(0);
    expect(layout.positions.get("raw.orders")!.x).toBeLessThan(
      layout.positions.get("staging.orders")!.x,
    );
    expect(layout.positions.get("staging.orders")!.x).toBeLessThan(
      layout.positions.get("core.orders")!.x,
    );
  });

  test("recommends layer bands when layer order is cyclic but the asset graph is a DAG", () => {
    const layout = computeAppLineageLayout({
      nodes: [
        node("raw.orders"),
        node("staging.orders"),
        node("core.orders"),
        node("marts.churn_features"),
        node("ml.churn_scores"),
        node("core.customers_scored"),
      ],
      edges: [
        edge("raw.orders", "staging.orders"),
        edge("staging.orders", "core.orders"),
        edge("core.orders", "marts.churn_features"),
        edge("marts.churn_features", "ml.churn_scores"),
        edge("ml.churn_scores", "core.customers_scored"),
      ],
    });

    expect(layout.recommendation?.id).toBe("bands");
    expect(layout.layoutId).toBe("bands");
    expect(layout.analysis.layerLinearizable).toBe(false);
    expect(layout.analysis.cyclicNodes).toHaveLength(0);
  });

  test("recommends force layout for graph cycles", () => {
    const layout = computeAppLineageLayout({
      nodes: [node("raw.a"), node("raw.b")],
      edges: [edge("raw.a", "raw.b"), edge("raw.b", "raw.a")],
    });

    expect(layout.recommendation?.id).toBe("force");
    expect(layout.layoutId).toBe("force");
    expect(layout.analysis.cyclicNodes.sort()).toEqual(["raw.a", "raw.b"]);
    expect(Number.isFinite(layout.positions.get("raw.a")?.x)).toBe(true);
    expect(Number.isFinite(layout.positions.get("raw.a")?.y)).toBe(true);
  });

  test("can explicitly compute layer bands without using dependency-rank layout", () => {
    const layout = computeAppLineageLayout({
      layoutId: "bands",
      nodes: [
        node("raw.orders"),
        node("staging.orders"),
        node("staging.items"),
        node("core.orders"),
      ],
      edges: [
        edge("raw.orders", "staging.orders"),
        edge("raw.orders", "staging.items"),
        edge("staging.orders", "core.orders"),
        edge("staging.items", "core.orders"),
      ],
    });

    expect(layout.layoutId).toBe("bands");
    expect(layout.recommendation?.id).not.toBe("topo");
    expect(layout.positions.get("raw.orders")!.x).toBeLessThan(
      layout.positions.get("core.orders")!.x,
    );
    expect(layout.positions.get("staging.orders")!.y).not.toBe(
      layout.positions.get("raw.orders")!.y,
    );
  });
});
