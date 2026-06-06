import { expect, type Page } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { liveTest as test } from "../live-app-fixture";

const pythonAssetPath = "analytics/assets/analytics/typecheck_task.py";
const pythonAssetName = "analytics.typecheck_task";

test.describe("python intellisense live", () => {
  test.use({ fixtureName: "configured-workspace" });

  test("shows ty diagnostics and formats Python assets in Monaco", async ({
    liveApp,
    page,
  }) => {
    await writeFile(
      join(liveApp.workspaceDir, pythonAssetPath),
      `"""@bruin
name: ${pythonAssetName}
type: python
image: python:3.11
@bruin"""

import pandas as pd

def returns_int(value:str)->int:
 return value

df = pd.DataFrame({"a": [1]})
`,
      "utf8"
    );
    await writeFile(
      join(liveApp.workspaceDir, "analytics", "assets", "analytics", "requirements.txt"),
      "pandas\n",
      "utf8"
    );
    const fakePandasPath = join(
      liveApp.workspaceDir,
      ".venv",
      "lib",
      "python3.11",
      "site-packages",
      "pandas"
    );
    await mkdir(fakePandasPath, { recursive: true });
    await writeFile(
      join(fakePandasPath, "__init__.py"),
      "from pandas.core.api import (\n    Series,\n    DataFrame,\n    Index,\n)\n",
      "utf8"
    );
    await mkdir(join(fakePandasPath, "core"), { recursive: true });
    await writeFile(
      join(fakePandasPath, "core", "api.py"),
      "from pandas.core.frame import DataFrame\nfrom pandas.core.indexes.base import Index\n",
      "utf8"
    );
    await mkdir(join(fakePandasPath, "core", "indexes"), { recursive: true });
    await writeFile(
      join(fakePandasPath, "core", "frame.py"),
      "class DataFrame:\n    def __init__(self, data=None, index=None, columns=None, dtype=None, copy=None): ...\n    columns = properties.AxisProperty(\n        doc=\"\"\"\n        Returns\n        -------\n        pandas.Index\n            The column labels.\n        \"\"\"\n    )\n    def head(self): ...\n    def merge(self): ...\n",
      "utf8"
    );
    await writeFile(
      join(fakePandasPath, "core", "indexes", "base.py"),
      "class Index:\n    name = None\n    def to_list(self): ...\n    def unique(self): ...\n",
      "utf8"
    );

    await page.goto(`${liveApp.baseURL}/`);
    const workspaceAsset = await waitForWorkspaceAsset(page, pythonAssetName);
    await openPythonEditor(page, liveApp.baseURL, workspaceAsset);

    await expect
      .poll(async () => await getPythonTyMarkers(page), { timeout: 15000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            owner: "bruin-python-ty",
            message: expect.stringMatching(/str|int|assignable|return/i),
          }),
        ])
      );

    const markerMessages = await getPythonTyMarkers(page);
    expect(markerMessages.map((marker) => marker.message).join("\n")).not.toContain(
      "Cannot resolve imported module `pandas`"
    );

    await formatDocument(page);

    await expect
      .poll(async () => normalizeTrailingWhitespace(await getEditorValue(page)), {
        timeout: 10000,
      })
      .toContain("import pandas as pd\n\n\ndef returns_int(value: str) -> int:\n    return value");

    await expectPythonCompletion(page, "returns_", "returns_int");
    await expectPythonCompletion(page, "pd.", "DataFrame");
    await expectPythonCompletion(page, "pd.DataFrame(col", "columns");
    await expectPythonCompletion(page, "x = pd.DataFrame()\nx.", "head");
    await expectPythonCompletion(page, "x = pd.DataFrame()\nb = x.columns\nb.", "to_list");
  });
});

