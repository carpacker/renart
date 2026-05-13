import { chromium } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

const repoRoot = path.resolve(import.meta.dirname, "..", "..");
const webRoot = path.resolve(import.meta.dirname, "..");
const staticDir = path.join(webRoot, "dist");
const outputDir = path.resolve(
  process.env.RENART_LANDING_MEDIA_DIR ?? path.join(webRoot, "public", "landing")
);
const port = Number(process.env.RENART_LANDING_MEDIA_PORT ?? "18183");
const goBinary = process.env.GO_BIN ?? "/usr/local/go/bin/go";
const baseURL = `http://127.0.0.1:${port}`;
const viewport = { width: 1920, height: 1080 };
const workspaceDir = await mkdtemp(path.join(tmpdir(), "renart-landing-media-"));

let server;
let browser;

try {
  await mkdir(outputDir, { recursive: true });
  await writeCaptureFramePage();
  await createEcommerceWorkspace(workspaceDir);
  await run("git", ["init"], workspaceDir);

  server = spawn(
    goBinary,
    [
      "run",
      ".",
      "web",
      workspaceDir,
      "--port",
      String(port),
      "--static-dir",
      staticDir,
      "--watch-mode",
      "fsnotify",
      "--no-open",
    ],
    { cwd: repoRoot, detached: true, stdio: ["ignore", "pipe", "pipe"] }
  );

  let serverOutput = "";
  server.stdout.on("data", (chunk) => {
    serverOutput += chunk.toString();
  });
  server.stderr.on("data", (chunk) => {
    serverOutput += chunk.toString();
  });

  await waitForServer(baseURL, () => serverOutput);
  browser = await chromium.launch({ headless: true, slowMo: 120 });

  const page = await newLandingPage(browser);
  const workspace = await loadWorkspace();
  const { pipeline, assets } = resolveEcommerceAssets(workspace);
  const positions = buildCanonicalPositions(assets);

  await materializePipeline(pipeline.id);
  await waitForAssetsMaterialized(pipeline.id);
  await assertAssetInspectRows(assets.revenue.id, "mart.daily_revenue");

  await setLandingLocalStorage(page, positions);
  await openAsset(page, pipeline.id, assets.staging.id);
  await page.waitForTimeout(1200);
  await captureDagCanvas(page, path.join(outputDir, "feature-dag-canvas.png"));

  await captureIntellisense(page, pipeline, assets);
  await writeFile(path.join(workspaceDir, "ecommerce", "assets", "mart", "daily_revenue.sql"), ecommerceMartRevenueSQL());
  await captureValidation(pipeline, assets, positions);
  await captureJumpToDefinition(pipeline, assets, positions);
  await writeFile(path.join(workspaceDir, "ecommerce", "assets", "mart", "daily_revenue.sql"), ecommerceMartRevenueSQL());
  await captureHeroRoundtrip(pipeline, assets, positions);

  console.log("Captured Renart landing media:");
  for (const fileName of [
    "hero-canvas-roundtrip.webm",
    "feature-intellisense.png",
    "feature-validation.png",
    "feature-jump-to-def.gif",
    "feature-dag-canvas.png",
  ]) {
    console.log(path.join(outputDir, fileName));
  }
} finally {
  await browser?.close().catch(() => undefined);
  if (server) {
    killServerGroup(server, "SIGTERM");
    await new Promise((resolve) => setTimeout(resolve, 500));
    killServerGroup(server, "SIGKILL");
  }
  if (process.env.RENART_KEEP_LANDING_WORKSPACE !== "1") {
    await rm(workspaceDir, { recursive: true, force: true }).catch(() => undefined);
  } else {
    console.log(`Kept landing media workspace at ${workspaceDir}`);
  }
}

async function captureHeroRoundtrip(pipeline, assets, positions) {
  const frameDir = await mkdtemp(path.join(tmpdir(), "renart-hero-frames-"));
  const context = await browser.newContext({
    viewport,
    deviceScaleFactor: 1,
  });
  const shellPage = await context.newPage();
  const page = await openCaptureFrame(shellPage, `${baseURL}/?pipeline=${encodeURIComponent(pipeline.id)}`);
  await setLandingLocalStorage(page, positions);
  await openPipeline(page, pipeline.id);
  await installLandingCursor(page);
  await page.waitForTimeout(1000);
  await fitCanvas(page);
  await resetIframeZoom(shellPage);
  const recorder = await startScreenshotRecorder(shellPage, frameDir, { fps: 12 });
  await page.waitForTimeout(1200);
  await clickAssetNode(page, assets.revenue.id);
  await page.locator(".monaco-editor").waitFor({ timeout: 60_000 });
  await waitForEditorText(page, "FROM staging.orders");
  await page.waitForTimeout(900);
  await revealTextInEditor(page, "FROM staging.orders");
  await focusIframe(shellPage, editorFocusTargets("FROM staging.orders"));
  await showEditorHover(page);
  await page.waitForTimeout(1400);
  await waitForEditorText(page, "AVG(total_amount) AS avg_order_value");
  await page.waitForTimeout(700);
  await addMaxOrderValueColumn(page);
  await waitForEditorText(page, "MAX(total_amount) AS max_order_value");
  await page.waitForTimeout(1200);
  await dismissMonacoOverlays(page);
  await focusIframe(shellPage, editorFocusTargets("max_order_value"), { maxZoom: 1.35 });
  await page.getByRole("button", { name: "Inspect Data" }).click();
  await waitForInspectPanel(page);
  await focusIframe(shellPage, [{ selector: '[data-testid="workspace-results-panel"]' }], { maxZoom: 1.35, padding: 96 });
  await page.waitForTimeout(900);
  await configureChartVisualization(page, shellPage);
  await waitForResultsChart(page);
  await focusIframe(shellPage, [{ selector: '[data-testid="workspace-results-panel"] [data-slot="chart"]' }], { maxZoom: 1.6, padding: 72 });
  await page.waitForTimeout(2200);
  await resetIframeZoom(shellPage);
  await reloadCanvasLayout(page);
  await page.waitForTimeout(1200);
  await resetIframeZoom(shellPage);
  await blurActiveElement(page);
  await assertIframeZoom(shellPage, 1);
  await page.waitForTimeout(2600);
  const recording = await recorder.stop();
  await context.close();
  const heroOutputPath = path.join(outputDir, "hero-canvas-roundtrip.webm");
  await encodeFramesToWebm(frameDir, heroOutputPath, { fps: recording.playbackFps });
  await assertMediaDuration(heroOutputPath, 14, "hero canvas roundtrip video");
  await rm(frameDir, { recursive: true, force: true }).catch(() => undefined);
}

