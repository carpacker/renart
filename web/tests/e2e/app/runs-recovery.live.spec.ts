import { expect, test, request as apiRequest, type APIRequestContext } from "@playwright/test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  liveServerRepoRoot,
  startLiveServer,
  stopLiveServer,
  type SpawnedServer,
} from "../live-app-fixture";

type RunDetail = {
  status: "ok" | "error";
  run: { id: string; status: string; error?: string; snapshot_version_id?: string };
  steps?: Array<{ asset: string; status: string }>;
};

type AssetStaleness = {
  asset_name: string;
  status: string;
  last_run_status?: string;
  last_run_on_current_content?: boolean;
};

const finishedAsset = "sleeper.finished";
const interruptedAsset = "sleeper.interrupted";
const unreachedAsset = "sleeper.unreached";

// A deployed three-step pipeline: one materialization finishes durably, a
// Python step sleeps long enough to kill the server, and its downstream never
// starts. Recovery must replay those persisted facts without executing again.
function buildSleeperWorkspace(): string {
  const root = join(liveServerRepoRoot, ".playwright-live-workspaces");
  mkdirSync(root, { recursive: true });
  const dir = mkdtempSync(join(root, "renart-recovery-"));
  mkdirSync(join(dir, ".git"));
  mkdirSync(join(dir, "duckdb-files"));
  writeFileSync(
    join(dir, ".bruin.yml"),
    "environments:\n  default:\n    connections:\n      duckdb:\n        - name: duckdb-default\n          path: duckdb-files/local.db\n",
  );
  const pipelineDir = join(dir, "sleeper");
  mkdirSync(join(pipelineDir, "assets"), { recursive: true });
  const today = new Date().toISOString().slice(0, 10);
  writeFileSync(
    join(pipelineDir, "pipeline.yml"),
    `id: d64de5cf-ec85-45aa-a8bd-1a92a2d7f467\nname: sleeper\nschedule: daily\nstart_date: "${today}"\ncatchup: false\ndefault_connections:\n  duckdb: duckdb-default\n`,
  );
  writeFileSync(
    join(pipelineDir, "assets", "finished.sql"),
    `/* @bruin
name: ${finishedAsset}
type: duckdb.sql
materialization:
  type: table
@bruin */

select 1 as value
`,
  );
  writeFileSync(
    join(pipelineDir, "assets", "interrupted.py"),
    `""" @bruin
name: ${interruptedAsset}
type: python
depends:
  - ${finishedAsset}
@bruin """
import time

time.sleep(60)
`,
  );
  writeFileSync(
    join(pipelineDir, "assets", "unreached.sql"),
    `/* @bruin
name: ${unreachedAsset}
type: duckdb.sql
materialization:
  type: table
depends:
  - ${interruptedAsset}
@bruin */

select 3 as value
`,
  );
  return dir;
}

async function pollRun(
  ctx: APIRequestContext,
  baseURL: string,
  runId: string,
  predicate: (detail: RunDetail) => boolean,
  timeoutMs: number,
  label: string,
): Promise<RunDetail> {
  const deadline = Date.now() + timeoutMs;
  let last: RunDetail | null = null;
  while (Date.now() < deadline) {
    const response = await ctx.get(`${baseURL}/api/runs/${encodeURIComponent(runId)}`);
    if (response.ok()) {
      const detail = (await response.json()) as RunDetail;
      last = detail;
      if (detail.status === "ok" && predicate(detail)) {
        return detail;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 400));
  }
  throw new Error(`timed out waiting for run ${runId} (${label}): ${JSON.stringify(last)}`);
}

