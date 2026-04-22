import { expect, Page, test } from "@playwright/test";

test.describe("workspace basic flows", () => {
  test("shows the empty workspace state", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createEmptyWorkspaceState());
    await page.goto("/");

    await expect(page).toHaveTitle("Workspace · Renart");
    await expect(
      page.getByRole("heading", { name: "Create your first pipeline" })
    ).toBeVisible();
    await expect(createPipelineButton(page)).toBeVisible();
  });

  test("opens the create pipeline dialog from the empty state", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createEmptyWorkspaceState());
    await page.goto("/");

    await createPipelineButton(page).click();

    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByLabel("Pipeline path")).toBeVisible();
  });

  test("renders the selected asset in the editor header", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState());
    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await expect(page).toHaveTitle("analytics.customers · analytics · Renart");
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.customers");
    await expect(page.getByTestId("editor-asset-path")).toHaveText(
      "pipelines/analytics/assets/customers.sql"
    );
  });

  test("switches assets from the sidebar", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState());
    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await page.getByRole("link", { name: "analytics.orders" }).click();

    await expect(page).toHaveTitle("analytics.orders · analytics · Renart");
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.orders");
    await expect(page.getByTestId("editor-asset-path")).toHaveText(
      "pipelines/analytics/assets/orders.sql"
    );
  });

  test("opens the rename pipeline dialog from the sidebar context menu", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState());
    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await page
      .getByRole("link", { name: "analytics", exact: true })
      .first()
      .click({ button: "right" });
    await page.getByRole("menuitem", { name: "Rename Pipeline" }).click();

    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByLabel("Pipeline name")).toHaveValue("analytics");
  });

  test("opens the delete asset dialog from the editor", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState());
    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await page.getByRole("button", { name: "Delete" }).click();

    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByText('This will permanently delete "analytics.customers"')).toBeVisible();
  });

  test("keeps previous inspect result visible and shows subtle warning on inspect error", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState());

    let inspectCallCount = 0;
    await page.route("**/api/assets/asset-customers/inspect**", async (route) => {
      inspectCallCount += 1;

      const body =
        inspectCallCount === 1
          ? {
              status: "ok",
              columns: ["customer_id", "customer_name"],
              rows: [{ customer_id: 1, customer_name: "Ada" }],
              raw_output: "",
            }
          : {
              status: "error",
              columns: [],
              rows: [],
              raw_output:
                '{"error":"query execution failed: Internal: Parser Error: syntax error at or near \")\"\\n\\nLINE 3: ) as t LIMIT 200\\n ^"}\n',
              error:
                "query execution failed: Internal: Parser Error: syntax error at or near \")\"\n\nLINE 3: ) as t LIMIT 200\n ^",
            };

      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    });

    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await expect(page.getByRole("cell", { name: "Ada" })).toBeVisible();

    await page.getByRole("button", { name: "Inspect Data" }).click();

    await expect(page.getByRole("cell", { name: "Ada" })).toBeVisible();
    await expect(page.getByTestId("inspect-warning-banner")).toBeVisible();
    await expect(page.getByTestId("inspect-warning-banner")).toContainText("Parser Error");
    await expect(page.getByTestId("inspect-warning-banner")).not.toContainText('"attempts"');
    await expect(page.getByTestId("inspect-warning-banner")).not.toContainText('Error: {');
  });

  test("switches environments from the top bar and threads them into requests", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState(), {
      environments: ["default", "prod"],
    });

    const inspectRequests: string[] = [];
    const materializationRequests: string[] = [];

    await page.route("**/api/assets/asset-customers/inspect**", async (route) => {
      inspectRequests.push(route.request().url());
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          status: "ok",
          columns: ["customer_id"],
          rows: [{ customer_id: 1 }],
          raw_output: "",
        }),
      });
    });

    await page.route(
      "**/api/assets/asset-customers/materialize/stream**",
      async (route) => {
        materializationRequests.push(route.request().url());
        await route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body:
            'event: start\ndata: {"command":["run","asset-customers"]}\n\n' +
            'event: done\ndata: {"status":"ok","command":["run","asset-customers"],"output":"ok","error":"","exit_code":0,"changed_asset_ids":["asset-customers"]}\n\n',
        });
      }
    );

    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await expect(page.getByRole("combobox", { name: "Environment" })).toBeVisible();

    await page.getByRole("combobox", { name: "Environment" }).click();
    await page.getByRole("option", { name: "prod" }).click();

    await expect(page).toHaveURL(/environment=prod/);

    await page.getByRole("button", { name: "Inspect Data" }).click();
    await page.getByRole("button", { name: "Materialize", exact: true }).click();

    await expect.poll(() => inspectRequests.at(-1) ?? "").toContain(
      "environment=prod"
    );
    await expect.poll(() => materializationRequests.at(-1) ?? "").toContain(
      "environment=prod"
    );
  });

  test("shows each environment only once in the selector", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState(), {
      environments: ["default", "prod"],
    });

    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await page.getByRole("combobox", { name: "Environment" }).click();

    await expect(page.getByRole("option", { name: "default", exact: true })).toHaveCount(1);
    await expect(page.getByRole("option", { name: "prod", exact: true })).toHaveCount(1);
  });

  test("preserves the selected environment when navigating to other assets", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState(), {
      environments: ["default", "prod"],
    });

    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await page.getByRole("combobox", { name: "Environment" }).click();
    await page.getByRole("option", { name: "prod", exact: true }).click();

    await expect(page).toHaveURL(/environment=prod/);

    await page.getByRole("link", { name: "analytics.orders" }).click();
    await expect(page).toHaveURL(/asset=asset-orders/);
    await expect(page).toHaveURL(/environment=prod/);

    await page.getByRole("button", { name: "Open search" }).click();
    await page.getByPlaceholder("Search workspace commands...").fill("finance.revenue");
    await page.getByRole("option", { name: /finance\.revenue/i }).click();

    await expect(page).toHaveURL(/pipeline=pipeline-finance/);
    await expect(page).toHaveURL(/asset=asset-revenue/);
    await expect(page).toHaveURL(/environment=prod/);
  });

  test("preserves the selected environment when clicking canvas nodes", async ({ page }) => {
    await mockWorkspaceEndpoints(page, createPopulatedWorkspaceState(), {
      environments: ["default", "prod"],
    });

    await page.goto("/?pipeline=pipeline-analytics&asset=asset-customers");

    await page.getByRole("combobox", { name: "Environment" }).click();
    await page.getByRole("option", { name: "prod", exact: true }).click();

    await expect(page).toHaveURL(/environment=prod/);

    await page
      .locator(".react-flow__node")
      .filter({ hasText: "orders" })
      .first()
      .click();

    await expect(page).toHaveURL(/asset=asset-orders/);
    await expect(page).toHaveURL(/environment=prod/);
  });
});

