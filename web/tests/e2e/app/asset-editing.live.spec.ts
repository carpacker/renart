import { expect, type Locator, type Page } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

const pipelineId = Buffer.from("analytics").toString("base64url");
const customersAssetId = Buffer.from("analytics/assets/analytics/customers.sql").toString(
  "base64url",
);
const ordersAssetId = Buffer.from("analytics/assets/analytics/orders.sql").toString("base64url");
const downstreamAssetId = Buffer.from("analytics/assets/analytics/downstream.sql").toString(
  "base64url",
);
const loadAssetPath = "analytics/assets/analytics/orders_load.asset.yml";
const loadAssetId = Buffer.from(loadAssetPath).toString("base64url");

type WorkspaceAsset = {
  id: string;
  name: string;
  type?: string;
  content?: string;
  connection?: string;
  explicit_connection?: string;
  parameters?: Record<string, string>;
  upstreams: string[];
  meta?: Record<string, string>;
  tags?: string[];
  materialization_strategy?: string;
  incremental_key?: string;
  time_granularity?: string;
  columns?: Array<{
    name: string;
    type?: string;
    primary_key?: boolean;
    update_on_merge?: boolean;
    merge_sql?: string;
  }>;
};
type WorkspaceResponse = { pipelines: Array<{ id: string; assets: WorkspaceAsset[] }> };

async function fetchAsset(
  liveApp: LiveApp,
  request: { get: (url: string) => Promise<{ json(): Promise<unknown> }> },
  assetName: string,
): Promise<WorkspaceAsset | undefined> {
  const response = await request.get(`${liveApp.baseURL}/api/workspace`);
  const workspace = (await response.json()) as WorkspaceResponse;
  return workspace.pipelines.flatMap((p) => p.assets).find((a) => a.name === assetName);
}

async function pollAsset(
  liveApp: LiveApp,
  request: { get: (url: string) => Promise<{ json(): Promise<unknown> }> },
  assetName: string,
  predicate: (asset: WorkspaceAsset) => boolean,
): Promise<WorkspaceAsset> {
  let found: WorkspaceAsset | undefined;
  await expect
    .poll(
      async () => {
        found = await fetchAsset(liveApp, request, assetName);
        return found ? predicate(found) : false;
      },
      { timeout: 30000 },
    )
    .toBe(true);
  if (!found) throw new Error(`asset ${assetName} never satisfied predicate`);
  return found;
}

async function openAssetProperties(page: Page): Promise<Locator> {
  const inspector = page.locator('[data-testid="asset-inspector"]:visible').first();
  if (!(await inspector.isVisible().catch(() => false))) {
    const trigger = page
      .getByRole("button", { name: "Asset properties" })
      .or(page.getByRole("button", { name: "Show properties" }))
      .first();
    await expect(trigger).toBeVisible({ timeout: 15000 });
    await trigger.click();
  }
  await expect(inspector).toBeVisible({ timeout: 15000 });
  return inspector;
}