async function captureIntellisense(page, pipeline, assets) {
  await openAsset(page, pipeline.id, assets.revenue.id);
  await page.locator(".monaco-editor").waitFor({ timeout: 60_000 });
  await revealTextInEditor(page, "GROUP BY order_date");
  await page.keyboard.press(process.platform === "darwin" ? "Meta+End" : "Control+End");
  await page.keyboard.type("\nHAVING\n    order_");
  await waitForSuggestWidget(page, { minimumSuggestions: 2 });
  await page.waitForTimeout(900);
  await captureEditorPanel(page, path.join(outputDir, "feature-intellisense.png"), {
    includeSuggest: true,
  });
}

async function captureValidation(pipeline, assets, positions) {
  const typoContent = ecommerceStagingOrdersSQL().replace(
    "c.customer_name,\n    o.order_date,",
    "c.custmer_name,\n    o.ordr_date,"
  );
  const stagingAssetPath = path.join(workspaceDir, "ecommerce", "assets", "staging", "orders.sql");
  await writeFile(stagingAssetPath, typoContent);
  const page = await newLandingPage(browser);
  await setLandingLocalStorage(page, positions);
  await openAsset(page, pipeline.id, assets.staging.id);
  await page.locator(".monaco-editor").waitFor({ timeout: 60_000 });
  await revealTextInEditor(page, "custmer_name");
  await hoverTextInEditor(page, "custmer_name");
  await showEditorHover(page);
  await waitForMonacoHoverOnScreen(page);
  await page.waitForTimeout(1500);
  await captureEditorPanel(page, path.join(outputDir, "feature-validation.png"), {
    includeHover: true,
  });
  await page.close();
  await writeFile(stagingAssetPath, ecommerceStagingOrdersSQL());
}

async function captureJumpToDefinition(pipeline, assets, positions) {
  const videoDir = await mkdtemp(path.join(tmpdir(), "renart-jump-video-"));
  const recordingStartedAt = Date.now();
  const context = await browser.newContext({
    viewport: { width: 1200, height: 700 },
    deviceScaleFactor: 1,
    recordVideo: { dir: videoDir, size: { width: 1200, height: 700 } },
  });
  const shellPage = await context.newPage();
  const page = await openCaptureFrame(shellPage, `${baseURL}/?pipeline=${encodeURIComponent(pipeline.id)}&asset=${encodeURIComponent(assets.revenue.id)}`);
  await setLandingLocalStorage(page, positions);
  await openAsset(page, pipeline.id, assets.revenue.id);
  await installLandingCursor(page);
  await page.locator(".monaco-editor").waitFor({ timeout: 60_000 });
  await waitForEditorText(page, "FROM staging.orders");
  await page.waitForTimeout(900);
  const readyAt = Date.now();
  await revealTextInEditor(page, "FROM staging.orders");
  await focusIframe(shellPage, editorFocusTargets("FROM staging.orders"));
  await showEditorHover(page);
  await page.waitForTimeout(1400);
  await focusIframe(shellPage, editorFocusTargets("AVG(total_amount) AS avg_order_value"), { maxZoom: 1.35 });
  await waitForEditorText(page, "AVG(total_amount) AS avg_order_value");
  await dismissMonacoOverlays(page);
  await page.getByRole("button", { name: "Inspect Data" }).click();
  await waitForInspectPanel(page);
  await focusIframe(shellPage, [{ selector: '[data-testid="workspace-results-panel"]' }], { maxZoom: 1.35, padding: 64 });
  await page.waitForTimeout(700);
  await configureChartVisualization(page, shellPage);
  await waitForResultsChart(page);
  await focusIframe(shellPage, [{ selector: '[data-testid="workspace-results-panel"] [data-slot="chart"]' }], { maxZoom: 1.6, padding: 48 });
  await page.waitForTimeout(3000);
  const video = shellPage.video();
  await context.close();
  if (!video) {
    throw new Error("Playwright did not produce the jump-to-definition video.");
  }
  const jumpOutputPath = path.join(outputDir, "feature-jump-to-def.gif");
  await convertVideoToGif(await video.path(), jumpOutputPath, {
    trimStartSeconds: trimSeconds(recordingStartedAt, readyAt),
  });
  await assertMediaDuration(jumpOutputPath, 12, "jump-to-definition GIF");
  await rm(videoDir, { recursive: true, force: true }).catch(() => undefined);
}

async function captureDagCanvas(page, outputPath) {
  await fitCanvas(page);
  await page.waitForTimeout(1000);
  await page.screenshot({ path: outputPath, fullPage: true });
}

async function newLandingPage(browserInstance) {
  const page = await browserInstance.newPage({ viewport, deviceScaleFactor: 2 });
  await page.addInitScript(() => {
    window.localStorage.setItem("renart-theme", "dark");
    window.localStorage.setItem("renart-quickstart-tour-dismissed", "true");
  });
  return page;
}

async function writeCaptureFramePage() {
  await writeFile(
    path.join(staticDir, "landing-capture-frame.html"),
    `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Renart capture frame</title>
    <style>
      html, body {
        width: 100%;
        height: 100%;
        margin: 0;
        overflow: hidden;
        background: #050505;
      }
      #renart-capture-frame {
        width: 100vw;
        height: 100vh;
        border: 0;
        transform-origin: 0 0;
        transition: transform 650ms cubic-bezier(.22,1,.36,1);
        will-change: transform;
      }
    </style>
  </head>
  <body>
    <iframe id="renart-capture-frame" src="/"></iframe>
    <script>
      const params = new URLSearchParams(window.location.search);
      const target = params.get("target") || "/";
      const frame = document.getElementById("renart-capture-frame");
      frame.src = target;
      window.renartSetCaptureTransform = ({ scale = 1, x = 0, y = 0 } = {}) => {
        frame.style.transform = "translate(" + x + "px, " + y + "px) scale(" + scale + ")";
      };
    </script>
  </body>
</html>
`,
  );
}

async function openCaptureFrame(shellPage, targetURL) {
  await shellPage.goto(
    `${baseURL}/landing-capture-frame.html?target=${encodeURIComponent(targetURL)}`,
    { waitUntil: "domcontentloaded" },
  );
  const frameElement = await shellPage.locator("#renart-capture-frame").elementHandle();
  const frame = await frameElement?.contentFrame();
  if (!frame) {
    throw new Error("Capture iframe did not load.");
  }
  await frame.locator(".react-flow").waitFor({ timeout: 60_000 });
  return frame;
}

