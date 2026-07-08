import { expect, type APIRequestContext } from "@playwright/test";
import { writeFile } from "node:fs/promises";
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
  run: { id: string; status: string; pipeline: string };
  steps?: Array<{ asset: string; status: string }>;
};

test.describe("app materialization status live", () => {
  test.use({ fixtureName: "basic-workspace" });

  test("updates canvas asset status from running to terminal without refresh", async ({ liveApp, page, request }) => {
    // This exercises live status transitions on the React Flow lineage canvas, a
    // desktop affordance: on a phone viewport the canvas doesn't surface a node's
    // terminal status the way this assertion needs.
    test.skip(test.info().project.name.includes("mobile"), "Canvas status transitions are a desktop affordance.");

    await addSlowDuckDbAsset(liveApp.workspaceDir);

    await page.goto(`${liveApp.baseURL}/catalog`);
    await expect(page.getByLabel("Execution context")).toBeVisible({ timeout: 15000 });
    await expect(page.getByLabel("Time range")).toHaveCount(0);
    await expect(page.getByText("slow_status", { exact: true })).toBeVisible({ timeout: 15000 });

    const triggerResponse = await triggerAnalyticsPipeline(liveApp, request);

    await expect(page.getByText("Running · running").first()).toBeVisible({ timeout: 30000 });

    const detail = await waitForRunDetail(liveApp, request, triggerResponse.run.id, (current) => ["success", "failed", "cancelled"].includes(current.run.status));
    expect(detail.steps?.some((step) => step.asset === "analytics.slow_status")).toBe(true);

    await expect(page.getByText("Running · running")).toHaveCount(0, { timeout: 30000 });
    await expect(page.locator("span:visible", { hasText: /^(Materialized|Failed) · / }).first()).toBeVisible({ timeout: 30000 });
  });
});

async function addSlowDuckDbAsset(workspaceDir: string) {
  await writeFile(
    join(workspaceDir, "analytics", "assets", "analytics", "slow_status.sql"),
    `/* @bruin
type: duckdb.sql
materialization:
  type: table
@bruin */

select sum(sqrt(i::double)) as slow_value
from range(50000000) as t(i)
`,
    "utf8",
  );
}

async function triggerAnalyticsPipeline(liveApp: LiveApp, request: APIRequestContext) {
  const scheduleResponse = await request.get(`${liveApp.baseURL}/api/schedules`);
  expect(scheduleResponse.ok()).toBe(true);
  const schedules = await scheduleResponse.json() as ScheduleResponse;
  const pipeline = schedules.schedules.find((item) => item.pipeline_name === "analytics") ?? schedules.schedules[0];
  expect(pipeline).toBeTruthy();

  const triggerResponse = await request.post(`${liveApp.baseURL}/api/pipelines/${encodeURIComponent(pipeline.pipeline_id)}/trigger`, {
    data: { trigger: "manual" },
  });
  expect(triggerResponse.ok()).toBe(true);
  const triggered = await triggerResponse.json() as TriggerResponse;
  expect(triggered.run.id).toBeTruthy();
  return triggered;
}

async function waitForRunDetail(
  liveApp: LiveApp,
  request: APIRequestContext,
  runId: string,
  predicate: (detail: RunDetailResponse) => boolean,
) {
  const deadline = Date.now() + 120000;
  let lastDetail: RunDetailResponse | null = null;
  while (Date.now() < deadline) {
    const response = await request.get(`${liveApp.baseURL}/api/runs/${encodeURIComponent(runId)}`);
    if (response.ok()) {
      const detail = await response.json() as RunDetailResponse;
      lastDetail = detail;
      if (detail.status === "ok" && predicate(detail)) {
        return detail;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Timed out waiting for run ${runId}: ${JSON.stringify(lastDetail)}`);
}
