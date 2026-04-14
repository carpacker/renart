import { expect } from "@playwright/test";
import { access, readFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test } from "./live-app-fixture";

test.describe("workspace onboarding live flows", () => {
  test.use({ fixtureName: "empty-workspace-postgres" });

  const countOccurrences = (source: string, needle: string) =>
    source.split(needle).length - 1;

  test("creates a postgres connection and imports real tables through onboarding", async ({
    page,
    liveApp,
    livePostgres,
  }) => {
    if (!livePostgres) {
      throw new Error("Expected live Postgres fixture to be available.");
    }

    await page.goto(`${liveApp.baseURL}/onboarding`);
    await page.evaluate(() => window.localStorage.removeItem("bruin-web-onboarding-dismissed"));
    await page.reload();

    await expect(page.getByTestId("workspace-onboarding")).toBeVisible({ timeout: 15000 });

    await page.getByRole("button", { name: /postgres/i }).click();
    await expect(page.getByTestId("onboarding-step-connection-config")).toBeVisible();

    await page.getByLabel("Host").fill(livePostgres.host);
    await page.getByLabel("Port").fill(String(livePostgres.port));
    await page.getByLabel("Username").fill(livePostgres.user);
    await page.getByLabel("Password").fill(livePostgres.password);
    await expect(page.getByLabel("Allow SSL")).not.toBeChecked();

    await page.getByRole("button", { name: /Validate and continue/i }).click();
    await expect(page.getByTestId("onboarding-step-import")).toBeVisible();

    await page.getByRole("button", { name: livePostgres.database }).click();
    await expect(page.getByLabel("bruin.analytics.orders")).toBeVisible();
    await expect(page.getByLabel("bruin.analytics.customers")).toBeVisible();

    const configAfterValidation = await readFile(join(liveApp.workspaceDir, ".bruin.yml"), "utf8");
    expect(configAfterValidation).not.toContain("postgres-default");

    const onboardingStateAfterValidation = await readFile(
      join(liveApp.workspaceDir, ".bruin-web-onboarding.json"),
      "utf8"
    );
    expect(onboardingStateAfterValidation).toContain('"step": "import"');
    expect(onboardingStateAfterValidation).toContain('"selected_type": "postgres"');

    await page.getByLabel("bruin.analytics.customers").uncheck();

    const importTextboxes = page.getByTestId("onboarding-step-import").getByRole("textbox");
    await importTextboxes.nth(0).fill("analytics");
    await importTextboxes.nth(1).fill("analytics");
    await page.getByRole("button", { name: /Save connection and import/i }).click();

    await expect(page.getByTestId("onboarding-step-success")).toBeVisible({
      timeout: 30000,
    });

    const configAfterImport = await readFile(join(liveApp.workspaceDir, ".bruin.yml"), "utf8");
    expect(configAfterImport).toContain("postgres-default");
    expect(configAfterImport).toContain(`database: ${livePostgres.database}`);
    expect(countOccurrences(configAfterImport, "name: postgres-default")).toBe(1);

    await page.getByRole("button", { name: "Open workspace" }).click();

    const onboardingStateAfterComplete = await readFile(
      join(liveApp.workspaceDir, ".bruin-web-onboarding.json"),
      "utf8"
    );
    expect(onboardingStateAfterComplete).toContain('"active": false');

    await expect(page).toHaveURL(/\/(?:\?.*)?$/, { timeout: 30000 });

    const importedOrderAsset = await readFile(
      join(
        liveApp.workspaceDir,
        "analytics",
        "assets",
        "analytics",
        "orders.asset.yml"
      ),
      "utf8"
    );
    expect(importedOrderAsset).toContain("analytics.orders");

    const importedCustomersAssetPath = join(
      liveApp.workspaceDir,
      "analytics",
      "assets",
      "analytics",
      "customers.asset.yml"
    );
    await expect
      .poll(async () => {
        try {
          await access(importedCustomersAssetPath);
          return true;
        } catch {
          return false;
        }
      })
      .toBe(false);
  });
});