async function resetIframeZoom(shellPage) {
  await shellPage.evaluate(() => window.renartSetCaptureTransform?.({ scale: 1, x: 0, y: 0 }));
  await shellPage.waitForTimeout(700);
}

async function assertIframeZoom(shellPage, expectedScale) {
  await shellPage.waitForFunction(
    (scale) => {
      const frame = document.getElementById("renart-capture-frame");
      const transform = frame?.style.transform ?? "";
      return transform.includes(`scale(${scale})`);
    },
    expectedScale,
    { timeout: 5_000 },
  );
}

async function focusIframe(shellPage, targets, options = {}) {
  const result = await shellPage.evaluate(
    ({ focusTargets, focusOptions }) => {
      const frame = document.getElementById("renart-capture-frame");
      const doc = frame?.contentDocument;
      if (!frame || !doc) {
        return { ok: false, reason: "capture iframe is unavailable" };
      }

      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const rects = [];

      for (const target of focusTargets) {
        let element = null;
        if (target.selector) {
          element = doc.querySelector(target.selector);
        }
        if (!element && target.monacoLineText) {
          element = [...doc.querySelectorAll(".monaco-editor .view-line")].find((line) =>
            line.textContent?.includes(target.monacoLineText)
          );
        }
        if (!element) {
          if (target.monacoLineText) {
            const editor = frame.contentWindow?.monaco?.editor?.getEditors?.()[0];
            const model = editor?.getModel?.();
            const match = model?.findMatches(target.monacoLineText, false, false, false, null, true)?.[0];
            const domNode = editor?.getDomNode?.();
            const editorRect = domNode?.getBoundingClientRect?.();
            if (editor && match && editorRect) {
              editor.revealRangeInCenter(match.range);
              const top = editor.getTopForLineNumber(match.range.startLineNumber) - editor.getScrollTop();
              const left = editor.getOffsetForColumn(match.range.startLineNumber, match.range.startColumn) - editor.getScrollLeft();
              rects.push({
                left: editorRect.left + left,
                top: editorRect.top + top,
                right: Math.min(editorRect.right, editorRect.left + left + Math.max(260, target.monacoLineText.length * 10)),
                bottom: Math.min(editorRect.bottom, editorRect.top + top + 28),
                width: Math.max(260, target.monacoLineText.length * 10),
                height: 28,
              });
              continue;
            }
          }
          if (target.required !== false) {
            return { ok: false, reason: `focus target not found: ${target.selector || target.monacoLineText}` };
          }
          continue;
        }
        const rect = element.getBoundingClientRect();
        if (rect.width <= 0 || rect.height <= 0) {
          continue;
        }
        rects.push(rect);
      }

      if (rects.length === 0) {
        return { ok: false, reason: "no measurable focus targets" };
      }

      const padding = Number(focusOptions.padding ?? 80);
      const maxZoom = Number(focusOptions.maxZoom ?? 1.7);
      const minX = Math.max(0, Math.min(...rects.map((rect) => rect.left)) - padding);
      const minY = Math.max(0, Math.min(...rects.map((rect) => rect.top)) - padding);
      const maxX = Math.min(viewportWidth, Math.max(...rects.map((rect) => rect.right)) + padding);
      const maxY = Math.min(viewportHeight, Math.max(...rects.map((rect) => rect.bottom)) + padding);
      const boxWidth = Math.max(1, maxX - minX);
      const boxHeight = Math.max(1, maxY - minY);
      const scale = Math.max(1, Math.min(maxZoom, viewportWidth / boxWidth, viewportHeight / boxHeight));
      const centerX = (minX + maxX) / 2;
      const centerY = (minY + maxY) / 2;
      const minTranslateX = viewportWidth - viewportWidth * scale;
      const minTranslateY = viewportHeight - viewportHeight * scale;
      const targetX = viewportWidth / 2 - centerX * scale;
      const targetY = viewportHeight / 2 - centerY * scale;
      const translateX = Math.min(0, Math.max(minTranslateX, targetX));
      const translateY = Math.min(0, Math.max(minTranslateY, targetY));

      window.renartSetCaptureTransform?.({ scale, x: translateX, y: translateY });
      return { ok: true, scale, x: translateX, y: translateY };
    },
    { focusTargets: targets, focusOptions: options },
  );

  if (!result.ok) {
    throw new Error(`Unable to focus capture iframe: ${result.reason}`);
  }
  await shellPage.waitForTimeout(700);
}

function editorFocusTargets(lineText) {
  return [
    { selector: '[data-testid="editor-asset-name"]' },
    { selector: '[data-testid="editor-asset-path"]', required: false },
    { monacoLineText: lineText },
  ];
}

async function setLandingLocalStorage(page, positions) {
  const setValues = (storedPositions) => {
    window.localStorage.setItem("renart-theme", "dark");
    window.localStorage.setItem("renart-node-positions-v1", JSON.stringify(storedPositions));
    window.localStorage.setItem("renart-quickstart-tour-dismissed", "true");
  };
  if (typeof page.addInitScript === "function") {
    await page.addInitScript(setValues, positions);
  } else {
    await page.evaluate(setValues, positions);
  }
}

async function openAsset(page, pipelineId, assetId) {
  await page.goto(`${baseURL}/?pipeline=${encodeURIComponent(pipelineId)}&asset=${encodeURIComponent(assetId)}`, {
    waitUntil: "domcontentloaded",
  });
  await page.getByTestId("editor-asset-name").waitFor({ timeout: 60_000 });
  await page.locator(".react-flow").waitFor({ timeout: 60_000 });
}

async function openPipeline(page, pipelineId) {
  await page.goto(`${baseURL}/?pipeline=${encodeURIComponent(pipelineId)}`, {
    waitUntil: "domcontentloaded",
  });
  await page.locator(".react-flow").waitFor({ timeout: 60_000 });
}

async function loadWorkspace() {
  const response = await fetch(`${baseURL}/api/workspace`);
  if (!response.ok) {
    throw new Error(`Failed to load workspace: ${response.status}`);
  }
  return response.json();
}

