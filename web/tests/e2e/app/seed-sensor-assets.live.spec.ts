import { expect, type Locator, type Page } from "@playwright/test";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test, type LiveApp } from "../live-app-fixture";

const pipelineId = Buffer.from("analytics").toString("base64url");
const ordersAssetId = Buffer.from("analytics/assets/analytics/orders.sql").toString("base64url");
const seedPath = "analytics/assets/analytics/regional_customers.asset.yml";
const seedAssetId = Buffer.from(seedPath).toString("base64url");
const sensorPath = "analytics/assets/analytics/orders_ready.asset.yml";
const sensorAssetId = Buffer.from(sensorPath).toString("base64url");

type WorkspaceAsset = {
  id: string;
  name: string;
  type: string;
  parameters?: Record<string, string>;
  meta?: Record<string, string>;
};

type WorkspaceResponse = {
  pipelines: Array<{ id: string; assets: WorkspaceAsset[] }>;
};

test.describe("seed and sensor assets live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("creates, edits, and runs a seed and a sensor from the workbench", async ({
    liveApp,
    page,
  }) => {
    test.setTimeout(120000);
    test.skip(
      test.info().project.name.includes("mobile"),
      "The canvas creation flow is desktop-only.",
    );

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${ordersAssetId}/canvas`);
    let dialog = await openNewAssetDialog(page);
    await dialog.getByRole("radio", { name: "Seed", exact: true }).click();
    await dialog.getByLabel("Asset name").fill("analytics.regional_customers");
    await expect(dialog.getByLabel("Seed type")).toContainText("duckdb.seed");
    await dialog.locator('input[type="file"]').setInputFiles({
      name: "regional_customers.csv",
      mimeType: "text/csv",
      buffer: Buffer.from("customer_id,customer_name\n10,Seed Ada\n", "utf8"),
    });
    await dialog.getByRole("switch", { name: "Enforce schema" }).click();

    const seedCreatedResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets`) &&
        response.request().method() === "POST",
      { timeout: 30000 },
    );
    await dialog.getByRole("button", { name: "Create", exact: true }).click();
    const seedCreate = await seedCreatedResponse;
    expect(seedCreate.ok(), await seedCreate.text()).toBe(true);

    const seed = await pollAsset(liveApp, page, "analytics.regional_customers", (asset) =>
      Boolean(asset.meta?.renart_seed_file),
    );
    expect(seed.type).toBe("duckdb.seed");
    expect(seed.parameters).toMatchObject({
      path: "./regional_customers.csv",
      file_type: "csv",
      enforce_schema: "false",
    });
    expect(seed.meta?.renart_seed_file).toBe("regional_customers.csv");
    expect(
      await readFile(
        join(liveApp.workspaceDir, "analytics/assets/analytics/regional_customers.csv"),
      ),
    ).toEqual(Buffer.from("customer_id,customer_name\n10,Seed Ada\n", "utf8"));
    const seedDefinition = await readFile(join(liveApp.workspaceDir, seedPath), "utf8");
    expect(seedDefinition).toContain("renart_seed_file: regional_customers.csv");
    expect(seedDefinition).toContain("path: ./regional_customers.csv");

    await page.getByRole("link", { name: "Code view" }).click();
    await expect(page.getByRole("button", { name: "Materialize", exact: true })).toBeVisible({
      timeout: 15000,
    });
    const seedMaterializeResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${seedAssetId}/materialize/stream`) && response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await seedMaterializeResponse;
    await expect(page.locator("pre.font-console").first()).toContainText(/regional_customers/i, {
      timeout: 30000,
    });

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${ordersAssetId}/canvas`);
    dialog = await openNewAssetDialog(page);
    await dialog.getByRole("radio", { name: "Sensor", exact: true }).click();
    await dialog.getByLabel("Asset name").fill("analytics.orders_ready");
    await expect(dialog.getByLabel("Sensor type")).toContainText("duckdb.sensor.query");
    await dialog.getByLabel("Ready condition query").fill("select true");

    const sensorCreatedResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets`) &&
        response.request().method() === "POST",
      { timeout: 30000 },
    );
    await dialog.getByRole("button", { name: "Create", exact: true }).click();
    const sensorCreate = await sensorCreatedResponse;
    expect(sensorCreate.ok(), await sensorCreate.text()).toBe(true);

    const sensor = await pollAsset(liveApp, page, "analytics.orders_ready", (asset) =>
      Boolean(asset.parameters?.query),
    );
    expect(sensor.type).toBe("duckdb.sensor.query");
    expect(sensor.parameters).toMatchObject({
      query: "select true",
      poke_interval: "30",
      timeout: "24h",
    });
    const sensorDefinition = await readFile(join(liveApp.workspaceDir, sensorPath), "utf8");
    expect(sensorDefinition).toContain("poke_interval: 30");
    expect(sensorDefinition).toContain("timeout: 24h");

    await page.getByRole("link", { name: "Code view" }).click();
    await expect(page.getByRole("button", { name: "Check now", exact: true })).toBeVisible({
      timeout: 15000,
    });
    const properties = await openAssetProperties(page);
    await expect(properties.getByRole("heading", { name: "Sensor condition" })).toBeVisible();
    const timeoutInput = properties.getByPlaceholder("24h");
    await timeoutInput.fill("2h");
    const timeoutResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${sensorAssetId}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await timeoutInput.press("Enter");
    await timeoutResponse;
    await pollAsset(
      liveApp,
      page,
      "analytics.orders_ready",
      (asset) => asset.parameters?.timeout === "2h",
    );

    const sensorRunResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${sensorAssetId}/materialize/stream`) && response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Check now", exact: true }).click();
    await sensorRunResponse;
    await expect
      .poll(
        async () => {
          const response = await page.request.get(
            `${liveApp.baseURL}/api/pipelines/${pipelineId}/staleness?environment=default`,
          );
          const body = (await response.json()) as {
            assets: Array<{
              asset_name: string;
              status: string;
              volatile?: boolean;
              last_run_status?: string;
            }>;
          };
          return body.assets.find((asset) => asset.asset_name === "analytics.orders_ready");
        },
        { timeout: 30000 },
      )
      .toMatchObject({ status: "volatile", volatile: true, last_run_status: "succeeded" });
  });

  test("creates a seed from the workspace picker and keeps asset choices aligned", async ({
    liveApp,
    page,
  }) => {
    test.setTimeout(120000);
    test.skip(
      test.info().project.name.includes("mobile"),
      "The canvas creation flow is desktop-only.",
    );

    const workspaceSeedPath = join(liveApp.workspaceDir, "data", "workspace_customers.csv");
    await mkdir(join(liveApp.workspaceDir, "data"), { recursive: true });
    await writeFile(workspaceSeedPath, "customer_id,customer_name\n20,Workspace Grace\n", "utf8");

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${ordersAssetId}/canvas`);
    const dialog = await openNewAssetDialog(page);

    const assetKinds = ["SQL", "Python", "HTTP API", "Seed", "Sensor", "Load"];
    const selectorBoxes = await Promise.all(
      assetKinds.map(async (name) => {
        const selector = dialog.getByRole("radio", { name, exact: true });
        await expect(selector).toBeVisible();
        const box = await selector.boundingBox();
        expect(box).not.toBeNull();
        return { width: Math.round(box!.width), height: Math.round(box!.height) };
      }),
    );
    expect(new Set(selectorBoxes.map(({ width }) => width)).size).toBe(1);
    expect(new Set(selectorBoxes.map(({ height }) => height)).size).toBe(1);

    await dialog.getByRole("radio", { name: "Seed", exact: true }).click();
    await dialog.getByLabel("Asset name").fill("analytics.workspace_customers");
    await dialog.getByRole("radio", { name: "Workspace path", exact: true }).click();
    await dialog.getByRole("button", { name: "Choose workspace seed file" }).click();

    const pathInput = page.getByPlaceholder("Type a path…");
    await expect(pathInput).toBeVisible();
    await pathInput.fill("./data/");
    await page.getByRole("option", { name: "./data/workspace_customers.csv", exact: true }).click();
    await expect(dialog.getByRole("button", { name: "Choose workspace seed file" })).toContainText(
      "./data/workspace_customers.csv",
    );

    const seedCreatedResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets`) &&
        response.request().method() === "POST",
      { timeout: 30000 },
    );
    await dialog.getByRole("button", { name: "Create", exact: true }).click();
    const seedCreate = await seedCreatedResponse;
    expect(seedCreate.ok(), await seedCreate.text()).toBe(true);

    const seed = await pollAsset(liveApp, page, "analytics.workspace_customers", (asset) =>
      Boolean(asset.parameters?.path),
    );
    expect(seed.type).toBe("duckdb.seed");
    expect(seed.parameters).toMatchObject({
      path: "../../../data/workspace_customers.csv",
      file_type: "csv",
      enforce_schema: "true",
    });

    const definition = await readFile(
      join(liveApp.workspaceDir, "analytics/assets/analytics/workspace_customers.asset.yml"),
      "utf8",
    );
    expect(definition).toContain("path: ../../../data/workspace_customers.csv");
    expect(definition).not.toContain("workspace_path");
  });
});

async function openNewAssetDialog(page: Page) {
  await page.getByRole("button", { name: "New asset" }).first().click();
  const dialog = page.getByRole("dialog", { name: "New asset" });
  await expect(dialog).toBeVisible({ timeout: 15000 });
  return dialog;
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

async function pollAsset(
  liveApp: LiveApp,
  page: Page,
  assetName: string,
  predicate: (asset: WorkspaceAsset) => boolean,
) {
  let found: WorkspaceAsset | undefined;
  await expect
    .poll(
      async () => {
        const response = await page.request.get(`${liveApp.baseURL}/api/workspace`);
        const workspace = (await response.json()) as WorkspaceResponse;
        found = workspace.pipelines
          .flatMap((pipeline) => pipeline.assets)
          .find((asset) => asset.name === assetName);
        return found ? predicate(found) : false;
      },
      { timeout: 30000 },
    )
    .toBe(true);
  if (!found) throw new Error(`asset ${assetName} was not found`);
  return found;
}
