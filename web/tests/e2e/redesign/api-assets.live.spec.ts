import { expect } from "@playwright/test";
import { createServer, type Server } from "node:http";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test } from "../live-app-fixture";

type WorkspaceResponse = {
  pipelines: Array<{
    id: string;
    assets: Array<{
      id: string;
      name: string;
      content: string;
      columns?: Array<{ name: string; type?: string }>;
    }>;
  }>;
};

const pipelineId = Buffer.from("analytics").toString("base64url");
const customersAssetId = Buffer.from("analytics/assets/analytics/customers.sql").toString("base64url");
const apiAssetPath = "analytics/assets/analytics/players_api.asset.yml";
const apiAssetId = Buffer.from(apiAssetPath).toString("base64url");

test.describe("redesign API assets live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("API YAML editor suggests API keys", async ({ liveApp, page }) => {
    await writeFile(
      join(liveApp.workspaceDir, apiAssetPath),
      `name: analytics.players_api
type: api

parameters:
`,
      "utf8"
    );
    await waitForWorkspaceAsset(page, liveApp.baseURL, apiAssetId);

    await page.goto(`${liveApp.baseURL}/redesign/pipelines/${pipelineId}/assets/${apiAssetId}/code`);
    const editor = page.locator(".monaco-editor").first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await replaceEditorContent(page, "type: api\n\nparameters:\n  ");
    await page.keyboard.press("ControlOrMeta+Space");

    const suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible({ timeout: 15000 });
    await expect(suggestWidget.getByText("openapi", { exact: true })).toBeVisible();
    await expect(suggestWidget.getByText("request", { exact: true })).toBeVisible();
    await expect(suggestWidget.getByText("response", { exact: true })).toBeVisible();
  });

  test("OpenAPI columns feed workspace and SQL parse-context", async ({ liveApp, page }) => {
    const specServer = await startOpenAPIServer();
    try {
      await writeFile(
        join(liveApp.workspaceDir, apiAssetPath),
        `name: analytics.players_api
type: api

parameters:
  openapi:
    url: ${specServer.url}/openapi.yaml
  request:
    url: https://api.example.com/players/{{ username }}
    method: GET
  response:
    records_path: data
`,
        "utf8"
      );

      await expect
        .poll(async () => {
          const asset = await waitForWorkspaceAsset(page, liveApp.baseURL, apiAssetId);
          return (asset.columns ?? []).map((column) => `${column.name}:${column.type ?? ""}`).sort();
        }, { timeout: 30000 })
        .toEqual(["active:boolean", "rating:integer", "username:string"]);

      const response = await page.request.post(`${liveApp.baseURL}/api/sql/parse-context`, {
        data: {
          asset_id: customersAssetId,
          content: "select username, rating, active from analytics.players_api",
          schema: [],
        },
      });
      expect(response.ok()).toBe(true);
      const body = await response.json() as { diagnostics?: Array<{ message?: string }>; errors?: string[] };
      expect(body.errors ?? []).toEqual([]);
      const messages = (body.diagnostics ?? []).map((diagnostic) => diagnostic.message);
      expect(messages).not.toContain("Unresolved table: analytics.players_api");
      expect(messages).not.toContain("Unresolved column: username");
      expect(messages).not.toContain("Unresolved column: rating");
      expect(messages).not.toContain("Unresolved column: active");
    } finally {
      await new Promise<void>((resolve) => specServer.server.close(() => resolve()));
    }
  });
});

async function waitForWorkspaceAsset(page: import("@playwright/test").Page, baseURL: string, assetId: string) {
  let found: WorkspaceResponse["pipelines"][number]["assets"][number] | undefined;
  await expect
    .poll(async () => {
      const response = await page.request.get(`${baseURL}/api/workspace`);
      if (!response.ok()) return false;
      const workspace = await response.json() as WorkspaceResponse;
      found = workspace.pipelines.flatMap((pipeline) => pipeline.assets).find((asset) => asset.id === assetId);
      return Boolean(found);
    }, { timeout: 30000 })
    .toBe(true);
  return found!;
}

async function replaceEditorContent(page: import("@playwright/test").Page, content: string) {
  const editor = page.locator(".monaco-editor").first();
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(content);
}

async function startOpenAPIServer(): Promise<{ server: Server; url: string }> {
  const server = createServer((req, res) => {
    if (req.url !== "/openapi.yaml") {
      res.writeHead(404).end();
      return;
    }
    res.setHeader("content-type", "application/yaml");
    res.end(`openapi: 3.0.3
paths:
  /players/{username}:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: object
                    properties:
                      username:
                        type: string
                      rating:
                        type: integer
                      active:
                        type: boolean
`);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("OpenAPI test server did not start on a TCP port");
  }
  return { server, url: `http://127.0.0.1:${address.port}` };
}
