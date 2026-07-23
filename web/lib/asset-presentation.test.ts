import { describe, expect, it } from "vitest";

import {
  assetDirectory,
  assetFileStem,
  assetKindForType,
  assetPresentationFields,
} from "@/lib/asset-presentation";
import type { WebAsset, WebPipeline } from "@/lib/types";

function asset(overrides: Partial<WebAsset> = {}): WebAsset {
  return {
    id: "asset-1",
    name: "analytics.orders",
    type: "duckdb.sql",
    path: "pipelines/analytics/assets/analytics/orders.sql",
    content: "select 1",
    upstreams: [],
    is_materialized: false,
    ...overrides,
  };
}

const pipeline: WebPipeline = {
  id: "pipeline-1",
  name: "analytics",
  path: "pipelines/analytics/pipeline.yml",
  assets: [],
};

describe("assetKindForType", () => {
  it.each([
    ["duckdb.sql", "sql"],
    ["python", "python"],
    ["api", "api"],
    ["load", "load"],
    ["bq.seed", "seed"],
    ["s3.sensor.key_sensor", "sensor"],
    ["pg.source", "source"],
    ["ingestr", "ingestr"],
    ["unit_test", "unittest"],
    ["agent.claude_code", "asset"],
    ["", "asset"],
  ] as const)("classifies %s as %s", (assetType, expected) => {
    expect(assetKindForType(assetType)).toBe(expected);
  });
});

describe("asset paths", () => {
  it("removes compound asset suffixes from fallback names", () => {
    expect(assetFileStem("assets/raw/orders.asset.yml")).toBe("orders");
    expect(assetFileStem("assets/raw/customers.source.yaml")).toBe("customers");
  });

  it("derives a pipeline-relative asset directory on either path separator", () => {
    expect(assetDirectory("pipelines/analytics/assets/raw/orders.sql", pipeline.path)).toBe("raw");
    expect(
      assetDirectory(
        "pipelines\\analytics\\assets\\staging\\orders.sql",
        "pipelines\\analytics\\pipeline.yml",
      ),
    ).toBe("staging");
  });
});

describe("assetPresentationFields", () => {
  it("uses the backend-resolved connection as the target label", () => {
    expect(assetPresentationFields(asset({ connection: "warehouse" }), pipeline)).toMatchObject({
      name: "analytics.orders",
      displayName: "orders",
      prefix: "analytics",
      kind: "sql",
      integration: "warehouse",
      dir: "analytics",
    });
  });

  it("shows the asset type when no effective connection was resolved", () => {
    expect(assetPresentationFields(asset({ connection: undefined }), pipeline).integration).toBe(
      "duckdb.sql",
    );
  });

  it("does not misclassify API and unknown asset types as SQL", () => {
    expect(assetPresentationFields(asset({ type: "api" }), pipeline).kind).toBe("api");
    expect(assetPresentationFields(asset({ type: "custom.operator" }), pipeline).kind).toBe(
      "asset",
    );
  });
});
