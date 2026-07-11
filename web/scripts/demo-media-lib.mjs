// Shared machinery for the landing-page and docs media pipelines: builds the
// demo "acme analytics" workspace, starts a renart server against it, stages
// realistic state via the HTTP API, and provides capture/convert helpers.
//
// Used by capture-landing-media.mjs (make landing-media) and
// capture-docs-media.mjs (make docs-media).
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, readFile, rm, unlink, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  createAcmeWorkspace,
  addMarketingPipeline,
  addStalenessEdits,
} from "./landing-media-workspace.mjs";

export const repoRoot = path.resolve(import.meta.dirname, "..", "..");
export const webRoot = path.resolve(import.meta.dirname, "..");

export const id = (repoRelPath) => Buffer.from(repoRelPath).toString("base64url");
export const ACME = id("acme");
export const MARKETING = id("marketing");
export const STAGING_ORDERS = id("acme/assets/staging/orders.sql");

// sharp lives in docs/ (the docs site needs it anyway).
const docsRequire = createRequire(path.join(repoRoot, "docs", "package.json"));

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// Builds the demo workspace, boots a renart server on `port`, and stages the
// full demo state. Returns a handle with API helpers and `stop()`.
export async function launchStagedDemo({ port }) {
  const baseURL = `http://127.0.0.1:${port}`;
  const goBinary =
    process.env.GO_BIN ?? (existsSync("/usr/local/go/bin/go") ? "/usr/local/go/bin/go" : "go");
  // nested so the project switcher in the shots shows "acme-ws", not a temp name
  const tempRoot = await mkdtemp(path.join(tmpdir(), "renart-demo-media-"));
  const workspaceDir = path.join(tempRoot, "acme-ws");

  await createAcmeWorkspace(workspaceDir);

  const server = spawn(
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
  let serverOutput = "";
  server.stdout.on("data", (chunk) => (serverOutput += chunk.toString()));
  server.stderr.on("data", (chunk) => (serverOutput += chunk.toString()));

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

  // wait for the server
  const deadline = Date.now() + 180_000;
  for (;;) {
    if (Date.now() > deadline) {
      throw new Error(`renart server did not start in time.\n${serverOutput}`);
    }
    const ok = await fetch(`${baseURL}/api/workspace`, { headers: { Origin: baseURL } })
      .then((response) => response.ok)
      .catch(() => false);
    if (ok) {
      break;
    }
    await sleep(500);
  }
  await waitForPipeline("acme");

  // --- staged state ---------------------------------------------------------

  console.log("staging: materializing acme…");
  await materializeStream(`/api/pipelines/${ACME}/materialize/stream`);

  // A believable run history: scheduled + manual + production runs, and one
  // failed scheduled run (a briefly broken column reference in
  // staging.order_items) whose binder error shows in the run-detail event log.
  console.log("staging: recording run history…");
  await triggerRun(ACME, "default", "manual");
  await triggerRun(ACME, "default", "schedule");
  await triggerRun(ACME, "default", "schedule");
  await triggerRun(ACME, "production", "schedule");
  const orderItems = path.join(workspaceDir, "acme", "assets", "staging", "order_items.sql");
  const orderItemsOriginal = await readFile(orderItems, "utf8");
  const breakNeedle = "i.quantity * i.unit_price AS line_total";
  if (!orderItemsOriginal.includes(breakNeedle)) {
    throw new Error(`failed-run edit: expected '${breakNeedle}' in ${orderItems}`);
  }
  await writeFile(
    orderItems,
    orderItemsOriginal.replace(breakNeedle, "i.quantity * i.unit_pricee AS line_total")
  );
  await sleep(3000); // let the poll watcher pick up the broken file
  await triggerRun(ACME, "default", "schedule");
  await writeFile(orderItems, orderItemsOriginal);
  await sleep(3000);
  await triggerRun(ACME, "default", "manual"); // leave everything fresh again

  console.log("staging: creating env schedules…");
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

  console.log("staging: building notebook…");
  const notebookId = await buildNotebook(api);

  console.log("staging: adding marketing pipeline…");
  await addMarketingPipeline(workspaceDir);
  await waitForPipeline("marketing");
  await materializeStream(`/api/pipelines/${MARKETING}/materialize/stream`);
  await api(`/api/pipelines/${MARKETING}/env-schedules/default`, {
    method: "PUT",
    body: { cron: "15 * * * *", timezone: "Europe/Berlin", catchup_policy: "skip", deploy_now: true },
  });
  await triggerRun(MARKETING, "default", "schedule");
  await triggerRun(MARKETING, "default", "schedule");

  console.log("staging: applying staleness edits…");
  await addStalenessEdits(workspaceDir);
  await waitForStalenessSettled(api);

  const { runs = [] } = await api("/api/runs");
  const failedRun = runs.find((run) => run.status === "failed" || run.status === "error");
  if (!failedRun) {
    throw new Error("expected a failed run in the staged history");
  }

  return {
    baseURL,
    workspaceDir,
    api,
    notebookId,
    failedRunId: failedRun.id,
    stop() {
      if (!server.pid) {
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
    },
    async cleanup() {
      if (process.env.RENART_KEEP_LANDING_WORKSPACE === "1") {
        console.log(`Kept demo workspace at ${workspaceDir}`);
      } else {
        await rm(tempRoot, { recursive: true, force: true }).catch(() => undefined);
      }
    },
  };
}

async function buildNotebook(api) {
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

// The staleness watcher and the workspace content model update on separate
// paths; the editor renders workspace `content`, so wait for both before
// capturing or shots show the pre-edit file.
async function waitForStalenessSettled(api) {
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

// --- capture helpers ---------------------------------------------------------

// Returns { withPage, goto, shot } bound to a browser + base URL + output dir.
export function makeCapture(browser, baseURL, outputDir) {
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

  return { withPage, goto, shot };
}

export function sharp() {
  return docsRequire("sharp");
}

// Converts `<name>.png` files in outputDir to webp (q92) and deletes the PNGs.
export async function convertShotsToWebp(outputDir, names) {
  const sharpLib = sharp();
  for (const name of names) {
    const png = path.join(outputDir, `${name}.png`);
    const info = await sharpLib(png).webp({ quality: 92 }).toFile(path.join(outputDir, `${name}.webp`));
    await unlink(png);
    console.log(`${name}.webp ${info.width}x${info.height} ${info.size} bytes`);
  }
}