test.describe("pipeline run crash recovery (live)", () => {
  test("replays persisted completed and interrupted steps into freshness on restart", async () => {
    test.skip(test.info().project.name.includes("mobile"), "Backend recovery needs one run.");
    test.setTimeout(150000);
    const workspaceDir = buildSleeperWorkspace();
    let serverA: SpawnedServer | null = null;
    let serverB: SpawnedServer | null = null;
    try {
      serverA = await startLiveServer(workspaceDir);
      const ctxA = await apiRequest.newContext();

      const schedulesResponse = await ctxA.get(`${serverA.baseURL}/api/schedules`);
      expect(schedulesResponse.ok()).toBe(true);
      const schedules = (await schedulesResponse.json()) as {
        schedules: Array<{ pipeline_id: string; pipeline_name: string }>;
      };
      const sleeper =
        schedules.schedules.find((item) => item.pipeline_name === "sleeper") ??
        schedules.schedules[0];
      expect(sleeper, "sleeper pipeline should be scheduled").toBeTruthy();

      const deployResponse = await ctxA.post(
        `${serverA.baseURL}/api/pipelines/${encodeURIComponent(sleeper.pipeline_id)}/deploy`,
      );
      expect(deployResponse.ok()).toBe(true);
      const snapshotVersionId = (
        (await deployResponse.json()) as { snapshot: { version_id: string } }
      ).snapshot.version_id;
      expect(snapshotVersionId).toBeTruthy();

      const scheduledEnd = new Date();
      const scheduledStart = new Date(scheduledEnd.getTime() - 30 * 60 * 1000);
      const triggerResponse = await ctxA.post(
        `${serverA.baseURL}/api/pipelines/${encodeURIComponent(sleeper.pipeline_id)}/trigger`,
        {
          data: {
            trigger: "schedule",
            environment: "default",
            start: scheduledStart.toISOString(),
            end: scheduledEnd.toISOString(),
          },
        },
      );
      expect(triggerResponse.ok()).toBe(true);
      const runId = ((await triggerResponse.json()) as { run: { id: string } }).run.id;
      expect(runId).toBeTruthy();

      // Wait until the first asset is durably successful and the second is
      // executing. The third must still be absent, not pipeline-level pending.
      const beforeKill = await pollRun(
        ctxA,
        serverA.baseURL,
        runId,
        (detail) =>
          (detail.steps ?? []).some(
            (step) => step.asset === finishedAsset && step.status === "success",
          ) &&
          (detail.steps ?? []).some(
            (step) => step.asset === interruptedAsset && step.status === "running",
          ),
        60000,
        "asset step running",
      );
      expect(beforeKill.run.status).toBe("running");
      expect(beforeKill.run.snapshot_version_id).toBe(snapshotVersionId);
      expect(beforeKill.steps?.some((step) => step.asset === unreachedAsset)).toBe(false);
      await ctxA.dispose();

      // Hard-kill the server (a crash) while the task runs — the run row and
      // its step stay "running".
      await stopLiveServer(serverA, "SIGKILL");
      serverA = null;

      // Restart on the same workspace; startup recovery must fail the orphan
      // run and close its open step.
      serverB = await startLiveServer(workspaceDir);
      const ctxB = await apiRequest.newContext();
      const detail = await pollRun(
        ctxB,
        serverB.baseURL,
        runId,
        (current) => current.run.status === "failed",
        30000,
        "run failed after restart",
      );
      expect(detail.run.error ?? "").toContain("interrupted");
      expect(
        (detail.steps ?? []).find((step) => step.asset === finishedAsset)?.status,
        "the completed step stays successful",
      ).toBe("success");
      expect(
        (detail.steps ?? []).find((step) => step.asset === interruptedAsset)?.status,
        "the in-flight step is closed",
      ).toBe("failed");
      expect(detail.steps?.some((step) => step.asset === unreachedAsset)).toBe(false);

      await expect
        .poll(
          async () => {
            const response = await ctxB.get(
              `${serverB!.baseURL}/api/pipelines/${encodeURIComponent(sleeper.pipeline_id)}/staleness?environment=default`,
            );
            if (!response.ok()) return null;
            const body = (await response.json()) as { assets: AssetStaleness[] };
            const finished = body.assets.find((asset) => asset.asset_name === finishedAsset);
            const interrupted = body.assets.find((asset) => asset.asset_name === interruptedAsset);
            const unreached = body.assets.find((asset) => asset.asset_name === unreachedAsset);
            return {
              finished: `${finished?.status}/${finished?.last_run_status}/${finished?.last_run_on_current_content}`,
              interrupted: `${interrupted?.last_run_status}/${interrupted?.last_run_on_current_content}`,
              unreached: unreached?.last_run_status ?? "none",
            };
          },
          { timeout: 30000 },
        )
        .toEqual({
          finished: "fresh/succeeded/true",
          interrupted: "failed/true",
          unreached: "none",
        });
      await ctxB.dispose();
    } finally {
      if (serverA) {
        await stopLiveServer(serverA, "SIGKILL").catch(() => undefined);
      }
      if (serverB) {
        await stopLiveServer(serverB, "SIGTERM").catch(() => undefined);
      }
      rmSync(workspaceDir, { recursive: true, force: true });
    }
  });
});
