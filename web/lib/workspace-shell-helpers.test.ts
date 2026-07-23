import { describe, expect, it } from "vitest";

import { buildSuggestedAssetName } from "@/lib/workspace-shell-helpers";

describe("buildSuggestedAssetName", () => {
  it("uses the pipeline prefix and first available sequence number", () => {
    expect(
      buildSuggestedAssetName(
        "sql",
        new Set(["sales_ops.my_sql_asset_1", "sales_ops.my_sql_asset_2"]),
        "Sales Ops",
      ),
    ).toBe("sales_ops.my_sql_asset_3");
  });

  it("uses a stable default prefix when the pipeline name has no slug characters", () => {
    expect(buildSuggestedAssetName("api", new Set(), "///")).toBe("default.my_api_asset_1");
  });
});