async function materializePipeline(pipelineId) {
  const response = await fetch(
    `${baseURL}/api/pipelines/${encodeURIComponent(pipelineId)}/materialize/stream?environment=default`,
    { method: "POST" }
  );
  if (!response.ok || !response.body) {
    throw new Error(`Failed to materialize ecommerce pipeline: ${response.status}`);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let lastDone;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const events = buffer.split("\n\n");
    buffer = events.pop() ?? "";
    for (const event of events) {
      const dataLine = event
        .split("\n")
        .find((line) => line.startsWith("data: "));
      if (!dataLine) {
        continue;
      }
      const payload = JSON.parse(dataLine.slice("data: ".length));
      if (event.includes("event: done")) {
        lastDone = payload;
      }
    }
  }

  const materializationSucceeded =
    lastDone &&
    (lastDone.status === "success" ||
      lastDone.exit_code === 0 ||
      String(lastDone.output ?? "").includes("completed") ||
      String(lastDone.output ?? "").includes("successfully"));
  if (!materializationSucceeded) {
    throw new Error(
      `Pipeline materialization failed: ${lastDone?.error || lastDone?.output || "unknown error"}`
    );
  }
}

async function waitForAssetsMaterialized(pipelineId) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    const response = await fetch(
      `${baseURL}/api/pipelines/${encodeURIComponent(pipelineId)}/materialization?environment=default`,
      { cache: "no-store" }
    );
    if (response.ok) {
      const state = await response.json();
      if (state.assets?.length && state.assets.every((asset) => asset.is_materialized)) {
        return;
      }
    }

    const workspace = await loadWorkspace();
    const pipeline = workspace.pipelines.find((candidate) => candidate.id === pipelineId);
    if (pipeline?.assets?.length && pipeline.assets.every((asset) => asset.is_materialized || asset.row_count !== undefined)) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error("Timed out waiting for ecommerce assets to become materialized.");
}

async function assertAssetInspectRows(assetId, assetName) {
  const response = await fetch(
    `${baseURL}/api/assets/${encodeURIComponent(assetId)}/inspect?limit=20&environment=default`,
    { cache: "no-store" },
  );
  if (!response.ok) {
    throw new Error(`Failed to inspect ${assetName}: ${response.status}`);
  }
  const body = await response.json();
  if (!Array.isArray(body.rows) || body.rows.length === 0) {
    throw new Error(`${assetName} inspect returned no rows after materialization.`);
  }
}

function resolveEcommerceAssets(workspace) {
  const pipeline = workspace.pipelines.find((candidate) => candidate.name === "ecommerce");
  if (!pipeline) {
    throw new Error("Ecommerce pipeline was not discovered by Renart.");
  }
  const byName = Object.fromEntries(pipeline.assets.map((asset) => [asset.name, asset]));
  const assets = {
    rawOrders: byName["raw.orders"],
    rawCustomers: byName["raw.customers"],
    staging: byName["staging.orders"],
    revenue: byName["mart.daily_revenue"],
    // topCustomers: byName["mart.top_customers"],
  };
  for (const [key, asset] of Object.entries(assets)) {
    if (!asset) {
      throw new Error(`Missing ecommerce asset ${key}.`);
    }
  }
  return { pipeline, assets };
}

function buildCanonicalPositions(assets) {
  return {
    [assets.rawOrders.id]: { x: 40, y: 120 },
    [assets.rawCustomers.id]: { x: 40, y: 410 },
    [assets.staging.id]: { x: 520, y: 265 },
    [assets.revenue.id]: { x: 1000, y: 120 },
    // [assets.topCustomers.id]: { x: 1000, y: 410 },
  };
}

async function fitCanvas(page) {
  const fitView = page.locator(".react-flow__controls-fitview").first();
  if (await fitView.isVisible().catch(() => false)) {
    await fitView.click();
  }
}

async function blurActiveElement(page) {
  await page.evaluate(() => {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  });
}

async function reloadCanvasLayout(page) {
  await page.getByRole("button", { name: "Reload layout" }).click();
  await page.waitForTimeout(900);
  await fitCanvas(page);
}

async function clickAssetNode(page, assetId) {
  const node = page.locator(`.react-flow__node[data-id="${assetId}"]`).first();
  await node.waitFor({ timeout: 60_000 });
  await node.click();
}

async function revealTextInEditor(page, text) {
  await page.locator(".monaco-editor").waitFor({ timeout: 60_000 });
  await page.evaluate((needle) => {
    const editor = window.monaco?.editor?.getEditors?.()[0];
    if (!editor) {
      return;
    }
    const model = editor.getModel();
    const match = model?.findMatches(needle, false, false, false, null, true)?.[0];
    if (!match) {
      return;
    }
    editor.revealRangeInCenter(match.range);
    editor.setPosition(match.range.getEndPosition());
    editor.focus();
  }, text);
  await page.waitForTimeout(400);
}

async function hoverTextInEditor(page, text, lineIncludes) {
  const box = await findTextBoxInEditor(page, text, lineIncludes);
  if (!box) {
    return;
  }
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 12 });
}

async function showEditorHover(page) {
  await page.evaluate(() => {
    const editor = window.monaco?.editor?.getEditors?.()[0];
    editor?.trigger("landing-media", "editor.action.showHover", {});
  });
}

async function dismissMonacoOverlays(page) {
  const owner = owningPage(page);
  await owner.keyboard.press("Escape").catch(() => undefined);
  await owner.mouse.move(16, 16, { steps: 8 }).catch(() => undefined);
  await page.waitForTimeout(250);
}

async function ctrlClickEditorToken(page, text, lineIncludes) {
  const tokenBox = await findTextBoxInEditor(page, text, lineIncludes);
  if (!tokenBox) {
    throw new Error(`Could not find editor token '${text}' for Ctrl-click navigation.`);
  }

  const modifier = process.platform === "darwin" ? "Meta" : "Control";
  try {
    const owner = owningPage(page);
    await owner.keyboard.down(modifier);
    await owner.mouse.move(tokenBox.x + tokenBox.width / 2, tokenBox.y + tokenBox.height / 2, { steps: 10 });
    await page.waitForTimeout(700);
    await owner.mouse.click(tokenBox.x + tokenBox.width / 2, tokenBox.y + tokenBox.height / 2);
  } finally {
    await owningPage(page).keyboard.up(modifier).catch(() => undefined);
  }
}

async function expectEditorAsset(page, assetName, message) {
  const reached = await page
    .getByTestId("editor-asset-name")
    .getByText(assetName, { exact: true })
    .waitFor({ timeout: 15_000 })
    .then(() => true)
    .catch(() => false);
  if (!reached) {
    const currentName = await page.getByTestId("editor-asset-name").textContent().catch(() => "unknown");
    throw new Error(`${message} Current editor asset: ${currentName}`);
  }
}

