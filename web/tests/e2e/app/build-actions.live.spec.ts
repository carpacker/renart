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
const customersAssetId = Buffer.from("analytics/assets/analytics/customers.sql").toString(
  "base64url",
);
const ordersAssetId = Buffer.from("analytics/assets/analytics/orders.sql").toString("base64url");
const pythonAssetId = Buffer.from("analytics/assets/analytics/py_metric.py").toString("base64url");

test.describe("app build actions live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("centers a fitting DAG and opens the first asset selection in split view", async ({
    liveApp,
    page,
  }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The full Build canvas is a desktop affordance.",
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/canvas`);
    const flow = page.locator(".react-flow").first();
    await expect(flow).toBeVisible({ timeout: 15000 });
    const assetNodes = page.locator('[data-testid="lineage-asset"]');
    await expect(assetNodes).toHaveCount(2, { timeout: 15000 });

    await expect
      .poll(async () => {
        const flowBox = await flow.boundingBox();
        const nodeBoxes = await assetNodes.evaluateAll((nodes) =>
          nodes.map((node) => {
            const box = node.getBoundingClientRect();
            return { left: box.left, right: box.right };
          }),
        );
        if (!flowBox || nodeBoxes.length === 0) return Number.POSITIVE_INFINITY;
        const graphLeft = Math.min(...nodeBoxes.map((box) => box.left));
        const graphRight = Math.max(...nodeBoxes.map((box) => box.right));
        return Math.abs((graphLeft + graphRight) / 2 - (flowBox.x + flowBox.width / 2));
      })
      .toBeLessThan(3);

    await page
      .locator(`[data-testid="lineage-asset"][data-asset-id="${customersAssetId}"]`)
      .click();
    await expect(page).toHaveURL(
      new RegExp(`/pipelines/${pipelineId}/assets/${customersAssetId}/split(?:[?].*)?$`),
    );

    await page.getByRole("link", { name: "Canvas view" }).click();
    await expect(page).toHaveURL(
      new RegExp(`/pipelines/${pipelineId}/assets/${customersAssetId}/canvas(?:[?].*)?$`),
    );
    await page.locator(`[data-testid="lineage-asset"][data-asset-id="${ordersAssetId}"]`).click();
    await expect(page).toHaveURL(
      new RegExp(`/pipelines/${pipelineId}/assets/${ordersAssetId}/canvas(?:[?].*)?$`),
    );
  });

  test("materialize and inspect buttons run the real asset", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });
    const materializeResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/materialize/stream`) &&
        response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await materializeResponse;

    // The results panel switches to the materialize tab and shows run output.
    await expect(page.locator("pre.font-console").first()).toContainText(/\S/, {
      timeout: 15000,
    });

    const inspectResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/inspect`) && response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Inspect", exact: true }).click();
    await inspectResponse;

    await expect(page.getByText("Ada").first()).toBeVisible({ timeout: 15000 });

    // The query that actually ran is shown as a collapsible line above the table.
    const disclosure = page.getByTestId("rendered-query-disclosure");
    await expect(disclosure).toBeVisible({ timeout: 15000 });
    await expect(disclosure).toContainText(/select/i);
    await disclosure.getByRole("button", { expanded: false }).click();
    await expect(disclosure.locator("pre")).toContainText(/select/i);
  });

  test("renders saved execution SQL without running the asset", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });
    const executionRequests: string[] = [];
    page.on("request", (request) => {
      const url = request.url();
      if (
        request.method() === "POST" &&
        (url.includes("/materialize/stream") ||
          url.includes("/trigger") ||
          url.endsWith("/api/run"))
      ) {
        executionRequests.push(url);
      }
    });

    const renderResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/render`) && response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Render saved asset", exact: true }).click();
    const response = await renderResponse;
    expect(response.request().postDataJSON()).toMatchObject({
      environment: "default",
      full_refresh: false,
    });

    const payload = (await response.json()) as {
      provenance: { source: { kind: string; merkle_root: string } };
      stages: Array<{ kind: string; content?: string; fidelity: string }>;
    };
    expect(payload.provenance.source).toMatchObject({ kind: "working_tree" });
    expect(payload.provenance.source.merkle_root).toMatch(/^[a-f0-9]{64}$/);
    expect(payload.stages.find((stage) => stage.kind === "compiled_query")).toMatchObject({
      fidelity: "exact",
    });
    const executionSQL = payload.stages.find((stage) => stage.kind === "execution_sql")?.content;
    expect(executionSQL).toMatch(/create(?:\s+or\s+replace)?\s+view/i);

    const preview = page.getByTestId("asset-render-view");
    await expect(preview).toBeVisible({ timeout: 15000 });
    await expect(preview).toContainText("Preview — not executed");
    await expect(preview).toContainText("Saved workspace");
    await expect(preview.getByRole("radio", { name: "Compiled query" })).toBeChecked();
    await preview.getByRole("radio", { name: "Execution SQL" }).click();
    await expect(preview.getByRole("radio", { name: "Execution SQL" })).toBeChecked();
    await expect(preview.locator(".view-lines").first()).toContainText(
      /create(?:\s+or\s+replace)?\s+view/i,
    );
    expect(executionRequests).toEqual([]);

    const assetEditor = page.locator(".monaco-editor").first();
    await assetEditor.click();
    await page.keyboard.press("ControlOrMeta+End");
    const savedDraftMarker = "-- render saved draft";
    await page.keyboard.type(`\n${savedDraftMarker}`);
    await expect(
      page.getByText("Render an asset to preview its saved operations here."),
    ).toBeVisible();

    const rerenderResponse = page.waitForResponse(
      (candidate) =>
        candidate.url().includes(`/api/assets/${customersAssetId}/render`) && candidate.ok(),
      { timeout: 30000 },
    );
    const savedWorkspaceRefresh = page.waitForResponse(
      (candidate) =>
        candidate.url().includes(`/api/pipelines/${pipelineId}/type-check`) && candidate.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Render saved asset", exact: true }).click();
    const rerenderPayload = (await (await rerenderResponse).json()) as {
      stages: Array<{ kind: string; content?: string }>;
    };
    expect(
      rerenderPayload.stages.find((stage) => stage.kind === "compiled_query")?.content,
    ).toContain(savedDraftMarker);

    await expect
      .poll(
        async () => {
          const workspaceResponse = await page.request.get(`${liveApp.baseURL}/api/workspace`);
          if (!workspaceResponse.ok()) return "";
          const workspace = (await workspaceResponse.json()) as WorkspaceResponse;
          return (
            workspace.pipelines
              .flatMap((pipeline) => pipeline.assets)
              .find((asset) => asset.id === customersAssetId)?.content ?? ""
          );
        },
        { timeout: 30000 },
      )
      .toContain(savedDraftMarker);
    await savedWorkspaceRefresh;
    await expect(preview).toBeVisible({ timeout: 15000 });

    const savedWorkspaceResponse = await page.request.get(`${liveApp.baseURL}/api/workspace`);
    expect(savedWorkspaceResponse.ok()).toBe(true);
    const savedWorkspace = (await savedWorkspaceResponse.json()) as WorkspaceResponse;
    const savedContent = savedWorkspace.pipelines
      .flatMap((pipeline) => pipeline.assets)
      .find((asset) => asset.id === customersAssetId)?.content;
    expect(savedContent).toContain(savedDraftMarker);
    const externalMarker = "-- external workspace change";
    const externalUpdate = await page.request.put(
      `${liveApp.baseURL}/api/pipelines/${pipelineId}/assets/${customersAssetId}`,
      {
        data: {
          content: `${savedContent?.trimEnd()}\n${externalMarker}\n`,
        },
      },
    );
    expect(externalUpdate.ok()).toBe(true);
    await expect(
      page.getByText("Render an asset to preview its saved operations here."),
    ).toBeVisible({ timeout: 30000 });
    await expect(preview).toHaveCount(0);

    const externalRenderResponse = page.waitForResponse(
      (candidate) =>
        candidate.url().includes(`/api/assets/${customersAssetId}/render`) && candidate.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Render saved asset", exact: true }).click();
    const externalRenderPayload = (await (await externalRenderResponse).json()) as {
      stages: Array<{ kind: string; content?: string }>;
    };
    expect(
      externalRenderPayload.stages.find((stage) => stage.kind === "compiled_query")?.content,
    ).toContain(externalMarker);
    await expect(preview).toBeVisible({ timeout: 15000 });

    await assetEditor.click();
    await page.keyboard.press("ControlOrMeta+End");
    await page.keyboard.type("\n-- newer unsaved render intent");
    await expect(
      page.getByText("Render an asset to preview its saved operations here."),
    ).toBeVisible();
    await expect(preview).toHaveCount(0);
  });

  test("pipeline run button triggers a scheduler run", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });

    let saveFinished = false;
    let saveFinishedWhenTriggered: boolean | undefined;
    await page.route(`**/api/pipelines/${pipelineId}/assets/${customersAssetId}`, async (route) => {
      if (route.request().method() !== "PUT") {
        await route.continue();
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 400));
      const response = await route.fetch();
      saveFinished = true;
      await route.fulfill({ response });
    });
    page.on("request", (request) => {
      if (
        request.method() === "POST" &&
        request.url().includes(`/api/pipelines/${pipelineId}/trigger`)
      ) {
        saveFinishedWhenTriggered = saveFinished;
      }
    });

    const editor = page.locator(".monaco-editor").first();
    await editor.click();
    await page.keyboard.press("Control+End");
    await page.keyboard.type("\n-- save barrier e2e");

    const runButton = page.getByRole("button", { name: "Run workspace", exact: true });
    await expect(runButton).toHaveAttribute(
      "title",
      /^Run workspace · default · \d{4}-\d{2}-\d{2} \d{2}:\d{2}–\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC$/,
    );
    await expect(
      page.getByRole("button", { name: /^(?:Fresh|Build needed: \d+ stale assets?)/ }),
    ).toBeVisible();

    const triggerResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/trigger`) && response.ok(),
      { timeout: 30000 },
    );
    await runButton.click();
    const response = await triggerResponse;
    expect(response.request().postDataJSON()).toMatchObject({ source: "working_tree" });
    expect(saveFinishedWhenTriggered).toBe(true);

    const output = page.locator("pre.font-console").first();
    await expect(output).toContainText("Analyzed the pipeline 'analytics'", {
      timeout: 30000,
    });
    await expect(output).not.toContainText(/Queued manual River run|Run started\.|Run queued\./);
  });

  test("does not report fresh when the freshness request is unavailable", async ({
    liveApp,
    page,
  }) => {
    await page.route(`**/api/pipelines/${pipelineId}/staleness**`, async (route) => {
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({
          status: "error",
          error: { code: "staleness_unavailable", message: "staleness store unavailable" },
        }),
      });
    });

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });

    const freshness = page.getByRole("button", { name: "Freshness unavailable", exact: true });
    await expect(freshness).toBeVisible();
    await expect(freshness).toBeDisabled();
    await expect(freshness).toHaveAttribute("title", /staleness store unavailable/);
    await expect(page.getByRole("button", { name: "Fresh", exact: true })).toHaveCount(0);
  });

  test("links a rejected pipeline trigger to the already active run", async ({ liveApp, page }) => {
    const activeRunId = "active-run-id";
    await page.route(`**/api/pipelines/${pipelineId}/trigger`, async (route) => {
      await route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          status: "error",
          error: {
            code: "pipeline_run_active",
            message: `pipeline ${pipelineId} already has active run ${activeRunId}`,
            details: { pipeline_id: pipelineId, active_run_id: activeRunId },
          },
        }),
      });
    });

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });

    await page.getByRole("button", { name: "Run workspace", exact: true }).click();
    await expect(page.getByText(`Run ${activeRunId}`, { exact: true })).toBeVisible();
    await expect(
      page.getByText(new RegExp(`Run ${activeRunId} is already queued or running`)),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: "Open run", exact: true })).toHaveAttribute(
      "href",
      `/runs/${activeRunId}`,
    );
  });

  test("explorer creation actions live at the workspace and pipeline scopes", async ({
    liveApp,
    page,
  }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The explorer action toolbar is desktop-only.",
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);

    await page.getByRole("button", { name: "New pipeline", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "New pipeline" })).toBeVisible();
    await page
      .getByRole("dialog", { name: "New pipeline" })
      .getByRole("button", { name: "Cancel" })
      .click();

    const newAsset = page.getByRole("button", { name: /^New asset in / });
    const newFolder = page.getByRole("button", { name: /^New folder in / });
    await expect(newAsset).toBeVisible();
    await expect(newFolder).toBeVisible();

    await newAsset.click();
    await expect(page.getByRole("dialog", { name: "New asset" })).toBeVisible();
    await page
      .getByRole("dialog", { name: "New asset" })
      .getByRole("button", { name: "Cancel" })
      .click();

    await newFolder.click();
    await expect(page.getByRole("dialog", { name: "New folder" })).toBeVisible();
    await page
      .getByRole("dialog", { name: "New folder" })
      .getByRole("button", { name: "Cancel" })
      .click();
  });

  test("ad hoc editor uses Monaco with SQL intellisense and runs queries", async ({
    liveApp,
    page,
  }) => {
    // Asserts the explorer entry and the top-bar "Ad-hoc" link highlight in
    // tandem; both are desktop chrome (the explorer is a drawer on mobile and the
    // top-bar link is hidden below lg).
    test.skip(
      test.info().project.name.includes("mobile"),
      "Explorer + top-bar ad-hoc affordances are desktop-only.",
    );

    // Bare asset URLs open the split editor/canvas view by default.
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}`);
    await expect(page).toHaveURL(
      new RegExp(`/pipelines/${pipelineId}/assets/${customersAssetId}/split$`),
    );
    await expect(page.locator(".react-flow").first()).toBeVisible({ timeout: 15000 });

    // Filtering narrows the current pipeline's assets and can be cleared.
    const filter = page.getByRole("textbox", { name: "Filter assets" });
    await filter.fill("orders");
    await expect(page.getByRole("button", { name: /orders\.sql/ })).toBeVisible();
    await expect(page.getByRole("button", { name: /customers\.sql/ })).toHaveCount(0);
    await filter.fill("no-such-asset");
    await expect(page.getByText("No matching assets.")).toBeVisible();
    await page.getByRole("button", { name: "Clear asset filter" }).click();
    await expect(page.getByRole("button", { name: /customers\.sql/ })).toBeVisible();

    // Opening ad hoc from a split view keeps the split layout.
    await page.getByRole("button", { name: "Ad-hoc query" }).click();
    await expect(page).toHaveURL(
      new RegExp(
        `/pipelines/${pipelineId}/assets/${customersAssetId}/split[?].*editor=adhoc(?:&|$)`,
      ),
    );

    const editor = page.locator(".monaco-editor").first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("Ad-hoc query").first()).toBeVisible();

    // Both the explorer entry and the top-bar button highlight the ad hoc mode.
    await expect(page.locator("button", { hasText: "Ad-hoc query" }).first()).toHaveClass(
      /ring-primary/,
    );
    await expect(page.getByRole("link", { name: "Ad-hoc" }).first()).toHaveClass(/ring-primary/);

    // Replace the default draft with a marker query that needs Jinja rendering.
    await editor.click();
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type("select 'adhoc_ok' as marker, '{{ start_date }}' as win_start");

    // The ad hoc editor reuses the SQL parse-context intellisense.
    const parseContextSeen = page.waitForResponse(
      (response) => response.url().includes("/api/sql/parse-context") && response.ok(),
      { timeout: 15000 },
    );
    await parseContextSeen;

    const queryResponse = page.waitForResponse(
      (response) => response.url().includes("/api/sql/query") && response.ok(),
      { timeout: 30000 },
    );
    await page.getByTitle("Run (⌘ + ↵)").click();
    const queryRequestBody = (await queryResponse).request().postDataJSON() as {
      query: string;
    };
    // The Jinja template was rendered before execution.
    expect(queryRequestBody.query).not.toContain("{{");
    expect(queryRequestBody.query).toMatch(/\d{4}-\d{2}-\d{2}/);
    const queryPayload = (await (await queryResponse).json()) as {
      status: string;
      columns: string[];
    };
    expect(queryPayload.status).toBe("ok");
    expect(queryPayload.columns).toContain("marker");

    await expect(page.getByText("adhoc_ok").first()).toBeVisible({
      timeout: 15000,
    });

    // The rendered query is shown collapsibly above the results.
    const disclosure = page.getByTestId("rendered-query-disclosure");
    await expect(disclosure).toBeVisible();
    await expect(disclosure).toContainText("adhoc_ok");
    await expect(disclosure).not.toContainText("{{");
  });

  test("ad hoc mode adds a split editor to canvas and preserves full-size code", async ({
    liveApp,
    page,
  }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The top-bar ad-hoc affordance is hidden below lg.",
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/canvas`);
    await page.getByRole("link", { name: "Ad-hoc" }).click();
    await expect(page).toHaveURL(
      new RegExp(
        `/pipelines/${pipelineId}/assets/${customersAssetId}/split[?].*editor=adhoc(?:&|$)`,
      ),
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await page.getByRole("link", { name: "Ad-hoc" }).click();
    await expect(page).toHaveURL(
      new RegExp(
        `/pipelines/${pipelineId}/assets/${customersAssetId}/code[?].*editor=adhoc(?:&|$)`,
      ),
    );
  });

  test("python assets never call the SQL parse-context endpoint", async ({ liveApp, page }) => {
    await writeFile(
      join(liveApp.workspaceDir, "analytics", "assets", "analytics", "py_metric.py"),
      `""" @bruin
name: analytics.py_metric
type: python
@bruin """

print("hello")
`,
      "utf8",
    );

    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${liveApp.baseURL}/api/workspace`);
          if (!response.ok()) {
            return "";
          }
          const workspace = (await response.json()) as WorkspaceResponse;
          return (
            workspace.pipelines
              .flatMap((pipeline) => pipeline.assets)
              .find((asset) => asset.id === pythonAssetId)?.content ?? ""
          );
        },
        { timeout: 30000 },
      )
      .toContain("print");

    const parseContextRequests: string[] = [];
    page.on("request", (request) => {
      if (request.url().includes("/api/sql/parse-context")) {
        parseContextRequests.push(request.url());
      }
    });

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${pythonAssetId}/code`);
    const editor = page.locator(".monaco-editor").first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await expect(page.locator(".view-lines").first()).toContainText("print", {
      timeout: 15000,
    });

    // Type into the editor and give the 350 ms parse-context debounce plenty
    // of time to fire if the guard were broken.
    await editor.click();
    await page.keyboard.press("ControlOrMeta+End");
    await page.keyboard.type("\n# comment");
    await page.waitForTimeout(1500);

    expect(parseContextRequests).toEqual([]);
  });
});