async function expectCompactAssetDescription(page: Page, description: string) {
  const describedNode = page.getByTestId(`rf__node-${customersAssetId}`);
  const baselineNode = page.getByTestId(`rf__node-${ordersAssetId}`);
  await expect(describedNode).toBeVisible({ timeout: 15000 });
  await expect(baselineNode).toBeVisible({ timeout: 15000 });

  const metadata = describedNode.locator('[data-slot="asset-node-metadata"]');
  const connection = metadata.locator('[data-slot="asset-node-connection"]');
  const descriptionElement = metadata.locator('[data-slot="asset-node-description"]');
  await expect(connection).toBeVisible();
  await expect(descriptionElement).toHaveText(description);
  await expect(metadata).toHaveCSS("display", "flex");
  await expect(metadata).toHaveCSS("align-items", "center");
  const metadataOrder = await metadata
    .locator(":scope > [data-slot]")
    .evaluateAll((elements) => elements.map((element) => element.getAttribute("data-slot")));
  expect(metadataOrder).toEqual(["asset-node-description", "asset-node-connection"]);
  const [descriptionX, connectionX] = await Promise.all(
    [descriptionElement, connection].map((element) =>
      element.evaluate((node) => node.getBoundingClientRect().x),
    ),
  );
  expect(descriptionX).toBeLessThan(connectionX);
  await expect(descriptionElement).toHaveCSS("overflow", "hidden");
  await expect(descriptionElement).toHaveCSS("text-overflow", "ellipsis");
  await expect(descriptionElement).toHaveCSS("white-space", "nowrap");

  const descriptionOverflows = await descriptionElement.evaluate(
    (element) => element.scrollWidth > element.clientWidth,
  );
  expect(descriptionOverflows).toBe(true);

  const [describedHeight, baselineHeight] = await Promise.all(
    [describedNode, baselineNode].map((node) =>
      node
        .locator('[data-slot="asset-node"]')
        .evaluate((element) => element.getBoundingClientRect().height),
    ),
  );
  expect(Math.abs(describedHeight - baselineHeight)).toBeLessThanOrEqual(1);
}

