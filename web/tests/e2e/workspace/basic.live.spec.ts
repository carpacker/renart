import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { expect } from "@playwright/test";

import { liveTest as test } from "../live-app-fixture";

test.describe("workspace live basic flows", () => {
  test.use({ fixtureName: "basic-workspace" });

  test("loads the fixture workspace and opens an asset in the editor", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    if (test.info().project.name.includes("mobile")) {
      await openCustomersEditor(page, liveApp.baseURL);
    } else {
      await openCustomersEditor(page, liveApp.baseURL);
    }

    await expect(page).toHaveTitle("analytics.customers · analytics · Renart");
    await expect(
      page.getByText("analytics.customers", { exact: true }).last()
    ).toBeVisible();
    await expect(
      page.getByTestId("editor-asset-path")
    ).toBeVisible();
  });

  test("switches assets from the sidebar against the real server", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);
    if (test.info().project.name.includes("mobile")) {
      await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3Mvb3JkZXJzLnNxbA`);
      await expect(page).toHaveTitle("analytics.orders · analytics · Renart");
      const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
      if (!(await editorDialog.isVisible().catch(() => false))) {
        await page.getByRole("button", { name: "Edit asset" }).click();
      }
      await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.orders");
    } else {
      await page.getByRole("link", { name: "orders", exact: true }).click();
    }

    await expect(page).toHaveTitle("analytics.orders · analytics · Renart");
    await expect(
      page.getByText("analytics.orders", { exact: true }).last()
    ).toBeVisible();
    await expect(
      page.getByTestId("editor-asset-path")
    ).toBeVisible();
  });

  test("does not refetch onboarding state when switching selected assets", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop sidebar navigation coverage.");

    let onboardingStateRequests = 0;
    page.on("request", (request) => {
      if (request.url().includes("/api/onboarding/state")) {
        onboardingStateRequests += 1;
      }
    });

    await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA`);
    await expect(page).toHaveTitle("analytics.customers · analytics · Renart");
    await page.getByRole("link", { name: "orders", exact: true }).click();
    await expect(page).toHaveTitle("analytics.orders · analytics · Renart");

    expect(onboardingStateRequests).toBeLessThanOrEqual(1);
  });

  test("runs inspect for the selected asset", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);
    await page.getByRole("button", { name: /Inspect/ }).click();

    if (test.info().project.name.includes("mobile")) {
      await expect(page.getByText("2 rows", { exact: true })).toBeVisible({ timeout: 15000 });
      await expect(page.getByText("Ada", { exact: true }).last()).toBeVisible();
      return;
    }

    await expect(page.getByRole("tab", { name: "Inspect" })).toBeVisible();
    await expect(page.getByText("2 rows", { exact: true })).toBeVisible({
      timeout: 15000,
    });
    await expect(
      page.getByRole("columnheader", { name: "customer_id", exact: true })
    ).toBeVisible();
    await expect(page.getByRole("cell", { name: "Ada", exact: true })).toBeVisible();
  });

  test("inspects DuckDB struct and array values as JSON", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop inspect table coverage.");

    const assetPath = "analytics/assets/analytics/complex_types.sql";
    await writeFile(
      join(liveApp.workspaceDir, assetPath),
      `/* @bruin
name: analytics.complex_types
type: duckdb.sql
materialization:
  type: view
@bruin */

select struct(test := 1) a, array(1,2,4) b, array(struct(test := 1), struct(test := 1)) c
`,
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/?pipeline=${encodeRouteId("analytics/pipeline.yml")}&asset=${encodeRouteId(assetPath)}`);
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.complex_types", {
      timeout: 15000,
    });

    await page.getByRole("button", { name: /Inspect/ }).click();

    await expect(page.getByText("1 rows", { exact: true })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByRole("columnheader", { name: "a", exact: true })).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "b", exact: true })).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "c", exact: true })).toBeVisible();
    await expect(page.getByRole("cell", { name: '{"test":1}', exact: true })).toBeVisible();
    await expect(page.getByRole("cell", { name: "[1,2,4]", exact: true })).toBeVisible();
    await expect(page.getByRole("cell", { name: '[{"test":1},{"test":1}]', exact: true })).toBeVisible();
  });

  test("loads a Bruin seed asset and keeps its YAML definition editable", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop seed asset editor coverage.");

    await page.goto(`${liveApp.baseURL}/`);
    await openSeedEditor(page, liveApp.baseURL);

    await expect(page).toHaveTitle("analytics.customer_seed · analytics · Renart");
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.customer_seed");
    await expect(page.getByTestId("editor-asset-path")).toHaveText(
      "analytics/assets/analytics/customer_seed.asset.yml"
    );
    await expect(page.locator(".view-line", { hasText: "type: duckdb.seed" })).toBeVisible();
    await expect(page.locator(".view-line", { hasText: "path: ./customer_seed.csv" })).toBeVisible();

    const seedAsset = await page.evaluate(async () => {
      const response = await fetch("/api/workspace", { cache: "no-store" });
      const workspace = await response.json() as {
        pipelines?: Array<{ assets?: Array<{ name: string; type: string; content: string }> }>;
      };
      return workspace.pipelines
        ?.flatMap((pipeline) => pipeline.assets ?? [])
        .find((asset) => asset.name === "analytics.customer_seed") ?? null;
    });
    expect(seedAsset).toMatchObject({
      name: "analytics.customer_seed",
      type: "duckdb.seed",
    });
    expect(seedAsset?.content).toContain("path: ./customer_seed.csv");
    await waitForWorkspaceAssetUpstreams(page, "analytics.seed_customers", [
      "analytics.customer_seed",
    ]);

    await page.getByRole("tab", { name: "Dependencies" }).click();
    await expect(page.getByText("No automatically inferred dependencies for this asset.")).toBeVisible();

    await page.getByRole("tab", { name: "Materialize" }).click();
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await expect(page.getByText(/Materialize asset: analytics\.customer_seed/)).toBeVisible({
      timeout: 60000,
    });

    const inspectResponse = page.waitForResponse(
      (response) => response.url().includes("/inspect") && response.status() === 200
    );
    await page.getByRole("button", { name: "Inspect Data" }).click();
    await inspectResponse;
    await page.getByRole("tab", { name: "Inspect" }).click();
    await expect(page.getByText("2 rows", { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole("cell", { name: "Grace", exact: true })).toBeVisible();

    await page.getByRole("tab", { name: "Configuration" }).click();
    await page.getByRole("combobox").filter({ hasText: "duckdb.seed" }).click();
    await expect(page.getByRole("option", { name: "duckdb.seed" })).toBeVisible();
  });

  test("switches environments, updates the selector label, and refetches inspect", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop-only environment selector coverage.");

    const configPath = join(liveApp.workspaceDir, ".bruin.yml");
    const currentConfig = await readFile(configPath, "utf8");
    await writeFile(
      configPath,
      `${currentConfig.trimEnd()}\n  prod:\n    connections:\n      duckdb:\n        - name: duckdb-default\n          path: duckdb-files/local.db\n`,
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await expect(page.getByRole("combobox", { name: "Environment" })).toContainText(
      /Environment:\s*default/
    );

    const prodInspectRequest = page.waitForRequest(
      (request) =>
        request.url().includes("/api/assets/") &&
        request.url().includes("/inspect") &&
        request.url().includes("environment=prod")
    );

    await page.getByRole("combobox", { name: "Environment" }).click();
    const prodOption = page.getByRole("option", { name: "prod", exact: true });
    await expect(prodOption).toBeVisible();
    await prodOption.dispatchEvent("click");

    await expect(page).toHaveURL(/environment=prod/);
    await expect(page.getByRole("combobox", { name: "Environment" })).toContainText(
      /Environment:\s*prod/
    );
    await prodInspectRequest;
  });

  test("reveals nested command palette matches from root search", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCommandPalette(page);

    const commandInput = page.locator('[data-slot="command-input"]');
    await commandInput.fill("orders");

    await expect(page.getByRole("option", { name: "analytics.orders analytics" })).toBeVisible();
    await page.getByRole("option", { name: "analytics.orders analytics" }).click();

    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.orders");
    await expect(page.getByTestId("editor-asset-path")).toHaveText(
      "analytics/assets/analytics/orders.sql"
    );
  });

  test("clears the command palette search when entering a nested page", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCommandPalette(page);

    const commandInput = page.locator('[data-slot="command-input"]');
    await commandInput.fill("asset");
    await page.getByRole("option", { name: /Go to asset/i }).click();

    await expect(commandInput).toHaveValue("");
    await expect(page.getByRole("option", { name: "analytics.customers analytics" })).toBeVisible();
    await expect(page.getByRole("option", { name: "analytics.orders analytics" })).toBeVisible();
  });

  test("removes stale Renart inferred dependencies but preserves manual ones", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "No mobile canvas asset-creation interaction yet.");

    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);

    const canvas = page.locator(".react-flow__pane").first();
    const box = await canvas.boundingBox();
    if (!box) {
      throw new Error("Could not locate the React Flow pane for asset creation.");
    }

    await canvas.click({
      position: {
        x: Math.round(box.width * 0.55),
        y: Math.round(box.height * 0.3),
      },
    });
    await page.getByPlaceholder("Asset name").fill("analytics.manual_seed");
    await page
      .getByTestId("rf__node-__new_asset__")
      .getByRole("button", { name: "Create", exact: true })
      .click();

    await expect(page.getByRole("link", { name: "manual_seed", exact: true })).toBeVisible();

    await openCustomersEditor(page, liveApp.baseURL);

    if (test.info().project.name.includes("mobile")) {
      test.skip(true, "No mobile canvas asset-creation interaction yet.");
    }

    await page.getByRole("tab", { name: "Dependencies" }).click();
    const dependencyInput = page.getByPlaceholder("Add dependency");
    const manualDependenciesSection = page.getByText("Manual dependencies").locator("..");
    await dependencyInput.click();
    await dependencyInput.fill("analytics.manual_seed");
    await dependencyInput.press("Enter");

    await expect
      .poll(async () => {
        if (!(await page.getByRole("tab", { name: "Dependencies" }).isVisible().catch(() => false))) {
          return 0;
        }
        await page.getByRole("tab", { name: "Dependencies" }).click();
        return await manualDependenciesSection
          .getByText("analytics.manual_seed", { exact: true })
          .count();
      })
      .toBe(1);

    const saveWithInferredDependency = page.waitForResponse(
      (response) =>
        response.url().includes("/api/pipelines/") &&
        response.url().includes("/assets/YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA") &&
        response.request().method() === "PUT" &&
        (response.request().postData() ?? "").includes("from analytics.orders")
    );
    await replaceEditorContent(page, "select *\nfrom analytics.orders\n");
    await page.keyboard.press("ControlOrMeta+S");
    await saveWithInferredDependency;
    await waitForWorkspaceAssetUpstreams(page, "analytics.customers", [
      "analytics.manual_seed",
      "analytics.orders",
    ]);
    await page.getByRole("tab", { name: "Dependencies" }).click();

    const inferredPanel = page.getByText("Automatically inferred").locator("..");
    await expect(inferredPanel.getByText("analytics.orders", { exact: true })).toBeVisible();
    await expect(
      manualDependenciesSection.getByText("analytics.manual_seed", { exact: true })
    ).toBeVisible();

    const saveWithoutInferredDependency = page.waitForResponse(
      (response) =>
        response.url().includes("/api/pipelines/") &&
        response.url().includes("/assets/YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA") &&
        response.request().method() === "PUT" &&
        (response.request().postData() ?? "").includes("select 1 as customer_id")
    );
    await replaceEditorContent(page, "select 1 as customer_id");
    await page.keyboard.press("ControlOrMeta+S");
    await saveWithoutInferredDependency;
    await waitForWorkspaceAssetUpstreams(page, "analytics.customers", ["analytics.manual_seed"]);
    await page.getByRole("tab", { name: "Dependencies" }).click();

    await expect
      .poll(async () => {
        return await inferredPanel.getByText("analytics.orders", { exact: true }).count();
      }, {
        timeout: 15000,
      })
      .toBe(0);

    await expect(
      manualDependenciesSection.getByText("analytics.manual_seed", { exact: true })
    ).toBeVisible();
    await expect(page.getByText("No automatically inferred dependencies for this asset.")).toBeVisible();
  });

  test("selects a dependency option by tapping it on mobile", async ({
    liveApp,
    page,
  }) => {
    test.skip(!test.info().project.name.includes("mobile"), "Mobile-only repro.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);
    await page.getByRole("tab", { name: "Dependencies" }).click();

    const dependencyInput = page.getByPlaceholder("Add dependency");
    await dependencyInput.fill("analytics.orders");

    const option = page.getByRole("option", { name: "analytics.orders" });
    await expect(option).toBeVisible();
    await option.dispatchEvent("pointerdown", {
      bubbles: true,
      pointerType: "touch",
      isPrimary: true,
      button: 0,
      buttons: 1,
    });
    await option.dispatchEvent("pointerup", {
      bubbles: true,
      pointerType: "touch",
      isPrimary: true,
      button: 0,
      buttons: 0,
    });
    await option.dispatchEvent("click", { bubbles: true });

    await expect(
      page.getByText("Manual dependencies").locator("..").getByText("analytics.orders", {
        exact: true,
      })
    ).toBeVisible();
  });

  test("selects visualization combobox options by tapping them on mobile", async ({
    liveApp,
    page,
  }) => {
    test.skip(!test.info().project.name.includes("mobile"), "Mobile-only repro.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await page.getByRole("tab", { name: "Preview" }).click();

    await page.getByRole("tab", { name: /^Chart$/ }).click();
    await expect(page.getByText("X Axis Column", { exact: true })).toBeVisible();
  });

  test("opens the rename pipeline dialog from the live sidebar context menu", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    if (test.info().project.name.includes("mobile")) {
      test.skip(true, "No mobile pipeline context-menu interaction yet.");
    }

    await page
      .getByRole("link", { name: "analytics", exact: true })
      .click({ button: "right" });
    await page.getByRole("menuitem", { name: "Rename Pipeline" }).click();

    await expect(page.getByLabel("Pipeline name")).toHaveValue("analytics");
  });

  test("opens pipeline settings, saves changes, and persists pipeline.yml", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "No mobile pipeline context-menu interaction yet.");

    const pipelinePath = join(liveApp.workspaceDir, "analytics", "pipeline.yml");
    const originalContent = await readFile(pipelinePath, "utf8");

    try {
      await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA`);

      const pipelineSettingsItem = page.getByRole("button", {
        name: "Pipeline settings",
        exact: true,
      });
      await expect(pipelineSettingsItem).toBeVisible();
      await pipelineSettingsItem.click();

      const dialog = page.getByRole("dialog");
      await expect(dialog.getByText("No unsaved changes")).toBeVisible();

      await dialog.getByRole("button", { name: "General" }).click();
      await dialog.getByLabel("Owner").fill("data-platform-renart");
      await dialog.getByPlaceholder("Add tag").fill("renart-live");
      await dialog.getByPlaceholder("Add tag").press("Enter");

      await dialog.getByRole("button", { name: "Execution" }).click();
      await dialog.getByLabel("Retries").fill("3");
      await dialog.getByLabel("Rerun Cooldown (s)").fill("120");

      await dialog.getByRole("button", { name: "Save" }).click();
      await expect(dialog.getByText("Saved changes", { exact: true })).toBeVisible();

      const updatedContent = await readFile(pipelinePath, "utf8");
      expect(updatedContent).toContain("owner: data-platform-renart");
      expect(updatedContent).toContain("tags:\n  - renart-live");
      expect(updatedContent).toContain("retries: 3");
      expect(updatedContent).toContain("rerun_cooldown: 120");

      await dialog.getByRole("button", { name: "Close", exact: true }).click();
      await expect(dialog).toHaveCount(0);
      await expect(page).toHaveURL(/\/?pipeline=YW5hbHl0aWNz/);
    } finally {
      await writeFile(pipelinePath, originalContent, "utf8");
    }
  });

  test("closes pipeline settings from the section index instead of redirecting to general", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop route redirect coverage.");

    await page.goto(`${liveApp.baseURL}/pipelines/YW5hbHl0aWNz/config?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA`);

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Close", exact: true }).click();

    await expect(dialog).toHaveCount(0);
    await expect(page).not.toHaveURL(/\/pipelines\/[^/]+\/config\/general/);
    await expect(page).toHaveURL(/\/?pipeline=YW5hbHl0aWNz/);
  });

  test("materializes the selected asset and records a history entry", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);
    const emptyHistoryMessage = page.getByText("No materialize runs yet.");

    if (test.info().project.name.includes("mobile")) {
      await expect(page.getByRole("button", { name: "Materialize", exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Materialize", exact: true }).click();
      await expect(page.getByText(/Materialize asset: analytics\.customers/)).toBeVisible({ timeout: 15000 });
      return;
    }

    await page.getByRole("tab", { name: "Materialize" }).click();
    await expect(emptyHistoryMessage).toBeVisible();
    await page.getByRole("button", { name: "Materialize", exact: true }).click();

    await expect(page.getByRole("tab", { name: "Materialize" })).toBeVisible();
    await expect(emptyHistoryMessage).toHaveCount(0);
    await expect(
      page.getByText(/Materialize asset: analytics\.customers/)
    ).toBeVisible({ timeout: 15000 });
  });

  test("runs the selected pipeline and records a pipeline history entry", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    const pipelineEntry = page.getByText("Pipeline: analytics", { exact: true });

    if (test.info().project.name.includes("mobile")) {
      await expect(page.getByRole("button", { name: "Run pipeline" })).toBeVisible();
      await page.getByRole("button", { name: "Run pipeline" }).click();
      await expect(pipelineEntry).toBeVisible({ timeout: 15000 });
      return;
    }

    await page.getByRole("tab", { name: "Materialize" }).click();
    await expect(page.getByText("No materialize runs yet.")).toBeVisible();
    await page.getByRole("button", { name: "Run pipeline" }).click();
    await expect(pipelineEntry).toBeVisible({ timeout: 15000 });
  });

  test("creates, renames, and deletes an asset in an isolated workspace", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    if (test.info().project.name.includes("mobile")) {
      test.skip(true, "No mobile canvas asset-creation interaction yet.");
    }

    await openCustomersEditor(page, liveApp.baseURL);

    const canvas = page.locator(".react-flow__pane").first();
    const box = await canvas.boundingBox();
    if (!box) {
      throw new Error("Could not locate the React Flow pane for asset creation.");
    }

    await canvas.click({
      position: {
        x: Math.round(box.width * 0.35),
        y: Math.round(box.height * 0.35),
      },
    });
    await page.getByPlaceholder("Asset name").fill("analytics.new_asset");
    await page
      .getByTestId("rf__node-__new_asset__")
      .getByRole("button", { name: "Create", exact: true })
      .click();

    await expect(page.getByRole("link", { name: "new_asset", exact: true })).toBeVisible();

    await page.getByRole("link", { name: "new_asset", exact: true }).click();
    await page.getByRole("button", { name: "Rename asset" }).click();
    await page.locator('input[value="analytics.new_asset"]').fill("analytics.renamed_asset");
    await page.getByRole("button", { name: "Save" }).first().click();

    await expect(page.getByRole("link", { name: "renamed_asset", exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("button", { name: "Delete" }).last().click();

    await expect(
      page.getByRole("link", { name: "renamed_asset", exact: true })
    ).toHaveCount(0);
  });

  test("names a downstream child asset inline before creating it", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop canvas hover coverage.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    const customersNode = page.getByTestId(
      `rf__node-${encodeRouteId("analytics/assets/analytics/customers.sql")}`
    );
    await expect(customersNode).toBeVisible({ timeout: 15000 });

    const box = await customersNode.boundingBox();
    if (!box) {
      throw new Error("Could not locate the customers node.");
    }

    await customersNode.hover({
      position: { x: Math.round(box.width / 2), y: Math.max(1, box.height - 8) },
    });
    await customersNode.getByRole("button", { name: "Add" }).click();

    await expect(page.getByText("New child asset")).toBeVisible();
    await expect(page.getByPlaceholder("Asset name")).toBeFocused();
    await expect(page.getByRole("link", { name: "named_child", exact: true })).toHaveCount(0);

    await page.getByPlaceholder("Asset name").fill("analytics.named_child");
    await page
      .getByTestId("rf__node-__new_asset__")
      .getByRole("button", { name: "Create child" })
      .click();

    await expect(page.getByRole("link", { name: "named_child", exact: true })).toBeVisible({
      timeout: 15000,
    });
    await waitForWorkspaceAssetUpstreams(page, "analytics.named_child", ["analytics.customers"]);
  });

  test("creates a seed asset by dropping a CSV file on the canvas", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop file drop coverage.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    const dropzone = page.getByTestId("workspace-canvas-dropzone");
    const dataTransfer = await page.evaluateHandle(() => {
      const transfer = new DataTransfer();
      transfer.items.add(
        new File(
          ["customer_id,customer_name\n10,Lin\n11,Barbara\n"],
          "regional_customers.csv",
          { type: "text/csv" }
        )
      );
      return transfer;
    });

    await dropzone.dispatchEvent("dragover", { dataTransfer });
    await expect(page.getByTestId("seed-drop-overlay")).toBeVisible();

    const createRequest = page.waitForRequest((request) =>
      request.method() === "POST" && request.url().includes("/api/pipelines/") && request.url().endsWith("/assets")
    );
    await dropzone.dispatchEvent("drop", { dataTransfer });
    await createRequest;

    await expect(page.getByRole("link", { name: "regional_customers", exact: true })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.regional_customers");
    await expect(page.locator(".view-line", { hasText: "type: duckdb.seed" })).toBeVisible();
    await expect(page.locator(".view-line", { hasText: "path: ./regional_customers.csv" })).toBeVisible();

    const seedDefinition = await readFile(
      join(liveApp.workspaceDir, "analytics/assets/analytics/regional_customers.asset.yml"),
      "utf8"
    );
    const seedCSV = await readFile(
      join(liveApp.workspaceDir, "analytics/assets/analytics/regional_customers.csv"),
      "utf8"
    );
    expect(seedDefinition).toContain("name: analytics.regional_customers");
    expect(seedDefinition).toContain("type: duckdb.seed");
    expect(seedDefinition).toContain("path: ./regional_customers.csv");
    expect(seedCSV).toContain("11,Barbara");
  });

  test.describe("empty workspace live flows", () => {
    test.use({ fixtureName: "empty-workspace" });

    test("creates, renames, and deletes a pipeline in an isolated workspace", async ({
      liveApp,
      page,
    }) => {
      test.skip(test.info().project.name.includes("mobile"), "No mobile pipeline context-menu interaction yet.");

      await page.goto(`${liveApp.baseURL}/`);

      await expect(page.getByTestId("workspace-onboarding")).toBeVisible();
      await expect(page).toHaveURL(/\/onboarding(?:\/connection)?$/);

      await page.getByRole("button", { name: /skip for now/i }).click();

      await expect(
        page.getByRole("heading", { name: "Create your first pipeline" })
      ).toBeVisible();
      await expect(page).toHaveTitle("Workspace · Renart");
      await page.getByRole("button", { name: "Create pipeline" }).last().click();
      await page.getByLabel("Pipeline path").fill("experiments");
      await page.getByRole("button", { name: "Create Pipeline", exact: true }).click();

      await expect(page).toHaveTitle("experiments · Renart");
      await expect(page.getByRole("link", { name: "experiments", exact: true })).toBeVisible();
      await expect(page).toHaveTitle("experiments · Renart");

      await page
        .getByRole("link", { name: "experiments", exact: true })
        .click({ button: "right" });
      await page.getByRole("menuitem", { name: "Rename Pipeline" }).click();
      await page.getByLabel("Pipeline name").fill("experiments_renamed");
      await page.getByRole("button", { name: "Save" }).click();

      await expect(
        page.getByRole("link", { name: "experiments_renamed", exact: true })
      ).toBeVisible();
      await expect(page).toHaveTitle("experiments_renamed · Renart");

      await page.reload();

      await expect(
        page.getByRole("link", { name: "experiments_renamed", exact: true })
      ).toBeVisible();

      await page
        .getByRole("link", { name: "experiments_renamed", exact: true })
        .click({ button: "right" });
      await page.getByRole("menuitem", { name: "Delete Pipeline" }).click();
      await page.getByRole("button", { name: "Delete Pipeline" }).click();

      await expect(
        page.getByRole("link", { name: "experiments_renamed", exact: true })
      ).toHaveCount(0);
    });
  });

  test.describe("postgres live flows", () => {
    test.use({ fixtureName: "empty-workspace-postgres" });

    test("dry-runs a postgres pipeline without materializing it", async ({
      liveApp,
      livePostgres,
      page,
    }) => {
      test.skip(test.info().project.name.includes("mobile"), "Desktop run options coverage.");
      if (!livePostgres) {
        throw new Error("Expected live Postgres fixture to be available.");
      }

      await mkdir(join(liveApp.workspaceDir, "analytics/assets/analytics"), {
        recursive: true,
      });
      await writeFile(
        join(liveApp.workspaceDir, ".bruin.yml"),
        `default_environment: default
environments:
  default:
    connections:
      postgres:
        - name: postgres-default
          host: ${livePostgres.host}
          port: ${livePostgres.port}
          database: ${livePostgres.database}
          username: ${livePostgres.user}
          password: ${livePostgres.password}
          ssl_mode: disable
`,
        "utf8"
      );
      await writeFile(
        join(liveApp.workspaceDir, ".renart-onboarding.json"),
        JSON.stringify({ active: false, step: "complete" }),
        "utf8"
      );
      await writeFile(
        join(liveApp.workspaceDir, "analytics/pipeline.yml"),
        `name: analytics
default_connections:
  postgres: postgres-default
`,
        "utf8"
      );
      await writeFile(
        join(liveApp.workspaceDir, "analytics/assets/analytics/orders.sql"),
        `/* @bruin
name: analytics.orders
type: pg.sql
materialization:
  type: table
@bruin */

select order_id, order_total from analytics.orders
`,
        "utf8"
      );

      await page.goto(`${liveApp.baseURL}/?pipeline=${encodeRouteId("analytics/pipeline.yml")}`);
      await expect(page).toHaveTitle(/analytics · Renart$/, { timeout: 15000 });
      await page.getByRole("tab", { name: "Materialize" }).click();
      await page.getByRole("button", { name: "Pipeline run options" }).click();
      await page.getByRole("menuitem", { name: "Dry run" }).click();

      await expect(page.getByText("Dry run: analytics", { exact: true })).toBeVisible({
        timeout: 30000,
      });
      await expect(page.getByText(/Successfully validated 1 assets across 1 pipeline/)).toBeVisible({
        timeout: 30000,
      });
      await expect(page.getByText(/Materialization failed/)).toHaveCount(0);
    });
  });
});

