// Regenerates all landing-page media in docs/public/landing.
//
// Run with `make landing-media` (or `pnpm landing:media` in web/, which builds
// web/dist first). The pipeline:
//   1. generates the demo "acme analytics" workspace in a temp dir,
//   2. starts a renart server against it,
//   3. stages realistic state via the HTTP API: a full materialization, a run
//      history (including one failed scheduled run), env schedules, and a
//      notebook with @viz charts,
//   4. adds the marketing pipeline and, last, the staleness edits so the
//      canvas shows all four freshness badges,
//   5. captures the seven screenshots with Playwright (dark theme, 2x DPR),
//   6. converts them to webp (q92) and renders the 1200x675 og-image.
//
// Env overrides: RENART_LANDING_MEDIA_DIR (output dir),
// RENART_LANDING_MEDIA_PORT, GO_BIN, RENART_KEEP_LANDING_WORKSPACE=1.
import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, mkdir, readFile, rm, unlink, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  createAcmeWorkspace,
  addMarketingPipeline,
  addStalenessEdits,
} from "./landing-media-workspace.mjs";

const repoRoot = path.resolve(import.meta.dirname, "..", "..");
const webRoot = path.resolve(import.meta.dirname, "..");
const outputDir = path.resolve(
  process.env.RENART_LANDING_MEDIA_DIR ?? path.join(repoRoot, "docs", "public", "landing")
);
const port = Number(process.env.RENART_LANDING_MEDIA_PORT ?? "18183");
const baseURL = `http://127.0.0.1:${port}`;
const goBinary =
  process.env.GO_BIN ?? (existsSync("/usr/local/go/bin/go") ? "/usr/local/go/bin/go" : "go");
// sharp lives in docs/ (it needs it for the og-image anyway).
const docsRequire = createRequire(path.join(repoRoot, "docs", "package.json"));

const id = (repoRelPath) => Buffer.from(repoRelPath).toString("base64url");
const ACME = id("acme");
const MARKETING = id("marketing");
const STAGING_ORDERS = id("acme/assets/staging/orders.sql");

// nested so the project switcher in the shots shows "acme-ws", not a temp name
const tempRoot = await mkdtemp(path.join(tmpdir(), "renart-landing-media-"));
const workspaceDir = path.join(tempRoot, "acme-ws");
let server;
let browser;

try {
  await mkdir(outputDir, { recursive: true });
  await createAcmeWorkspace(workspaceDir);
  server = await startServer();
  await waitForPipeline("acme");

  console.log("staging: materializing acme…");
  await materializeStream(`/api/pipelines/${ACME}/materialize/stream`);
  console.log("staging: recording run history…");
  await recordRunHistory();
  console.log("staging: creating env schedules…");
  await createSchedules();
  console.log("staging: building notebook…");
  const notebookId = await buildNotebook();
  console.log("staging: adding marketing pipeline…");
  await stageMarketing();
  console.log("staging: applying staleness edits…");
  await addStalenessEdits(workspaceDir);
  await waitForStaleness();

  console.log("capturing screenshots…");
  browser = await chromium.launch();
  await captureAll(notebookId);
  await browser.close();
  browser = undefined;

  console.log("converting to webp + og-image…");
  await convertMedia();
} finally {
  await browser?.close().catch(() => undefined);
  stopServer();
  if (process.env.RENART_KEEP_LANDING_WORKSPACE === "1") {
    console.log(`Kept landing media workspace at ${workspaceDir}`);
  } else {
    await rm(tempRoot, { recursive: true, force: true }).catch(() => undefined);
  }
}

// --- server ------------------------------------------------------------------