async function ensureAssetOpen(page, pipelineId, assetId, assetName, message) {
  const reached = await page
    .getByTestId("editor-asset-name")
    .getByText(assetName, { exact: true })
    .waitFor({ timeout: 3000 })
    .then(() => true)
    .catch(() => false);
  if (!reached) {
    await openAsset(page, pipelineId, assetId);
  }
  await expectEditorAsset(page, assetName, message);
}

async function waitForInspectPanel(page) {
  await page.getByTestId("workspace-results-panel").waitFor({ timeout: 60_000 });
  const ready = await page
    .getByTestId("workspace-results-panel")
    .locator('text=/Inspect|rows|order_date|Preview Failed|No rows/i')
    .first()
    .waitFor({ timeout: 60_000 })
    .then(() => true)
    .catch(() => false);
  if (!ready) {
    throw new Error("Inspect panel opened but did not reach a visible result state.");
  }
}

async function configureChartVisualization(page, shellPage) {
  await page.getByTestId("editor-preview-tab").click();
  await page.getByTestId("visualization-view-type-tabs").waitFor({ timeout: 60_000 });
  await focusIframe(shellPage, [{ selector: '[data-testid="visualization-view-type-tabs"]' }], { maxZoom: 1.6, padding: 96 });
  const chartTab = page.getByRole("tab", { name: /^Chart$/ });
  const alreadyChart = (await chartTab.getAttribute("data-state")) === "active";
  const visualizationSave = shellPage.waitForResponse(
    (response) =>
      response.request().method() === "PUT" &&
      response.url().includes("/api/pipelines/") &&
      response.url().includes("/assets/") &&
      response.ok(),
    { timeout: 15_000 }
  );
  await chartTab.click();
  await page.getByText("X Axis Column", { exact: true }).waitFor({ timeout: 60_000 });
  if (!alreadyChart) {
    await visualizationSave;
  } else {
    await visualizationSave.catch(() => undefined);
  }
  await page.waitForTimeout(1200);
}

async function waitForResultsChart(page) {
  const chartVisible = await page
    .locator('[data-testid="workspace-results-panel"] [data-slot="chart"] .recharts-surface')
    .first()
    .waitFor({ timeout: 20_000 })
    .then(() => true)
    .catch(() => false);
  if (!chartVisible) {
    const debugState = await page.evaluate(() => {
      const panel = document.querySelector('[data-testid="workspace-results-panel"]');
      const chartTab = Array.from(document.querySelectorAll('[role="tab"]')).find((element) => element.textContent?.trim() === "Chart");
      return {
        panelText: panel?.textContent?.replace(/\s+/g, " ").trim().slice(0, 500) ?? null,
        chartSlotCount: panel?.querySelectorAll('[data-slot="chart"]').length ?? 0,
        svgCount: panel?.querySelectorAll("svg").length ?? 0,
        chartTabState: chartTab?.getAttribute("data-state") ?? null,
      };
    });
    throw new Error(`Chart visualization did not render in the inspect panel: ${JSON.stringify(debugState)}`);
  }
}

async function waitForEditorText(page, text) {
  const found = await page
    .waitForFunction(
      (needle) => {
        const editor = window.monaco?.editor?.getEditors?.()[0];
        const value = editor?.getModel()?.getValue() ?? "";
        return value.includes(needle);
      },
      text,
      { timeout: 60_000 }
    )
    .then(() => true)
    .catch(() => false);
  if (!found) {
    throw new Error(`Editor never contained expected text: ${text}`);
  }
}

async function addMaxOrderValueColumn(page) {
  const positioned = await page.evaluate(() => {
    const editor = window.monaco?.editor?.getEditors?.()[0];
    const model = editor?.getModel();
    if (!editor || !model) {
      return false;
    }
    const matches = model.findMatches("AVG(total_amount) AS avg_order_value", false, false, false, null, true);
    const match = matches[0];
    if (!match) {
      return false;
    }
    editor.revealRangeInCenter(match.range);
    editor.setPosition(match.range.getEndPosition());
    editor.focus();
    return true;
  });
  if (!positioned) {
    throw new Error("Could not position cursor to add max_order_value.");
  }
  const keyboard = owningPage(page).keyboard;
  await keyboard.type(",", { delay: 75 });
  await keyboard.press("Enter");
  await keyboard.type("MAX(tot", { delay: 80 });
  await keyboard.press(process.platform === "darwin" ? "Meta+Space" : "Control+Space");
  await page.waitForTimeout(1400);
  await keyboard.press("Tab");
  await keyboard.type(") AS max_order_value", { delay: 80 });
}

function owningPage(pageOrFrame) {
  return typeof pageOrFrame.page === "function" ? pageOrFrame.page() : pageOrFrame;
}

function trimSeconds(startedAt, readyAt) {
  return Math.max(0, (readyAt - startedAt) / 1000);
}

async function installLandingCursor(page) {
  await page.evaluate(() => {
    if (document.getElementById("renart-landing-cursor-style")) {
      return;
    }
    const style = document.createElement("style");
    style.id = "renart-landing-cursor-style";
    style.textContent = `
      html.renart-landing-recording *,
      html.renart-landing-recording *::before,
      html.renart-landing-recording *::after {
        cursor: none !important;
      }
      .renart-landing-cursor {
        position: fixed;
        z-index: 2147483647;
        left: 0;
        top: 0;
        width: 28px;
        height: 28px;
        border: 2px solid rgba(34, 211, 238, 0.98);
        border-radius: 999px;
        background: radial-gradient(circle, rgba(34, 211, 238, 0.9) 0 3px, transparent 4px);
        box-shadow: 0 0 0 8px rgba(34, 211, 238, 0.14), 0 0 26px rgba(34, 211, 238, 0.55);
        pointer-events: none;
        transform: translate(-50%, -50%);
        transition: width 120ms ease, height 120ms ease, box-shadow 120ms ease, border-color 120ms ease;
      }
      .renart-landing-cursor.is-clicking {
        width: 38px;
        height: 38px;
        border-color: rgba(16, 185, 129, 1);
        box-shadow: 0 0 0 14px rgba(16, 185, 129, 0.22), 0 0 36px rgba(34, 211, 238, 0.8);
      }
      .renart-landing-click-ripple {
        position: fixed;
        z-index: 2147483646;
        width: 18px;
        height: 18px;
        border: 3px solid rgba(16, 185, 129, 0.95);
        border-radius: 999px;
        pointer-events: none;
        transform: translate(-50%, -50%) scale(0.6);
        animation: renart-landing-ripple 650ms ease-out forwards;
      }
      @keyframes renart-landing-ripple {
        to {
          opacity: 0;
          transform: translate(-50%, -50%) scale(3.8);
        }
      }
    `;
    document.head.append(style);
    document.documentElement.classList.add("renart-landing-recording");

    const cursor = document.createElement("div");
    cursor.className = "renart-landing-cursor";
    document.body.append(cursor);

    window.addEventListener("mousemove", (event) => {
      cursor.style.left = `${event.clientX}px`;
      cursor.style.top = `${event.clientY}px`;
    });
    window.addEventListener("mousedown", (event) => {
      cursor.classList.add("is-clicking");
      const ripple = document.createElement("div");
      ripple.className = "renart-landing-click-ripple";
      ripple.style.left = `${event.clientX}px`;
      ripple.style.top = `${event.clientY}px`;
      document.body.append(ripple);
      ripple.addEventListener("animationend", () => ripple.remove(), { once: true });
    });
    window.addEventListener("mouseup", () => {
      window.setTimeout(() => cursor.classList.remove("is-clicking"), 120);
    });
  });
}

