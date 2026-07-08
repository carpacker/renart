import { expect } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test } from "../live-app-fixture";

type WorkspaceResponse = {
  pipelines: Array<{
    id: string;
    assets: Array<{ id: string; name: string; content: string }>;
  }>;
};

const pipelineId = Buffer.from("analytics").toString("base64url");
const customersAssetId = Buffer.from(
  "analytics/assets/analytics/customers.sql"
).toString("base64url");
const customerStatsAssetId = Buffer.from(
  "analytics/assets/analytics/customer_stats.sql"
).toString("base64url");

test.describe("app build editor live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("edits an asset in Monaco and persists the change", async ({
    liveApp,
    page,
  }) => {
    await page.goto(
      `${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`
    );

    const editor = page.locator(".monaco-editor").first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await expect(page.locator(".view-lines").first()).toContainText(
      "customer_id",
      { timeout: 15000 }
    );

    await editor.click();
    await page.keyboard.press("ControlOrMeta+End");
    const saveResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 }
    );
    await page.keyboard.press("End");
    await page.keyboard.type("\n-- app editor smoke");
    await saveResponse;

    const workspaceResponse = await page.request.get(
      `${liveApp.baseURL}/api/workspace`
    );
    expect(workspaceResponse.ok()).toBe(true);
    const workspace = (await workspaceResponse.json()) as WorkspaceResponse;
    const customers = workspace.pipelines
      .flatMap((pipeline) => pipeline.assets)
      .find((asset) => asset.id === customersAssetId);
    expect(customers?.content).toContain("-- app editor smoke");
  });

  test("ctrl+click on an upstream table opens that asset", async ({
    liveApp,
    page,
  }) => {
    await writeFile(
      join(
        liveApp.workspaceDir,
        "analytics",
        "assets",
        "analytics",
        "customer_stats.sql"
      ),
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select customer_id, customer_name from analytics.customers
`,
      "utf8"
    );

    // Wait for the watcher to pick the new asset up so the initial workspace
    // load already contains it with full content.
    await expect
      .poll(
        async () => {
          const response = await page.request.get(
            `${liveApp.baseURL}/api/workspace`
          );
          if (!response.ok()) {
            return "";
          }
          const workspace = (await response.json()) as WorkspaceResponse;
          return (
            workspace.pipelines
              .flatMap((pipeline) => pipeline.assets)
              .find((asset) => asset.id === customerStatsAssetId)?.content ?? ""
          );
        },
        { timeout: 30000 }
      )
      .toContain("analytics.customers");

    await page.goto(
      `${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customerStatsAssetId}/code`
    );

    const viewLines = page.locator(".view-lines").first();
    await expect(viewLines).toContainText("analytics.customers", {
      timeout: 15000,
    });

    const upstreamToken = viewLines
      .locator("span", { hasText: "customers" })
      .last();
    await expect
      .poll(
        async () => {
          await upstreamToken.click({ modifiers: ["ControlOrMeta"] });
          return page.url();
        },
        { timeout: 15000 }
      )
      .toContain(`/assets/${customersAssetId}/code`);

    await expect(page.locator(".view-lines").first()).toContainText(
      "customer_name",
      { timeout: 15000 }
    );
  });
});
