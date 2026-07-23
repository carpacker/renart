import { describe, expect, it } from "vitest";

import {
  buildSchemaForAsset,
  effectiveConnectionForAsset,
  effectiveConnectionTypeForAsset,
} from "@/lib/sql-schema";
import type { WebAsset, WorkspaceState } from "@/lib/types";

function asset(overrides: Partial<WebAsset> = {}): WebAsset {
  return {
    id: "asset-1",
    name: "analytics.orders",
    type: "duckdb.sql",
    path: "assets/orders.sql",
    content: "select 1",
    upstreams: [],
    is_materialized: false,
    ...overrides,
  };
}

describe("effectiveConnectionForAsset", () => {
  it("returns the effective connection supplied by the backend", () => {
    expect(effectiveConnectionForAsset({ connection: "  warehouse  " })).toBe("warehouse");
  });

  it("does not infer a connection when the backend did not resolve one", () => {
    expect(effectiveConnectionForAsset({ connection: undefined })).toBeNull();
    expect(effectiveConnectionForAsset({ connection: " " })).toBeNull();
  });
});

describe("effectiveConnectionTypeForAsset", () => {
  it("uses the backend-resolved connection name to find the platform", () => {
    expect(
      effectiveConnectionTypeForAsset(
        { connections: { warehouse: " DuckDB " } },
        { connection: " warehouse " },
      ),
    ).toBe("duckdb");
  });

  it("does not infer a platform when the asset has no resolved connection", () => {
    expect(
      effectiveConnectionTypeForAsset(
        { connections: { warehouse: "duckdb" } },
        { connection: undefined },
      ),
    ).toBeNull();
  });

  it("does not substitute another connection when the resolved name is unknown", () => {
    expect(
      effectiveConnectionTypeForAsset(
        { connections: { warehouse: "duckdb" } },
        { connection: "missing" },
      ),
    ).toBeNull();
  });
});

describe("buildSchemaForAsset", () => {
  it("does not leak tables from an arbitrary same-platform connection", () => {
    const current = asset({ id: "current", connection: undefined });
    const other = asset({
      id: "other",
      name: "analytics.customers",
      connection: "duckdb-secondary",
    });
    const workspace = {
      pipelines: [
        {
          id: "pipeline-1",
          name: "analytics",
          path: "pipeline.yml",
          assets: [current, other],
        },
      ],
      connections: {
        "duckdb-primary": "duckdb",
        "duckdb-secondary": "duckdb",
      },
      selected_environment: "",
      errors: [],
      updated_at: "",
      metadata: {},
    } satisfies WorkspaceState;

    expect(buildSchemaForAsset(workspace, current)).toEqual([]);
  });

  it("includes only assets on the resolved connection", () => {
    const current = asset({ id: "current", connection: "duckdb-primary" });
    const same = asset({
      id: "same",
      name: "analytics.customers",
      connection: "duckdb-primary",
    });
    const other = asset({
      id: "other",
      name: "analytics.events",
      connection: "duckdb-secondary",
    });
    const workspace = {
      pipelines: [
        {
          id: "pipeline-1",
          name: "analytics",
          path: "pipeline.yml",
          assets: [current, same, other],
        },
      ],
      connections: {
        "duckdb-primary": "duckdb",
        "duckdb-secondary": "duckdb",
      },
      selected_environment: "",
      errors: [],
      updated_at: "",
      metadata: {},
    } satisfies WorkspaceState;

    expect(buildSchemaForAsset(workspace, current).map((table) => table.name)).toEqual([
      "analytics.orders",
      "analytics.customers",
    ]);
  });
});