async function findTextBoxInEditor(page, text, lineIncludes) {
  return page.evaluate(({ needle, requiredLineText }) => {
    const visibleLine = [...document.querySelectorAll(".monaco-editor .view-line")].find((line) =>
      line.textContent?.includes(needle) && (!requiredLineText || line.textContent?.includes(requiredLineText))
    );
    if (visibleLine) {
      const walker = document.createTreeWalker(visibleLine, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        const value = node.textContent ?? "";
        const index = value.indexOf(needle);
        if (index === -1) {
          continue;
        }
        const range = document.createRange();
        range.setStart(node, index);
        range.setEnd(node, index + needle.length);
        const rect = range.getBoundingClientRect();
        if (rect.width > 0 && rect.height > 0) {
          return {
            x: rect.left,
            y: rect.top,
            width: rect.width,
            height: rect.height,
          };
        }
      }
    }

    const editor = window.monaco?.editor?.getEditors?.()[0];
    if (!editor) {
      return null;
    }
    const model = editor.getModel();
    const matches = model?.findMatches(needle, false, false, false, null, true) ?? [];
    const match = requiredLineText
      ? matches.find((candidate) => model.getLineContent(candidate.range.startLineNumber).includes(requiredLineText))
      : matches[0];
    if (!match) {
      return null;
    }
    const position = match.range.getStartPosition();
    editor.revealPositionInCenter(position);
    editor.setPosition(position);
    editor.focus();
    const topForLine = editor.getTopForLineNumber(position.lineNumber) - editor.getScrollTop();
    const leftForColumn = editor.getOffsetForColumn(position.lineNumber, position.column) - editor.getScrollLeft();
    const domNode = editor.getDomNode();
    const rect = domNode?.getBoundingClientRect();
    if (!rect) {
      return null;
    }
    return {
      x: rect.left + leftForColumn,
      y: rect.top + topForLine + 8,
      width: Math.max(60, needle.length * 7),
      height: 18,
    };
  }, { needle: text, requiredLineText: lineIncludes });
}

async function captureEditorPanel(page, outputPath, options = {}) {
  const editorPane = page.locator(".monaco-editor").first();
  await editorPane.waitFor({ timeout: 60_000 });
  if (options.includeHover || options.includeSuggest) {
    const clip = await page.evaluate(() => {
      const editor = document.querySelector(".monaco-editor")?.getBoundingClientRect();
      const hover = document.querySelector(".monaco-hover")?.getBoundingClientRect();
      const suggest = document.querySelector(".suggest-widget.visible")?.getBoundingClientRect();
      if (!editor) {
        return null;
      }

      const textRects = Array.from(document.querySelectorAll(".monaco-editor .view-line"))
        .filter((line) => (line.textContent ?? "").trim().length > 0)
        .flatMap((line) => Array.from(line.getClientRects()))
        .filter((rect) => rect.width > 0 && rect.height > 0);

      const textBounds = textRects.length > 0
        ? textRects.reduce(
            (bounds, rect) => ({
              left: Math.min(bounds.left, rect.left),
              top: Math.min(bounds.top, rect.top),
              right: Math.max(bounds.right, rect.right),
              bottom: Math.max(bounds.bottom, rect.bottom),
            }),
            {
              left: Number.POSITIVE_INFINITY,
              top: Number.POSITIVE_INFINITY,
              right: Number.NEGATIVE_INFINITY,
              bottom: Number.NEGATIVE_INFINITY,
            },
          )
        : editor;

      const overlayRects = [hover, suggest].filter(Boolean);
      const left = Math.max(0, Math.min(textBounds.left, ...overlayRects.map((rect) => rect.left)) - 24);
      const top = Math.max(0, Math.min(textBounds.top, ...overlayRects.map((rect) => rect.top)) - 24);
      const right = Math.min(window.innerWidth, Math.max(textBounds.right, ...overlayRects.map((rect) => rect.right)) + 24);
      const bottom = Math.min(window.innerHeight, Math.max(textBounds.bottom, ...overlayRects.map((rect) => rect.bottom)) + 24);
      return {
        x: left,
        y: top,
        width: right - left,
        height: bottom - top,
      };
    });
    if (!clip) {
      throw new Error("Could not determine validation screenshot bounds.");
    }
    await page.screenshot({ path: outputPath, clip });
    return;
  }
  await editorPane.screenshot({ path: outputPath });
}

async function waitForSuggestWidget(page, { minimumSuggestions = 1 } = {}) {
  const visible = await page
    .waitForFunction((minimum) => {
      const widget = document.querySelector(".suggest-widget.visible");
      if (!widget) {
        return false;
      }
      const suggestions = widget.querySelectorAll(".monaco-list-row");
      return suggestions.length >= minimum;
    }, minimumSuggestions, { timeout: 10_000 })
    .then(() => true)
    .catch(() => false);

  if (!visible) {
    throw new Error(`Monaco suggest widget did not show at least ${minimumSuggestions} suggestions.`);
  }
}

async function waitForMonacoHoverOnScreen(page) {
  const visible = await page
    .waitForFunction(() => {
      const hover = document.querySelector(".monaco-hover")?.getBoundingClientRect();
      if (!hover || hover.width === 0 || hover.height === 0) {
        return false;
      }

      return (
        hover.left >= 0 &&
        hover.top >= 0 &&
        hover.right <= window.innerWidth &&
        hover.bottom <= window.innerHeight
      );
    }, { timeout: 10_000 })
    .then(() => true)
    .catch(() => false);

  if (!visible) {
    throw new Error("Monaco validation hover is not fully visible in the viewport.");
  }
}

