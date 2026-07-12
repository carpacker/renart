// Regenerates all landing-page media in docs/public/landing.
//
// Run with `make landing-media` (or `pnpm landing:media` in web/, which builds
// web/dist first). The demo workspace, server, and staged state come from
// demo-media-lib.mjs (shared with make docs-media); this script captures the
// seven landing shots, converts them to webp (q92), and renders the 1200x675
// og-image.
//
// Env overrides: RENART_LANDING_MEDIA_DIR (output dir),
// RENART_LANDING_MEDIA_PORT, GO_BIN, RENART_KEEP_LANDING_WORKSPACE=1.
import { chromium } from "@playwright/test";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import {
  ACME,
  STAGING_ORDERS,
  convertShotsToWebp,
  launchStagedDemo,
  makeCapture,
  repoRoot,
  sharp,
} from "./demo-media-lib.mjs";

const outputDir = path.resolve(
  process.env.RENART_LANDING_MEDIA_DIR ?? path.join(repoRoot, "docs", "public", "landing"),
);
const port = Number(process.env.RENART_LANDING_MEDIA_PORT ?? "18183");

let demo;
let browser;

try {
  await mkdir(outputDir, { recursive: true });
  demo = await launchStagedDemo({ port });

  console.log("capturing screenshots…");
  browser = await chromium.launch();
  const { withPage, goto, shot } = makeCapture(browser, demo.baseURL, outputDir);

  // hero: split view of a staging asset — editor, canvas, results, workbench
  await withPage({ width: 1920, height: 1080 }, async (page) => {
    await goto(page, `/pipelines/${ACME}/assets/${STAGING_ORDERS}/split`, 6000);
    await shot(page, "hero-workspace");
  });

  // notebook: table + line chart in view, cell-actions menu open on the chart
  // cell so "Promote to pipeline" is visible
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, `/notebooks/${demo.notebookId}`, 5000);
    await page.evaluate(() => {
      const scroller = Array.from(document.querySelectorAll("*")).find(
        (el) => el.scrollHeight > el.clientHeight + 100 && el.clientHeight > 400,
      );
      (scroller ?? document.scrollingElement).scrollTop = 760;
    });
    await page.waitForTimeout(1200);
    const menuButtons = page.getByRole("button", { name: "Cell actions" });
    if ((await menuButtons.count()) >= 2) {
      await menuButtons.nth(1).click();
      await page.waitForTimeout(900);
    }
    await shot(page, "lifecycle-notebook");
  });

  // schedules: New-schedule dialog with realistic values over the gantt; a
  // tighter 14:9 viewport so the dialog fills the frame
  await withPage({ width: 1176, height: 756 }, async (page) => {
    await goto(page, "/schedules", 3500);
    await page.getByRole("button", { name: /New schedule/i }).click();
    await page.waitForTimeout(800);
    const dialog = page.locator('[role="dialog"]').last();
    await dialog.locator("select").first().selectOption({ label: "marketing" });
    const inputs = dialog.locator("input:not([type=checkbox])");
    await inputs.nth(0).fill("production");
    await inputs.nth(1).fill("0 7 * * 1-5");
    await inputs.nth(2).fill("Europe/Berlin");
    await page.evaluate(() => document.activeElement?.blur());
    await page.waitForTimeout(600);
    await shot(page, "lifecycle-schedules");
  });

  // staleness: full canvas with all four badges, the stale mart selected
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, `/pipelines/${ACME}/canvas`, 5000);
    await page
      .locator(`.react-flow__node`, { hasText: "customer_ltv" })
      .first()
      .click()
      .catch(() => console.log("could not select customer_ltv node"));
    await page.waitForTimeout(1000);
    await page
      .getByRole("button", { name: "Hide explorer" })
      .click()
      .catch(() => {});
    await page.waitForTimeout(600);
    await page
      .getByRole("button", { name: "Collapse results panel" })
      .click()
      .catch(() => {});
    await page.waitForTimeout(600);
    await page
      .locator(".react-flow__controls-fitview")
      .first()
      .click()
      .catch(() => {});
    await page.waitForTimeout(1200);
    await shot(page, "lifecycle-staleness");
  });

  // runs: the failed run's detail — per-asset gantt + the binder error events
  await withPage({ width: 1200, height: 675 }, async (page) => {
    await goto(page, `/runs/${demo.failedRunId}`, 3500);
    await shot(page, "feature-runs");
  });

  // catalog: cross-pipeline lineage with daily_revenue's upstream path lit
  await withPage({ width: 1200, height: 675 }, async (page) => {
    await goto(page, "/catalog", 4000);
    await page.getByText("daily_revenue", { exact: true }).first().click();
    await page.waitForTimeout(1800);
    await shot(page, "feature-catalog");
  });

  // build: completion popup over upstream columns. Typing in the editor
  // AUTOSAVES to disk, so this shot runs last and restores the file after.
  const stagingOrdersFile = path.join(demo.workspaceDir, "acme", "assets", "staging", "orders.sql");
  const stagingOrdersContent = await readFile(stagingOrdersFile, "utf8");
  try {
    await withPage({ width: 1400, height: 900 }, async (page) => {
      await goto(page, `/pipelines/${ACME}/assets/${STAGING_ORDERS}/code`, 5000);
      await page.getByText("o.total_amount").first().click();
      await page.keyboard.press("End");
      await page.keyboard.press("Enter");
      await page.keyboard.type("    c.", { delay: 60 });
      await page.waitForTimeout(400);
      await page.keyboard.press("Control+Space");
      await page.waitForSelector(".suggest-widget.visible", { timeout: 5000 }).catch(() => {});
      await page.waitForTimeout(1200);
      // hide the card the half-typed query triggers
      await page.evaluate(() => {
        for (const el of Array.from(document.querySelectorAll("div"))) {
          if (
            el.textContent?.startsWith("Preview failed") &&
            el.clientHeight > 0 &&
            el.clientHeight < 300
          ) {
            el.style.visibility = "hidden";
            break;
          }
        }
      });
      await shot(page, "lifecycle-build");
    });
  } finally {
    await writeFile(stagingOrdersFile, stagingOrdersContent);
  }

  await browser.close();
  browser = undefined;

  console.log("converting to webp + og-image…");
  const ogInfo = await sharp()(path.join(outputDir, "hero-workspace.png"))
    .resize(1200, 675, { fit: "cover" })
    .png({ compressionLevel: 9 })
    .toFile(path.join(outputDir, "og-image.png"));
  console.log(`og-image.png ${ogInfo.width}x${ogInfo.height} ${ogInfo.size} bytes`);
  await convertShotsToWebp(outputDir, [
    "hero-workspace",
    "lifecycle-build",
    "lifecycle-notebook",
    "lifecycle-schedules",
    "lifecycle-staleness",
    "feature-runs",
    "feature-catalog",
  ]);
  console.log(`\nLanding media written to ${outputDir}`);
  console.log(
    "If a capture changed size, update the <img> width/height in docs/src/pages/index.astro.",
  );
} finally {
  await browser?.close().catch(() => undefined);
  demo?.stop();
  await demo?.cleanup();
}