async function startServer() {
  const child = spawn(
    goBinary,
    [
      "run",
      ".",
      "web",
      workspaceDir,
      "--host",
      "127.0.0.1",
      "--port",
      String(port),
      "--static-dir",
      path.join(webRoot, "dist"),
      "--watch-mode",
      "poll",
      "--no-open",
    ],
    { cwd: repoRoot, detached: true, stdio: ["ignore", "pipe", "pipe"] }
  );
  let output = "";
  child.stdout.on("data", (chunk) => (output += chunk.toString()));
  child.stderr.on("data", (chunk) => (output += chunk.toString()));

  const deadline = Date.now() + 180_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL}/api/workspace`, { headers: { Origin: baseURL } });
      if (response.ok) {
        return child;
      }
    } catch {
      // still starting
    }
    await sleep(500);
  }
  throw new Error(`renart server did not start in time.\n${output}`);
}

function stopServer() {
  if (!server?.pid) {
    return;
  }
  try {
    process.kill(-server.pid, "SIGTERM");
  } catch {
    try {
      server.kill("SIGTERM");
    } catch {
      // already gone
    }
  }
}

// --- api helpers ---------------------------------------------------------------

async function api(pathname, { method = "GET", body } = {}) {
  const response = await fetch(baseURL + pathname, {
    method,
    headers: { Origin: baseURL, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`${method} ${pathname} -> ${response.status}: ${text.slice(0, 300)}`);
  }
  return text ? JSON.parse(text) : {};
}

// Drains the materialize SSE stream and fails on a non-success final event.
async function materializeStream(pathname) {
  const response = await fetch(baseURL + pathname, {
    method: "POST",
    headers: { Origin: baseURL },
  });
  if (!response.ok) {
    throw new Error(`POST ${pathname} -> ${response.status}`);
  }
  const raw = await response.text();
  const doneEvent = raw
    .split("\n\n")
    .filter((event) => event.includes("event: done"))
    .at(-1);
  if (!doneEvent || !/"status"\s*:\s*"(ok|success)"/.test(doneEvent)) {
    throw new Error(`materialization did not succeed:\n${raw.slice(-800)}`);
  }
}

async function waitForPipeline(name) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    const workspace = await api("/api/workspace").catch(() => null);
    if (workspace?.pipelines?.some((pipeline) => pipeline.name === name)) {
      return;
    }
    await sleep(1000);
  }
  throw new Error(`pipeline ${name} was not discovered in time`);
}

async function triggerRun(pipelineId, environment, trigger) {
  const before = (await api("/api/runs")).runs?.length ?? 0;
  await api(`/api/pipelines/${pipelineId}/trigger`, {
    method: "POST",
    body: { environment, trigger },
  });
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    const { runs = [] } = await api("/api/runs");
    const settled = runs.every((run) => !["running", "pending", "queued"].includes(run.status));
    if (runs.length > before && settled) {
      return;
    }
    await sleep(1000);
  }
  throw new Error(`run of ${pipelineId} (${environment}, ${trigger}) did not settle in time`);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// --- staged state ----------------------------------------------------------------

// A believable run history: scheduled + manual + production runs, and one
// failed scheduled run (a briefly broken column reference in staging.order_items)
// whose binder error shows up in the run-detail event log.
async function recordRunHistory() {
  await triggerRun(ACME, "default", "manual");
  await triggerRun(ACME, "default", "schedule");
  await triggerRun(ACME, "default", "schedule");
  await triggerRun(ACME, "production", "schedule");

  const orderItems = path.join(workspaceDir, "acme", "assets", "staging", "order_items.sql");
  const original = await readFile(orderItems, "utf8");
  const needle = "i.quantity * i.unit_price AS line_total";
  if (!original.includes(needle)) {
    throw new Error(`failed-run edit: expected '${needle}' in ${orderItems}`);
  }
  await writeFile(orderItems, original.replace(needle, "i.quantity * i.unit_pricee AS line_total"));
  await sleep(3000); // let the poll watcher pick up the broken file
  await triggerRun(ACME, "default", "schedule");
  await writeFile(orderItems, original);
  await sleep(3000);

  // leave everything fresh again
  await triggerRun(ACME, "default", "manual");
}

async function createSchedules() {
  await api(`/api/pipelines/${ACME}/env-schedules/default`, {
    method: "PUT",
    body: { cron: "0 6 * * *", timezone: "Europe/Berlin", catchup_policy: "skip" },
  });
  // non-default environments need a deployed snapshot to schedule against
  await api(`/api/pipelines/${ACME}/env-schedules/production`, {
    method: "PUT",
    body: {
      cron: "30 5 * * 1-5",
      timezone: "Europe/Berlin",
      catchup_policy: "run_once",
      deploy_now: true,
    },
  });
}

async function buildNotebook() {
  const created = await api("/api/notebooks", { method: "POST", body: { title: "Revenue deep-dive" } });
  const notebookId = created.notebook.id;
  const cells = async () => (await api(`/api/notebooks/${notebookId}`)).notebook.cells;
  const cellId = async (name) => {
    const cell = (await cells()).find((candidate) => candidate.name === name);
    if (!cell) {
      throw new Error(`notebook cell ${name} not found`);
    }
    return cell.cell_id; // cell endpoints take the short id, not the base64 path id
  };

  const firstCell = (await cells())[0];
  await api(`/api/notebooks/${notebookId}/cells/${firstCell.cell_id}/rename`, {
    method: "POST",
    body: { name: "recent_revenue" },
  });
  await api(`/api/notebooks/${notebookId}/cells/${await cellId("recent_revenue")}`, {
    method: "PUT",
    body: {
      content: `SELECT
    order_date,
    order_count,
    ROUND(revenue, 2) AS revenue,
    ROUND(avg_order_value, 2) AS avg_order_value