async function startScreenshotRecorder(page, frameDir, { fps }) {
  let stopped = false;
  let frameCount = 0;
  let captureError = null;
  const frameIntervalMs = 1000 / fps;
  const startedAt = Date.now();

  const recording = (async () => {
    let nextFrameAt = Date.now();
    while (!stopped) {
      const framePath = path.join(frameDir, `frame-${String(frameCount + 1).padStart(5, "0")}.png`);
      try {
        await page.screenshot({ path: framePath });
        frameCount += 1;
      } catch (error) {
        if (!stopped) {
          captureError = error;
        }
        break;
      }

      nextFrameAt += frameIntervalMs;
      const waitMs = Math.max(0, nextFrameAt - Date.now());
      if (waitMs > 0) {
        await new Promise((resolve) => setTimeout(resolve, waitMs));
      }
    }
  })();

  return {
    fps,
    stop: async () => {
      stopped = true;
      await recording;
      if (captureError) {
        throw captureError;
      }
      if (frameCount === 0) {
        throw new Error("Hero screenshot recorder did not capture any frames.");
      }
      const durationSeconds = Math.max(1, (Date.now() - startedAt) / 1000);
      return {
        frameCount,
        playbackFps: frameCount / durationSeconds,
      };
    },
  };
}

async function encodeFramesToWebm(frameDir, outputPath, { fps }) {
  await run("ffmpeg", [
    "-y",
    "-framerate",
    String(fps),
    "-i",
    path.join(frameDir, "frame-%05d.png"),
    "-an",
    "-c:v",
    "libvpx-vp9",
    "-pix_fmt",
    "yuv420p",
    "-crf",
    "8",
    "-b:v",
    "0",
    "-row-mt",
    "1",
    outputPath,
  ]);
}

async function convertVideo(inputPath, outputPath, options = {}) {
  const args = ["-y"];
  args.push(
    "-i",
    inputPath,
    "-an",
  );
  if (options.trimStartSeconds) {
    args.push("-ss", String(options.trimStartSeconds));
  }
  if (options.videoFilter) {
    args.push("-vf", options.videoFilter);
  }
  args.push(
    "-c:v",
    "copy",
    "-avoid_negative_ts",
    "make_zero",
    outputPath,
  );
  await run("ffmpeg", [
    ...args,
  ]);
}

async function convertVideoToGif(inputPath, outputPath, options = {}) {
  const args = ["-y"];
  if (options.trimStartSeconds) {
    args.push("-ss", String(options.trimStartSeconds));
  }
  args.push(
    "-i",
    inputPath,
    "-vf",
    `${options.videoFilter ? `${options.videoFilter},` : ""}fps=16,scale=1200:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse`,
    "-loop",
    "0",
    outputPath,
  );
  await run("ffmpeg", args);
}

async function assertMediaDuration(filePath, minimumSeconds, label) {
  const rawDuration = await runCapture("ffprobe", [
    "-v",
    "error",
    "-show_entries",
    "format=duration",
    "-of",
    "default=noprint_wrappers=1:nokey=1",
    filePath,
  ]);
  const duration = Number(rawDuration.trim());
  if (!Number.isFinite(duration)) {
    throw new Error(`Could not read duration for ${label}: ${filePath}`);
  }
  if (duration < minimumSeconds) {
    throw new Error(
      `${label} ended too early: ${duration.toFixed(1)}s, expected at least ${minimumSeconds}s.`
    );
  }
}

async function createEcommerceWorkspace(root) {
  await mkdir(path.join(root, "ecommerce", "assets", "raw"), { recursive: true });
  await mkdir(path.join(root, "ecommerce", "assets", "staging"), { recursive: true });
  await mkdir(path.join(root, "ecommerce", "assets", "mart"), { recursive: true });
  await mkdir(path.join(root, "ecommerce", "seeds"), { recursive: true });

  await writeFile(
    path.join(root, ".bruin.yml"),
    `default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: "duckdb-default"
          path: "ecommerce.db"
`
  );
  await writeFile(
    path.join(root, "ecommerce", "pipeline.yml"),
    `name: ecommerce
schedule: "daily"
start_date: "2025-01-01"
default_environment: default
default:
  interval_modifiers:
    start: -5d
    end: 1d
`
  );
  await writeFile(path.join(root, "ecommerce", "assets", "raw", "orders.sql"), rawOrdersAssetSQL());
  await writeFile(path.join(root, "ecommerce", "assets", "raw", "customers.sql"), rawCustomersAssetSQL());
  await writeFile(path.join(root, "ecommerce", "assets", "staging", "orders.sql"), ecommerceStagingOrdersSQL());
  await writeFile(path.join(root, "ecommerce", "assets", "mart", "daily_revenue.sql"), ecommerceMartRevenueSQL());
  // await writeFile(path.join(root, "ecommerce", "assets", "mart", "top_customers.sql"), ecommerceTopCustomersSQL());
  await writeFile(path.join(root, "ecommerce", "seeds", "orders.csv"), ordersCSV());
  await writeFile(path.join(root, "ecommerce", "seeds", "customers.csv"), customersCSV());
}

function rawOrdersAssetSQL() {
  return `/* @bruin
name: raw.orders
type: duckdb.sql
materialization:
  type: table
columns:
  - name: order_id
    type: INTEGER
    description: "Unique order identifier"
    primary_key: true
    checks:
      - name: not_null
      - name: unique
  - name: customer_id
    type: INTEGER
    description: "FK to customers table"
    checks:
      - name: not_null
  - name: order_date
    type: DATE
    description: "Date the order was placed"
  - name: status
    type: VARCHAR
    description: "Order status: pending, paid, shipped, refunded"
  - name: total_amount
    type: DOUBLE
    description: "Order total in USD"
    checks:
      - name: not_null
      - name: positive
@bruin */

SELECT *
FROM (VALUES
  (1001, 1, DATE '2026-05-01', 'paid', 125.50),
  (1002, 2, DATE '2026-05-01', 'shipped', 89.99),
  (1003, 1, DATE '2026-05-02', 'refunded', 42.00),
  (1004, 3, DATE '2026-05-02', 'paid', 310.25),
  (1005, 2, DATE '2026-05-03', 'paid', 176.40),
  (1006, 3, DATE '2026-05-03', 'shipped', 245.10),
  (1007, 1, DATE '2026-05-04', 'paid', 98.25),
  (1008, 2, DATE '2026-05-04', 'paid', 410.75),
  (1009, 3, DATE '2026-05-05', 'shipped', 157.80),
  (1010, 1, DATE '2026-05-05', 'paid', 220.00),
  (1011, 2, DATE '2026-05-06', 'paid', 345.60),
  (1012, 3, DATE '2026-05-06', 'shipped', 132.90)
) AS orders(order_id, customer_id, order_date, status, total_amount)
`;
}

