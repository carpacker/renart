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
  run: { id: string; status: string; error?: string };
  steps?: Array<{ asset: string; status: string }>;
};

// A workspace whose only pipeline runs a Python asset that sleeps, so a
// triggered run stays "running" long enough to kill the server mid-execution.
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
    `name: sleeper\nschedule: daily\nstart_date: "${today}"\ncatchup: false\ndefault_connections:\n  duckdb: duckdb-default\n`,
  );
  writeFileSync(
    join(pipelineDir, "assets", "sleep.py"),
    `""" @bruin\nname: sleeper.sleep\ntype: python\n@bruin """\nimport time\n\ntime.sleep(60)\n`,
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
  test("a run left running by a killed server is reconciled to failed on restart", async () => {
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

      const triggerResponse = await ctxA.post(
        `${serverA.baseURL}/api/pipelines/${encodeURIComponent(sleeper.pipeline_id)}/trigger`,
        { data: { trigger: "manual" } },
      );
      expect(triggerResponse.ok()).toBe(true);
      const runId = ((await triggerResponse.json()) as { run: { id: string } }).run.id;
      expect(runId).toBeTruthy();

      // Wait until the asset itself is executing (a step marked running), not
      // just the run — so the kill genuinely lands while a task is in flight.
      const beforeKill = await pollRun(
        ctxA,
        serverA.baseURL,
        runId,
        (detail) => (detail.steps ?? []).some((step) => step.status === "running"),
        60000,
        "asset step running",
      );
      expect(beforeKill.run.status).toBe("running");
      const runningStep = beforeKill.steps!.find((step) => step.status === "running")!.asset;
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
      const reconciledStep = (detail.steps ?? []).find((step) => step.asset === runningStep);
      expect(reconciledStep?.status, "the in-flight step is closed").toBe("failed");
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
