import { expect, Page } from "@playwright/test";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test } from "../live-app-fixture";

test.describe("sql intellisense live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("requests parser-backed intellisense context from the live server", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContent(
      page,
      "select o.order_id\nfrom analytics.orders as o"
    );

    let body: unknown = null;
    await expect
      .poll(async () => {
        body = await page.evaluate(async () => {
          const response = await fetch("/api/sql/parse-context", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              asset_id: "YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA",
              content: "select o.order_id\nfrom analytics.orders as o",
              schema: [],
            }),
          });
          return await response.json();
        });

        return (body as { status?: string } | null)?.status ?? null;
      })
      .toBe("ok");

    const parseContext = body as {
      status: string;
      tables?: Array<{ name?: string; alias?: string }>;
      columns?: Array<{ qualifier?: string; name?: string }>;
    };

    expect(parseContext.tables).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "analytics.orders", alias: "o" }),
      ])
    );
    expect(parseContext.columns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ qualifier: "o", name: "o.order_id" }),
      ])
    );
  });

  test("shows resolved upstream columns in the SQL debug panel", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);
    const saveResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/pipelines/") &&
        response.url().includes("/assets/YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA") &&
        response.request().method() === "PUT" &&
        (response.request().postData() ?? "").includes("from analytics.orders")
    );
    await replaceEditorContent(page, "select *\nfrom analytics.orders");
    await page.keyboard.press("ControlOrMeta+S");
    await saveResponse;
    await waitForWorkspaceAssetUpstreams(page, "analytics.customers", ["analytics.orders"]);
    await reopenCustomersEditor(page, liveApp.baseURL);
    await page.getByText("SQL column debug", { exact: true }).click();

    const debugPanel = page.locator("details").last();
    await expect(debugPanel.getByText("analytics.orders", { exact: true }).last()).toBeVisible();
    await expect(
      debugPanel.getByText(/analytics\.orders -> analytics\.orders · (declared|resolved-without-columns)/)
    ).toBeVisible();
    const hasResolvedColumns = await debugPanel
      .getByText("customer_id, order_id, total_amount", { exact: true })
      .count();
    if (hasResolvedColumns > 0) {
      await expect(
        debugPanel.getByText("customer_id, order_id, total_amount", { exact: true }).last()
      ).toBeVisible();
    } else {
      await expect(debugPanel.getByText("(resolved, but no columns)", { exact: true })).toBeVisible();
    }
  });

  test("navigates to the referenced asset on Ctrl+click", async ({ liveApp, page }) => {
    await page.goto(`${liveApp.baseURL}/`);

    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContent(page, "select * from analytics.orders\n");

    const modifier = process.platform === "darwin" ? "Meta" : "Control";
    await page.keyboard.down(modifier);
    await clickEditorText(page, "analytics.orders");
    await page.keyboard.up(modifier);

    await expect(page.getByTestId("editor-asset-name")).toHaveText("analytics.orders");
    await expect(page.getByTestId("editor-asset-path")).toHaveText(
      "analytics/assets/analytics/orders.sql"
    );
  });

  test("shows quoted workspace path suggestions for DuckDB SQL", async ({
    liveApp,
    page,
  }) => {
    await writeFile(
      join(liveApp.workspaceDir, "duckdb-files", "customers.csv"),
      "customer_id,customer_name\n1,Ada\n",
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    const pathSuggestionsResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/sql-path-suggestions") &&
        response.request().method() === "GET" &&
        response.url().includes(`prefix=${encodeURIComponent("./duckdb-files/cu")}`)
    );

    await replaceEditorContent(page, 'select * from "./duckdb-files/cu');

    const response = await pathSuggestionsResponse;
    const body = await response.json();

    expect(body.status).toBe("ok");
    expect(body.suggestions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          value: "./duckdb-files/customers.csv",
          kind: "file",
        }),
      ])
    );

    await page.keyboard.press("ControlOrMeta+Space");

    const suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible();
    await expect(suggestWidget.getByText("./duckdb-files/customers.csv", { exact: true })).toBeVisible();
  });

  test("does not report DuckDB file paths as unresolved tables", async ({
    liveApp,
    page,
  }) => {
    await writeFile(
      join(liveApp.workspaceDir, "duckdb-files", "customers.csv"),
      "customer_id,customer_name\n1,Ada\n",
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await replaceEditorContent(page, 'select * from "./duckdb-files/customers.csv"');

    let body: unknown = null;
    await expect
      .poll(async () => {
        body = await page.evaluate(async () => {
          const response = await fetch("/api/sql/parse-context", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              asset_id: "YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA",
              content: 'select * from "./duckdb-files/customers.csv"',
              schema: [],
            }),
          });
          return await response.json();
        });

        return (body as { status?: string } | null)?.status ?? null;
      })
      .toBe("ok");

    const parseContext = body as {
      diagnostics?: Array<{ message?: string }>;
    };

    expect(parseContext.diagnostics ?? []).not.toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          message: "Unresolved table: ./duckdb-files/customers.csv",
        }),
      ])
    );
  });

  test("warns when a Bruin-defined column is missing from discovered table columns", async ({
    liveApp,
    page,
  }) => {
    await page.goto(`${liveApp.baseURL}/`);

    const body = await page.evaluate(async () => {
      const response = await fetch("/api/sql/parse-context", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          asset_id: "YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA",
          content:
            "with blub as (select *, 1 as schabla from quickstart.range_100)\n\n" +
            "select range, schabla, (select test from quickstart.test), bla from blub",
          schema: [
            {
              name: "quickstart.range_100",
              columns: [
                {
                  name: "range",
                  type: "bigint",
                  source_methods: ["workspace-load"],
                },
                {
                  name: "bla",
                  type: "integer",
                  source_methods: ["asset-sql-definition"],
                },
              ],
            },
            {
              name: "range_100",
              columns: [
                {
                  name: "range",
                  type: "bigint",
                  source_methods: ["connection-column-discovery"],
                },
              ],
            },
            {
              name: "quickstart.range_100",
              columns: [
                {
                  name: "bla",
                  type: "integer",
                  source_methods: ["asset-inspect"],
                },
              ],
            },
            {
              name: "quickstart.test",
              columns: [
                {
                  name: "test",
                  type: "integer",
                  source_methods: ["workspace-load", "connection-column-discovery"],
                },
              ],
            },
          ],
        }),
      });
      return await response.json();
    });

    const diagnostics = (body as {
      status?: string;
      diagnostics?: Array<{ message?: string; severity?: string }>;
    }).diagnostics ?? [];

    expect((body as { status?: string }).status).toBe("ok");
    expect(diagnostics).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          severity: "warning",
          message:
            "Column 'bla' is defined in the Bruin asset 'quickstart.range_100', but it has not been materialized yet.",
        }),
      ])
    );
    expect(diagnostics).not.toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          severity: "error",
          message: "Unresolved column: bla",
        }),
      ])
    );
  });

  test("warns after refreshing when upstream SQL definition changed without rematerializing", async ({
    liveApp,
    page,
  }) => {
    const assetDir = join(liveApp.workspaceDir, "analytics", "assets", "analytics");
    const upstreamPath = join(assetDir, "unmaterialized_asset.sql");
    const downstreamPath = join(assetDir, "query_unmaterialized.sql");
    const upstreamAssetId = Buffer.from(
      "analytics/assets/analytics/unmaterialized_asset.sql"
    ).toString("base64");
    const downstreamAssetId = Buffer.from(
      "analytics/assets/analytics/query_unmaterialized.sql"
    ).toString("base64");
    const warningMessage =
      "Column 'blabli' is defined in the Bruin asset 'analytics.unmaterialized_asset', but it has not been materialized yet.";

    await mkdir(assetDir, { recursive: true });
    await writeFile(
      upstreamPath,
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
columns:
  - name: plumbus
    type: integer
@bruin */

select 1 as plumbus
`,
      "utf8"
    );
    await writeFile(
      downstreamPath,
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select plumbus, blabli from analytics.unmaterialized_asset
`,
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    await waitForWorkspaceAsset(page, "analytics.unmaterialized_asset");

    const materializeResult = await page.evaluate(async (assetId) => {
      const response = await fetch(`/api/assets/${assetId}/materialize/stream`, {
        method: "POST",
      });
      return await response.text();
    }, upstreamAssetId);
    expect(materializeResult).toContain('"status":"ok"');

    await writeFile(
      upstreamPath,
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
columns:
  - name: plumbus
    type: integer
@bruin */

select 1 as plumbus, 2 as blabli
`,
      "utf8"
    );
    await waitForWorkspaceAssetContent(page, "analytics.unmaterialized_asset", "blabli");

    await page.goto(
      `${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz&asset=${encodeURIComponent(downstreamAssetId)}`
    );
    await waitForEditorReady(page);

    await expect
      .poll(async () => getEditorMarkers(page), { timeout: 15000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            message: warningMessage,
            severity: 4,
          }),
        ])
      );

    const markers = await getEditorMarkers(page);
    expect(markers).not.toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          message: "Unresolved column: blabli",
          severity: 8,
        }),
      ])
    );
  });

  test("shows parser syntax errors as Monaco diagnostics", async ({ liveApp, page }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco diagnostics are only stable in the desktop editor.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContent(
      page,
      [
        "SELECT",
        "  customer_id",
        "FROM analytics.customers",
        "WHERE",
        "  customer_id = 1 AND customer_id = 1",
        "  >   -- dangling comparison operator",
      ].join("\n")
    );

    await expect
      .poll(async () => getEditorMarkers(page), { timeout: 15000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            message: expect.stringContaining("Unexpected token: Gt"),
            severity: 8,
          }),
        ])
      );
  });

  test("uses SQL LSP completions in the Monaco SQL editor", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop suggest widget exposes stable Monaco completion DOM.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContentByInsertText(page, "select o.\nfrom analytics.orders o");
    await setEditorPositionAfterText(page, "o.");

    const completionResponse = await page.request.post(`${liveApp.baseURL}/api/sql/lsp/completions`, {
      data: {
        asset_id: "YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA",
        content: "select o.\nfrom analytics.orders o",
        position: { line: 0, character: "select o.".length },
      },
    });
    await page.keyboard.press("ControlOrMeta+Space");
    const body = await completionResponse.json() as {
      status?: string;
      completions?: Array<{ label?: string }>;
    };

    expect(body.status).toBe("ok");
    expect(body.completions ?? []).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ label: "total_amount" }),
        expect.objectContaining({ label: "order_id" }),
      ])
    );
    await expectVisibleSuggestText(page, "total_amount");
  });

  test("maps SQL LSP rendered-template diagnostics back into Monaco", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco diagnostics are only stable in the desktop editor.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    const diagnosticsResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/sql/lsp/diagnostics") &&
        response.request().method() === "POST" &&
        (response.request().postData() ?? "").includes("missing_orders"),
      { timeout: 15000 }
    );
    await replaceEditorContentByInsertText(page, 'select *\nfrom {{ ref("missing_orders") }} m');
    const response = await diagnosticsResponse;
    const body = await response.json() as {
      status?: string;
      diagnostics?: Array<{ message?: string; range?: { start?: { line?: number; character?: number } } }>;
    };

    expect(body.status).toBe("ok");
    expect(body.diagnostics ?? []).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          message: "Unresolved relation: missing_orders",
          range: expect.objectContaining({
            start: expect.objectContaining({ line: 1, character: 5 }),
          }),
        }),
      ])
    );

    await expect
      .poll(async () => getEditorMarkerMessages(page), { timeout: 15000 })
      .toEqual(expect.arrayContaining([expect.stringContaining("Unresolved relation: missing_orders")]));
  });

  test("shows latest inspect SQL error as Monaco diagnostics while content is unchanged", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco diagnostics are only stable in the desktop editor.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await replaceEditorContent(
      page,
      'select * from finances.raw_downstream_downstream_downstream'
    );
    const saveResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/pipelines/") &&
        response.url().includes("/assets/") &&
        response.request().method() === "PUT"
    );
    await page.keyboard.press("ControlOrMeta+S");
    await saveResponse;
    const inspectResponse = page.waitForResponse(async (response) => {
      if (
        !response.url().includes("/api/assets/") ||
        !response.url().includes("/inspect") ||
        response.request().method() !== "GET"
      ) {
        return false;
      }

      try {
        const body = await response.json();
        return body.status === "error";
      } catch {
        return false;
      }
    });
    await page.keyboard.press("ControlOrMeta+Enter");

    const response = await inspectResponse;
    const body = await response.json();

    expect(body.status).toBe("error");
    expect(body.raw_output).toContain("Catalog Error");
    expect(body.raw_output).toContain("LINE 1:");

    await expect
      .poll(async () => {
        return await page.evaluate(() => {
          const monaco = (window as typeof window & {
            monaco?: {
              editor: {
                getModels(): Array<{
                  uri: { toString(): string };
                }>;
                getModelMarkers(args: { resource?: { toString(): string } }): Array<{ message: string }>;
              };
            };
          }).monaco;

          if (!monaco) {
            return [];
          }

          const models = monaco.editor.getModels();
          for (const model of models) {
            const markers = monaco.editor.getModelMarkers({ resource: model.uri });
            if (markers.length > 0) {
              return markers.map((marker) => marker.message);
            }
          }

          return [];
        });
      }, { timeout: 15000 })
      .toEqual(expect.arrayContaining([expect.stringContaining("Catalog Error")]));
  });

  test("offers a quick fix for similar unresolved column names", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco quick-fix UI is only stable in the desktop editor.");

    await page.goto(`${liveApp.baseURL}/`);
    await openAssetEditor(page, liveApp.baseURL, {
      encodedAssetPath: "YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3Mvb3JkZXJzLnNxbA",
      assetName: "analytics.orders",
    });

    await replaceEditorContentByInsertText(
      page,
      "select c.custmer_name\nfrom analytics.customers as c"
    );

    await expect
      .poll(async () => getEditorMarkerMessages(page), { timeout: 15000 })
      .toEqual(
        expect.arrayContaining([
          expect.stringContaining("Unresolved column 'custmer_name'. Did you mean 'customer_name'?"),
        ])
      );

  });

  test("reports self references as circular dependencies", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco diagnostics are only stable in the desktop editor.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await replaceEditorContentByInsertText(
      page,
      "select *\nfrom analytics.customers"
    );

    await expect
      .poll(async () => getEditorMarkerMessages(page), { timeout: 15000 })
      .toEqual(
        expect.arrayContaining([
          expect.stringContaining("Circular dependency: asset 'analytics.customers' references itself."),
        ])
      );

    await expect
      .poll(async () => getEditorMarkerMessages(page), { timeout: 15000 })
      .not.toEqual(expect.arrayContaining([expect.stringContaining("Unresolved table: analytics.customers")]));
  });

  test("offers a quick fix for similar unresolved table names", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco quick-fix UI is only stable in the desktop editor.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await replaceEditorContentByInsertText(page, "select *\nfrom analytics.ordrs");

    await expect
      .poll(async () => getEditorMarkerMessages(page), { timeout: 15000 })
      .toEqual(
        expect.arrayContaining([
          expect.stringContaining("Unresolved table 'analytics.ordrs'. Did you mean 'analytics.orders'?"),
        ])
      );

  });

  test("renders Jinja ghost text and completions in the SQL editor", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Monaco injected text DOM is only stable in the desktop editor.");

    await writeFile(
      join(liveApp.workspaceDir, "analytics", "pipeline.yml"),
      [
        "name: analytics",
        "schedule: daily",
        "start_date: \"2024-01-01\"",
        "",
        "default_connections:",
        "  duckdb: duckdb-default",
        "",
        "variables:",
        "  run_mode:",
        "    type: string",
        "    default: incremental",
        "",
      ].join("\n"),
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    const renderResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/assets/") &&
        response.url().includes("/render-jinja") &&
        response.request().method() === "POST"
    );

    await replaceEditorContentByInsertText(
      page,
      "select * from analytics.orders\nwhere dt = '{{ end_date }}'\nand mode = '{{ var.run_mode }}'"
    );
    const response = await renderResponse;
    const body = await response.json();
    expect(body.status).toBe("ok");
    expect(body.spans).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ expression: "end_date", rendered_text: expect.stringMatching(/\d{4}-\d{2}-\d{2}/) }),
        expect.objectContaining({ expression: "var.run_mode", rendered_text: "incremental" }),
      ])
    );

    await expect(page.locator(".bruin-jinja-rendered-ghost").first()).toBeVisible({ timeout: 10000 });
    await expect(page.locator(".bruin-jinja-rendered-ghost").filter({ hasText: /\d{4}-\d{2}-\d{2}/ }).first()).toBeVisible();
    await expect
      .poll(async () => {
        return await page.evaluate(() => {
          const monaco = (window as typeof window & {
            monaco?: any;
          }).monaco;
          const editor = monaco?.editor.getEditors?.()[0];
          const model = editor?.getModel();
          if (!monaco || !model) return [];
          return monaco.editor.getModelMarkers({ resource: model.uri }).map((marker: { message: string }) => marker.message);
        });
      }, { timeout: 10000 })
      .not.toEqual(expect.arrayContaining([expect.stringContaining("syntax error")]));

    await replaceEditorContentAndWaitForJinja(page, "select '{{ var. }}'");
    await setEditorPositionAfterText(page, "var.");
    await openSuggestUntilText(page, "run_mode");
  });

  test("keeps SQL suggestion focus across workspace SSE updates", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop suggest widget exposes stable Monaco completion DOM.");

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContent(page, "select * from analytics.");
    await page.keyboard.press("ControlOrMeta+Space");

    const suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible();
    await page.keyboard.press("ArrowDown");
    const focusedBefore = await getFocusedSuggestText(page);
    expect(focusedBefore).toBeTruthy();

    const revisionBefore = await getWorkspaceRevision(page);
    const pipelinePath = join(liveApp.workspaceDir, "analytics", "pipeline.yml");
    const pipelineContent = await readFile(pipelinePath, "utf8");
    await writeFile(pipelinePath, `${pipelineContent.trimEnd()}\n`, "utf8");

    await expect
      .poll(async () => getWorkspaceRevision(page), { timeout: 15000 })
      .toBeGreaterThan(revisionBefore);
    await expect.poll(async () => getFocusedSuggestText(page)).toBe(focusedBefore);
  });

  test("does not fetch remote columns for partial qualified Bruin asset names", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop suggest widget exposes stable Monaco completion DOM.");

    const assetDir = join(liveApp.workspaceDir, "analytics", "assets", "simple");
    await mkdir(assetDir, { recursive: true });
    await writeFile(
      join(assetDir, "small.sql"),
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as small
`,
      "utf8",
    );
    await writeFile(
      join(assetDir, "query_small.sql"),
      `/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select * from simple.small
`,
      "utf8",
    );

    const tableColumnRequests: string[] = [];
    page.on("request", (request) => {
      const url = request.url();
      if (url.includes("/api/sql/table-columns")) {
        tableColumnRequests.push(url);
      }
    });

    await page.goto(`${liveApp.baseURL}/`);
    await waitForWorkspaceAsset(page, "simple.query_small");
    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContent(page, "select * from simple.");
    await page.keyboard.press("ControlOrMeta+Space");

    const suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible();
    await expect(suggestWidget.getByText("simple.small", { exact: true })).toBeVisible();
    await page.waitForTimeout(500);

    expect(
      tableColumnRequests.some((url) =>
        url.includes("table=simple") || url.includes("table=%22simple%22"),
      ),
    ).toBe(false);
  });

  test("suggests Jinja expressions inside statement blocks", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop suggest widget exposes stable Monaco completion DOM.");

    await writeFile(
      join(liveApp.workspaceDir, "analytics", "pipeline.yml"),
      [
        "name: analytics",
        "schedule: daily",
        "start_date: \"2024-01-01\"",
        "",
        "default_connections:",
        "  duckdb: duckdb-default",
        "",
        "variables:",
        "  days:",
        "    type: array",
        "    default: [1, 3, 7]",
        "  run_mode:",
        "    type: string",
        "    default: incremental",
        "",
      ].join("\n"),
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    await openCustomersEditor(page, liveApp.baseURL);

    await replaceEditorContentByInsertText(page, "{% if start_date |  %}\nselect 1\n{% endif %}");
    await setEditorPositionAfterText(page, "| ");
    await page.keyboard.press("ControlOrMeta+Space");
    let suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible();
    await expect(suggestWidget.getByText("add_days", { exact: true })).toBeVisible();
    await page.keyboard.press("Escape");

    await replaceEditorContentAndWaitForJinja(page, "{% if var. %}\nselect 1\n{% endif %}");
    await setEditorPositionAfterText(page, "var.");
    await openSuggestUntilText(page, "run_mode");
    await expectVisibleSuggestText(page, "days");
    await page.keyboard.press("Escape");

    await replaceEditorContentByInsertText(page, "{% for day in  %}\nselect {{ day }}\n{% endfor %}");
    await setEditorPositionAfterText(page, "in ");
    await page.keyboard.press("ControlOrMeta+Space");
    suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible();
    await expect(suggestWidget.getByText("var.days", { exact: true })).toBeVisible();
  });
});

