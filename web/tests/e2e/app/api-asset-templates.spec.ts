import { expect, test } from "@playwright/test";

import { buildAPIAssetTemplate } from "../../../lib/api-asset-templates";

test("OpenAPI asset template uses the provided spec URL", () => {
  const content = buildAPIAssetTemplate(
    "openapi",
    "duckdb-default",
    "https://api.example.com/openapi.json?version=1&format=yaml",
  );

  expect(content).toContain("connection: duckdb-default");
  expect(content).toContain('url: "https://api.example.com/openapi.json?version=1&format=yaml"');
  expect(content).toContain('request:\n    url: ""');
  expect(content).not.toContain("api.weather.gov");
});
