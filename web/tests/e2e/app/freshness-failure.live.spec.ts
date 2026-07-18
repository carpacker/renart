import { expect, type Page } from "@playwright/test";
import { rm, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

// Freshness and the latest attempt are deliberately independent:
//   - edited but not run yet                 → "Edited"
//   - an exact-target write fails            → fail closed + "Build failed"
//   - a runtime-only Python asset succeeds   → "Never built" + successful attempt
//   - a Python table returns data             → "Fresh" from its confirmed write

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

  test("tells an untested edit apart from an exact-target write that failed", async ({
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

    // Edit to something that fails, then run it. Once an exact physical target
    // has been claimed, a failed write makes its previous contents uncertain.
    // Freshness therefore fails closed while the attempt remains independently
    // visible as a build failure.
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
    await expect(customersNode.locator('[title="Staleness: Never built"]')).toBeVisible({
      timeout: 20000,
    });
    await expect(customersNode.locator('[data-last-run="failed"]')).toHaveText("Build failed");
  });

  test("keeps runtime-only Python attempts separate from physical freshness", async ({
    liveApp,
    page,
  }) => {
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
          target_fidelity?: string;
        }>;
      };
      return body.assets.find((a) => a.asset_name === "analytics.sentinel_check");
    };

    // Run 1 — sentinel present → succeeds. User Python can write anywhere (or
    // nowhere), so without a reliable reported target it remains runtime-only
    // and cannot claim reusable physical freshness.
    const firstRun = page.waitForResponse(
      (response) => response.url().includes(`/api/assets/${pyAssetId}/materialize/stream`),
      { timeout: 90000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await firstRun;
    await expect
      .poll(
        async () => {
          const s = await sentinelStaleness();
          return `${s?.status}/${s?.last_run_status}/${s?.last_run_on_current_content}/${s?.target_fidelity}`;
        },
        { timeout: 30000 },
      )
      .toBe("never_built/succeeded/true/runtime_only");

    // Remove the sentinel — the asset's content is untouched.
    await rm(sentinelPath);

    // Run 2 — identical content, now fails. Its base status remains
    // deliberately unknown while the current-content failure is recorded.
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
          return `${s?.status}/${s?.last_run_status}/${s?.last_run_on_current_content}/${s?.target_fidelity}`;
        },
        { timeout: 30000 },
      )
      .toBe("never_built/failed/true/runtime_only");

    await page.goto(`${liveApp.baseURL}/catalog?asset=${pyAssetId}`);
    const sentinelNode = page.getByTestId(`rf__node-${pyAssetId}`);
    await expect(sentinelNode.locator('[title="Staleness: Never built"]')).toBeVisible({
      timeout: 20000,
    });
    await expect(sentinelNode.locator('[data-last-run="failed"]')).toHaveText("Build failed");
  });

  test("marks a materialized Python table fresh after its confirmed write", async ({
    liveApp,
    page,
  }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The freshness badge is a desktop sidebar/canvas affordance.",
    );

    const pyAssetId = Buffer.from("analytics/assets/analytics/python_freshness.py").toString(
      "base64url",
    );
    await writeFile(
      join(liveApp.workspaceDir, "analytics", "assets", "analytics", "python_freshness.py"),
      `""" @bruin
name: analytics.python_freshness
type: python
connection: duckdb-default
materialization:
  type: table
@bruin """

import pyarrow as pa


def materialize():
    return pa.table({"value": [1, 2, 3]})
`,
      "utf8",
    );

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
    await expect(page.locator(".view-lines").first()).toContainText("materialize", {
      timeout: 15000,
    });

    const materialize = page.waitForResponse(
      (response) => response.url().includes(`/api/assets/${pyAssetId}/materialize/stream`),
      { timeout: 90000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    const response = await materialize;
    expect(response.ok()).toBe(true);

    await expect
      .poll(
        async () => {
          const stalenessResponse = await page.request.get(
            `${liveApp.baseURL}/api/pipelines/${pipelineId}/staleness?environment=default`,
          );
          if (!stalenessResponse.ok()) return "";
          const body = (await stalenessResponse.json()) as {
            assets: Array<{
              asset_name: string;
              status: string;
              last_run_status?: string;
              last_run_on_current_content?: boolean;
              target_fidelity?: string;
            }>;
          };
          const status = body.assets.find(
            (asset) => asset.asset_name === "analytics.python_freshness",
          );
          return `${status?.status}/${status?.last_run_status}/${status?.last_run_on_current_content}/${status?.target_fidelity}`;
        },
        { timeout: 45000 },
      )
      .toBe("fresh/succeeded/true/exact");
  });
});
