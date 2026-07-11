// Regenerates the docs screenshots in docs/public/docs-media.
//
// Run with `make docs-media` (or `pnpm docs:media` in web/, which builds
// web/dist first). Shares the demo workspace, server, and staged state with
// the landing pipeline (demo-media-lib.mjs) so the docs show the same
// coherent acme project at the same quality bar: dark theme, 2x DPR, webp.
//
// Env overrides: RENART_DOCS_MEDIA_DIR (output dir), RENART_DOCS_MEDIA_PORT,
// GO_BIN, RENART_KEEP_LANDING_WORKSPACE=1.
import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import {
  ACME,
  STAGING_ORDERS,
  convertShotsToWebp,
  launchStagedDemo,
  makeCapture,
  repoRoot,
} from "./demo-media-lib.mjs";

const outputDir = path.resolve(
  process.env.RENART_DOCS_MEDIA_DIR ?? path.join(repoRoot, "docs", "public", "docs-media")
);
const port = Number(process.env.RENART_DOCS_MEDIA_PORT ?? "18184");

let demo;
let browser;

try {
  await mkdir(outputDir, { recursive: true });
  demo = await launchStagedDemo({ port });

  console.log("capturing screenshots…");
  browser = await chromium.launch();
  const { withPage, goto, shot } = makeCapture(browser, demo.baseURL, outputDir);

  // workspace-overview: the split view — explorer, editor, canvas, results,
  // workbench in one frame (interface tour, docs landing, quickstart)
  await withPage({ width: 1600, height: 1000 }, async (page) => {
    await goto(page, `/pipelines/${ACME}/assets/${STAGING_ORDERS}/split`, 6000);
    await shot(page, "workspace-overview");
  });

  // pipeline-canvas: the full DAG with all four freshness badges
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, `/pipelines/${ACME}/canvas`, 5000);
    await page
      .locator(`.react-flow__node`, { hasText: "customer_ltv" })
      .first()
      .click()
      .catch(() => console.log("could not select customer_ltv node"));
    await page.waitForTimeout(1000);
    await page.getByRole("button", { name: "Hide explorer" }).click().catch(() => {});
    await page.waitForTimeout(600);
    await page.getByRole("button", { name: "Collapse results panel" }).click().catch(() => {});
    await page.waitForTimeout(600);
    await page.locator(".react-flow__controls-fitview").first().click().catch(() => {});
    await page.waitForTimeout(1200);
    await shot(page, "pipeline-canvas");
  });

  // asset-editor: code view with the completion popup over upstream columns.
  // Typing autosaves; the demo workspace is disposable but restore anyway so
  // later shots (if reordered) see the staged content.
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
    await page.evaluate(() => {
      for (const el of Array.from(document.querySelectorAll("div"))) {
        if (el.textContent?.startsWith("Preview failed") && el.clientHeight > 0 && el.clientHeight < 300) {
          el.style.visibility = "hidden";
          break;
        }
      }
    });
    await shot(page, "asset-editor");
    // undo the typed line so the buffer autosaves back to the staged content
    await page.keyboard.press("Escape");
    for (let i = 0; i < 8; i++) {
      await page.keyboard.press("Control+Z");
    }
    await page.waitForTimeout(1200);
  });

  // notebook: table result + @viz chart + the cell-actions menu with
  // "Promote to pipeline" visible
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, `/notebooks/${demo.notebookId}`, 5000);
    await page.evaluate(() => {
      const scroller = Array.from(document.querySelectorAll("*")).find(
        (el) => el.scrollHeight > el.clientHeight + 100 && el.clientHeight > 400
      );
      (scroller ?? document.scrollingElement).scrollTop = 760;
    });
    await page.waitForTimeout(1200);
    const menuButtons = page.getByRole("button", { name: "Cell actions" });
    if ((await menuButtons.count()) >= 2) {
      await menuButtons.nth(1).click();
      await page.waitForTimeout(900);
    }
    await shot(page, "notebook");
  });

  // schedules: the schedule list with the run timeline
  await withPage({ width: 1400, height: 760 }, async (page) => {
    await goto(page, "/schedules", 3500);
    await shot(page, "schedules");
  });

  // run-detail: the failed run — per-asset gantt + the error in the event log
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, `/runs/${demo.failedRunId}`, 3500);
    await shot(page, "run-detail");
  });

  // catalog: cross-pipeline lineage with daily_revenue's upstream path lit
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, "/catalog", 4000);
    await page.getByText("daily_revenue", { exact: true }).first().click();
    await page.waitForTimeout(1800);
    await shot(page, "catalog");
  });

  await browser.close();
  browser = undefined;

  console.log("converting to webp…");
  await convertShotsToWebp(outputDir, [
    "workspace-overview",
    "pipeline-canvas",
    "asset-editor",
    "notebook",
    "schedules",
    "run-detail",
    "catalog",
  ]);
  console.log(`\nDocs media written to ${outputDir}`);
  console.log("If a capture changed size, update the width/height where the image is referenced.");
} finally {
  await browser?.close().catch(() => undefined);
  demo?.stop();
  await demo?.cleanup();
}
