import { expect, type APIRequestContext } from "@playwright/test";
import { appendFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

type ScheduleResponse = {
  status: "ok" | "error";
  schedules: Array<{ pipeline_id: string; pipeline_name: string }>;
};

type TriggerResponse = {
  status: "ok" | "error";
  run: { id: string };
};

type RunDetailResponse = {
  status: "ok" | "error";
  run: { id: string; status: string; pipeline: string; error?: string };
  logs?: Array<{ at: string; line: string }>;
  steps?: Array<{ asset: string; status: string }>;
};

const analyticsPipelineId = Buffer.from("analytics").toString("base64url");

test.describe("app scheduler pages live", () => {
  test.use({ fixtureName: "basic-workspace" });

  test("shows configured schedules", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/schedules`);

    await expect(page.getByRole("heading", { name: "Schedules" })).toBeVisible();
    await expect(page.getByText("analytics", { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("daily", { exact: true })).toBeVisible();
    // The catchup column renders the policy itself (here "skip", from the
    // fixture's default), not a generic "Catch up" label.
    await expect(page.getByText("skip", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Run" }).first()).toBeVisible();
  });

  test("shows and updates a schedule pinned to an older deployment", async ({
    liveApp,
    page,
    request,
  }) => {
    const pinResponse = await request.put(
      `${liveApp.baseURL}/api/pipelines/${analyticsPipelineId}/env-schedules/default`,
      {
        data: {
          cron: "0 0 * * *",
          timezone: "UTC",
          catchup_policy: "skip",
          deploy_now: true,
        },
      },
    );
    expect(pinResponse.ok()).toBe(true);
    const pinnedVersion = (
      (await pinResponse.json()) as { schedule: { snapshot_version_id: string } }
    ).schedule.snapshot_version_id;
    expect(pinnedVersion).toBeTruthy();

    await appendFile(
      join(liveApp.workspaceDir, "analytics/assets/analytics/orders.sql"),
      "\n-- deployment mismatch regression\n",
      "utf8",
    );
    const deployResponse = await request.post(
      `${liveApp.baseURL}/api/pipelines/${analyticsPipelineId}/deploy`,
    );
    expect(deployResponse.ok()).toBe(true);
    const latestVersion = ((await deployResponse.json()) as { snapshot: { version_id: string } })
      .snapshot.version_id;
    expect(latestVersion).not.toBe(pinnedVersion);

    await page.goto(`${liveApp.baseURL}/schedules`);
    const olderBadge = page.getByText("Older deployment", { exact: true });
    await expect(olderBadge).toBeVisible({ timeout: 15000 });
    await olderBadge.hover();
    await expect(page.getByText(/Data freshness is tracked separately/)).toBeVisible();

    const updateResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${analyticsPipelineId}/env-schedules/default`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await page.getByRole("button", { name: "Update deployment" }).click();
    await updateResponse;

    await expect
      .poll(
        async () => {
          const response = await request.get(`${liveApp.baseURL}/api/env-schedules`);
          if (!response.ok()) return "";
          const body = (await response.json()) as {
            schedules: Array<{ environment: string; snapshot_version_id?: string }>;
          };
          return body.schedules.find((schedule) => schedule.environment === "default")
            ?.snapshot_version_id;
        },
        { timeout: 15000 },
      )
      .toBe(latestVersion);
    await expect(olderBadge).toBeHidden({ timeout: 15000 });
  });

  test("shows triggered runs in the runs list", async ({ liveApp, page, request }) => {
    const runId = await triggerPipelineRun(liveApp, request);

    await page.goto(`${liveApp.baseURL}/runs`);

    await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible();
    await expect(page.getByText(runId, { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("analytics", { exact: true }).first()).toBeVisible();
  });

  test("opens a run with structured events and one combined output stream", async ({
    liveApp,
    page,
    request,
  }) => {
    const runId = await triggerPipelineRun(liveApp, request);
    const detail = await waitForRunDetail(
      liveApp,
      request,
      runId,
      (current) =>
        (current.steps?.length ?? 0) > 0 &&
        ["success", "failed", "cancelled"].includes(current.run.status),
    );
    const stepAsset = detail.steps?.[0]?.asset;

    await page.goto(`${liveApp.baseURL}/runs/${runId}`);

    await expect(page.getByRole("heading", { name: `Run ${runId}` })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByText(/Run of analytics/)).toBeVisible();
    await expect(page.getByRole("tab", { name: "Events" })).toBeVisible();
    const outputTab = page.getByRole("tab", { name: "Output" });
    await expect(outputTab).toBeVisible();
    await expect(page.getByRole("tab", { name: "stderr" })).toHaveCount(0);
    if (stepAsset) {
      const timelineLabel = page
        .locator('[data-testid="run-timeline-asset-label"]')
        .filter({ hasText: stepAsset })
        .first();
      await expect(timelineLabel).toBeVisible({
        timeout: 30000,
      });
      expect(
        await timelineLabel.evaluate((element) => ({
          overflow: getComputedStyle(element).overflow,
          textOverflow: getComputedStyle(element).textOverflow,
          whiteSpace: getComputedStyle(element).whiteSpace,
        })),
      ).toEqual({ overflow: "visible", textOverflow: "clip", whiteSpace: "normal" });

      const assetLink = page.getByRole("link", { name: stepAsset, exact: true }).first();
      await expect(assetLink).toHaveAttribute(
        "href",
        new RegExp(`/pipelines/${analyticsPipelineId}/assets/[^/]+/split$`),
      );
    }
    const startBadge = page.locator('[data-event-type="asset_start"]').first();
    const successBadge = page.locator('[data-event-type="asset_success"]').first();
    await expect(startBadge).toHaveAttribute("data-event-tone", "progress", { timeout: 30000 });
    await expect(successBadge).toHaveAttribute("data-event-tone", "success", {
      timeout: 30000,
    });
    expect(
      await startBadge.evaluate((element) => getComputedStyle(element).backgroundColor),
    ).not.toBe(await successBadge.evaluate((element) => getComputedStyle(element).backgroundColor));

    await outputTab.click();
    const terminal = page.locator('[data-slot="tabs-content"][data-state="active"] pre');
    await expect(terminal).toContainText("Analyzed the pipeline 'analytics'", { timeout: 30000 });
    await expect(terminal).toContainText("bruin run completed successfully", { timeout: 30000 });
    const output = (await terminal.innerText()).replace(/\r\n/g, "\n");
    expect(output.match(/Analyzed the pipeline 'analytics'/g)).toHaveLength(1);
    expect(output).toMatch(/PASS analytics\.[^\n]+\nPASS analytics\./);
  });

  test("keeps a terminal failure in the combined output", async ({ liveApp, page, request }) => {
    await writeFile(
      join(liveApp.workspaceDir, "analytics/assets/analytics/orders.sql"),
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select * from analytics.table_that_does_not_exist
`,
      "utf8",
    );

    const runId = await triggerPipelineRun(liveApp, request);
    const detail = await waitForRunDetail(
      liveApp,
      request,
      runId,
      (current) => current.run.status === "failed",
    );
    expect(detail.run.error).toBeTruthy();

    await page.goto(`${liveApp.baseURL}/runs/${runId}`);
    await expect(page.getByRole("tab", { name: "stderr" })).toHaveCount(0);
    await expect(page.locator('[data-event-tone="failure"]').first()).toBeVisible({
      timeout: 30000,
    });
    await page.getByRole("tab", { name: "Output" }).click();

    const terminal = page.locator('[data-slot="tabs-content"][data-state="active"] pre');
    await expect(terminal).toContainText(detail.run.error!, { timeout: 30000 });
  });

  test("rejects a missing dependency before any asset starts", async ({ liveApp, request }) => {
    await writeFile(
      join(liveApp.workspaceDir, "analytics/assets/analytics/orders.sql"),
      `/* @bruin
type: duckdb.sql
depends:
  - analytics.missing
materialization:
  type: view
@bruin */

select 1 as id
`,
      "utf8",
    );

    const runId = await triggerPipelineRun(liveApp, request);
    const detail = await waitForRunDetail(
      liveApp,
      request,
      runId,
      (current) => current.run.status === "failed",
    );
    const output = detail.logs?.map((line) => line.line).join("") ?? "";

    expect(detail.run.error).toContain("pipeline dependency validation failed with 1 issue");
    expect(output).toContain("Dependency 'analytics.missing' does not exist");
    expect(output).toContain("(dependency-exists)");
    expect(output).not.toContain("Starting the pipeline execution");
    expect(detail.steps ?? []).toEqual([]);
  });
});

async function triggerPipelineRun(liveApp: LiveApp, request: APIRequestContext) {
  const scheduleResponse = await request.get(`${liveApp.baseURL}/api/schedules`);
  expect(scheduleResponse.ok()).toBe(true);
  const schedules = (await scheduleResponse.json()) as ScheduleResponse;
  const pipeline =
    schedules.schedules.find((item) => item.pipeline_name === "analytics") ??
    schedules.schedules[0];
  expect(pipeline).toBeTruthy();

  const triggerResponse = await request.post(
    `${liveApp.baseURL}/api/pipelines/${encodeURIComponent(pipeline.pipeline_id)}/trigger`,
    {
      data: { trigger: "manual" },
    },
  );
  expect(triggerResponse.ok()).toBe(true);
  const triggered = (await triggerResponse.json()) as TriggerResponse;
  expect(triggered.run.id).toBeTruthy();

  await waitForRunDetail(liveApp, request, triggered.run.id, (detail) =>
    ["success", "failed", "running"].includes(detail.run.status),
  );
  return triggered.run.id;
}

async function waitForRunDetail(
  liveApp: LiveApp,
  request: APIRequestContext,
  runId: string,
  predicate: (detail: RunDetailResponse) => boolean,
) {
  const deadline = Date.now() + 60000;
  let lastDetail: RunDetailResponse | null = null;
  while (Date.now() < deadline) {
    const response = await request.get(`${liveApp.baseURL}/api/runs/${encodeURIComponent(runId)}`);
    if (response.ok()) {
      const detail = (await response.json()) as RunDetailResponse;
      lastDetail = detail;
      if (detail.status === "ok" && predicate(detail)) {
        return detail;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Timed out waiting for run ${runId}: ${JSON.stringify(lastDetail)}`);
}
