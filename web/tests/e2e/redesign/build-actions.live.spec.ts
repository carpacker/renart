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
const pythonAssetId = Buffer.from(
  "analytics/assets/analytics/py_metric.py"
).toString("base64url");

test.describe("redesign build actions live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("materialize and inspect buttons run the real asset", async ({
    liveApp,
    page,
  }) => {
    await page.goto(
      `${liveApp.baseURL}/redesign/pipelines/${pipelineId}/assets/${customersAssetId}/code`
    );
    await expect(page.locator(".view-lines").first()).toContainText(
      "customer_id",
      { timeout: 15000 }
    );

    const materializeResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/materialize/stream`) &&
        response.ok(),
      { timeout: 30000 }
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await materializeResponse;

    // The results panel switches to the materialize tab and shows run output.
    await expect(page.locator("pre.font-console").first()).toContainText(/\S/, {
      timeout: 15000,
    });

    const inspectResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/inspect`) &&
        response.ok(),
      { timeout: 30000 }
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

  test("pipeline run button triggers a scheduler run", async ({
    liveApp,
    page,
  }) => {
    await page.goto(
      `${liveApp.baseURL}/redesign/pipelines/${pipelineId}/assets/${customersAssetId}/code`
    );
    await expect(page.locator(".view-lines").first()).toContainText(
      "customer_id",
      { timeout: 15000 }
    );

    const triggerResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/trigger`) &&
        response.ok(),
      { timeout: 30000 }
    );
    await page.getByRole("button", { name: "Run", exact: true }).click();
    await triggerResponse;

    await expect(page.getByText(/Queued manual River run/).first()).toBeVisible({
      timeout: 15000,
    });
  });

  test("ad hoc editor uses Monaco with SQL intellisense and runs queries", async ({
    liveApp,
    page,
  }) => {
    // Asserts the explorer entry and the top-bar "Ad-hoc" link highlight in
    // tandem; both are desktop chrome (the explorer is a drawer on mobile and the
    // top-bar link is hidden below lg).
    test.skip(test.info().project.name.includes("mobile"), "Explorer + top-bar ad-hoc affordances are desktop-only.");

    await page.goto(
      `${liveApp.baseURL}/redesign/pipelines/${pipelineId}/assets/${customersAssetId}/code?editor=adhoc`
    );

    const editor = page.locator(".monaco-editor").first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("Ad-hoc query").first()).toBeVisible();

    // Both the explorer entry and the top-bar button highlight the ad hoc mode.
    await expect(
      page.locator("button", { hasText: "Ad-hoc query" }).first()
    ).toHaveClass(/ring-primary/);
    await expect(page.getByRole("link", { name: "Ad-hoc" }).first()).toHaveClass(
      /ring-primary/
    );

    // Replace the default draft with a marker query that needs Jinja rendering.
    await editor.click();
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type("select 'adhoc_ok' as marker, '{{ start_date }}' as win_start");

    // The ad hoc editor reuses the SQL parse-context intellisense.
    const parseContextSeen = page.waitForResponse(
      (response) =>
        response.url().includes("/api/sql/parse-context") && response.ok(),
      { timeout: 15000 }
    );
    await parseContextSeen;

    const queryResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/sql/query") && response.ok(),
      { timeout: 30000 }
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

  test("python assets never call the SQL parse-context endpoint", async ({
    liveApp,
    page,
  }) => {
    await writeFile(
      join(liveApp.workspaceDir, "analytics", "assets", "analytics", "py_metric.py"),
      `""" @bruin
name: analytics.py_metric
type: python
@bruin """

print("hello")
`,
      "utf8"
    );

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
              .find((asset) => asset.id === pythonAssetId)?.content ?? ""
          );
        },
        { timeout: 30000 }
      )
      .toContain("print");

    const parseContextRequests: string[] = [];
    page.on("request", (request) => {
      if (request.url().includes("/api/sql/parse-context")) {
        parseContextRequests.push(request.url());
      }
    });

    await page.goto(
      `${liveApp.baseURL}/redesign/pipelines/${pipelineId}/assets/${pythonAssetId}/code`
    );
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