async function selectCustomersInWorkspace(
  page: import("@playwright/test").Page,
  baseURL: string
) {
  const isMobile = test.info().project.name.includes("mobile");
  if (isMobile) {
    await page.goto(`${baseURL}/?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA`);
    await expect(page).toHaveTitle("analytics.customers · analytics · Renart");
    return;
  }
}

async function openCustomersEditor(
  page: import("@playwright/test").Page,
  baseURL: string
) {
  const isMobile = test.info().project.name.includes("mobile");
  if (isMobile) {
    await selectCustomersInWorkspace(page, baseURL);
    const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
    if (!(await editorDialog.isVisible().catch(() => false))) {
      await page.getByRole("button", { name: "Edit asset" }).click();
    }
    await expect(editorDialog).toBeVisible();
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.customers");
    await waitForEditorReady(page);
  } else {
    await page.goto(`${baseURL}/?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA`);
    const editorAssetName = page.getByTestId("editor-asset-name");
    const selectedAssetName = await editorAssetName.textContent().catch(() => null);
    if (
      !(await editorAssetName.isVisible().catch(() => false)) ||
      selectedAssetName?.trim() !== "analytics.customers"
    ) {
      const analyticsLink = page.getByRole("link", { name: "analytics", exact: true });
      const customersLink = page.getByRole("link", { name: "customers", exact: true });
      if (!(await customersLink.isVisible().catch(() => false))) {
        await expect(analyticsLink).toBeVisible({ timeout: 15000 });
        await analyticsLink.click();
      }
      await expect(customersLink).toBeVisible({ timeout: 15000 });
      await customersLink.click();
    }
    await expect(editorAssetName).toHaveText("analytics.customers", { timeout: 15000 });
    await waitForEditorReady(page);
  }
}

