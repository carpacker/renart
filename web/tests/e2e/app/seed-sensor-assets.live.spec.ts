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
  columns?: Array<{ name: string; type?: string }>;
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
    const seedEditor = page.getByTestId("semantic-parameters-editor");
    await expect(seedEditor).toHaveAttribute("data-asset-kind", "seed");
    await expect(seedEditor.locator(".monaco-editor")).toHaveCount(0);
    await expect(seedEditor.getByLabel("Seed path")).toHaveValue("./regional_customers.csv");
    await expect(seedEditor.getByLabel("Seed file format")).toContainText("csv");
    await expect(seedEditor.getByTestId("seed-file-dropzone")).toBeVisible();
    await expect(
      seedEditor.getByText("Columns and checks are configured in Properties."),
    ).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "Seed file" })).toHaveCount(0);

    const seedProperties = await openAssetProperties(page);
    const seedColumns = seedProperties.locator("section").filter({ hasText: "Columns" }).first();
    await expect(seedColumns.getByRole("heading", { name: "Columns" })).toBeVisible({
      timeout: 15000,
    });
    const initialRefreshResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${seedAssetId}/columns/refresh-from-definition`) &&
        response.request().method() === "POST",
      { timeout: 30000 },
    );
    await seedColumns.getByRole("button", { name: "Refresh", exact: true }).click();
    const initialRefresh = await initialRefreshResponse;
    expect(initialRefresh.ok(), await initialRefresh.text()).toBe(true);
    await pollAsset(
      liveApp,
      page,
      "analytics.regional_customers",
      (asset) =>
        asset.columns?.some((column) => column.name === "customer_id") === true &&
        asset.columns?.some((column) => column.name === "customer_name") === true,
    );

    const replacement = Buffer.from(
      "customer_id,customer_name,segment\n20,Replacement Grace,enterprise\n",
      "utf8",
    );
    const seedUploadResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${seedAssetId}/seed-file`) &&
        response.request().method() === "POST",
      { timeout: 30000 },
    );
    const replacementRefreshResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/assets/${seedAssetId}/columns/refresh-from-definition`) &&
        response.request().method() === "POST",
      { timeout: 30000 },
    );
    await seedEditor.getByLabel("Upload seed file").setInputFiles({
      name: "regional_customers_v2.csv",
      mimeType: "text/csv",
      buffer: replacement,
    });
    const [seedUpload, replacementRefresh] = await Promise.all([
      seedUploadResponse,
      replacementRefreshResponse,
    ]);
    expect(seedUpload.ok(), await seedUpload.text()).toBe(true);
    expect(replacementRefresh.ok(), await replacementRefresh.text()).toBe(true);
    await expect(
      seedEditor.getByText("regional_customers_v2.csv uploaded and columns refreshed."),
    ).toBeVisible({ timeout: 15000 });

    const replacedSeed = await pollAsset(
      liveApp,
      page,
      "analytics.regional_customers",
      (asset) =>
        asset.parameters?.path === "./regional_customers_v2.csv" &&
        asset.meta?.renart_seed_file === "regional_customers_v2.csv" &&
        asset.columns?.some((column) => column.name === "segment") === true,
    );
    expect(replacedSeed.parameters?.file_type).toBe("csv");
    expect(
      await readFile(
        join(liveApp.workspaceDir, "analytics/assets/analytics/regional_customers_v2.csv"),
      ),
    ).toEqual(replacement);
    let oldSeedError: NodeJS.ErrnoException | undefined;
    try {
      await readFile(
        join(liveApp.workspaceDir, "analytics/assets/analytics/regional_customers.csv"),
      );
    } catch (error) {
      oldSeedError = error as NodeJS.ErrnoException;
    }
    expect(oldSeedError?.code).toBe("ENOENT");
    const replacedDefinition = await readFile(join(liveApp.workspaceDir, seedPath), "utf8");
    expect(replacedDefinition).toContain("path: ./regional_customers_v2.csv");
    expect(replacedDefinition).toContain("renart_seed_file: regional_customers_v2.csv");
    expect(replacedDefinition).toContain("name: segment");

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
    const ordersMaterialize = await materializeAsset(page, liveApp.baseURL, ordersAssetId);
    expect(ordersMaterialize.status).toBe("ok");

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
    const sensorEditor = page.getByTestId("semantic-parameters-editor");
    await expect(sensorEditor).toHaveAttribute("data-asset-kind", "sensor-query");
    await expect(sensorEditor.locator(".monaco-editor")).toBeVisible({ timeout: 15000 });
    await expect.poll(() => monacoEditorValue(page)).toBe("select true");
    await expect(
      sensorEditor.getByText("Columns and checks are configured in Properties."),
    ).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "Sensor condition" })).toHaveCount(0);

    const completionQuery = "select count(*) > 0\nfrom analytics.orders o\nwhere o. > 0";
    const completionResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/sql/lsp/completions") &&
        response.request().method() === "POST" &&
        (response.request().postData() ?? "").includes(sensorAssetId),
      { timeout: 15000 },
    );
    await setMonacoContentAndCursor(page, completionQuery, "where o.");
    await page.keyboard.press("ControlOrMeta+Space");
    await completionResponse;
    const suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible({ timeout: 15000 });
    const orderIDCompletion = suggestWidget
      .locator(".monaco-list-row")
      .filter({ hasText: "order_id" })
      .first();
    await expect(orderIDCompletion).toBeVisible();
    const querySaveResponse = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/pipelines/${pipelineId}/assets/${sensorAssetId}`) &&
        response.request().method() === "PUT" &&
        (response.request().postData() ?? "").includes("order_id") &&
        response.ok(),
      { timeout: 15000 },
    );
    await orderIDCompletion.click();
    await querySaveResponse;
    await expect.poll(() => monacoEditorValue(page)).toContain("where o.order_id > 0");
    await pollAsset(
      liveApp,
      page,
      "analytics.orders_ready",
      (asset) => asset.parameters?.query?.includes("where o.order_id > 0") === true,
    );

    const timeoutInput = sensorEditor.getByLabel("Sensor timeout");
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

    const sensorProperties = await openAssetProperties(page);
    await expect(sensorProperties.getByRole("heading", { name: "Identity" })).toBeVisible({
      timeout: 15000,
    });
    await expect(sensorProperties.getByRole("heading", { name: "Columns" })).toHaveCount(0);
    await expect(sensorProperties.getByRole("heading", { name: "Quality checks" })).toHaveCount(0);

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