async function openPythonEditor(
  page: Page,
  baseURL: string,
  workspaceAsset: { pipelineId: string; assetId: string }
) {
  const assetURL = `${baseURL}/?pipeline=${encodeURIComponent(
    workspaceAsset.pipelineId
  )}&asset=${encodeURIComponent(workspaceAsset.assetId)}`;
  await page.goto(assetURL);

  if (test.info().project.name.includes("mobile")) {
    const editorDialog = page.getByRole("dialog", { name: "Asset Editor" });
    if (!(await editorDialog.isVisible().catch(() => false))) {
      const editButton = page.getByRole("button", { name: "Edit asset" });
      if (await editButton.isVisible().catch(() => false)) {
        await editButton.click();
      }
    }
    await expect(editorDialog).toBeVisible({ timeout: 15000 });
  }

  await expect(page.getByTestId("editor-asset-name")).toHaveText(pythonAssetName, {
    timeout: 15000,
  });
  await waitForEditorReady(page);
}

async function waitForWorkspaceAsset(page: Page, assetName: string) {
  let foundAsset: { pipelineId: string; assetId: string } | null = null;
  await expect
    .poll(async () => {
      foundAsset = await page.evaluate(async (targetAssetName) => {
        const response = await fetch("/api/workspace", { cache: "no-store" });
        const workspace = (await response.json()) as {
          pipelines?: Array<{ id?: string; assets?: Array<{ id?: string; name?: string }> }>;
        };

        for (const pipeline of workspace.pipelines ?? []) {
          for (const asset of pipeline.assets ?? []) {
            if (pipeline.id && asset.id && asset.name === targetAssetName) {
              return { pipelineId: pipeline.id, assetId: asset.id };
            }
          }
        }

        return null;
      }, assetName);
      return foundAsset?.assetId ?? null;
    }, { timeout: 15000 })
    .not.toBeNull();

  if (!foundAsset) {
    throw new Error(`Workspace asset not found: ${assetName}`);
  }
  return foundAsset;
}

async function waitForEditorReady(page: Page) {
  const editor = page.locator(".monaco-editor").first();
  await expect(editor).toBeVisible({ timeout: 15000 });
  await expect(page.locator(".view-lines").first()).toBeVisible({ timeout: 15000 });
  return editor;
}

async function getPythonTyMarkers(page: Page) {
  return await page.evaluate<Array<{ owner?: string; message: string; severity: number }>>(() => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    const model = editor?.getModel();
    if (!monaco || !model) return [];
    return monaco.editor
      .getModelMarkers({ resource: model.uri })
      .filter((marker: { owner?: string }) => marker.owner === "bruin-python-ty")
      .map((marker: { owner?: string; message: string; severity: number }) => ({
        owner: marker.owner,
        message: marker.message,
        severity: marker.severity,
      }));
  });
}

async function getEditorValue(page: Page) {
  return await page.evaluate(() => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    return editor?.getModel()?.getValue() ?? "";
  });
}

async function formatDocument(page: Page) {
  await page.evaluate(async () => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    await editor?.getAction("editor.action.formatDocument")?.run();
  });
}

async function expectPythonCompletion(page: Page, prefix: string, expectedLabel: string) {
  await page.evaluate((completionPrefix) => {
    const monaco = (window as typeof window & { monaco?: any }).monaco;
    const editor = monaco?.editor.getEditors?.()[0];
    const model = editor?.getModel();
    if (!editor || !model) return;
    const lineNumber = model.getLineCount();
    const column = model.getLineMaxColumn(lineNumber);
    editor.executeEdits("test", [
      {
        range: new monaco.Range(lineNumber, column, lineNumber, column),
        text: `\n${completionPrefix}`,
      },
    ]);
    const insertedLines = completionPrefix.split("\n");
    editor.setPosition({
      lineNumber: lineNumber + insertedLines.length,
      column: insertedLines[insertedLines.length - 1].length + 1,
    });
    editor.trigger("test", "editor.action.triggerSuggest", {});
  }, prefix);

  await expect(page.locator(".suggest-widget .monaco-list-row").filter({ hasText: expectedLabel }).first()).toBeVisible({
    timeout: 15000,
  });
}

function normalizeTrailingWhitespace(value: string) {
  return value
    .split("\n")
    .map((line) => line.trimEnd())
    .join("\n");
}
