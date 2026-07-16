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
  run: {
    id: string;
    pipeline_id: string;
    status: string;
    pipeline: string;
    environment: string;
    error?: string;
    win_start?: string;
    win_end?: string;
    snapshot_version_id?: string;
    execution_context_resolved?: boolean;
  };
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
    await expect(page.getByTestId("schedule-run-window-context")).toContainText(
      "skip · runtime window from pinned pipeline",
    );
    await expect(page.getByText("Needs deployment", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Deploy & pin" }).first()).toBeVisible();
  });

  test("surfaces run list and run-detail transport failures", async ({ liveApp, page }) => {
    await page.route("**/api/runs**", async (route) => {
      const pathname = new URL(route.request().url()).pathname;
      if (pathname === "/api/runs" || pathname === "/api/runs/unavailable-run") {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: { message: "scheduler store unavailable" } }),
        });
        return;
      }
      await route.fallback();
    });

    await page.goto(`${liveApp.baseURL}/runs`);
    await expect(page.getByRole("alert")).toContainText("Runs could not be refreshed");
    await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();

    await page.goto(`${liveApp.baseURL}/runs/unavailable-run`);
    await expect(page.getByRole("alert")).toContainText("Run details unavailable");
    await expect(page.getByText("Loading run details", { exact: true })).toHaveCount(0);
  });

  test("renders follower ownership as read-only", async ({ liveApp, page }) => {
    await page.route("**/api/env-schedules", async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "ok",
          scheduler: {
            state: "follower",
            message: "Schedules are managed by another Renart process.",
          },
          schedules: [],
          archived: [],
        }),
      });
    });

    await page.goto(`${liveApp.baseURL}/schedules`);
    await expect(
      page.getByText("Schedules are managed by another Renart process", { exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "New schedule" })).toBeDisabled();
    await expect(page.getByText("Read-only", { exact: true })).toBeVisible();
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

    const pinnedRunRequest = page.waitForRequest(
      (request) =>
        request.url().endsWith(`/api/pipelines/${analyticsPipelineId}/trigger`) &&
        request.method() === "POST",
    );
    await page.route(`**/api/pipelines/${analyticsPipelineId}/trigger`, async (route) => {
      await route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({
          status: "ok",
          run: {
            id: "pinned-ui-check",
            pipeline_id: analyticsPipelineId,
            environment: "default",
            trigger: "manual",
            status: "queued",
          },
        }),
      });
    });
    await page.getByRole("button", { name: `Run pinned ${pinnedVersion.slice(0, 8)}` }).click();
    expect((await pinnedRunRequest).postDataJSON()).toMatchObject({
      source: "snapshot",
      snapshot_version_id: pinnedVersion,
      environment: "default",
    });
    await page.unroute(`**/api/pipelines/${analyticsPipelineId}/trigger`);

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

  test("blocks a corrupt latest pin and offers repair", async ({ liveApp, page, request }) => {
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

    await page.route("**/api/pipelines/**/deploy/status", async (route) => {
      const pathname = new URL(route.request().url()).pathname;
      if (pathname === `/api/pipelines/${analyticsPipelineId}/deploy/status`) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            has_snapshot: true,
            executable: false,
            integrity_error: "snapshot blob is missing",
            in_sync: false,
            version_id: pinnedVersion,
            snapshot_count: 1,
          }),
        });
        return;
      }
      await route.fallback();
    });

    await page.goto(`${liveApp.baseURL}/schedules`);
    await expect(page.getByText("Deployment needs repair", { exact: true })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByRole("button", { name: "Repair & pin" })).toBeEnabled();
    await expect(
      page.getByRole("button", { name: `Run pinned ${pinnedVersion.slice(0, 8)}` }),
    ).toBeDisabled();
    await expect(page.getByTestId("schedule-run-window-context")).toContainText(
      "runtime window from pinned pipeline",
    );
  });

  test("shows triggered runs in the runs list", async ({ liveApp, page, request }) => {
    const runId = await triggerPipelineRun(liveApp, request);

    await page.goto(`${liveApp.baseURL}/runs`);

    await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible();
    await expect(page.getByText(runId, { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("analytics", { exact: true }).first()).toBeVisible();
  });

  test("reruns a non-default original with visibly labeled defaults", async ({
    liveApp,
    page,
    request,
  }) => {
    const runId = await triggerPipelineRun(liveApp, request, {
      start: "2026-07-15T00:00:00Z",
      end: "2026-07-16T00:00:00Z",
      sensor_mode: "skip",
    });
    const original = await waitForRunDetail(
      liveApp,
      request,
      runId,
      (detail) => detail.run.execution_context_resolved === true,
    );
    expect(original.run.execution_context_resolved).toBe(true);
    expect(original.run.win_start).toBeTruthy();
    expect(original.run.win_end).toBeTruthy();
    const acceptedRun = {
      id: "default-mode-rerun",
      pipeline_id: analyticsPipelineId,
      pipeline: original.run.pipeline,
      environment: original.run.environment,
      trigger: "manual",
      status: "queued",
    };

    await page.route(`**/api/pipelines/${analyticsPipelineId}/trigger`, async (route) => {
      await route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({ status: "ok", run: acceptedRun }),
      });
    });
    await page.route("**/api/runs/default-mode-rerun", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "ok", run: acceptedRun, logs: [], steps: [] }),
      });
    });

    await page.goto(`${liveApp.baseURL}/runs/${runId}`);
    const context = page.getByTestId("run-again-context");
    await expect(context).toBeVisible();
    await expect(context).toContainText("Rerun source current saved workspace");
    await expect(context).toContainText("Environment default");
    await expect(context).toContainText("Recorded window");
    await expect(context).not.toContainText("no recorded window");
    await expect(context).toContainText("Mode default execution");

    const rerunRequest = page.waitForRequest(
      (candidate) =>
        candidate.url().endsWith(`/api/pipelines/${analyticsPipelineId}/trigger`) &&
        candidate.method() === "POST",
    );
    await page.getByRole("button", { name: "Run current workspace with defaults" }).click();
    expect((await rerunRequest).postDataJSON()).toEqual({
      source: "working_tree",
      environment: original.run.environment,
      start: original.run.win_start,
      end: original.run.win_end,
    });

    await expect(page).toHaveURL(new RegExp("/runs/default-mode-rerun$"));
    await expect(page.getByRole("heading", { name: "Run default-mode-rerun" })).toBeVisible();
  });

  test("omits unresolved legacy environment and window from a rerun", async ({ liveApp, page }) => {
    await page.setViewportSize({ width: 900, height: 800 });
    const unresolvedRun = {
      id: "legacy-unresolved-context",
      pipeline_id: analyticsPipelineId,
      pipeline: "analytics",
      environment: "request-only-environment",
      trigger: "manual",
      status: "failed",
      win_start: "2026-07-15T00:00:00Z",
      win_end: "2026-07-16T00:00:00Z",
      execution_context_resolved: false,
    };
    const acceptedRun = {
      id: "legacy-default-rerun",
      pipeline_id: analyticsPipelineId,
      pipeline: "analytics",
      environment: "",
      trigger: "manual",
      status: "queued",
      execution_context_resolved: false,
    };
    await page.route("**/api/runs/legacy-unresolved-context", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "ok", run: unresolvedRun, logs: [], steps: [] }),
      });
    });
    await page.route(`**/api/pipelines/${analyticsPipelineId}/trigger`, async (route) => {
      await route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({ status: "ok", run: acceptedRun }),
      });
    });
    await page.route("**/api/runs/legacy-default-rerun", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "ok", run: acceptedRun, logs: [], steps: [] }),
      });
    });

    await page.goto(`${liveApp.baseURL}/runs/legacy-unresolved-context`);
    const context = page.getByTestId("run-again-context");
    await expect(context).toContainText("Environment current default resolved at start");
    await expect(context).toContainText("current pipeline default resolved at start");
    await expect(page.getByText(/execution context unavailable/)).toBeVisible();
    const rerunButton = page.getByRole("button", {
      name: "Run current workspace with defaults",
    });
    await expect(rerunButton).toContainText("Run with defaults");

    const rerunRequest = page.waitForRequest(
      (candidate) =>
        candidate.url().endsWith(`/api/pipelines/${analyticsPipelineId}/trigger`) &&
        candidate.method() === "POST",
    );
    await rerunButton.click();
    expect((await rerunRequest).postDataJSON()).toEqual({ source: "working_tree" });
    await expect(page).toHaveURL(new RegExp("/runs/legacy-default-rerun$"));
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
    await expect(page.getByTestId("run-again-context")).toContainText("default");
    await expect(page.getByTestId("run-again-context")).toContainText("Recorded window");
    await expect(page.getByTestId("run-again-context")).toContainText("Mode default execution");
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

      const timelineTrack = page.locator('[data-testid="run-timeline-track"]').first();
      const timelineBar = timelineTrack.getByTestId("run-timeline-bar");
      await expect(timelineBar).toHaveAttribute("data-slot", "tooltip-trigger");
      await expect(
        timelineTrack.locator('xpath=ancestor::*[@data-slot="scroll-area-viewport"]'),
      ).toHaveCount(1);

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

async function triggerPipelineRun(
  liveApp: LiveApp,
  request: APIRequestContext,
  input: { start?: string; end?: string; sensor_mode?: "once" | "wait" | "skip" } = {},
) {
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
      data: { source: "working_tree", ...input },
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