test.describe("app asset editing workbench live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("keeps asset descriptions left of connections on both canvases", async ({
    liveApp,
    page,
  }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The lineage canvas is a desktop affordance.",
    );

    const description = "Customer profile records";
    const update = await page.request.put(
      `${liveApp.baseURL}/api/pipelines/${pipelineId}/assets/${customersAssetId}`,
      { data: { meta: { description } } },
    );
    expect(update.ok()).toBe(true);
    await pollAsset(
      liveApp,
      page.request,
      "analytics.customers",
      (asset) => asset.meta?.description === description,
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/canvas`);
    await expectCompactAssetDescription(page, description);

    await page.goto(`${liveApp.baseURL}/catalog?asset=${customersAssetId}`);
    await expectCompactAssetDescription(page, description);
  });

  test("guided cards render and adding a manual dependency persists provenance", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });

    const properties = await openAssetProperties(page);

    // The guided metadata panel renders its focused cards.
    await expect(properties.getByRole("heading", { name: "Identity" })).toBeVisible({
      timeout: 15000,
    });
    await expect(properties.getByRole("heading", { name: "Materialization" })).toBeVisible();
    await expect(properties.getByRole("heading", { name: "Dependencies" })).toBeVisible();
    await expect(properties.getByRole("heading", { name: "Columns" })).toBeVisible();

    const identity = properties.getByRole("heading", { name: "Identity" }).locator("../..");
    await identity.getByRole("combobox").first().click();
    await expect(page.getByText("SQL assets", { exact: true })).toBeVisible();
    await expect(page.getByText("Non-SQL assets", { exact: true })).toBeVisible();
    await page.keyboard.press("Escape");

    const descriptionInput = properties.getByPlaceholder("What this asset produces");
    await descriptionInput.fill("Customer profile records");
    const descriptionResponse = page.waitForResponse(
      (r) =>
        r.url().includes(`/api/pipelines/${pipelineId}/assets/${customersAssetId}`) &&
        r.request().method() === "PUT" &&
        r.ok(),
      { timeout: 15000 },
    );
    await descriptionInput.press("Enter");
    await descriptionResponse;
    const withDescription = await pollAsset(
      liveApp,
      page.request,
      "analytics.customers",
      (a) => a.meta?.description === "Customer profile records",
    );
    expect(withDescription.meta?.description).toBe("Customer profile records");

    const tagInput = properties.getByPlaceholder("Add tag");
    await tagInput.fill("finance, north");
    const tagResponse = page.waitForResponse(
      (r) =>
        r.url().includes(`/api/pipelines/${pipelineId}/assets/${customersAssetId}`) &&
        r.request().method() === "PUT" &&
        r.ok(),
      { timeout: 15000 },
    );
    await tagInput.press("Enter");
    await tagResponse;
    const withCommaTag = await pollAsset(liveApp, page.request, "analytics.customers", (a) =>
      (a.tags ?? []).includes("finance, north"),
    );
    expect(withCommaTag.tags).toContain("finance, north");

    // Add a manual dependency via the Dependencies card.
    const txResponse = page.waitForResponse(
      (r) => r.url().includes(`/api/assets/${customersAssetId}/transactions`) && r.ok(),
      { timeout: 15000 },
    );
    const input = properties.getByPlaceholder("Add dependency (asset name)");
    await input.fill("analytics.orders");
    await input.press("Enter");
    await txResponse;

    // It surfaces under Manual and is written to the asset's provenance.
    await expect(properties.getByText("Manual")).toBeVisible({ timeout: 15000 });
    await expect(properties.getByText("analytics.orders").first()).toBeVisible();

    const customers = await pollAsset(liveApp, page.request, "analytics.customers", (a) =>
      a.upstreams.includes("analytics.orders"),
    );
    expect(customers.meta?.renart_dep_add).toContain("a:analytics.orders#full");
  });

  test("merge metadata is editable through the guided form", async ({ liveApp, page }) => {
    const declareColumns = await page.request.put(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/columns`,
      {
        data: {
          columns: [
            { name: "customer_id", type: "integer" },
            { name: "customer_name", type: "varchar" },
          ],
        },
      },
    );
    expect(declareColumns.ok()).toBe(true);

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    const properties = await openAssetProperties(page);
    const materialization = properties
      .getByRole("heading", { name: "Materialization" })
      .locator("../..");

    const strategyResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${customersAssetId}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await materialization.getByRole("combobox").click();
    await page.getByRole("option", { name: "Merge by key" }).click();
    await strategyResponse;
    await expect(
      materialization.getByText(/Merge needs at least one primary-key column/),
    ).toBeVisible({ timeout: 15000 });

    const invalidTypeCheck = await page.request.get(
      `${liveApp.baseURL}/api/pipelines/${pipelineId}/type-check`,
    );
    expect(invalidTypeCheck.ok()).toBe(true);
    const invalidReport = (await invalidTypeCheck.json()) as {
      assets: Array<{ name: string; findings: Array<{ message: string }> }>;
    };
    expect(
      invalidReport.assets
        .find((asset) => asset.name === "analytics.customers")
        ?.findings.some((finding) => finding.message.includes("primary-key")),
    ).toBe(true);

    const primaryKeyResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/columns`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await properties.getByRole("button", { name: "Set customer_id as primary key" }).click();
    await primaryKeyResponse;

    const updateOnMergeResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/columns`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await properties.getByRole("button", { name: "Update customer_name on merge" }).click();
    await updateOnMergeResponse;

    const mergeSQLResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/columns`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    const mergeSQL = properties.getByPlaceholder("merge SQL (optional)").nth(1);
    await mergeSQL.fill("COALESCE(source.customer_name, target.customer_name)");
    await mergeSQL.press("Enter");
    await mergeSQLResponse;

    const configured = await pollAsset(liveApp, page.request, "analytics.customers", (asset) => {
      const id = asset.columns?.find((column) => column.name === "customer_id");
      const name = asset.columns?.find((column) => column.name === "customer_name");
      return (
        asset.materialization_strategy === "merge" &&
        id?.primary_key === true &&
        name?.update_on_merge === true &&
        name.merge_sql === "COALESCE(source.customer_name, target.customer_name)"
      );
    });
    expect(configured.materialization_strategy).toBe("merge");

    await expect(properties.getByRole("button", { name: "YAML", exact: true })).toHaveCount(0);
    await expect(properties.getByRole("button", { name: "Form", exact: true })).toHaveCount(0);
    await expect(
      properties
        .getByRole("textbox", { name: "Name" })
        .locator('xpath=ancestor::*[@data-slot="field"]'),
    ).toHaveAttribute("data-orientation", "vertical");
    await expect(
      properties
        .getByRole("combobox", { name: "Type" })
        .locator('xpath=ancestor::*[@data-slot="field"]'),
    ).toHaveAttribute("data-orientation", "vertical");

    const unsetResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/columns`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await properties.getByRole("button", { name: "Unset customer_id as primary key" }).click();
    await unsetResponse;
    await expect(
      properties.getByRole("button", { name: "Set customer_id as primary key" }),
    ).toBeVisible({ timeout: 15000 });

    const replacementKeyResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${customersAssetId}/columns`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await properties.getByRole("button", { name: "Set customer_name as primary key" }).click();
    await replacementKeyResponse;

    const replacementConfigured = await pollAsset(
      liveApp,
      page.request,
      "analytics.customers",
      (asset) => {
        const keys = (asset.columns ?? [])
          .filter((column) => column.primary_key)
          .map((column) => column.name);
        return keys.length === 1 && keys[0] === "customer_name";
      },
    );
    expect(
      (replacementConfigured.columns ?? [])
        .filter((column) => column.primary_key)
        .map((column) => column.name),
    ).toEqual(["customer_name"]);
  });

  test("time-interval metadata defaults granularity from the selected key", async ({
    liveApp,
    page,
  }) => {
    const declareColumns = await page.request.put(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/columns`,
      {
        data: {
          columns: [
            { name: "customer_id", type: "integer" },
            { name: "event_date", type: "date" },
          ],
        },
      },
    );
    expect(declareColumns.ok()).toBe(true);

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    const properties = await openAssetProperties(page);
    const materialization = properties
      .getByRole("heading", { name: "Materialization" })
      .locator("../..");

    const strategyResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${customersAssetId}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await materialization.getByRole("combobox").first().click();
    await page.getByRole("option", { name: "Incremental (time interval)" }).click();
    await strategyResponse;

    const keyResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${customersAssetId}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    const incrementalKey = materialization.getByRole("combobox").nth(1);
    await incrementalKey.click();
    await page.getByPlaceholder("Search columns…").fill("event");
    await page.getByRole("option", { name: "event_date" }).click();
    await keyResponse;

    const configured = await pollAsset(
      liveApp,
      page.request,
      "analytics.customers",
      (asset) =>
        asset.materialization_strategy === "time_interval" &&
        asset.incremental_key === "event_date" &&
        asset.time_granularity === "date",
    );
    expect(configured.time_granularity).toBe("date");
    await expect(materialization.getByRole("combobox").nth(2)).toContainText("Date");

    await expect(properties.getByRole("button", { name: "YAML", exact: true })).toHaveCount(0);
  });

  test("load asset editors only offer Sling-compatible materializations", async ({
    liveApp,
    page,
  }) => {
    await writeFile(
      join(liveApp.workspaceDir, loadAssetPath),
      `name: analytics.orders_load
type: load
connection: duckdb-default
parameters:
  source_connection: duckdb-default
  source_table: analytics.orders
materialization:
  type: table
  strategy: create+replace
`,
      "utf8",
    );
    await pollAsset(liveApp, page.request, "analytics.orders_load", () => true);

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${loadAssetId}/code`);
    const properties = await openAssetProperties(page);
    const materialization = properties
      .getByRole("heading", { name: "Materialization" })
      .locator("../..");

    const expectSlingOptions = async () => {
      await expect(page.getByRole("option", { name: "Table (replace)" })).toBeVisible();
      await expect(page.getByRole("option", { name: "Table (truncate)" })).toBeVisible();
      await expect(page.getByRole("option", { name: "Append rows" })).toBeVisible();
      await expect(page.getByRole("option", { name: "Merge by key" })).toBeVisible();
      await expect(page.getByRole("option", { name: "None (run only)" })).toHaveCount(0);
      await expect(page.getByRole("option", { name: "View" })).toHaveCount(0);
      await expect(page.getByRole("option", { name: "Incremental (time interval)" })).toHaveCount(
        0,
      );
    };

    await materialization.getByRole("combobox").click();
    await expectSlingOptions();
    const mergeResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${loadAssetId}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await page.getByRole("option", { name: "Merge by key" }).click();
    await mergeResponse;

    const emptyUpdateKey = materialization.getByRole("combobox").nth(1);
    await emptyUpdateKey.click();
    await expect(page.getByText("No declared columns. Add or infer columns first.")).toBeVisible();
    await page.keyboard.press("Escape");

    const declareColumns = await page.request.put(
      `${liveApp.baseURL}/api/assets/${loadAssetId}/columns`,
      {
        data: {
          columns: [
            { name: "id", type: "integer" },
            { name: "updated_at", type: "timestamp" },
          ],
        },
      },
    );
    expect(declareColumns.ok()).toBe(true);
    await pollAsset(
      liveApp,
      page.request,
      "analytics.orders_load",
      (asset) => (asset.columns ?? []).length === 2,
    );
    await expect(properties.getByRole("button", { name: "Set id as primary key" })).toBeVisible({
      timeout: 15000,
    });

    const updateKeyResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${loadAssetId}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    const updateKey = materialization.getByRole("combobox").nth(1);
    await updateKey.click();
    await page.getByPlaceholder("Search columns…").fill("updated");
    await page.getByRole("option", { name: "updated_at" }).click();
    await updateKeyResponse;
    const configured = await pollAsset(
      liveApp,
      page.request,
      "analytics.orders_load",
      (asset) =>
        asset.materialization_strategy === "merge" && asset.incremental_key === "updated_at",
    );
    expect(configured.incremental_key).toBe("updated_at");

    await expect(updateKey).toContainText("updated_at");
    await expect(properties.getByRole("button", { name: "YAML", exact: true })).toHaveCount(0);
  });

  test("creates a canonical Load asset, navigates to its source, and edits target connections", async ({
    liveApp,
    page,
  }) => {
    test.skip(
      test.info().project.name.includes("mobile"),
      "The canvas creation flow is desktop-only.",
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${ordersAssetId}/canvas`);
    await page.getByRole("button", { name: "New asset" }).first().click();
    const dialog = page.getByRole("dialog", { name: "New asset" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("radio", { name: /^Load/ }).click();
    await dialog.getByLabel("Asset name").fill("analytics.orders_copy");

    await dialog.getByLabel("Source connection").click();
    await page.getByRole("option", { name: "local", exact: true }).click();
    const sourceFilePicker = dialog.getByRole("button", { name: "Choose source file" });
    await expect(sourceFilePicker).toBeVisible();
    await sourceFilePicker.click();
    const sourcePathInput = page.getByPlaceholder("Type a path…");
    await expect(sourcePathInput).toBeVisible();
    await sourcePathInput.fill("data/orders.csv");
    await sourcePathInput.press("Enter");
    await expect(sourceFilePicker).toContainText("data/orders.csv");

    await dialog.getByLabel("Source connection").click();
    await page.getByRole("option", { name: "duckdb-default", exact: true }).click();
    await dialog.getByLabel("Source table or object").fill("analytics.orders");

    const createdResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets`) &&
        response.request().method() === "POST",
      { timeout: 15000 },
    );
    await dialog.getByRole("button", { name: "Create" }).click();
    expect((await createdResponse).ok()).toBe(true);

    const created = await pollAsset(
      liveApp,
      page.request,
      "analytics.orders_copy",
      (asset) =>
        asset.parameters?.source_connection === "duckdb-default" &&
        asset.parameters?.source_table === "analytics.orders",
    );
    expect(created.materialization_strategy).toBe("create+replace");
    expect(created.upstreams).toContain("analytics.orders");
    expect(created.parameters).not.toHaveProperty("destination_connection");
    expect(created.parameters).not.toHaveProperty("destination_table");
    expect(created.parameters).not.toHaveProperty("mode");
    expect(created.content).toContain("strategy: create+replace");
    expect(created.content).not.toContain("destination_table:");

    await page.getByRole("link", { name: "Code view" }).click();
    await expect(page.getByRole("button", { name: "Go to analytics.orders" })).toBeVisible({
      timeout: 15000,
    });
    await page.getByRole("button", { name: "Go to analytics.orders" }).click();
    await expect(page).toHaveURL(new RegExp(`/assets/${ordersAssetId}/`));

    const createdLoadId = Buffer.from("analytics/assets/analytics/orders_copy.asset.yml").toString(
      "base64url",
    );
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${createdLoadId}/code`);
    const loadProperties = await openAssetProperties(page);
    const loadIdentity = loadProperties.getByRole("heading", { name: "Identity" }).locator("../..");
    await loadIdentity
      .getByText("Connection", { exact: true })
      .locator("..")
      .getByRole("combobox")
      .click();
    await page.getByRole("option", { name: "duckdb-default", exact: true }).click();
    const loadWithExplicitTarget = await pollAsset(
      liveApp,
      page.request,
      "analytics.orders_copy",
      (asset) => asset.explicit_connection === "duckdb-default",
    );
    expect(loadWithExplicitTarget.connection).toBe("duckdb-default");

    const pythonCreate = await page.request.post(
      `${liveApp.baseURL}/api/pipelines/${pipelineId}/assets`,
      { data: { name: "analytics.python_target", type: "python" } },
    );
    expect(pythonCreate.ok()).toBe(true);
    const pythonAssetId = Buffer.from("analytics/assets/analytics/python_target.py").toString(
      "base64url",
    );
    await pollAsset(liveApp, page.request, "analytics.python_target", () => true);
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${pythonAssetId}/code`);
    const pythonProperties = await openAssetProperties(page);
    const pythonIdentity = pythonProperties
      .getByRole("heading", { name: "Identity" })
      .locator("../..");
    await pythonIdentity
      .getByText("Connection", { exact: true })
      .locator("..")
      .getByRole("combobox")
      .click();
    await page.getByRole("option", { name: "duckdb-default", exact: true }).click();
    const pythonWithExplicitTarget = await pollAsset(
      liveApp,
      page.request,
      "analytics.python_target",
      (asset) => asset.explicit_connection === "duckdb-default",
    );
    expect(pythonWithExplicitTarget.connection).toBe("duckdb-default");
  });

  test("materializing an asset on a stale upstream warns before building", async ({
    liveApp,
    page,
  }) => {
    const { request } = page;
    // An upstream that is never built (so it reads as stale) and a downstream
    // that selects from it.
    const upstream = await request.post(`${liveApp.baseURL}/api/pipelines/${pipelineId}/assets`, {
      data: {
        name: "analytics.warn_up",
        type: "duckdb.sql",
        content: `/* @bruin\ntype: duckdb.sql\nmaterialization:\n  type: table\n@bruin */\n\nselect 1 as x\n`,
      },
    });
    expect(upstream.ok()).toBe(true);
    const downstream = await request.post(`${liveApp.baseURL}/api/pipelines/${pipelineId}/assets`, {
      data: {
        name: "analytics.warn_down",
        type: "duckdb.sql",
        content: `/* @bruin\ntype: duckdb.sql\nmaterialization:\n  type: table\n@bruin */\n\nselect x from analytics.warn_up\n`,
      },
    });
    expect(downstream.ok()).toBe(true);
    await pollAsset(liveApp, request, "analytics.warn_down", (a) =>
      a.upstreams.includes("analytics.warn_up"),
    );

    const warnDownId = Buffer.from("analytics/assets/analytics/warn_down.sql").toString(
      "base64url",
    );
    const staleness = page.waitForResponse(
      (r) => r.url().includes(`/api/pipelines/${pipelineId}/staleness`) && r.ok(),
      { timeout: 15000 },
    );
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${warnDownId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    await staleness;

    // Materializing warns: the build would read the un-built upstream's table, so
    // the asset would stay stale (the §9 achieved-vs-target rule).
    await page.getByRole("button", { name: "Materialize" }).click();
    await expect(page.getByText("Upstream is out of date")).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("analytics.warn_up").first()).toBeVisible();

    // The user can back out without building.
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByText("Upstream is out of date")).toBeHidden();
  });

  test("ignoring an inferred dependency persists as a drop and survives reconcile", async ({
    liveApp,
    request,
  }) => {
    // Create a downstream asset via the API so the SQL reconcile runs and infers
    // analytics.customers as an inferred (not manual) dependency.
    const create = await request.post(`${liveApp.baseURL}/api/pipelines/${pipelineId}/assets`, {
      data: {
        name: "analytics.downstream",
        type: "duckdb.sql",
        content: `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select customer_id from analytics.customers
`,
      },
    });
    expect(create.ok()).toBe(true);

    // Wait until the SQL reconcile has inferred the upstream.
    await pollAsset(liveApp, request, "analytics.downstream", (a) =>
      a.upstreams.includes("analytics.customers"),
    );

    // Ignore the inferred dependency.
    const ignore = await request.post(
      `${liveApp.baseURL}/api/assets/${downstreamAssetId}/transactions`,
      {
        data: { type: "dependency.inferred.ignore", dependency_key: "a:analytics.customers#full" },
      },
    );
    expect(ignore.ok()).toBe(true);

    const ignored = await pollAsset(
      liveApp,
      request,
      "analytics.downstream",
      (a) => !a.upstreams.includes("analytics.customers"),
    );
    expect(ignored.meta?.renart_dep_drop).toContain("a:analytics.customers#full");

    // Re-saving the SQL triggers a reconcile; the dropped dependency must not
    // reappear even though the query still references analytics.customers.
    const save = await request.put(
      `${liveApp.baseURL}/api/pipelines/${pipelineId}/assets/${downstreamAssetId}`,
      {
        data: { content: "select customer_id from analytics.customers -- edited\n" },
      },
    );
    expect(save.ok()).toBe(true);

    // Give the reconcile a beat, then assert it stayed dropped.
    const afterReconcile = await pollAsset(
      liveApp,
      request,
      "analytics.downstream",
      (a) => a.meta?.renart_dep_drop?.includes("a:analytics.customers#full") ?? false,
    );
    expect(afterReconcile.upstreams).not.toContain("analytics.customers");
  });

  test("columns are derived from the asset definition, not the warehouse", async ({
    liveApp,
    request,
  }) => {
    // Declare the upstream's columns (the source of truth for downstream types).
    const declare = await request.post(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`,
      {
        data: {
          columns: [
            { name: "customer_id", type: "INTEGER" },
            { name: "customer_name", type: "VARCHAR" },
          ],
        },
      },
    );
    expect(declare.ok()).toBe(true);

    // A downstream asset selecting from the upstream plus a computed column.
    const create = await request.post(`${liveApp.baseURL}/api/pipelines/${pipelineId}/assets`, {
      data: {
        name: "analytics.report",
        type: "duckdb.sql",
        content: `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select customer_id, upper(customer_name) as shout from analytics.customers
`,
      },
    });
    expect(create.ok()).toBe(true);
    const reportAssetId = Buffer.from("analytics/assets/analytics/report.sql").toString(
      "base64url",
    );

    // Deriving the downstream's columns from its definition resolves the bare
    // column type from the upstream asset and types the computed column — all
    // without touching the database.
    const refresh = await request.post(
      `${liveApp.baseURL}/api/assets/${reportAssetId}/columns/refresh-from-definition`,
    );
    expect(refresh.ok()).toBe(true);
    const body = (await refresh.json()) as { columns: Array<{ name: string; type?: string }> };
    const byName = new Map(body.columns.map((c) => [c.name, (c.type ?? "").toUpperCase()]));
    expect(byName.get("customer_id")).toBe("INTEGER");
    expect(byName.get("shout")).toBe("VARCHAR");
  });

  test("column type ownership is preserved across reconciliation", async ({ liveApp, request }) => {
    // Reconcile customers' columns from an inferred set (no declared columns yet).
    const first = await request.post(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`,
      {
        data: { columns: [{ name: "customer_id", type: "integer" }] },
      },
    );
    expect(first.ok()).toBe(true);

    // Take ownership of the column's type.
    const own = await request.post(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/transactions`,
      {
        data: { type: "column.field.own", column: "customer_id", field: "type" },
      },
    );
    expect(own.ok()).toBe(true);

    // A later inference saying bigint must not override the owned integer type.
    const second = await request.post(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`,
      {
        data: { columns: [{ name: "customer_id", type: "bigint" }] },
      },
    );
    expect(second.ok()).toBe(true);
    const body = (await second.json()) as { columns: Array<{ name: string; type?: string }> };
    const customerId = body.columns.find((c) => c.name === "customer_id");
    expect(customerId?.type).toBe("integer");
  });

  test("guided metadata form is the only inspector mode and edits a tag", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    const properties = await openAssetProperties(page);

    // The guided form is the only exposed metadata surface for now.
    await expect(properties.getByRole("heading", { name: "Identity" })).toBeVisible({
      timeout: 15000,
    });
    await expect(properties.getByRole("button", { name: "YAML", exact: true })).toHaveCount(0);
    await expect(properties.getByRole("button", { name: "Form", exact: true })).toHaveCount(0);

    // Adding a tag through the guided field persists it.
    const input = properties.getByRole("textbox", { name: "Tags" });
    await input.fill("daily");
    await input.press("Enter");

    const customers = await pollAsset(liveApp, page.request, "analytics.customers", (a) =>
      (a.tags ?? []).includes("daily"),
    );
    expect(customers.tags).toContain("daily");

    // Removing the last tag must clear it from the live view, not only after a
    // refresh (the workspace SSE merge omits empty fields).
    await properties.getByRole("button", { name: "Remove daily" }).click();
    await expect(properties.getByRole("button", { name: "Remove daily" })).toBeHidden({
      timeout: 15000,
    });
    const cleared = await pollAsset(
      liveApp,
      page.request,
      "analytics.customers",
      (a) => (a.tags ?? []).length === 0,
    );
    expect(cleared.tags ?? []).not.toContain("daily");
  });

  test("quality checks card adds and removes a column check", async ({ liveApp, page }) => {
    const { request } = page;
    // Declare a column so the checks card has a target.
    const declare = await request.post(
      `${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`,
      {
        data: { columns: [{ name: "customer_id", type: "INTEGER" }] },
      },
    );
    expect(declare.ok()).toBe(true);

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    const properties = await openAssetProperties(page);

    const card = properties.locator("section").filter({ hasText: "Quality checks" });
    await expect(card.getByRole("heading", { name: "Quality checks" })).toBeVisible({
      timeout: 15000,
    });

    // Pick the column (the check name defaults to not_null) and add the check.
    await card.getByRole("combobox").first().click();
    await page.getByRole("option", { name: "customer_id" }).click();
    const added = page.waitForResponse(
      (r) => r.url().includes(`/api/assets/${customersAssetId}/transactions`) && r.ok(),
      { timeout: 15000 },
    );
    await card.getByRole("button", { name: "Add check" }).click();
    await added;

    const removeButton = card.getByRole("button", { name: "Remove not_null from customer_id" });
    await expect(removeButton).toBeVisible({ timeout: 15000 });
    const removed = page.waitForResponse(
      (r) => r.url().includes(`/api/assets/${customersAssetId}/transactions`) && r.ok(),
      { timeout: 15000 },
    );
    await removeButton.click();
    await removed;
    await expect(card.getByRole("button", { name: "Remove not_null from customer_id" })).toBeHidden(
      { timeout: 15000 },
    );
  });
});
