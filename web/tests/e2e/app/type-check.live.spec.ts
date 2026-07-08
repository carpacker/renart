import { expect } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

const pipelineId = Buffer.from("analytics").toString("base64url");

type TypeCheckFinding = { severity: string; message: string; line?: number };
type TypeCheckAsset = {
  id?: string;
  name: string;
  type: string;
  status: string;
  findings: TypeCheckFinding[];
};
type TypeCheckReport = {
  status: string;
  pipeline_name: string;
  assets: TypeCheckAsset[];
  summary: { assets: number; errors: number; warnings: number };
};

async function seedTypeCheckAssets(liveApp: LiveApp) {
  const assetsDir = join(liveApp.workspaceDir, "analytics", "assets", "analytics");
  // A Python asset with no declared columns -> warning.
  await writeFile(
    join(assetsDir, "py_metric.py"),
    `""" @bruin
name: analytics.py_metric
type: python
@bruin """

print("hello")
`,
    "utf8",
  );
  // A SQL asset that selects a column the known upstream does not have -> error.
  await writeFile(
    join(assetsDir, "bad_downstream.sql"),
    `/* @bruin
name: analytics.bad_downstream
type: duckdb.sql
materialization:
  type: view
depends:
  - analytics.customers
@bruin */

select nonexistent_col from analytics.customers
`,
    "utf8",
  );
}

async function pollTypeCheck(
  liveApp: LiveApp,
  request: { get: (url: string) => Promise<{ ok(): boolean; json(): Promise<unknown> }> },
): Promise<TypeCheckReport> {
  let report: TypeCheckReport | null = null;
  await expect
    .poll(
      async () => {
        const response = await request.get(`${liveApp.baseURL}/api/pipelines/${pipelineId}/type-check`);
        if (!response.ok()) {
          return "";
        }
        report = (await response.json()) as TypeCheckReport;
        return report.assets.map((asset) => asset.name).sort().join(",");
      },
      { timeout: 30000 },
    )
    .toContain("analytics.bad_downstream");
  if (!report) {
    throw new Error("type-check report never resolved");
  }
  return report;
}

test.describe("app pipeline type check live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("type-check endpoint reports column errors and missing-column warnings", async ({ liveApp, request }) => {
    await seedTypeCheckAssets(liveApp);
    const report = await pollTypeCheck(liveApp, request);

    const byName = new Map(report.assets.map((asset) => [asset.name, asset]));

    // Undeclared Python asset -> warning about missing columns.
    const py = byName.get("analytics.py_metric");
    expect(py?.status).toBe("warning");
    expect(py?.findings.some((f) => f.severity === "warning" && /no columns/i.test(f.message))).toBe(true);

    // Downstream selecting a non-existent column of a known upstream -> error.
    const bad = byName.get("analytics.bad_downstream");
    expect(bad?.status).toBe("error");
    expect(bad?.findings.some((f) => f.severity === "error" && /Unresolved column/i.test(f.message))).toBe(true);

    // A clean upstream asset reports no findings.
    const customers = byName.get("analytics.customers");
    expect(customers?.status).toBe("ok");
    expect(customers?.findings).toEqual([]);

    expect(report.summary.errors).toBeGreaterThanOrEqual(1);
    expect(report.summary.warnings).toBeGreaterThanOrEqual(1);
    expect(report.status).toBe("error");
  });

  test("notification bell opens the type-check tab in the bottom panel", async ({ liveApp, page }) => {
    await seedTypeCheckAssets(liveApp);
    // Make sure the server can see the new assets before we open the page.
    await pollTypeCheck(liveApp, page.request);

    const customersAssetId = Buffer.from(
      "analytics/assets/analytics/customers.sql",
    ).toString("base64url");

    const typeCheckResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/type-check`) && response.ok(),
      { timeout: 30000 },
    );
    await page.goto(
      `${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/canvas`,
    );
    await typeCheckResponse;

    // The repurposed notification bell opens the Type check results tab.
    await page.getByRole("button", { name: "Type check" }).first().click();

    await expect(page.getByText("analytics.bad_downstream").first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(/Unresolved column/i).first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(/no columns/i).first()).toBeVisible({ timeout: 15000 });
  });
});
