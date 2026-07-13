import { expect, type APIRequestContext } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";

import { liveTest as test } from "../live-app-fixture";

type NotebookEnvelope = {
  notebook: {
    id: string;
    path: string;
    cells: Array<{ id: string; cell_id: string; name: string; content: string }>;
  };
};

async function getCellAssetId(
  request: APIRequestContext,
  baseURL: string,
  notebookId: string,
  cellId: string,
) {
  const response = await request.get(`${baseURL}/api/notebooks/${notebookId}`);
  expect(response.ok()).toBe(true);
  const notebook = ((await response.json()) as NotebookEnvelope).notebook;
  return notebook.cells.find((cell) => cell.cell_id === cellId)!.id;
}

type PythonDiagnosticsResponse = {
  status: string;
  diagnostics?: Array<{ id?: string; message: string }>;
};

async function pythonDiagnostics(
  request: APIRequestContext,
  baseURL: string,
  assetId: string,
  content: string,
) {
  const response = await request.post(`${baseURL}/api/assets/${assetId}/python-diagnostics`, {
    data: { content },
  });
  expect(response.ok()).toBe(true);
  return (await response.json()) as PythonDiagnosticsResponse;
}

async function createNotebook(request: APIRequestContext, baseURL: string, title: string) {
  const response = await request.post(`${baseURL}/api/notebooks`, { data: { title } });
  expect(response.ok()).toBe(true);
  return ((await response.json()) as NotebookEnvelope).notebook;
}

async function addCell(
  request: APIRequestContext,
  baseURL: string,
  notebookId: string,
  name: string,
) {
  const response = await request.post(`${baseURL}/api/notebooks/${notebookId}/cells`, {
    data: { name },
  });
  expect(response.ok()).toBe(true);
  const notebook = ((await response.json()) as NotebookEnvelope).notebook;
  return notebook.cells.find((cell) => cell.name === name)!.cell_id;
}

async function setCell(
  request: APIRequestContext,
  baseURL: string,
  notebookId: string,
  cellId: string,
  body: string,
) {
  const content = `/* @bruin\ntype: duckdb.sql\n@bruin */\n${body}\n`;
  const response = await request.put(`${baseURL}/api/notebooks/${notebookId}/cells/${cellId}`, {
    data: { content },
  });
  expect(response.ok()).toBe(true);
}

async function addPythonCell(
  request: APIRequestContext,
  baseURL: string,
  notebookId: string,
  name: string,
) {
  const response = await request.post(`${baseURL}/api/notebooks/${notebookId}/cells`, {
    data: { name, language: "python" },
  });
  expect(response.ok()).toBe(true);
  const notebook = ((await response.json()) as NotebookEnvelope).notebook;
  return notebook.cells.find((cell) => cell.name === name)!.cell_id;
}

async function setPythonCell(
  request: APIRequestContext,
  baseURL: string,
  notebookId: string,
  cellId: string,
  body: string,
) {
  const response = await request.put(`${baseURL}/api/notebooks/${notebookId}/cells/${cellId}`, {
    data: { content: `${body}\n` },
  });
  expect(response.ok()).toBe(true);
}

type NotebookWithDependencies = NotebookEnvelope & { notebook: { dependencies?: string[] } };

async function getDependencies(request: APIRequestContext, baseURL: string, notebookId: string) {
  const response = await request.get(`${baseURL}/api/notebooks/${notebookId}`);
  expect(response.ok()).toBe(true);
  return ((await response.json()) as NotebookWithDependencies).notebook.dependencies ?? [];
}

