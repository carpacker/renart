import { expect } from "@playwright/test";
import { liveTest as test } from "./live-app-fixture";

test.describe("implemented feature requests live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("opens the environment editor for a configured workspace", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/settings/environments`);

    if (!test.info().project.name.includes("mobile")) {
      await page.getByRole("button", { name: "Edit" }).first().click();
    }

    await expect(page.getByText("Environment Editor", { exact: true }).last()).toBeVisible();
    await expect(page.getByText(".bruin.yml", { exact: true }).last()).toBeVisible();
    await expect(page.locator('input[placeholder="staging"]')).toHaveValue("default");
    await expect(page.locator('input[placeholder="staging_"]')).toHaveValue("dev_");
  });

  test("opens the connection editor with an explicit type selector", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/settings/connections`);

    if (!test.info().project.name.includes("mobile")) {
      await page.getByRole("button", { name: "Edit" }).first().click();
    }

    await expect(page.getByText("Connection Editor", { exact: true }).last()).toBeVisible();
    await expect(page.getByRole("textbox", { name: "MY_CONNECTION" })).toHaveValue(
      "duckdb-default"
    );
    await expect(page.getByRole("combobox").nth(1)).toContainText("duckdb");
    await expect(page.locator('input[value="duckdb-files/local.db"]')).toBeVisible();
  });

  test("renames an asset inline from the editor header with Enter", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page);
    await page.getByRole("button", { name: "Rename asset" }).click();
    const renameInput = test.info().project.name.includes("mobile")
      ? page.getByRole("dialog", { name: "Asset Editor" }).getByRole("textbox").first()
      : page.locator("main").getByRole("textbox").first();
    await renameInput.fill("analytics.customers_live");
    await renameInput.press("Enter");

    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.customers_live");
  });

  test("shows the already-saved message on Ctrl+S", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page);
    await page.locator(".monaco-editor").click();
    await page.keyboard.press("ControlOrMeta+S");

    await expect(page.getByText("Already saved.", { exact: true })).toBeVisible();
  });

  test("runs inspect from the SQL editor with Ctrl+Enter", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page);
    await page.locator(".monaco-editor").click();
    const inspectResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/assets/") &&
        response.url().includes("/inspect") &&
        response.request().method() === "GET"
    );
    await page.keyboard.press("ControlOrMeta+Enter");

    await inspectResponse;

    if (test.info().project.name.includes("mobile")) {
      await expect(page.getByText("2 rows", { exact: true })).toBeVisible({ timeout: 15000 });
      await expect(page.getByText("Ada", { exact: true }).last()).toBeVisible();
      return;
    }

    await expect(page.getByRole("tab", { name: "Inspect" })).toBeVisible();
    await expect(page.getByText("2 rows", { exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole("cell", { name: "Ada", exact: true })).toBeVisible();
  });
});

async function openCustomersEditor(page: import("@playwright/test").Page) {
  const isMobile = test.info().project.name.includes("mobile");
  if (isMobile) {
    await page.goto(`${page.url().split("?")[0]}?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9jdXN0b21lcnMuc3Fs`);
    await expect(page).toHaveTitle("analytics.customers · analytics · Bruin Web");
    const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
    if (!(await editorDialog.isVisible().catch(() => false))) {
      const editButton = page.getByRole("button", { name: "Edit asset" });
      if (await editButton.isVisible().catch(() => false)) {
        await editButton.click();
      }
    }
    await expect(editorDialog).toBeVisible();
    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.customers");
  } else {
    await page.goto(`${page.url().split("?")[0]}?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9jdXN0b21lcnMuc3Fs`);
    const editorAssetName = page.getByTestId("editor-asset-name");
    if (!(await editorAssetName.isVisible().catch(() => false))) {
      const analyticsLink = page.getByRole("link", { name: "analytics", exact: true });
      await expect(analyticsLink).toBeVisible({ timeout: 15000 });
      await analyticsLink.click();
    }

    await expect(editorAssetName).toHaveText("analytics.customers", { timeout: 15000 });
  }
}