function rawCustomersAssetSQL() {
  return `/* @bruin
name: raw.customers
type: duckdb.sql
materialization:
  type: table
columns:
  - name: customer_id
    type: INTEGER
    description: "Unique customer identifier"
    primary_key: true
    checks:
      - name: not_null
      - name: unique
  - name: customer_name
    type: VARCHAR
    description: "Full name of the customer"
  - name: email
    type: VARCHAR
    description: "Customer email address"
    checks:
      - name: not_null
      - name: unique
  - name: country
    type: VARCHAR
    description: "ISO country code"
  - name: created_at
    type: TIMESTAMP
    description: "Account creation timestamp"
@bruin */

SELECT *
FROM (VALUES
  (1, 'Ada Lovelace', 'ada@example.com', 'GB', TIMESTAMP '2024-10-01 09:15:00'),
  (2, 'Grace Hopper', 'grace@example.com', 'US', TIMESTAMP '2024-10-03 11:30:00'),
  (3, 'Katherine Johnson', 'katherine@example.com', 'US', TIMESTAMP '2024-10-05 16:45:00')
) AS customers(customer_id, customer_name, email, country, created_at)
`;
}

function ecommerceStagingOrdersSQL() {
  return `/* @bruin
name: staging.orders
type: duckdb.sql
materialization:
  type: table
depends:
  - raw.orders
  - raw.customers
columns:
  - name: order_id
    type: INTEGER
    description: "Unique order identifier"
    primary_key: true
    checks:
      - name: not_null
      - name: unique
  - name: customer_id
    type: INTEGER
    description: "FK to customers"
    checks:
      - name: not_null
  - name: customer_name
    type: VARCHAR
    description: "Denormalized customer name"
  - name: order_date
    type: DATE
    description: "Date the order was placed"
  - name: status
    type: VARCHAR
    description: "Order status"
    checks:
      - name: accepted_values
        value: ["pending", "paid", "shipped", "refunded"]
  - name: total_amount
    type: DOUBLE
    description: "Order total in USD"
    checks:
      - name: positive
@bruin */

SELECT
    -- downstream asset: mart.daily_revenue
    o.order_id,
    o.customer_id,
    c.customer_name,
    o.order_date,
    o.status,
    o.total_amount
FROM raw.orders o
JOIN raw.customers c USING (customer_id)
WHERE o.status != 'refunded'
`;
}

function ecommerceMartRevenueSQL() {
  return `/* @bruin
name: mart.daily_revenue
type: duckdb.sql
meta:
  web_chart_type: line
  web_chart_x: order_date
  web_chart_series: revenue,order_count
  web_chart_title: Daily revenue
materialization:
  type: table
depends:
  - staging.orders
columns:
  - name: order_date
    type: DATE
    description: "Calendar date"
    primary_key: true
    checks:
      - name: not_null
      - name: unique
  - name: order_count
    type: INTEGER
    description: "Number of orders"
    checks:
      - name: positive
  - name: revenue
    type: DOUBLE
    description: "Total revenue in USD"
    checks:
      - name: not_null
  - name: avg_order_value
    type: DOUBLE
    description: "Average order value"
@bruin */

SELECT
    order_date,
    COUNT(*) AS order_count,
    SUM(total_amount) AS revenue,
    AVG(total_amount) AS avg_order_value
FROM staging.orders
-- WHERE order_date >= DATE '{{ start_date }}'
-- AND order_date <= DATE '{{ end_date }}'
GROUP BY order_date
`;
}

function ecommerceTopCustomersSQL() {
  return `/* @bruin
name: mart.top_customers
type: duckdb.sql
materialization:
  type: table
depends:
  - staging.orders
columns:
  - name: customer_id
    type: INTEGER
    primary_key: true
  - name: customer_name
    type: VARCHAR
  - name: total_spent
    type: DOUBLE
    checks:
      - name: positive
  - name: order_count
    type: INTEGER
    checks:
      - name: positive
@bruin */

SELECT
    customer_id,
    customer_name,
    SUM(total_amount) AS total_spent,
    COUNT(*) AS order_count
FROM staging.orders
GROUP BY customer_id, customer_name
ORDER BY total_spent DESC
`;
}

function ordersCSV() {
  return `order_id,customer_id,order_date,status,total_amount
1001,1,2026-05-01,paid,125.50
1002,2,2026-05-01,shipped,89.99
1003,1,2026-05-02,refunded,42.00
1004,3,2026-05-02,paid,310.25
1005,2,2026-05-03,paid,176.40
1006,3,2026-05-03,shipped,245.10
1007,1,2026-05-04,paid,98.25
1008,2,2026-05-04,paid,410.75
1009,3,2026-05-05,shipped,157.80
1010,1,2026-05-05,paid,220.00
1011,2,2026-05-06,paid,345.60
1012,3,2026-05-06,shipped,132.90
`;
}

function customersCSV() {
  return `customer_id,customer_name,email,country,created_at
1,Ada Lovelace,ada@example.com,GB,2024-10-01T09:15:00Z
2,Grace Hopper,grace@example.com,US,2024-10-03T11:30:00Z
3,Katherine Johnson,katherine@example.com,US,2024-10-05T16:45:00Z
`;
}

function killServerGroup(child, signal) {
  if (!child.pid) {
    return;
  }
  try {
    process.kill(-child.pid, signal);
  } catch {
    try {
      child.kill(signal);
    } catch {
      // Server already exited.
    }
  }
}

async function waitForServer(url, getOutput) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
    } catch {
      // Server is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Renart server did not start in time.\n${getOutput()}`);
}

async function run(command, args, cwd = process.cwd()) {
  await new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, stdio: "inherit" });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`${command} ${args.join(" ")} exited with ${code}`));
      }
    });
  });
}

async function runCapture(command, args, cwd = process.cwd()) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += chunk.toString();
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString();
    });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve(stdout);
      } else {
        reject(new Error(`${command} ${args.join(" ")} exited with ${code}\n${stderr}`));
      }
    });
  });
}
