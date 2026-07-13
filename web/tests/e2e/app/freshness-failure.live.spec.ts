import { expect, type Page } from "@playwright/test";
import { rm, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

// Freshness and the latest attempt are deliberately independent:
//   - edited but not run yet                 → "Edited"
//   - edited, then run, and the run failed   → "Edited" + "Build failed"
//   - unchanged after a good build, but the
//     latest identical run failed            → "Fresh" + "Last run failed"

const pipelineId = Buffer.from("analytics").toString("base64url");
const customersAssetId = Buffer.from("analytics/assets/analytics/customers.sql").toString(
  "base64url",
);

const frontmatter = `/* @bruin
type: duckdb.sql
materialization:
  type: table
@bruin */`;

const validEdit = `${frontmatter}

select 1 as customer_id, 'Ada' as customer_name -- edited_marker_not_re_run
`;

const brokenEdit = `${frontmatter}

select * from does_not_exist_table
`;

// Writes asset content, then waits until the server has re-parsed it from disk.
// The recorder fingerprints the workspace as the coordinator currently sees it,
// so materializing before the watcher catches up would record the run against
// stale content. Real users edit, see it settle, then run — this mirrors that.
async function editAssetAndSettle(page: Page, liveApp: LiveApp, content: string, marker: string) {
  const put = await page.request.put(
    `${liveApp.baseURL}/api/pipelines/${pipelineId}/assets/${customersAssetId}`,
    { data: { content } },
  );
  expect(put.ok()).toBe(true);
  await expect
    .poll(
      async () => {
        const response = await page.request.get(`${liveApp.baseURL}/api/workspace`);
        if (!response.ok()) return "";
        const workspace = (await response.json()) as {
          pipelines: Array<{ assets: Array<{ id: string; content: string }> }>;
        };
        return (
          workspace.pipelines
            .flatMap((pipeline) => pipeline.assets)
            .find((asset) => asset.id === customersAssetId)?.content ?? ""
        );
      },
      { timeout: 20000 },
    )
    .toContain(marker);
}

test.describe("app freshness failure states live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("tells an untested edit apart from an edit that was run and failed", async ({
    liveApp,
    page,
  }) => {
    // The freshness badge lives on the explorer sidebar / canvas node — desktop
    // chrome, the same affordance the other status specs treat as desktop-only.
    test.skip(
      test.info().project.name.includes("mobile"),
      "The freshness badge is a desktop sidebar/canvas affordance.",
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });

    // Build it once so it is fresh — the baseline every "edited" state needs.
    const firstMaterialize = page.waitForResponse(
      (response) => response.url().includes(`/api/assets/${customersAssetId}/materialize/stream`),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await firstMaterialize;
    await expect(page.locator("pre.font-console").first()).toContainText(/\S/, {
      timeout: 15000,
    });

    // Edit (a valid change) but do not re-run: only the freshness dimension
    // changes, so the sidebar reads "Edited" with no attempt warning.
    await editAssetAndSettle(page, liveApp, validEdit, "edited_marker_not_re_run");
    await expect(page.locator('[title="Staleness: Edited"]').first()).toBeVisible({
      timeout: 20000,
    });
    await expect(page.locator('[title="Build failed"]')).toHaveCount(0);

    // Edit to something that fails, then run it. The sidebar prioritizes the
    // warning, while the canvas node shows both dimensions side by side.
    await editAssetAndSettle(page, liveApp, brokenEdit, "does_not_exist_table");
    const failedMaterialize = page.waitForResponse(
      (response) => response.url().includes(`/api/assets/${customersAssetId}/materialize/stream`),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await failedMaterialize;

    await expect(page.locator('[title="Build failed"]').first()).toBeVisible({
      timeout: 20000,
    });

    await page.goto(`${liveApp.baseURL}/catalog?asset=${customersAssetId}`);
    const customersNode = page.getByTestId(`rf__node-${customersAssetId}`);
    await expect(customersNode.locator('[title="Staleness: Edited"]')).toBeVisible({
      timeout: 20000,
    });
    await expect(customersNode.locator('[data-last-run="failed"]')).toHaveText("Build failed");
  });

  test("keeps unchanged built data fresh when its latest run failed", async ({ liveApp, page }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The freshness badge is a desktop sidebar/canvas affordance.",
    );

    // A python asset that only succeeds while a sentinel file exists. Its content
    // never changes, so deleting the sentinel makes an *identical* re-run fail —
    // the only reliable way to produce "unchanged, but the last run failed". The
    // absolute path is baked in so it is independent of the run's working dir.
    const sentinelPath = join(liveApp.workspaceDir, "sentinel.txt");
    const pyAssetId = Buffer.from("analytics/assets/analytics/sentinel_check.py").toString(
      "base64url",
    );
    const pyContent = `""" @bruin
name: analytics.sentinel_check
type: python
@bruin """
import os
assert os.path.exists(${JSON.stringify(sentinelPath)}), "sentinel missing"
print("sentinel ok")
`;

    await writeFile(sentinelPath, "present\n", "utf8");
    await writeFile(
      join(liveApp.workspaceDir, "analytics", "assets", "analytics", "sentinel_check.py"),
      pyContent,
      "utf8",
    );

    // Wait for the watcher to add the new asset to the workspace.
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${liveApp.baseURL}/api/workspace`);
          if (!response.ok()) return false;
          const workspace = (await response.json()) as {
            pipelines: Array<{ assets: Array<{ id: string }> }>;
          };
          return workspace.pipelines.flatMap((p) => p.assets).some((a) => a.id === pyAssetId);
        },
        { timeout: 20000 },
      )
      .toBe(true);

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${pyAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("sentinel", {
      timeout: 15000,
    });

    // The canonical state comes from the staleness API. Polling avoids depending
    // on the exact SSE push timing before checking the rendered node as well.
    const sentinelStaleness = async () => {
      const response = await page.request.get(
        `${liveApp.baseURL}/api/pipelines/${pipelineId}/staleness?environment=default`,
      );
      const body = (await response.json()) as {
        assets: Array<{
          asset_name: string;
          status: string;
          last_run_status?: string;
          last_run_on_current_content?: boolean;
        }>;
      };
      return body.assets.find((a) => a.asset_name === "analytics.sentinel_check");
    };

    // Run 1 — sentinel present → succeeds → fresh. A python asset writes no
    // warehouse table, so "fresh" here also proves it is not falsely "missing".
    const firstRun = page.waitForResponse(
      (response) => response.url().includes(`/api/assets/${pyAssetId}/materialize/stream`),
      { timeout: 90000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await firstRun;
    await expect
      .poll(async () => (await sentinelStaleness())?.status, { timeout: 30000 })
      .toBe("fresh");

    // Remove the sentinel — the asset's content is untouched.
    await rm(sentinelPath);

    // Run 2 — identical content, now fails. The asset stays fresh (an earlier
    // build exists) but its last run failed on the current content.
    const secondRun = page.waitForResponse(
      (response) => response.url().includes(`/api/assets/${pyAssetId}/materialize/stream`),
      { timeout: 90000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await secondRun;

    await expect
      .poll(
        async () => {
          const s = await sentinelStaleness();
          return `${s?.status}/${s?.last_run_status}/${s?.last_run_on_current_content}`;
        },
        { timeout: 30000 },
      )
      .toBe("fresh/failed/true");

    await page.goto(`${liveApp.baseURL}/catalog?asset=${pyAssetId}`);
    const sentinelNode = page.getByTestId(`rf__node-${pyAssetId}`);
    await expect(sentinelNode.locator('[title="Staleness: Fresh"]')).toBeVisible({
      timeout: 20000,
    });
    await expect(sentinelNode.locator('[data-last-run="failed"]')).toHaveText("Last run failed");
  });
});
