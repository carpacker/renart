import { expect } from "@playwright/test";

import { liveTest as test } from "./live-app-fixture";

test.describe("graph layout live", () => {
  test.use({ fixtureName: "graph-layout-workspace" });

  test("reload layout packs disconnected graph components into multiple rows", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz`);

    const reloadButton = test.info().project.name.includes("mobile")
      ? page.locator("main button").nth(2)
      : page.getByRole("button", { name: "Reload layout" });

    await expect(reloadButton).toBeVisible();
    await reloadButton.click();

    const positions = await page.locator(".react-flow__node").evaluateAll((nodes) =>
      nodes.map((node) => {
        const style = window.getComputedStyle(node as HTMLElement);
        const transform = style.transform;
        const match = transform.match(/matrix\([^,]+,[^,]+,[^,]+,[^,]+,\s*([^,]+),\s*([^\)]+)\)/);
        const rect = (node as HTMLElement).getBoundingClientRect();
        return {
          text: (node.textContent ?? "").trim(),
          x: match ? Number.parseFloat(match[1]) : rect.left,
          y: match ? Number.parseFloat(match[2]) : rect.top,
        };
      })
    );

    const orderedRows = [...new Set(positions.map((item) => Math.round(item.y / 50) * 50))];
    expect(orderedRows.length).toBeGreaterThan(1);
  });

  test("wide preview nodes stay within 80 percent of viewport width", async ({
    liveApp,
    page,
  }) => {
    await page.setViewportSize({ width: 900, height: 900 });
    await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz`);
    await page.getByRole("button", { name: "Reload layout" }).click();

    const wideNode = page
      .locator(".react-flow__node")
      .filter({ hasText: "wide_preview" })
      .first();

    await expect(wideNode).toBeVisible();

    const box = await wideNode.boundingBox();
    if (!box) {
      throw new Error("Wide preview node bounding box not available.");
    }

    expect(box.width).toBeLessThanOrEqual(720);
  });
});
