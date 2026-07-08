import { expect, type Locator, type Page } from "@playwright/test";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

const pipelineId = Buffer.from("analytics").toString("base64url");
const customersAssetId = Buffer.from("analytics/assets/analytics/customers.sql").toString("base64url");
const downstreamAssetId = Buffer.from("analytics/assets/analytics/downstream.sql").toString("base64url");

type WorkspaceAsset = {
  id: string;
  name: string;
  upstreams: string[];
  meta?: Record<string, string>;
  tags?: string[];
  columns?: Array<{ name: string; type?: string }>;
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

test.describe("app asset editing workbench live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("guided cards render and adding a manual dependency persists provenance", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });

    const properties = await openAssetProperties(page);

    // The guided metadata panel renders its focused cards.
    await expect(properties.getByRole("heading", { name: "Identity" })).toBeVisible({ timeout: 15000 });
    await expect(properties.getByRole("heading", { name: "Materialization" })).toBeVisible();
    await expect(properties.getByRole("heading", { name: "Dependencies" })).toBeVisible();
    await expect(properties.getByRole("heading", { name: "Columns" })).toBeVisible();

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

  test("materializing an asset on a stale upstream warns before building", async ({ liveApp, page }) => {
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
    await pollAsset(liveApp, request, "analytics.warn_down", (a) => a.upstreams.includes("analytics.warn_up"));

    const warnDownId = Buffer.from("analytics/assets/analytics/warn_down.sql").toString("base64url");
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

  test("ignoring an inferred dependency persists as a drop and survives reconcile", async ({ liveApp, request }) => {
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
    const ignore = await request.post(`${liveApp.baseURL}/api/assets/${downstreamAssetId}/transactions`, {
      data: { type: "dependency.inferred.ignore", dependency_key: "a:analytics.customers#full" },
    });
    expect(ignore.ok()).toBe(true);

    const ignored = await pollAsset(liveApp, request, "analytics.downstream", (a) =>
      !a.upstreams.includes("analytics.customers"),
    );
    expect(ignored.meta?.renart_dep_drop).toContain("a:analytics.customers#full");

    // Re-saving the SQL triggers a reconcile; the dropped dependency must not
    // reappear even though the query still references analytics.customers.
    const save = await request.put(`${liveApp.baseURL}/api/pipelines/${pipelineId}/assets/${downstreamAssetId}`, {
      data: { content: "select customer_id from analytics.customers -- edited\n" },
    });
    expect(save.ok()).toBe(true);

    // Give the reconcile a beat, then assert it stayed dropped.
    const afterReconcile = await pollAsset(liveApp, request, "analytics.downstream", (a) =>
      a.meta?.renart_dep_drop?.includes("a:analytics.customers#full") ?? false,
    );
    expect(afterReconcile.upstreams).not.toContain("analytics.customers");
  });

  test("columns are derived from the asset definition, not the warehouse", async ({ liveApp, request }) => {
    // Declare the upstream's columns (the source of truth for downstream types).
    const declare = await request.post(`${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`, {
      data: {
        columns: [
          { name: "customer_id", type: "INTEGER" },
          { name: "customer_name", type: "VARCHAR" },
        ],
      },
    });
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
    const reportAssetId = Buffer.from("analytics/assets/analytics/report.sql").toString("base64url");

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
    const first = await request.post(`${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`, {
      data: { columns: [{ name: "customer_id", type: "integer" }] },
    });
    expect(first.ok()).toBe(true);

    // Take ownership of the column's type.
    const own = await request.post(`${liveApp.baseURL}/api/assets/${customersAssetId}/transactions`, {
      data: { type: "column.field.own", column: "customer_id", field: "type" },
    });
    expect(own.ok()).toBe(true);

    // A later inference saying bigint must not override the owned integer type.
    const second = await request.post(`${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`, {
      data: { columns: [{ name: "customer_id", type: "bigint" }] },
    });
    expect(second.ok()).toBe(true);
    const body = (await second.json()) as { columns: Array<{ name: string; type?: string }> };
    const customerId = body.columns.find((c) => c.name === "customer_id");
    expect(customerId?.type).toBe("integer");
  });

  test("interactive YAML view renders the metadata and edits a tag", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    const properties = await openAssetProperties(page);

    // Wait for the properties surface to settle (cards render first), then switch views.
    await expect(properties.getByRole("heading", { name: "Identity" })).toBeVisible({ timeout: 15000 });
    const yamlToggle = properties.getByRole("button", { name: "YAML", exact: true });
    await yamlToggle.click();
    await expect(yamlToggle).toHaveAttribute("aria-pressed", "true");

    // It renders the metadata as YAML keys.
    await expect(properties.getByText("depends:", { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(properties.getByText("columns:", { exact: true })).toBeVisible();
    await expect(properties.getByText("tags:", { exact: true })).toBeVisible();

    // Adding a tag through the YAML list input persists it.
    const input = properties.getByPlaceholder("add tag");
    await input.fill("daily");
    await input.press("Enter");

    const customers = await pollAsset(liveApp, page.request, "analytics.customers", (a) =>
      (a.tags ?? []).includes("daily"),
    );
    expect(customers.tags).toContain("daily");

    // Removing the last tag must clear it from the live view, not only after a
    // refresh (the workspace SSE merge omits empty fields).
    await properties.getByRole("button", { name: "Remove tag daily" }).click();
    await expect(properties.getByRole("button", { name: "Remove tag daily" })).toBeHidden({ timeout: 15000 });
    const cleared = await pollAsset(liveApp, page.request, "analytics.customers", (a) => (a.tags ?? []).length === 0);
    expect(cleared.tags ?? []).not.toContain("daily");

    // A custom column can be added directly from the columns list.
    const addColumn = properties.getByPlaceholder("add column");
    await addColumn.fill("region");
    await addColumn.press("Enter");
    const withColumn = await pollAsset(liveApp, page.request, "analytics.customers", (a) =>
      (a.columns ?? []).some((c) => c.name === "region"),
    );
    expect((withColumn.columns ?? []).map((c) => c.name)).toContain("region");

    // The check dropdown stays collapsed behind an "add check…" affordance until
    // the user opts in (no bare dropdown with nothing selected).
    const addCheck = properties.getByRole("button", { name: "add check…" }).first();
    await expect(addCheck).toBeVisible({ timeout: 15000 });
    await addCheck.click();
    await expect(properties.getByRole("button", { name: /Confirm check on region/ })).toBeVisible();

    // Existing assets are offered as dependency proposals.
    await expect(properties.getByRole("button", { name: "pick from existing assets…" })).toBeVisible();

    // A column can be removed/ignored, after which it is no longer on the asset.
    await properties.getByRole("button", { name: "Remove column region" }).click();
    const withoutColumn = await pollAsset(liveApp, page.request, "analytics.customers", (a) =>
      !(a.columns ?? []).some((c) => c.name === "region"),
    );
    expect((withoutColumn.columns ?? []).map((c) => c.name)).not.toContain("region");
  });

  test("quality checks card adds and removes a column check", async ({ liveApp, page }) => {
    const { request } = page;
    // Declare a column so the checks card has a target.
    const declare = await request.post(`${liveApp.baseURL}/api/assets/${customersAssetId}/columns/reconcile`, {
      data: { columns: [{ name: "customer_id", type: "INTEGER" }] },
    });
    expect(declare.ok()).toBe(true);

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${customersAssetId}/code`);
    await expect(page.locator(".monaco-editor").first()).toBeVisible({ timeout: 15000 });
    const properties = await openAssetProperties(page);

    const card = properties.locator("section").filter({ hasText: "Quality checks" });
    await expect(card.getByRole("heading", { name: "Quality checks" })).toBeVisible({ timeout: 15000 });

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
    await expect(card.getByRole("button", { name: "Remove not_null from customer_id" })).toBeHidden({ timeout: 15000 });
  });
});