function createPipelineButton(page: Page) {
  return page.getByRole("button", { name: "Create pipeline" }).last();
}

async function mockWorkspaceEndpoints(
  page: Page,
  workspace: Record<string, unknown>,
  options?: { environments?: string[] }
) {
  await page.route("**/api/onboarding/state", async (route) => {
    if (route.request().method() !== "GET") {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ status: "ok" }),
      });
      return;
    }

    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        active: false,
        step: "connection-config",
      }),
    });
  });

  await page.route("**/api/workspace", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(workspace),
    });
  });

  await page.route("**/api/config", async (route) => {
    const environments = options?.environments ?? [];
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        status: "ok",
        path: ".bruin.yml",
        default_environment: "default",
        selected_environment: "default",
        environments: environments.map((name) => ({
          name,
          connections: [],
          schema_prefix: "",
        })),
        connection_types: [],
      }),
    });
  });

  await page.route("**/api/pipelines/*/materialization", async (route) => {
    const pipelineID = route.request().url().split("/").at(-2) ?? "pipeline-analytics";

    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        pipeline_id: pipelineID,
        assets: [],
      }),
    });
  });

  await page.route("**/api/events", async (route) => {
    await route.fulfill({
      contentType: "text/event-stream",
      body: "",
    });
  });
}

function createEmptyWorkspaceState() {
  return {
    pipelines: [],
    connections: {},
    selected_environment: "default",
    errors: [],
    updated_at: new Date().toISOString(),
    revision: 1,
  };
}

function createPopulatedWorkspaceState() {
  return {
    pipelines: [
      {
        id: "pipeline-analytics",
        name: "analytics",
        path: "pipelines/analytics",
        assets: [
          {
            id: "asset-customers",
            name: "analytics.customers",
            type: "duckdb.sql",
            path: "pipelines/analytics/assets/customers.sql",
            content: "select 1 as customer_id",
            upstreams: [],
            is_materialized: false,
          },
          {
            id: "asset-orders",
            name: "analytics.orders",
            type: "duckdb.sql",
            path: "pipelines/analytics/assets/orders.sql",
            content: "select 1 as order_id",
            upstreams: ["analytics.customers"],
            is_materialized: false,
          },
        ],
      },
      {
        id: "pipeline-finance",
        name: "finance",
        path: "pipelines/finance",
        assets: [
          {
            id: "asset-revenue",
            name: "finance.revenue",
            type: "duckdb.sql",
            path: "pipelines/finance/assets/revenue.sql",
            content: "select 1 as revenue",
            upstreams: [],
            is_materialized: false,
          },
        ],
      },
    ],
    connections: {},
    selected_environment: "default",
    errors: [],
    updated_at: new Date().toISOString(),
    revision: 1,
  };
}