async function openSeedEditor(
  page: import("@playwright/test").Page,
  baseURL: string
) {
  const pipelineId = encodeRouteId("analytics/pipeline.yml");
  const assetId = encodeRouteId("analytics/assets/analytics/customer_seed.asset.yml");
  await page.goto(`${baseURL}/?pipeline=${pipelineId}&asset=${assetId}`);
  await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.customer_seed", {
    timeout: 15000,
  });
  await waitForEditorReady(page);
}

function encodeRouteId(value: string) {
  return Buffer.from(value, "utf8").toString("base64url");
}

async function reopenCustomersEditor(
  page: import("@playwright/test").Page,
  baseURL: string
) {
  if (test.info().project.name.includes("mobile")) {
    await openCustomersEditor(page, baseURL);
    return;
  }

  await page.getByRole("link", { name: "orders", exact: true }).click();
  await openCustomersEditor(page, baseURL);
}

async function openCommandPalette(page: import("@playwright/test").Page) {
  if (test.info().project.name.includes("mobile")) {
    await page.getByRole("button", { name: "Open search" }).click();
  } else {
    await page.getByRole("button", { name: "Open search" }).click();
  }

  await expect(page.locator('[data-slot="command-input"]')).toBeVisible();
}

async function replaceEditorContent(
  page: import("@playwright/test").Page,
  content: string
) {
  const editor = await waitForEditorReady(page);
  await editor.click();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.type(content);
}

