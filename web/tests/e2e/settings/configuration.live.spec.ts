import { expect } from "@playwright/test";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test } from "../live-app-fixture";

test.describe("settings configuration live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("opens the environment edit dialog from the environments hub", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/settings/environments`);
    await page.getByRole("link", { name: "Edit" }).first().click();

    await expect(page).toHaveURL(/\/settings\/environments\/default\/edit/);
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByRole("textbox").first()).toHaveValue("default");
    await expect(page.getByRole("button", { name: "Save Changes" })).toBeVisible();
  });

  test("opens the connection dialog with an explicit type selector", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/settings/environments/default`);
    await page.getByRole("row", { name: /duckdb-default/i }).getByRole("link", { name: "View" }).click();

    await expect(page).toHaveURL(/\/settings\/environments\/default\/connections\/duckdb-default/);
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("textbox").first()).toHaveValue("duckdb-default");
    await expect(dialog.getByRole("combobox").first()).toContainText("duckdb");
    await expect(dialog.locator('input[value="duckdb-files/local.db"]')).toBeVisible();
  });

  test("edits and persists chess connection players", async ({ liveApp, page }) => {
    const configPath = join(liveApp.workspaceDir, ".bruin.yml");
    await writeFile(
      configPath,
      [
        "default_environment: default",
        "environments:",
        "  default:",
        "    connections:",
        "      duckdb:",
        "        - name: duckdb-default",
        "          path: duckdb-files/local.db",
        "      chess:",
        "        - name: chess-default",
        "          players:",
        "            - MagnusCarlsen",
        "            - Hikaru",
        "",
      ].join("\n")
    );

    await page.goto(`${liveApp.baseURL}/settings/environments/default/connections/chess-default`);
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("MagnusCarlsen", { exact: true })).toBeVisible();
    const hikaruChip = dialog.locator("[data-slot=combobox-chip]", { hasText: "Hikaru" });
    await expect(hikaruChip).toBeVisible();

    const playersInput = dialog.getByPlaceholder("Add values...");
    await playersInput.click();
    await page.getByRole("option", { name: "Hikaru", exact: true }).click();
    await expect(hikaruChip).toBeHidden();

    await playersInput.click();
    await page.getByRole("option", { name: "Hikaru", exact: true }).click();
    await expect(hikaruChip).toBeVisible();

    await playersInput.fill("FabianoCaruana");
    await playersInput.press("Enter");
    await expect(
      dialog.locator("[data-slot=combobox-chip]", { hasText: "FabianoCaruana" })
    ).toBeVisible();

    const saveResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/config/connections") &&
        response.request().method() === "PUT"
    );
    await dialog.getByRole("button", { name: "Save Changes" }).click();
    await expect((await saveResponse).ok()).toBe(true);

    const savedConfig = await readFile(configPath, "utf8");
    expect(savedConfig).toContain("chess-default");
    expect(savedConfig).toContain("MagnusCarlsen");
    expect(savedConfig).toContain("Hikaru");
    expect(savedConfig).toContain("FabianoCaruana");
  });
});