test.describe("app notebooks live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("Jinja in a SQL cell is rendered when the cell runs", async ({ liveApp, page }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Jinja Run");
    const cell = await addCell(request, liveApp.baseURL, notebook.id, "templated");
    await setCell(request, liveApp.baseURL, notebook.id, cell, "select '{{ start_date }}' as d");

    // Run with an explicit execution window; the rendered SQL must substitute
    // start_date instead of executing the literal "{{ start_date }}".
    const response = await request.post(`${liveApp.baseURL}/api/notebooks/${notebook.id}/run`, {
      data: { all: true, start_date: "2024-01-15T00:00:00Z", end_date: "2024-01-16T00:00:00Z" },
    });
    expect(response.ok()).toBe(true);
    const payload = (await response.json()) as {
      status: string;
      results: Array<{ name: string; status: string; rows: unknown[][]; error?: string }>;
    };
    expect(payload.status).toBe("ok");
    const result = payload.results.find((entry) => entry.name === "templated")!;
    expect(result.status).toBe("ok");
    expect(result.rows[0][0]).toBe("2024-01-15");
  });

  test("create, edit, and run a notebook against the local session", async ({ liveApp, page }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Revenue Exploration");

    const baseCell = await addCell(request, liveApp.baseURL, notebook.id, "base");
    await setCell(
      request,
      liveApp.baseURL,
      notebook.id,
      baseCell,
      "select 10 as amount union all select 20",
    );
    const doubledCell = await addCell(request, liveApp.baseURL, notebook.id, "doubled");
    await setCell(
      request,
      liveApp.baseURL,
      notebook.id,
      doubledCell,
      "select amount * 2 as doubled from base order by 1",
    );

    await page.goto(`${liveApp.baseURL}/notebooks/${notebook.id}`);
    await expect(page.getByText("Revenue Exploration").first()).toBeVisible({ timeout: 15000 });
    // Both cells render with their names.
    await expect(page.getByText("base", { exact: true }).first()).toBeVisible();
    await expect(page.getByText("doubled", { exact: true }).first()).toBeVisible();

    const runResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/notebooks/${notebook.id}/run`) && response.ok(),
      { timeout: 30000 },
    );
    await page.getByRole("button", { name: "Run all" }).click();
    const payload = (await (await runResponse).json()) as {
      status: string;
      results: Array<{ name: string; status: string; rows: unknown[][] }>;
    };
    expect(payload.status).toBe("ok");
    const doubled = payload.results.find((result) => result.name === "doubled")!;
    expect(doubled.status).toBe("ok");
    expect(doubled.rows).toEqual([[20], [40]]);

    // The result table shows the computed values in the UI.
    await expect(page.getByText("40", { exact: true }).first()).toBeVisible({ timeout: 15000 });
  });

  test("a Python cell queries an upstream cell through the renart SDK", async ({
    liveApp,
    page,
  }) => {
    test.setTimeout(120000);
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Python Query");

    const baseCell = await addCell(request, liveApp.baseURL, notebook.id, "base");
    await setCell(
      request,
      liveApp.baseURL,
      notebook.id,
      baseCell,
      "select 10 as amount union all select 20",
    );
    const pythonCell = await addPythonCell(request, liveApp.baseURL, notebook.id, "doubled");
    await setPythonCell(
      request,
      liveApp.baseURL,
      notebook.id,
      pythonCell,
      [
        "import os",
        "",
        "from renart import query",
        "",
        "",
        "def materialize():",
        '    assert "RENART_NOTEBOOK_INPUTS" not in os.environ',
        '    return query("select amount * 2 as doubled from base order by 1", format="arrow")',
      ].join("\n"),
    );

    const response = await request.post(`${liveApp.baseURL}/api/notebooks/${notebook.id}/run`, {
      data: { all: true },
      timeout: 110000,
    });
    expect(response.ok()).toBe(true);
    const payload = (await response.json()) as {
      status: string;
      results: Array<{
        name: string;
        status: string;
        rows: unknown[][];
        error?: string;
        logs?: string;
      }>;
    };
    const doubled = payload.results.find((result) => result.name === "doubled")!;
    expect(doubled.status, `${doubled.error ?? ""}\n${doubled.logs ?? ""}`).toBe("ok");
    expect(doubled.rows).toEqual([[20], [40]]);
  });

  test("rename is reference-rewriting and the chart type writes a @viz directive", async ({
    liveApp,
    page,
  }) => {
    const notebook = await createNotebook(page.request, liveApp.baseURL, "Viz And Rename");
    const baseCell = await addCell(page.request, liveApp.baseURL, notebook.id, "base");
    await setCell(
      page.request,
      liveApp.baseURL,
      notebook.id,
      baseCell,
      "select 'jan' as month, 10 as revenue union all select 'feb', 20",
    );
    const chartCell = await addCell(page.request, liveApp.baseURL, notebook.id, "chart");
    await setCell(
      page.request,
      liveApp.baseURL,
      notebook.id,
      chartCell,
      "select month, revenue from base order by 1",
    );

    await page.goto(`${liveApp.baseURL}/notebooks/${notebook.id}`);
    await page.getByRole("button", { name: "Run all" }).click();
    await page.waitForResponse(
      (response) => response.url().includes(`/api/notebooks/${notebook.id}/run`) && response.ok(),
      {
        timeout: 30000,
      },
    );
    // Result table renders the column.
    await expect(page.getByText("revenue", { exact: true }).first()).toBeVisible({
      timeout: 15000,
    });

    // Switching to a bar chart writes a @viz directive into the chart cell.
    const updateSeen = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/notebooks/${notebook.id}/cells/${chartCell}`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await page.getByRole("button", { name: "bar", exact: true }).last().click();
    await updateSeen;
    // The directive landed in the cell file.
    const afterViz = (await (
      await page.request.get(`${liveApp.baseURL}/api/notebooks/${notebook.id}`)
    ).json()) as NotebookEnvelope;
    const chartContent = afterViz.notebook.cells.find((cell) => cell.cell_id === chartCell)!;
    expect(chartContent.content).toContain("@viz(bar");

    // Rename base → revenue and confirm the chart cell's reference updated.
    const renamed = await page.request.post(
      `${liveApp.baseURL}/api/notebooks/${notebook.id}/cells/${baseCell}/rename`,
      {
        data: { name: "revenue" },
      },
    );
    expect(renamed.ok()).toBe(true);

    const final = (await (
      await page.request.get(`${liveApp.baseURL}/api/notebooks/${notebook.id}`)
    ).json()) as NotebookEnvelope;
    const chartFinal = final.notebook.cells.find((cell) => cell.cell_id === chartCell)!;
    expect(chartFinal.content).toContain("from revenue");
    expect(final.notebook.cells.find((cell) => cell.cell_id === baseCell)!.name).toBe("revenue");
  });

  test("promote a cell into a pipeline asset and rewrite remaining references", async ({
    liveApp,
    page,
  }) => {
    const notebook = await createNotebook(page.request, liveApp.baseURL, "Promotion");
    const baseCell = await addCell(page.request, liveApp.baseURL, notebook.id, "base");
    await setCell(
      page.request,
      liveApp.baseURL,
      notebook.id,
      baseCell,
      "select 1 as id, 10 as amount",
    );
    const childCell = await addCell(page.request, liveApp.baseURL, notebook.id, "child");
    await setCell(
      page.request,
      liveApp.baseURL,
      notebook.id,
      childCell,
      "select sum(amount) as total from base",
    );

    const pipelineId = Buffer.from("analytics").toString("base64url");
    const response = await page.request.post(
      `${liveApp.baseURL}/api/notebooks/${notebook.id}/cells/${baseCell}/promote`,
      { data: { pipeline_id: pipelineId, target_name: "marts.promoted_base" } },
    );
    expect(response.ok()).toBe(true);
    const result = (await response.json()) as {
      status: string;
      asset_path: string;
      notebook: NotebookEnvelope["notebook"];
    };
    expect(result.status).toBe("ok");
    expect(result.asset_path).toContain("analytics/assets/");

    // The promoted asset now shows up as a pipeline asset, class=pipeline.
    const workspace = (await (
      await page.request.get(`${liveApp.baseURL}/api/workspace`)
    ).json()) as {
      pipelines: Array<{ assets: Array<{ name: string; class?: string }> }>;
    };
    const promoted = workspace.pipelines
      .flatMap((p) => p.assets)
      .find((asset) => asset.name === "marts.promoted_base");
    expect(promoted?.class).toBe("pipeline");

    // The remaining child cell references the promoted pipeline asset.
    const child = result.notebook.cells.find((cell) => cell.cell_id === childCell)!;
    expect(child.content).toContain("from marts.promoted_base");
    expect(result.notebook.cells.some((cell) => cell.cell_id === baseCell)).toBe(false);
  });

  test("manage Python dependencies via the dialog and the missing-import suggestion", async ({
    liveApp,
    page,
  }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Deps");
    const fetchCell = await addPythonCell(request, liveApp.baseURL, notebook.id, "fetch");
    await setPythonCell(
      request,
      liveApp.baseURL,
      notebook.id,
      fetchCell,
      "import os\nimport requests\n\n\ndef materialize():\n    return requests.get(os.environ['URL']).json()",
    );

    await page.goto(`${liveApp.baseURL}/notebooks/${notebook.id}`);
    await expect(page.getByText("Deps").first()).toBeVisible({ timeout: 15000 });

    // The cell imports `requests`, which is not declared → a suggestion appears.
    await expect(page.getByText("Imported but not in dependencies:")).toBeVisible({
      timeout: 15000,
    });
    const importButton = page.getByRole("button", { name: "requests" });
    await expect(importButton).toBeVisible();

    // Clicking it searches PyPI and offers candidate packages; picking one adds
    // it to the dependencies and clears the suggestion.
    await importButton.click();
    const candidate = page.getByRole("menuitem", { name: /requests/i }).first();
    await expect(candidate).toBeVisible({ timeout: 20000 });
    const addSaved = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/notebooks/${notebook.id}/dependencies`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await candidate.click();
    await addSaved;
    expect(await getDependencies(request, liveApp.baseURL, notebook.id)).toContain("requests");
    await expect(page.getByText("Imported but not in dependencies:")).toBeHidden({
      timeout: 15000,
    });

    // The Dependencies dialog edits the dependency list directly.
    await page.getByRole("button", { name: "Dependencies" }).click();
    const editor = page.getByLabel("dependencies", { exact: true });
    await expect(editor).toBeVisible();
    await editor.fill("pandas\nrequests\nnumpy");
    const dialogSaved = page.waitForResponse(
      (response) =>
        response.url().includes(`/api/notebooks/${notebook.id}/dependencies`) &&
        response.request().method() === "PUT" &&
        response.ok(),
      { timeout: 15000 },
    );
    await page.getByRole("button", { name: "Save" }).click();
    await dialogSaved;

    const dependencies = await getDependencies(request, liveApp.baseURL, notebook.id);
    expect(dependencies).toContain("numpy");
    expect(dependencies).toContain("pandas");
  });

  test("resolves imports against the venv so import≠package names are not flagged", async ({
    liveApp,
    page,
  }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Resolve");

    // Simulate an installed package whose import name differs from its PyPI
    // name (skimage is provided by scikit-image) by dropping it into the
    // notebook's venv site-packages.
    const venvSite = join(
      liveApp.workspaceDir,
      ".renart",
      "notebooks",
      "venvs",
      basename(notebook.path),
      "lib",
      "python3.11",
      "site-packages",
      "skimage",
    );
    mkdirSync(venvSite, { recursive: true });
    writeFileSync(join(venvSite, "__init__.py"), "# skimage\n");

    const cell = await addPythonCell(request, liveApp.baseURL, notebook.id, "imps");
    await setPythonCell(
      request,
      liveApp.baseURL,
      notebook.id,
      cell,
      "import skimage\nimport totally_made_up_pkg\n\n\ndef materialize():\n    return None",
    );

    await page.goto(`${liveApp.baseURL}/notebooks/${notebook.id}`);
    await expect(page.getByText("Resolve").first()).toBeVisible({ timeout: 15000 });

    // The unresolved import is flagged; the installed one (import≠package) is not.
    await expect(page.getByText("Imported but not in dependencies:")).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByRole("button", { name: "totally_made_up_pkg" })).toBeVisible();
    await expect(page.getByRole("button", { name: "skimage" })).toHaveCount(0);
  });

  test("the Python language server resolves notebook cell imports against the notebook venv", async ({
    liveApp,
    page,
  }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "LSP");

    // Drop a package into the notebook's per-notebook venv site-packages so the
    // language server (ty) can resolve it. The venv lives outside the notebook
    // folder, so this exercises the notebook-venv search root specifically.
    const venvSite = join(
      liveApp.workspaceDir,
      ".renart",
      "notebooks",
      "venvs",
      basename(notebook.path),
      "lib",
      "python3.11",
      "site-packages",
      "marshmallow",
    );
    mkdirSync(venvSite, { recursive: true });
    writeFileSync(join(venvSite, "__init__.py"), "value = 1\n");

    const cellId = await addPythonCell(request, liveApp.baseURL, notebook.id, "lsp");
    const assetId = await getCellAssetId(request, liveApp.baseURL, notebook.id, cellId);

    // The installed package resolves (no unresolved-import); a missing one does
    // not — the language server is wired to the notebook cell and its venv.
    const diagnostics = await pythonDiagnostics(
      request,
      liveApp.baseURL,
      assetId,
      "import marshmallow\nimport totally_made_up_pkg\n",
    );
    expect(diagnostics.status).toBe("ok");
    const messages = (diagnostics.diagnostics ?? []).map((diagnostic) => diagnostic.message);
    expect(messages.join("\n")).toContain("totally_made_up_pkg");
    expect(messages.join("\n")).not.toContain("marshmallow");
  });

  test("promote dialog can pull in downstream assets", async ({ liveApp, page }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Promote Chain");
    const baseCell = await addCell(request, liveApp.baseURL, notebook.id, "base");
    await setCell(request, liveApp.baseURL, notebook.id, baseCell, "select 1 as id, 10 as amount");
    const childCell = await addCell(request, liveApp.baseURL, notebook.id, "child");
    await setCell(
      request,
      liveApp.baseURL,
      notebook.id,
      childCell,
      "select sum(amount) as total from base",
    );

    await page.goto(`${liveApp.baseURL}/notebooks/${notebook.id}`);
    await expect(page.getByText("Promote Chain").first()).toBeVisible({ timeout: 15000 });

    // Open the base cell's actions menu and start a promotion.
    const baseCard = page
      .locator('[data-slot="delimited-card"]')
      .filter({ has: page.getByRole("button", { name: "base", exact: true }) });
    await baseCard.getByRole("button", { name: "Cell actions" }).click();
    await page.getByRole("menuitem", { name: "Promote to pipeline" }).click();

    // The dialog offers to also promote the downstream cell.
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Promote to pipeline")).toBeVisible();
    await dialog.getByRole("checkbox", { name: /Downstream assets/ }).click();
    const promoteResponse = page.waitForResponse(
      (response) => response.url().includes(`/cells/${baseCell}/promote`) && response.ok(),
      { timeout: 30000 },
    );
    await dialog.getByRole("button", { name: "Promote", exact: true }).click();
    const result = (await (await promoteResponse).json()) as { promoted_count: number };
    expect(result.promoted_count).toBe(2);

    // Both cells became pipeline assets in the same schema; the downstream
    // asset reads its upstream by the new name.
    const workspace = (await (await request.get(`${liveApp.baseURL}/api/workspace`)).json()) as {
      pipelines: Array<{ assets: Array<{ name: string; content: string; class?: string }> }>;
    };
    const assets = workspace.pipelines.flatMap((pipeline) => pipeline.assets);
    expect(assets.find((asset) => asset.name === "marts.base")?.class).toBe("pipeline");
    const child = assets.find((asset) => asset.name === "marts.child");
    expect(child?.class).toBe("pipeline");
    expect(child?.content).toContain("from marts.base");
  });

  test("notebooks appear in the index and a notebook cell never reaches the catalog", async ({
    liveApp,
    page,
  }) => {
    const notebook = await createNotebook(page.request, liveApp.baseURL, "Catalog Isolation");
    const cellId = await addCell(page.request, liveApp.baseURL, notebook.id, "scratch");
    await setCell(page.request, liveApp.baseURL, notebook.id, cellId, "select 1 as x");

    // Index lists the notebook.
    await page.goto(`${liveApp.baseURL}/notebooks`);
    await expect(page.getByText("Catalog Isolation").first()).toBeVisible({ timeout: 15000 });

    // The workspace payload tags the cell as a notebook asset and keeps it
    // out of the pipelines list.
    const workspace = (await (
      await page.request.get(`${liveApp.baseURL}/api/workspace`)
    ).json()) as {
      pipelines: Array<{ assets: Array<{ name: string; class?: string }> }>;
      notebooks?: Array<{ title: string; cells: Array<{ name: string; class?: string }> }>;
    };
    const pipelineAssetNames = workspace.pipelines.flatMap((p) => p.assets.map((a) => a.name));
    expect(pipelineAssetNames).not.toContain("scratch");
    const isolation = workspace.notebooks?.find((nb) => nb.title === "Catalog Isolation");
    expect(isolation).toBeTruthy();
    expect(isolation!.cells[0].class).toBe("notebook");
  });
});
