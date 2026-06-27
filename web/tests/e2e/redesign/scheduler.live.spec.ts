import { expect, type APIRequestContext } from "@playwright/test";

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

test.describe("redesign scheduler pages live", () => {
  test.use({ fixtureName: "basic-workspace" });

  test("shows configured schedules", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/redesign/schedules`);

    await expect(page.getByRole("heading", { name: "Schedules" })).toBeVisible();
    await expect(page.getByText("analytics", { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("daily", { exact: true })).toBeVisible();
    // The catchup column renders the policy itself (here "skip", from the
    // fixture's default), not a generic "Catch up" label.
    await expect(page.getByText("skip", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Run" }).first()).toBeVisible();
  });

  test("shows triggered runs in the runs list", async ({ liveApp, page, request }) => {
    const runId = await triggerPipelineRun(liveApp, request);

    await page.goto(`${liveApp.baseURL}/redesign/runs`);

    await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible();
    await expect(page.getByText(runId, { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("analytics", { exact: true }).first()).toBeVisible();
  });

  test("opens a single run page with asset timings and high level events", async ({ liveApp, page, request }) => {
    const runId = await triggerPipelineRun(liveApp, request);
    const detail = await waitForRunDetail(liveApp, request, runId, (current) => (current.steps?.length ?? 0) > 0 || current.run.status !== "running");
    const stepAsset = detail.steps?.[0]?.asset;

    await page.goto(`${liveApp.baseURL}/redesign/runs/${runId}`);

    await expect(page.getByRole("heading", { name: `Run ${runId}` })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(/Run of analytics/)).toBeVisible();
    await expect(page.getByRole("tab", { name: "Events" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "stdout" })).toBeVisible();
    if (stepAsset) {
      await expect(page.getByText(stepAsset, { exact: true }).first()).toBeVisible({ timeout: 30000 });
    }
    await expect(page.getByText(/asset_(start|success)/).first()).toBeVisible({ timeout: 30000 });
  });
});

async function triggerPipelineRun(liveApp: LiveApp, request: APIRequestContext) {
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

  await waitForRunDetail(liveApp, request, triggered.run.id, (detail) => ["success", "failed", "running"].includes(detail.run.status));
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