FROM mart.daily_revenue
ORDER BY order_date DESC
LIMIT 10`,
    },
  });

  // note: @viz arguments use "key: value", not "key=value"
  await api(`/api/notebooks/${notebookId}/cells`, {
    method: "POST",
    body: { name: "revenue_trend", language: "sql" },
  });
  await api(`/api/notebooks/${notebookId}/cells/${await cellId("revenue_trend")}`, {
    method: "PUT",
    body: {
      content: `-- @viz(line, x: order_date, y: revenue)
SELECT order_date, ROUND(revenue, 2) AS revenue
FROM mart.daily_revenue
ORDER BY order_date`,
    },
  });

  await api(`/api/notebooks/${notebookId}/cells`, {
    method: "POST",
    body: { name: "category_mix", language: "sql" },
  });
  await api(`/api/notebooks/${notebookId}/cells/${await cellId("category_mix")}`, {
    method: "PUT",
    body: {
      content: `-- @viz(bar, x: category, y: revenue)
SELECT category, ROUND(SUM(line_total), 2) AS revenue
FROM staging.order_items
GROUP BY category
ORDER BY revenue DESC`,
    },
  });

  const result = await api(`/api/notebooks/${notebookId}/run`, { method: "POST", body: { all: true } });
  const failed = (result.results ?? []).filter((cell) => !["ok", "success"].includes(cell.status));
  if (failed.length > 0) {
    throw new Error(`notebook cells failed: ${JSON.stringify(failed).slice(0, 400)}`);
  }
  return notebookId;
}

async function stageMarketing() {
  await addMarketingPipeline(workspaceDir);
  await waitForPipeline("marketing");
  await materializeStream(`/api/pipelines/${MARKETING}/materialize/stream`);
  await api(`/api/pipelines/${MARKETING}/env-schedules/default`, {
    method: "PUT",
    body: { cron: "15 * * * *", timezone: "Europe/Berlin", catchup_policy: "skip", deploy_now: true },
  });
  await triggerRun(MARKETING, "default", "schedule");
  await triggerRun(MARKETING, "default", "schedule");
}

// The staleness watcher and the workspace content model update on separate
// paths; the editor renders workspace `content`, so wait for both before
// capturing or the hero shows the pre-edit file.
async function waitForStaleness() {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    const staleness = await api(`/api/pipelines/${ACME}/staleness`).catch(() => null);
    const states = JSON.stringify(staleness ?? {});
    const workspace = await api("/api/workspace").catch(() => null);
    const acme = workspace?.pipelines?.find((pipeline) => pipeline.name === "acme");
    const contentCurrent = acme?.assets
      ?.find((asset) => asset.name === "staging.orders")
      ?.content?.includes("NOT IN ('refunded', 'pending')");
    const weeklyDiscovered = acme?.assets?.some((asset) => asset.name === "mart.weekly_summary");
    if (states.includes("stale_edited") && states.includes("never_built") && contentCurrent && weeklyDiscovered) {
      return;
    }
    await sleep(1000);
  }
  throw new Error("staleness states did not settle in time");
}

// --- captures --------------------------------------------------------------------

async function withPage(viewport, fn) {
  const ctx = await browser.newContext({ viewport, colorScheme: "dark", deviceScaleFactor: 2 });
  await ctx.addInitScript(() => localStorage.setItem("renart-theme", "dark"));
  const page = await ctx.newPage();
  page.on("pageerror", (err) => console.log("PAGEERROR:", err.message));
  try {
    await fn(page);
  } finally {
    await ctx.close();
  }
}

// networkidle never fires (the SSE /api/events connection stays open), so
// navigation always uses domcontentloaded plus a fixed settle.
async function goto(page, url, settle) {
  await page.goto(baseURL + url, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(settle);
}

async function shot(page, name) {
  await page.screenshot({ path: path.join(outputDir, `${name}.png`) });
  console.log("captured", name);
}

async function captureAll(notebookId) {
  const { runs = [] } = await api("/api/runs");
  const failedRun = runs.find((run) => run.status === "failed" || run.status === "error");
  if (!failedRun) {
    throw new Error("expected a failed run for the feature-runs shot");
  }

  // hero: split view of a staging asset — editor, canvas, results, workbench
  await withPage({ width: 1920, height: 1080 }, async (page) => {
    await goto(page, `/pipelines/${ACME}/assets/${STAGING_ORDERS}/split`, 6000);
    await shot(page, "hero-workspace");
  });

  // notebook: table + line chart in view, cell-actions menu open on the chart
  // cell so "Promote to pipeline" is visible
  await withPage({ width: 1400, height: 900 }, async (page) => {
    await goto(page, `/notebooks/${notebookId}`, 5000);
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
    await page.getByRole("button", { name: "Hide explorer" }).click().catch(() => {});
    await page.waitForTimeout(600);
    await page.getByRole("button", { name: "Collapse results panel" }).click().catch(() => {});
    await page.waitForTimeout(600);
    await page.locator(".react-flow__controls-fitview").first().click().catch(() => {});
    await page.waitForTimeout(1200);
    await shot(page, "lifecycle-staleness");
  });

  // runs: the failed run's detail — per-asset gantt + the binder error events
  await withPage({ width: 1200, height: 675 }, async (page) => {
    await goto(page, `/runs/${failedRun.id}`, 3500);
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
  const stagingOrdersFile = path.join(workspaceDir, "acme", "assets", "staging", "orders.sql");
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
      // hide the toast the half-typed query triggers
      await page.evaluate(() => {
        for (const el of Array.from(document.querySelectorAll("div"))) {
          if (el.textContent?.startsWith("Preview failed") && el.clientHeight > 0 && el.clientHeight < 300) {
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
}

// --- conversion ------------------------------------------------------------------

async function convertMedia() {
  const sharp = docsRequire("sharp");
  const names = [
    "hero-workspace",
    "lifecycle-build",
    "lifecycle-notebook",
    "lifecycle-schedules",
    "lifecycle-staleness",
    "feature-runs",
    "feature-catalog",
  ];
  const ogInfo = await sharp(path.join(outputDir, "hero-workspace.png"))
    .resize(1200, 675, { fit: "cover" })
    .png({ compressionLevel: 9 })
    .toFile(path.join(outputDir, "og-image.png"));
  console.log(`og-image.png ${ogInfo.width}x${ogInfo.height} ${ogInfo.size} bytes`);
  for (const name of names) {
    const png = path.join(outputDir, `${name}.png`);
    const info = await sharp(png).webp({ quality: 92 }).toFile(path.join(outputDir, `${name}.webp`));
    await unlink(png);
    console.log(`${name}.webp ${info.width}x${info.height} ${info.size} bytes`);
  }
  console.log(`\nLanding media written to ${outputDir}`);
  console.log("If a capture changed size, update the <img> width/height in docs/src/pages/index.astro.");
}