async function waitForEditorReady(page: import("@playwright/test").Page) {
  const editor = page.locator(".monaco-editor").first();
  await expect(page.getByTestId("editor-asset-name")).toHaveText(/analytics\./, {
    timeout: 15000,
  });
  await expect(editor).toBeVisible({ timeout: 15000 });
  await expect(page.locator(".view-lines").first()).toBeVisible({ timeout: 15000 });
  return editor;
}

async function waitForWorkspaceAssetUpstreams(
  page: import("@playwright/test").Page,
  assetName: string,
  expectedUpstreams: string[]
) {
  const sortedExpected = [...expectedUpstreams].sort();
  await expect
    .poll(async () => {
      const upstreams = await page.evaluate(async (targetAssetName) => {
        const response = await fetch("/api/workspace", { cache: "no-store" });
        const workspace = (await response.json()) as {
          pipelines?: Array<{
            assets?: Array<{ name?: string; upstreams?: string[] }>;
          }>;
        };

        for (const pipeline of workspace.pipelines ?? []) {
          for (const asset of pipeline.assets ?? []) {
            if (asset.name === targetAssetName) {
              return asset.upstreams ?? [];
            }
          }
        }

        return null;
      }, assetName);

      return upstreams ? [...upstreams].sort() : null;
    }, { timeout: 15000 })
    .toEqual(sortedExpected);
}
