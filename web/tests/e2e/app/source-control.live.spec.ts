import { expect } from "@playwright/test";
import { execFile } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";

import { liveTest as test } from "../live-app-fixture";

const execFileAsync = promisify(execFile);

test.describe("app source control live", () => {
  test.use({ fixtureName: "basic-workspace" });

  test("shows unstaged changes, previews a diff, stages and unstages a file", async ({
    liveApp,
    page,
  }) => {
    await initializeGitRepository(liveApp.workspaceDir);
    await writeFile(join(liveApp.workspaceDir, "e2e-git.txt"), "first line\nsecond line\n", "utf8");

    await page.goto(`${liveApp.baseURL}`);
    await page.getByRole("button", { name: "Source control" }).click();

    await expect(page.getByRole("heading", { name: "Source control" })).toBeVisible();
    await expect(page.getByText("Changes · 1")).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("Staged · 0")).toBeHidden();
    await expect(page.getByRole("button", { name: "e2e-git.txt" })).toBeVisible();

    await page.getByRole("button", { name: "e2e-git.txt" }).click();
    await expect(page.getByText("+++ b/e2e-git.txt")).toBeVisible();
    await expect(page.getByText("+first line")).toBeVisible();

    await page.getByRole("button", { name: "Stage", exact: true }).click();
    await expect(page.getByText("Staged · 1")).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("Changes · 0")).toBeHidden();

    await page.getByRole("button", { name: "e2e-git.txt" }).click();
    await expect(page.getByText("staged", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Unstage", exact: true }).click();
    await expect(page.getByText("Changes · 1")).toBeVisible({ timeout: 15000 });
  });
});

async function initializeGitRepository(workspaceDir: string) {
  await execFileAsync("git", ["init"], { cwd: workspaceDir });
  await execFileAsync("git", ["config", "user.email", "renart-e2e@example.com"], {
    cwd: workspaceDir,
  });
  await execFileAsync("git", ["config", "user.name", "Renart E2E"], { cwd: workspaceDir });
  await execFileAsync("git", ["add", "."], { cwd: workspaceDir });
  await execFileAsync("git", ["commit", "-m", "Initial fixture"], { cwd: workspaceDir });
}
