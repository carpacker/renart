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

  test("pipeline run button triggers a scheduler run", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".view-lines").first()).toContainText("customer_id", {
      timeout: 15000,
    });

    const triggerResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/trigger`) && response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Run", exact: true }).click();
    await triggerResponse;

    const output = page.locator("pre.font-console").first();
    await expect(output).toContainText("Analyzed the pipeline 'analytics'", {
      timeout: 30000,
    });
    await expect(output).not.toContainText(/Queued manual River run|Run started\.|Run queued\./);
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
