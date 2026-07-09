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
      type: string;
      content: string;
      parse_error?: string;
      columns?: Array<{ name: string; type?: string }>;
    }>;
  }>;
};

const pipelineId = Buffer.from("analytics").toString("base64url");
const customersAssetId = Buffer.from("analytics/assets/analytics/customers.sql").toString("base64url");
const apiAssetPath = "analytics/assets/analytics/players_api.asset.yml";
const apiAssetId = Buffer.from(apiAssetPath).toString("base64url");
const paginatedAPIAssetPath = "analytics/assets/analytics/paginated_api.asset.yml";
const paginatedAPIAssetId = Buffer.from(paginatedAPIAssetPath).toString("base64url");
const postAPIAssetPath = "analytics/assets/analytics/post_api.asset.yml";
const postAPIAssetId = Buffer.from(postAPIAssetPath).toString("base64url");

test.describe("app API assets live", () => {
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

    await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${apiAssetId}/code`);
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

  test("OpenAPI records_path suggestions render in the API YAML editor", async ({ liveApp, page }) => {
    const specServer = await startRecordsPathOpenAPIServer();
    try {
      const assetContent = `name: analytics.players_api
type: api

parameters:
  openapi:
    url: ${specServer.url}/openapi.yaml
  request:
    url: https://api.example.com/players/Ada
    method: GET
  response:
    records_path: ""
`;
      await writeFile(
        join(liveApp.workspaceDir, apiAssetPath),
        assetContent,
        "utf8"
      );
      await waitForWorkspaceAssetContent(page, liveApp.baseURL, apiAssetId, specServer.url);

      await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${apiAssetId}/code`);
      const editor = page.locator(".monaco-editor").first();
      await expect(editor).toBeVisible({ timeout: 15000 });
      await setEditorContentAtYamlField(page, assetContent, "records_path");

      const suggestionsResponse = page.waitForResponse(
        (response) =>
          response.url().includes("/api/api-assets/openapi-suggestions") &&
          response.url().includes("request_url=") &&
          response.ok(),
        { timeout: 15000 }
      );
      await page.keyboard.press("ControlOrMeta+Space");
      const response = await suggestionsResponse;
      const body = await response.json() as { records_paths?: Array<{ path: string; detail?: string }> };
      expect(body.records_paths).toContainEqual(expect.objectContaining({
        detail: expect.stringContaining("array of objects"),
        path: "data",
      }));

      const suggestWidget = page.locator(".suggest-widget.visible").first();
      await expect(suggestWidget).toBeVisible({ timeout: 15000 });
      await expect(suggestWidget.getByText("data", { exact: true })).toBeVisible();
    } finally {
      await new Promise<void>((resolve) => specServer.server.close(() => resolve()));
    }
  });

  test("OpenAPI records_path suggestions complete inside an existing quoted value", async ({ liveApp, page }) => {
    const specServer = await startRecordsPathOpenAPIServer();
    try {
      const assetContent = `name: analytics.players_api
type: api

parameters:
  openapi:
    url: ${specServer.url}/openapi.yaml
  request:
    url: https://api.example.com/players/Ada
    method: GET
  response:
    records_path: "fea"
`;
      await writeFile(
        join(liveApp.workspaceDir, apiAssetPath),
        assetContent,
        "utf8"
      );
      await waitForWorkspaceAssetContent(page, liveApp.baseURL, apiAssetId, specServer.url);

      await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${apiAssetId}/code`);
      const editor = page.locator(".monaco-editor").first();
      await expect(editor).toBeVisible({ timeout: 15000 });
      await setEditorContentAtYamlField(page, assetContent, "records_path", "fea");

      const suggestionsResponse = page.waitForResponse(
        (response) =>
          response.url().includes("/api/api-assets/openapi-suggestions") &&
          response.url().includes("request_url=") &&
          response.ok(),
        { timeout: 15000 }
      );
      await page.keyboard.press("ControlOrMeta+Space");
      const response = await suggestionsResponse;
      const body = await response.json() as { records_paths?: Array<{ path: string; detail?: string }> };
      expect(body.records_paths).toContainEqual(expect.objectContaining({
        detail: expect.stringContaining("array of objects"),
        path: "features",
      }));

      const suggestWidget = page.locator(".suggest-widget.visible").first();
      await expect(suggestWidget).toBeVisible({ timeout: 15000 });
      await expect(suggestWidget.getByText("features", { exact: true })).toBeVisible();
    } finally {
      await new Promise<void>((resolve) => specServer.server.close(() => resolve()));
    }
  });

  test("OpenAPI next_url_path suggestions render in the API YAML editor", async ({ liveApp, page }) => {
    const specServer = await startRecordsPathOpenAPIServer();
    try {
      const assetContent = `name: analytics.players_api
type: api

parameters:
  openapi:
    url: ${specServer.url}/openapi.yaml
  request:
    url: https://api.example.com/players/Ada
    method: GET
  response:
    records_path: data
  pagination:
    type: next_url
    next_url_path: "pag"
`;
      await writeFile(
        join(liveApp.workspaceDir, apiAssetPath),
        assetContent,
        "utf8"
      );
      await waitForWorkspaceAssetContent(page, liveApp.baseURL, apiAssetId, specServer.url);

      await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${apiAssetId}/code`);
      const editor = page.locator(".monaco-editor").first();
      await expect(editor).toBeVisible({ timeout: 15000 });
      await setEditorContentAtYamlField(page, assetContent, "next_url_path", "pag");

      const suggestionsResponse = page.waitForResponse(
        (response) =>
          response.url().includes("/api/api-assets/openapi-suggestions") &&
          response.url().includes("request_url=") &&
          response.ok(),
        { timeout: 15000 }
      );
      await page.keyboard.press("ControlOrMeta+Space");
      const response = await suggestionsResponse;
      const body = await response.json() as { response_paths?: Array<{ path: string; detail?: string }> };
      expect(body.response_paths).toContainEqual(expect.objectContaining({
        detail: expect.stringContaining("string"),
        path: "pagination.next",
      }));

      const suggestWidget = page.locator(".suggest-widget.visible").first();
      await expect(suggestWidget).toBeVisible({ timeout: 15000 });
      await expect(suggestWidget.getByText("pagination.next", { exact: true })).toBeVisible();
    } finally {
      await new Promise<void>((resolve) => specServer.server.close(() => resolve()));
    }
  });

  test("API YAML editor stays mounted while parameters YAML is incomplete", async ({ liveApp, page }) => {
    const specServer = await startRecordsPathOpenAPIServer();
    try {
      const assetContent = `name: analytics.players_api
type: api

parameters:
  openapi:
    url: ${specServer.url}/openapi.yaml
  request:
    url: https://api.example.com/players/Ada
    method: GET
    headers:
      Accept: application/json
  response:
    records_path: ""
    fields

  pagination:
    type: next_url
    next_url_path: "pag"
    start_page: 1
    max_pages: 10
`;
      await writeFile(
        join(liveApp.workspaceDir, apiAssetPath),
        assetContent,
        "utf8"
      );

      await expect
        .poll(async () => {
          const response = await page.request.get(`${liveApp.baseURL}/api/workspace`);
          if (!response.ok()) return "";
          const workspace = await response.json() as WorkspaceResponse;
          const asset = workspace.pipelines.flatMap((pipeline) => pipeline.assets).find((item) => item.id === apiAssetId);
          return [asset?.type ?? "", asset?.parse_error ? "parse_error" : "", asset?.content.includes("    fields") ? "content" : ""].join("|");
        }, { timeout: 30000 })
        .toBe("api|parse_error|content");

      await page.goto(`${liveApp.baseURL}/pipelines/${pipelineId}/assets/${apiAssetId}/code`);
      await expect(page.getByText("This asset could not be parsed.")).toBeVisible({ timeout: 15000 });
      const editor = page.locator(".monaco-editor").first();
      await expect(editor).toBeVisible({ timeout: 15000 });
      await setEditorContentAtYamlField(page, assetContent, "next_url_path", "pag");

      const suggestionsResponse = page.waitForResponse(
        (response) =>
          response.url().includes("/api/api-assets/openapi-suggestions") &&
          response.url().includes("request_url=") &&
          response.ok(),
        { timeout: 15000 }
      );
      await page.keyboard.press("ControlOrMeta+Space");
      const response = await suggestionsResponse;
      const body = await response.json() as { response_paths?: Array<{ path: string; detail?: string }> };
      expect(body.response_paths?.map((item) => item.path)).toContain("pagination.next");

      const suggestWidget = page.locator(".suggest-widget.visible").first();
      await expect(suggestWidget.getByText("pagination.next", { exact: true })).toBeVisible({ timeout: 15000 });
    } finally {
      await new Promise<void>((resolve) => specServer.server.close(() => resolve()));
    }
  });

  test("paginated API asset materializes all requested pages", async ({ liveApp, page }) => {
    const apiServer = await startAPIExecutionServer();
    try {
      await writeFile(
        join(liveApp.workspaceDir, paginatedAPIAssetPath),
        `name: analytics.paginated_api
type: api

parameters:
  request:
    url: ${apiServer.url}/page-items
    method: GET
  response:
    records_path: data
    fields:
      id: id
  pagination:
    type: page_number
    page_param: page
    start_page: 1
    has_more_path: pagination.has_next_page
    max_pages: 5
`,
        "utf8"
      );
      await waitForWorkspaceAsset(page, liveApp.baseURL, paginatedAPIAssetId);

      const inference = await page.request.post(`${liveApp.baseURL}/api/assets/${paginatedAPIAssetId}/api-infer`);
      expect(inference.ok()).toBe(true);
      const inferred = await inference.json() as {
        status: string;
        records_paths?: Array<{ path: string }>;
        columns?: Array<{ name: string; type?: string }>;
      };
      expect(inferred.status).toBe("ok");
      expect(inferred.records_paths?.map((item) => item.path)).toContain("data");
      expect(inferred.columns).toContainEqual(expect.objectContaining({ name: "id", type: "integer" }));
      apiServer.pageRequests.length = 0;

      const done = await materializeAsset(page, liveApp.baseURL, paginatedAPIAssetId);
      expect(done.status).toBe("ok");
      expect(done.output).toContain("Fetched 2 records from API asset analytics.paginated_api");
      expect(apiServer.pageRequests).toEqual(["1", "2"]);

      const inspect = await page.request.get(`${liveApp.baseURL}/api/assets/${paginatedAPIAssetId}/inspect?limit=10`);
      expect(inspect.ok()).toBe(true);
      const body = await inspect.json() as { status: string; rows?: Array<Record<string, unknown>> };
      expect(body.status).toBe("ok");
      expect((body.rows ?? []).map((row) => String(row.id)).sort()).toEqual(["1", "2"]);
    } finally {
      await new Promise<void>((resolve) => apiServer.server.close(() => resolve()));
    }
  });

  test("POST API asset sends JSON body and auth header", async ({ liveApp, page }) => {
    const apiServer = await startAPIExecutionServer();
    try {
      await writeFile(
        join(liveApp.workspaceDir, postAPIAssetPath),
        `name: analytics.post_api
type: api

parameters:
  request:
    url: ${apiServer.url}/post-search
    method: POST
    body:
      query: Ada
  auth:
    type: api_key
    name: X-Test-Key
    value: secret-token
    in: header
  response:
    records_path: data
    fields:
      id: id
      name: name
`,
        "utf8"
      );
      await waitForWorkspaceAsset(page, liveApp.baseURL, postAPIAssetId);

      const done = await materializeAsset(page, liveApp.baseURL, postAPIAssetId);
      expect(done.status).toBe("ok");
      expect(done.output).toContain("Fetched 1 records from API asset analytics.post_api");
      expect(apiServer.postBodies).toEqual([{ query: "Ada" }]);
      expect(apiServer.authHeaders).toEqual(["secret-token"]);

      const inspect = await page.request.get(`${liveApp.baseURL}/api/assets/${postAPIAssetId}/inspect?limit=10`);
      expect(inspect.ok()).toBe(true);
      const body = await inspect.json() as { status: string; rows?: Array<Record<string, unknown>> };
      expect(body.status).toBe("ok");
      expect(body.rows?.[0]?.name).toBe("Ada");
    } finally {
      await new Promise<void>((resolve) => apiServer.server.close(() => resolve()));
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

async function waitForWorkspaceAssetContent(page: import("@playwright/test").Page, baseURL: string, assetId: string, expected: string) {
  let found: WorkspaceResponse["pipelines"][number]["assets"][number] | undefined;
  await expect
    .poll(async () => {
      const response = await page.request.get(`${baseURL}/api/workspace`);
      if (!response.ok()) return "";
      const workspace = await response.json() as WorkspaceResponse;
      found = workspace.pipelines.flatMap((pipeline) => pipeline.assets).find((asset) => asset.id === assetId);
      return found?.content ?? "";
    }, { timeout: 30000 })
    .toContain(expected);
  return found!;
}

async function replaceEditorContent(page: import("@playwright/test").Page, content: string) {
  const editor = page.locator(".monaco-editor").first();
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(content);
}

async function setEditorContentAtYamlField(page: import("@playwright/test").Page, content: string, fieldName: string, cursorAfter?: string) {
  await page.waitForFunction(
    () => {
      const monaco = (window as typeof window & { monaco?: any }).monaco;
      const editor = monaco?.editor.getEditors?.()[0];
      return Boolean(editor?.getModel?.());
    },
    undefined,
    { timeout: 15000 }
  );
  await page.evaluate(({ content, cursorAfter, fieldName }) => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    const model = editor?.getModel?.();
    if (!editor || !model) {
      throw new Error("Monaco editor is not ready");
    }
    model.setValue(content);
    const match = model.findMatches(fieldName, false, false, false, null, true)[0];
    if (!match) {
      throw new Error(`${fieldName} was not found in the Monaco model`);
    }
    const lineNumber = match.range.startLineNumber;
    const lineText = model.getLineContent(lineNumber);
    let column = lineText.length + 1;
    if (cursorAfter) {
      const index = lineText.indexOf(cursorAfter);
      if (index === -1) {
        throw new Error(`cursor text ${cursorAfter} was not found in ${fieldName} line`);
      }
      column = index + cursorAfter.length + 1;
    }
    editor.focus();
    editor.setPosition({ lineNumber, column });
  }, { content, cursorAfter, fieldName });
}

async function materializeAsset(page: import("@playwright/test").Page, baseURL: string, assetId: string) {
  const response = await page.request.post(`${baseURL}/api/assets/${assetId}/materialize/stream?environment=default`);
  expect(response.ok()).toBe(true);
  const text = await response.text();
  const doneLine = text
    .split(/\r?\n/)
    .reverse()
    .find((line) => line.startsWith("data: ") && line.includes('"status"'));
  if (!doneLine) {
    throw new Error(`materialize stream did not contain a done event:\n${text}`);
  }
  return JSON.parse(doneLine.slice("data: ".length)) as {
    status: string;
    output: string;
    error?: string;
  };
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

async function startRecordsPathOpenAPIServer(): Promise<{ server: Server; url: string }> {
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
                    type: array
                    items:
                      type: object
                      properties:
                        username:
                          type: string
                        rating:
                          type: integer
                        active:
                          type: boolean
                  features:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        status:
                          type: string
                  pagination:
                    type: object
                    properties:
                      next:
                        type: string
                      has_more:
                        type: boolean
`);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("OpenAPI records_path test server did not start on a TCP port");
  }
  return { server, url: `http://127.0.0.1:${address.port}` };
}

async function startAPIExecutionServer(): Promise<{
  server: Server;
  url: string;
  pageRequests: string[];
  postBodies: Array<Record<string, unknown>>;
  authHeaders: string[];
}> {
  const pageRequests: string[] = [];
  const postBodies: Array<Record<string, unknown>> = [];
  const authHeaders: string[] = [];
  const server = createServer((req, res) => {
    if (!req.url) {
      res.writeHead(400).end();
      return;
    }
    const url = new URL(req.url, "http://127.0.0.1");
    if (url.pathname === "/page-items") {
      const page = url.searchParams.get("page") ?? "";
      pageRequests.push(page);
      res.setHeader("content-type", "application/json");
      if (page === "1") {
        res.end(JSON.stringify({ data: [{ id: 1 }], pagination: { has_next_page: true } }));
        return;
      }
      if (page === "2") {
        res.end(JSON.stringify({ data: [{ id: 2 }], pagination: { has_next_page: false } }));
        return;
      }
      res.writeHead(400).end(JSON.stringify({ error: "unexpected page" }));
      return;
    }
    if (url.pathname === "/post-search") {
      if (req.method !== "POST") {
        res.writeHead(405).end();
        return;
      }
      authHeaders.push(req.headers["x-test-key"]?.toString() ?? "");
      let body = "";
      req.setEncoding("utf8");
      req.on("data", (chunk) => {
        body += chunk;
      });
      req.on("end", () => {
        postBodies.push(JSON.parse(body || "{}") as Record<string, unknown>);
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ data: [{ id: 1, name: "Ada" }] }));
      });
      return;
    }
    res.writeHead(404).end();
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("API execution test server did not start on a TCP port");
  }
  return { server, url: `http://127.0.0.1:${address.port}`, pageRequests, postBodies, authHeaders };
}