async function materializeAsset(page: Page, baseURL: string, assetId: string) {
  const response = await page.request.post(
    `${baseURL}/api/assets/${assetId}/materialize/stream?environment=default`,
  );
  expect(response.ok()).toBe(true);
  const stream = await response.text();
  const doneLine = stream
    .split(/\r?\n/)
    .reverse()
    .find((line) => line.startsWith("data: ") && line.includes('"status"'));
  if (!doneLine) throw new Error(`materialize stream did not contain a done event:\n${stream}`);
  return JSON.parse(doneLine.slice("data: ".length)) as { status: string; error?: string };
}

async function setMonacoContentAndCursor(page: Page, content: string, cursorAfter: string) {
  await page.waitForFunction(
    () => {
      const monaco = (window as typeof window & { monaco?: any }).monaco;
      return Boolean(monaco?.editor.getEditors?.()[0]?.getModel?.());
    },
    undefined,
    { timeout: 15000 },
  );
  await page.evaluate(
    ({ content, cursorAfter }) => {
      const monaco = (window as typeof window & { monaco?: any }).monaco;
      const editor = monaco?.editor.getEditors?.()[0];
      const model = editor?.getModel?.();
      if (!editor || !model) throw new Error("Monaco editor is not ready");
      const cursorOffset = content.indexOf(cursorAfter);
      if (cursorOffset < 0) throw new Error(`cursor text ${cursorAfter} was not found`);
      model.setValue(content);
      editor.focus();
      editor.setPosition(model.getPositionAt(cursorOffset + cursorAfter.length));
    },
    { content, cursorAfter },
  );
}

async function monacoEditorValue(page: Page) {
  return page.evaluate(() => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    return monaco?.editor.getEditors?.()[0]?.getModel?.()?.getValue?.() ?? "";
  });
}
