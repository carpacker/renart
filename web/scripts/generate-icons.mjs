import { mkdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { chromium } from "@playwright/test";

const webRoot = resolve(import.meta.dirname, "..");
const iconDir = resolve(webRoot, "public", "icons");
const sourceSvg = resolve(iconDir, "icon.svg");
const svgMarkup = readFileSync(sourceSvg, "utf8");
const svgDataUrl = `data:image/svg+xml;base64,${Buffer.from(svgMarkup).toString("base64")}`;

mkdirSync(iconDir, { recursive: true });

const rasterTargets = [
  [512, "icon-512.png"],
  [256, "icon-256.png"],
  [192, "icon-192.png"],
  [180, "apple-touch-icon.png"],
  [128, "icon-128.png"],
  [64, "icon-64.png"],
  [48, "icon-48.png"],
  [32, "icon-32.png"],
  [16, "icon-16.png"],
];

const browser = await chromium.launch({ headless: true });

try {
  for (const [size, fileName] of rasterTargets) {
    const output = resolve(iconDir, fileName);
    mkdirSync(dirname(output), { recursive: true });

    const page = await browser.newPage({
      viewport: { width: size, height: size },
      deviceScaleFactor: 1,
    });

    try {
      await page.setContent(
        `<!doctype html>
         <html>
           <body style="margin:0;background:transparent;overflow:hidden;display:grid;place-items:center;width:100vw;height:100vh;">
             <img id="icon" src="${svgDataUrl}" width="${size}" height="${size}" style="display:block;width:${size}px;height:${size}px;object-fit:contain;" />
           </body>
         </html>`
      );
      await page.waitForFunction(() => {
        const img = document.getElementById("icon");
        return img instanceof HTMLImageElement && img.complete && img.naturalWidth > 0;
      });
      await page.screenshot({ path: output, omitBackground: true });
    } finally {
      await page.close();
    }
  }
} finally {
  await browser.close();
}

execFileSync(
  "convert",
  [resolve(iconDir, "icon-32.png"), resolve(iconDir, "icon-16.png"), resolve(iconDir, "favicon.ico")],
  {
    cwd: webRoot,
    stdio: "inherit",
  }
);
