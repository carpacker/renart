import { expect, type APIRequestContext } from "@playwright/test";

import { liveTest as test } from "../live-app-fixture";

async function createNotebook(request: APIRequestContext, baseURL: string, title: string) {
  const response = await request.post(`${baseURL}/api/notebooks`, { data: { title } });
  expect(response.ok()).toBe(true);
  return (await response.json()).notebook as { id: string };
}

async function addPythonCell(request: APIRequestContext, baseURL: string, notebookId: string, name: string) {
  const response = await request.post(`${baseURL}/api/notebooks/${notebookId}/cells`, {
    data: { name, language: "python" },
  });
  expect(response.ok()).toBe(true);
  const notebook = (await response.json()).notebook as { cells: Array<{ cell_id: string; name: string }> };
  return notebook.cells.find((cell) => cell.name === name)!.cell_id;
}

async function setPy(request: APIRequestContext, baseURL: string, notebookId: string, cellId: string, body: string) {
  expect((await request.put(`${baseURL}/api/notebooks/${notebookId}/cells/${cellId}`, { data: { content: `${body}\n` } })).ok()).toBe(true);
}

test.describe("notebook python logs", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("python stdout is captured and shown as collapsible output", async ({ liveApp, page }) => {
    const { request } = page;
    const notebook = await createNotebook(request, liveApp.baseURL, "Py Logs");
    const cell = await addPythonCell(request, liveApp.baseURL, notebook.id, "printer");
    await setPy(
      request,
      liveApp.baseURL,
      notebook.id,
      cell,
      "import pandas as pd\n\n\ndef materialize():\n    print('notebook stdout marker')\n    return pd.DataFrame({'x': [1, 2]})",
    );

    await page.goto(`${liveApp.baseURL}/notebooks/${notebook.id}`);
    await expect(page.getByText("Py Logs").first()).toBeVisible({ timeout: 15000 });

    // First run installs the venv via uv, so allow generous time.
    const runResponse = page.waitForResponse(
      (response) => response.url().includes(`/api/notebooks/${notebook.id}/run`) && response.ok(),
      { timeout: 180000 },
    );
    await page.getByRole("button", { name: "Run all" }).click();
    const payload = (await (await runResponse).json()) as {
      results: Array<{ name: string; status: string; logs?: string; error?: string }>;
    };
    const result = payload.results.find((entry) => entry.name === "printer")!;
    expect(result.status, `run error: ${result.error ?? ""}`).toBe("ok");
    expect(result.logs ?? "").toContain("notebook stdout marker");

    // The Output section is present and collapsed by default (success run): the
    // log body is not rendered until expanded (the marker also appears in the
    // editor source, so scope the assertion to the log pre).
    const toggle = page.getByRole("button", { name: "Output" });
    await expect(toggle).toBeVisible({ timeout: 15000 });
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(page.getByTestId("cell-logs")).toHaveCount(0);

    // Expanding reveals the captured stdout.
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await expect(page.getByTestId("cell-logs")).toContainText("notebook stdout marker");
  });
});
