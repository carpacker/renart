import { expect, test, request as apiRequest, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  liveServerRepoRoot,
  startLiveServer,
  stopLiveServer,
  type SpawnedServer,
} from "../live-app-fixture";

type RunDetail = {
  status: "ok" | "error";
  run: { id: string; status: string; error?: string };
  steps?: Array<{ asset: string; status: string }>;
};

function findDuckDBBinary(): string | null {
  if (process.env.DUCKDB_BINARY && existsSync(process.env.DUCKDB_BINARY)) {
    return process.env.DUCKDB_BINARY;
  }
  try {
    return execFileSync("sh", ["-c", "command -v duckdb"], { encoding: "utf8" }).trim();
  } catch {
    return null;
  }
}

function buildContendingWorkspace(): {
  workspaceDir: string;
  databasePath: string;
  fakeSlingPath: string;
  lockMarkerPath: string;
} {
  const root = join(liveServerRepoRoot, ".playwright-live-workspaces");
  mkdirSync(root, { recursive: true });
  const workspaceDir = mkdtempSync(join(root, "renart-duckdb-coordination-"));
  mkdirSync(join(workspaceDir, ".git"));
  mkdirSync(join(workspaceDir, "duckdb-files"));
  const databasePath = join(workspaceDir, "duckdb-files", "shared.duckdb");
  const lockMarkerPath = join(workspaceDir, "sling-holds-duckdb-lock");
  writeFileSync(
    join(workspaceDir, ".bruin.yml"),
    "environments:\n  default:\n    connections:\n      duckdb:\n        - name: duckdb-default\n          path: duckdb-files/shared.duckdb\n",
  );
  writeFileSync(join(workspaceDir, "source.csv"), "id\n1\n");

  const loaderDir = join(workspaceDir, "loader");
  mkdirSync(join(loaderDir, "assets"), { recursive: true });
  writeFileSync(
    join(loaderDir, "pipeline.yml"),
    `name: loader
schedule: daily
start_date: "2024-01-01"
catchup: false
default_connections:
  duckdb: duckdb-default
`,
  );
  writeFileSync(
    join(loaderDir, "assets", "load.asset.yml"),
    `name: loader.loaded
type: load
connection: duckdb-default
materialization:
  type: table
  strategy: create+replace
parameters:
  source_connection: local
  source_table: ${join(workspaceDir, "source.csv")}
`,
  );

  const writerDir = join(workspaceDir, "writer");
  mkdirSync(join(writerDir, "assets"), { recursive: true });
  writeFileSync(
    join(writerDir, "pipeline.yml"),
    `name: writer
schedule: daily
start_date: "2024-01-01"
catchup: false
default_connections:
  duckdb: duckdb-default
`,
  );
  writeFileSync(
    join(writerDir, "assets", "write.sql"),
    `/* @bruin
name: writer.written
type: duckdb.sql
materialization:
  type: table
@bruin */

select 42 as value
`,
  );

  const fakeSlingPath = join(workspaceDir, "fake-sling.sh");
  writeFileSync(
    fakeSlingPath,
    `#!/bin/sh
set -eu
target=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--tgt-conn" ]; then
    shift
    target="$1"
  fi
  shift
done
database="\${target#duckdb:///}"
"$DUCKDB_BIN" "$database" <<SQL
.shell touch "$SLING_LOCK_MARKER"
.shell sleep 2
create schema if not exists loader;
create or replace table loader.loaded as select 1 as id;
SQL
`,
  );
  chmodSync(fakeSlingPath, 0o755);

  return { workspaceDir, databasePath, fakeSlingPath, lockMarkerPath };
}

async function waitForRun(
  request: APIRequestContext,
  baseURL: string,
  runId: string,
): Promise<RunDetail> {
  let last: RunDetail | null = null;
  await expect
    .poll(
      async () => {
        const response = await request.get(`${baseURL}/api/runs/${encodeURIComponent(runId)}`);
        if (!response.ok()) return "missing";
        last = (await response.json()) as RunDetail;
        return last.run.status;
      },
      { timeout: 90000 },
    )
    .toMatch(/^(success|failed|cancelled)$/);
  if (!last) throw new Error(`run ${runId} disappeared`);
  return last;
}

test.describe("DuckDB pipeline coordination (live)", () => {
  test("serializes a child-process load and another pipeline writing the same file", async () => {
    test.skip(test.info().project.name.includes("mobile"), "Backend concurrency needs one run.");
    test.setTimeout(150000);
    const duckDBBinary = findDuckDBBinary();
    test.skip(!duckDBBinary, "DuckDB CLI is required for the live lock contention test.");

    const { workspaceDir, databasePath, fakeSlingPath, lockMarkerPath } =
      buildContendingWorkspace();
    let server: SpawnedServer | null = null;
    try {
      server = await startLiveServer(workspaceDir, {
        RENART_SLING_BINARY: fakeSlingPath,
        DUCKDB_BIN: duckDBBinary!,
        SLING_LOCK_MARKER: lockMarkerPath,
      });
      const request = await apiRequest.newContext();
      const schedulesResponse = await request.get(`${server.baseURL}/api/schedules`);
      expect(schedulesResponse.ok()).toBe(true);
      const schedules = (await schedulesResponse.json()) as {
        schedules: Array<{ pipeline_id: string; pipeline_name: string }>;
      };
      const loader = schedules.schedules.find((item) => item.pipeline_name === "loader");
      const writer = schedules.schedules.find((item) => item.pipeline_name === "writer");
      expect(loader).toBeTruthy();
      expect(writer).toBeTruthy();

      const loaderResponse = await request.post(
        `${server.baseURL}/api/pipelines/${encodeURIComponent(loader!.pipeline_id)}/trigger`,
        { data: { trigger: "manual", environment: "default" } },
      );
      expect(loaderResponse.ok()).toBe(true);
      const loaderRunId = ((await loaderResponse.json()) as { run: { id: string } }).run.id;

      await expect.poll(() => existsSync(lockMarkerPath), { timeout: 30000 }).toBe(true);

      const writerResponse = await request.post(
        `${server.baseURL}/api/pipelines/${encodeURIComponent(writer!.pipeline_id)}/trigger`,
        { data: { trigger: "manual", environment: "default" } },
      );
      expect(writerResponse.ok()).toBe(true);
      const writerRunId = ((await writerResponse.json()) as { run: { id: string } }).run.id;

      const [loaderRun, writerRun] = await Promise.all([
        waitForRun(request, server.baseURL, loaderRunId),
        waitForRun(request, server.baseURL, writerRunId),
      ]);
      expect(loaderRun.run.status).toBe("success");
      expect(writerRun.run.status).toBe("success");
      expect(loaderRun.steps?.find((step) => step.asset === "loader.loaded")?.status).toBe(
        "success",
      );
      expect(writerRun.steps?.find((step) => step.asset === "writer.written")?.status).toBe(
        "success",
      );

      const tables = execFileSync(
        duckDBBinary!,
        [
          databasePath,
          "select table_schema || '.' || table_name from information_schema.tables where table_schema in ('loader', 'writer') order by 1",
        ],
        { encoding: "utf8" },
      );
      expect(tables).toContain("loader.loaded");
      expect(tables).toContain("writer.written");
      await request.dispose();
    } finally {
      if (server) {
        await stopLiveServer(server, "SIGTERM").catch(() => undefined);
      }
      rmSync(workspaceDir, { recursive: true, force: true });
    }
  });
});