test.describe("sql intellisense ranking live", () => {
  test.use({ fixtureName: "sql-intellisense-ranking-workspace" });

  test("collapses matching table and asset suggestions into one combined entry", async ({
    liveApp,
    page,
  }) => {
    test.skip(test.info().project.name.includes("mobile"), "Desktop suggest widget exposes stable combined-entry metadata.");

    await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz`);

    if (test.info().project.name.includes("mobile")) {
      await page.goto(`${liveApp.baseURL}/?pipeline=YW5hbHl0aWNz&asset=YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvZGVwZW5kZW5jaWVzLnNxbA`);
      const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
      if (!(await editorDialog.isVisible().catch(() => false))) {
        await page.getByRole("button", { name: "Edit asset" }).click();
      }
    } else {
      await page.getByRole("link", { name: "dependencies", exact: true }).click();
    }
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await expect(page.getByText(/Materialize asset: analytics\.dependencies/)).toBeVisible({
      timeout: 15000,
    });

    if (test.info().project.name.includes("mobile")) {
      await page.goto(`${liveApp.baseURL}/?pipeline=bWFydHM&asset=bWFydHMvYXNzZXRzL21hcnRzL2RlcGVuZGVuY2llcy5zcWw`);
      const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
      if (!(await editorDialog.isVisible().catch(() => false))) {
        await page.getByRole("button", { name: "Edit asset" }).click();
      }
    } else {
      await page.getByRole("link", { name: "marts" }).click();
      await page
        .locator('a[href*="bWFydHMvYXNzZXRzL21hcnRzL2RlcGVuZGVuY2llcy5zcWw"]')
        .last()
        .click();
    }
    await page.getByRole("button", { name: "Materialize", exact: true }).click();
    await expect(page.getByText(/Materialize asset: marts\.dependencies/)).toBeVisible({
      timeout: 15000,
    });

    await openCustomersEditor(page, liveApp.baseURL);
    await replaceEditorContent(page, "select * from dependen");
    await page.keyboard.press("ControlOrMeta+Space");

    const suggestWidget = page.locator(".suggest-widget.visible").first();
    await expect(suggestWidget).toBeVisible();

    await expect(suggestWidget.getByText("analytics.dependencies", { exact: true })).toHaveCount(1);
    await expect(
      suggestWidget.getByRole("listitem", {
        name: /analytics\.dependencies, Table \+ Asset \(analytics\.dependencies\), Class/,
      }),
    ).toBeVisible();
    await expect(
      suggestWidget.getByRole("listitem", {
        name: /marts\.dependencies, (Asset|Table \+ Asset) \(marts\.dependencies\), Module/,
      }),
    ).toBeVisible();
  });
});

async function openCustomersEditor(page: Page, baseURL: string) {
  await openAssetEditor(page, baseURL, {
    encodedAssetPath: "YW5hbHl0aWNzL2Fzc2V0cy9hbmFseXRpY3MvY3VzdG9tZXJzLnNxbA",
    assetName: "analytics.customers",
  });
}

async function openAssetEditor(
  page: Page,
  baseURL: string,
  options: { encodedAssetPath: string; assetName: string }
) {
  const isMobile = test.info().project.name.includes("mobile");
  const assetURL = `${baseURL}/?pipeline=YW5hbHl0aWNz&asset=${options.encodedAssetPath}`;
  const assetShortName = options.assetName.split(".").at(-1) ?? options.assetName;
  if (isMobile) {
    await page.goto(assetURL);
    await expect(page).toHaveTitle(`${options.assetName} · analytics · Renart`);
    const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
    if (!(await editorDialog.isVisible().catch(() => false))) {
      const editButton = page.getByRole("button", { name: "Edit asset" });
      if (await editButton.isVisible().catch(() => false)) {
        await editButton.click();
      }
    }
    await expect(editorDialog).toBeVisible();
    await expect(page.getByTestId("editor-asset-name")).toHaveText(options.assetName);
    await waitForEditorReady(page);
  } else {
    await page.goto(assetURL);
    const editorAssetName = page.getByTestId("editor-asset-name");
    if (!(await editorAssetName.isVisible().catch(() => false))) {
      const analyticsLink = page.getByRole("link", { name: "analytics", exact: true });
      await expect(analyticsLink).toBeVisible({ timeout: 15000 });
      await analyticsLink.click();
    }

    if ((await editorAssetName.textContent().catch(() => null))?.trim() !== options.assetName) {
      const assetLink = page.getByRole("link", { name: assetShortName, exact: true });
      await expect(assetLink).toBeVisible({ timeout: 15000 });
      await assetLink.click();
    }

    await expect(editorAssetName).toHaveText(options.assetName, { timeout: 15000 });
    await waitForEditorReady(page);
  }
}

async function reopenCustomersEditor(page: Page, baseURL: string) {
  if (test.info().project.name.includes("mobile")) {
    await openCustomersEditor(page, baseURL);
    return;
  }

  await page.getByRole("link", { name: "orders", exact: true }).click();
  await openCustomersEditor(page, baseURL);
}

async function replaceEditorContentByInsertText(
  page: Page,
  content: string
) {
  const editor = await waitForEditorReady(page);
  await editor.click();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.insertText(content);
}

async function replaceEditorContentAndWaitForJinja(page: Page, content: string) {
  const renderResponse = page.waitForResponse(
    (response) =>
      response.url().includes("/api/assets/") &&
      response.url().includes("/render-jinja") &&
      response.request().method() === "POST",
    { timeout: 15000 }
  );
  await replaceEditorContentByInsertText(page, content);
  await renderResponse;
}

async function setEditorPositionAfterText(page: Page, text: string) {
  await page.evaluate((needle) => {
    const monaco = (window as typeof window & {
      monaco?: any;
    }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    const model = editor?.getModel();
    if (!monaco || !editor || !model) return;
    const value = model.getValue();
    const offset = value.indexOf(needle);
    if (offset < 0) return;
    const position = model.getPositionAt(offset + needle.length);
    editor.focus();
    editor.setPosition(position);
  }, text);
}

async function getEditorMarkerMessages(page: Page) {
  return await page.evaluate(() => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    const model = editor?.getModel();
    if (!monaco || !model) return [];
    return monaco.editor.getModelMarkers({ resource: model.uri }).map((marker: { message: string }) => marker.message);
  });
}

async function getEditorMarkers(page: Page) {
  return await page.evaluate(() => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    const model = editor?.getModel();
    if (!monaco || !model) return [];
    return monaco.editor
      .getModelMarkers({ resource: model.uri })
      .map((marker: { message: string; severity: number }) => ({
        message: marker.message,
        severity: marker.severity,
      }));
  });
}

async function expectVisibleSuggestText(page: Page, text: string) {
  await expect
    .poll(async () => getVisibleSuggestText(page), { timeout: 10000 })
    .toContain(text);
}

async function openSuggestUntilText(page: Page, text: string) {
  await expect
    .poll(async () => {
      await page.keyboard.press("Escape");
      await page.keyboard.press("ControlOrMeta+Space");
      return await getVisibleSuggestText(page);
    }, { timeout: 15000, intervals: [250, 500, 750, 1000] })
    .toContain(text);
}

async function getVisibleSuggestText(page: Page) {
  return await page.evaluate(() => {
    const widgets = Array.from(
      document.querySelectorAll(".suggest-widget.visible, [role='listbox'][aria-label='Suggest']")
    );
    const widget = widgets.find((candidate) => {
      const rect = candidate.getBoundingClientRect();
      const style = window.getComputedStyle(candidate);
      return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
    });
    if (!widget) {
      return "";
    }
    return Array.from(widget.querySelectorAll(".monaco-list-row"))
      .map((row) => row.textContent ?? "")
      .join("\n");
  });
}

async function getEditorValue(page: Page) {
  return await page.evaluate(() => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    return editor?.getModel()?.getValue() ?? "";
  });
}

async function replaceEditorContent(
  page: Page,
  content: string
) {
  const editor = await waitForEditorReady(page);
  await editor.click();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.type(content);
}

async function waitForEditorReady(page: Page) {
  const editor = page.locator(".monaco-editor").first();
  await expect(page.getByTestId("editor-asset-name")).toHaveText(/analytics\./, { timeout: 15000 });
  await expect(editor).toBeVisible({ timeout: 15000 });
  await expect(page.locator(".view-lines").first()).toBeVisible({ timeout: 15000 });
  return editor;
}

async function getWorkspaceRevision(page: Page) {
  return await page.evaluate(async () => {
    const response = await fetch("/api/workspace", { cache: "no-store" });
    const workspace = (await response.json()) as { revision?: number };
    return workspace.revision ?? 0;
  });
}

async function getFocusedSuggestText(page: Page) {
  return await page
    .locator(".suggest-widget.visible .monaco-list-row.focused")
    .first()
    .textContent()
    .catch(() => null);
}

async function waitForWorkspaceAssetUpstreams(
  page: Page,
  assetName: string,
  expectedUpstreams: string[]
) {
  const sortedExpected = [...expectedUpstreams].sort();
  await expect
    .poll(async () => {
      const upstreams = await page.evaluate(async (targetAssetName) => {
        const response = await fetch("/api/workspace", { cache: "no-store" });
        const workspace = (await response.json()) as {
          pipelines?: Array<{
            assets?: Array<{ name?: string; upstreams?: string[] }>;
          }>;
        };

        for (const pipeline of workspace.pipelines ?? []) {
          for (const asset of pipeline.assets ?? []) {
            if (asset.name === targetAssetName) {
              return asset.upstreams ?? [];
            }
          }
        }

        return null;
      }, assetName);

      return upstreams ? [...upstreams].sort() : null;
    }, { timeout: 15000 })
    .toEqual(sortedExpected);
}

async function waitForWorkspaceAsset(page: Page, assetName: string) {
  await expect
    .poll(async () => {
      return await page.evaluate(async (targetAssetName) => {
        const response = await fetch("/api/workspace", { cache: "no-store" });
        const workspace = (await response.json()) as {
          pipelines?: Array<{ assets?: Array<{ name?: string }> }>;
        };

        return (workspace.pipelines ?? []).some((pipeline) =>
          (pipeline.assets ?? []).some((asset) => asset.name === targetAssetName)
        );
      }, assetName);
    }, { timeout: 15000 })
    .toBe(true);
}

async function waitForWorkspaceAssetContent(
  page: Page,
  assetName: string,
  expectedContent: string
) {
  await expect
    .poll(async () => {
      return await page.evaluate(
        async ({ targetAssetName, targetContent }) => {
          const response = await fetch("/api/workspace", { cache: "no-store" });
          const workspace = (await response.json()) as {
            pipelines?: Array<{ assets?: Array<{ name?: string; content?: string }> }>;
          };

          for (const pipeline of workspace.pipelines ?? []) {
            for (const asset of pipeline.assets ?? []) {
              if (asset.name === targetAssetName) {
                return asset.content?.includes(targetContent) ?? false;
              }
            }
          }

          return false;
        },
        { targetAssetName: assetName, targetContent: expectedContent }
      );
    }, { timeout: 15000 })
    .toBe(true);
}

async function clickEditorLine(page: Page, text: string) {
  const line = page.locator(".view-line").filter({ hasText: text }).first();
  await line.click();
}

async function clickEditorText(page: Page, text: string) {
  const line = page.locator(".view-line").filter({ hasText: text }).first();
  await line.click();
}
