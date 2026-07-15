import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import net from "node:net";
import { resolve } from "node:path";

const webDir = resolve(__dirname, "..", "..");
const repoRoot = resolve(webDir, "..");
const trinoCatalogDir = resolve(
  webDir,
  "tests",
  "fixtures",
  "multi-warehouse-workspace",
  "trino-catalog",
);

const POSTGRES_IMAGE = "postgres:16-alpine";
const CLICKHOUSE_IMAGE = "clickhouse/clickhouse-server:25.8.28-alpine";
const TRINO_IMAGE = "trinodb/trino:482";

export type LiveWarehouseMatrix = {
  postgresPort: number;
  clickhouseNativePort: number;
  clickhouseHTTPPort: number;
  trinoPort: number;
  dispose: () => Promise<void>;
};

export async function createLiveWarehouseMatrix(): Promise<LiveWarehouseMatrix> {
  const suffix = randomUUID().slice(0, 8);
  const networkName = `renart-e2e-warehouse-${suffix}`;
  const postgresName = `renart-e2e-pg-${suffix}`;
  const clickhouseName = `renart-e2e-ch-${suffix}`;
  const trinoName = `renart-e2e-trino-${suffix}`;
  const [postgresPort, clickhouseNativePort, clickhouseHTTPPort, trinoPort] = await Promise.all([
    getAvailablePort(),
    getAvailablePort(),
    getAvailablePort(),
    getAvailablePort(),
  ]);

  const containers: string[] = [];
  let networkCreated = false;
  const dispose = async () => {
    for (const container of containers.reverse()) {
      await runCommand(["docker", "rm", "-f", container], true);
    }
    if (networkCreated) {
      await runCommand(["docker", "network", "rm", networkName], true);
    }
  };

  try {
    await runCommand(["docker", "network", "create", networkName]);
    networkCreated = true;

    await runCommand([
      "docker",
      "run",
      "--rm",
      "-d",
      "--name",
      postgresName,
      "--network",
      networkName,
      "--network-alias",
      "postgres",
      "-e",
      "POSTGRES_DB=renart_postgres",
      "-e",
      "POSTGRES_USER=postgres",
      "-e",
      "POSTGRES_PASSWORD=postgres",
      "--tmpfs",
      "/var/lib/postgresql/data:rw,noexec,nosuid,size=256m",
      "-p",
      `127.0.0.1:${postgresPort}:5432`,
      POSTGRES_IMAGE,
    ]);
    containers.push(postgresName);
    await waitForCommand([
      "docker",
      "exec",
      postgresName,
      "psql",
      "-U",
      "postgres",
      "-d",
      "renart_postgres",
      "-tAc",
      "select 1",
    ]);
    await runCommand([
      "docker",
      "exec",
      postgresName,
      "psql",
      "-U",
      "postgres",
      "-d",
      "renart_postgres",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      "create schema if not exists analytics;",
    ]);
    await runCommand([
      "docker",
      "exec",
      postgresName,
      "psql",
      "-U",
      "postgres",
      "-d",
      "renart_postgres",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      "create database renart_source;",
    ]);
    await runCommand([
      "docker",
      "exec",
      postgresName,
      "psql",
      "-U",
      "postgres",
      "-d",
      "renart_source",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      `create schema if not exists analytics;
create table analytics.customer_activity_source (
  customer_id integer primary key,
  activity_score integer not null
);
insert into analytics.customer_activity_source (customer_id, activity_score) values
  (1, 5),
  (2, 7),
  (3, 3),
  (4, 4),
  (5, 6);`,
    ]);

    await runCommand([
      "docker",
      "run",
      "--rm",
      "-d",
      "--name",
      clickhouseName,
      "--network",
      networkName,
      "-e",
      "CLICKHOUSE_DB=analytics",
      "-e",
      "CLICKHOUSE_USER=renart",
      "-e",
      "CLICKHOUSE_PASSWORD=renart",
      "-e",
      "CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1",
      "--tmpfs",
      "/var/lib/clickhouse:rw,noexec,nosuid,size=512m",
      "-p",
      `127.0.0.1:${clickhouseNativePort}:9000`,
      "-p",
      `127.0.0.1:${clickhouseHTTPPort}:8123`,
      CLICKHOUSE_IMAGE,
    ]);
    containers.push(clickhouseName);
    await waitForCommand([
      "docker",
      "exec",
      clickhouseName,
      "clickhouse-client",
      "--user",
      "renart",
      "--password",
      "renart",
      "--query",
      "select 1",
    ]);

    await runCommand([
      "docker",
      "run",
      "--rm",
      "-d",
      "--name",
      trinoName,
      "--network",
      networkName,
      "-p",
      `127.0.0.1:${trinoPort}:8080`,
      "--volume",
      `${trinoCatalogDir}:/etc/trino/catalog:ro`,
      TRINO_IMAGE,
    ]);
    containers.push(trinoName);
    await waitForCommand(["docker", "exec", trinoName, "trino", "--execute", "select 1"]);
    await runCommand([
      "docker",
      "exec",
      trinoName,
      "trino",
      "--catalog",
      "memory",
      "--execute",
      "create schema if not exists analytics",
    ]);

    return {
      postgresPort,
      clickhouseNativePort,
      clickhouseHTTPPort,
      trinoPort,
      dispose,
    };
  } catch (error) {
    await dispose();
    throw error;
  }
}

async function waitForCommand(args: string[]) {
  const deadline = Date.now() + 180_000;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      await runCommand(args);
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 500));
    }
  }
  throw lastError ?? new Error(`Timed out waiting for ${args.join(" ")}`);
}

function getAvailablePort() {
  return new Promise<number>((resolvePort, reject) => {
    const server = net.createServer();
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close();
        reject(new Error("Could not allocate a live warehouse port."));
        return;
      }
      server.close(() => resolvePort(address.port));
    });
    server.on("error", reject);
  });
}

function runCommand(args: string[], allowFailure = false) {
  return new Promise<void>((resolveRun, rejectRun) => {
    const child = spawn(args[0], args.slice(1), {
      cwd: repoRoot,
      env: process.env,
      stdio: "pipe",
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += String(chunk);
    });
    child.stderr.on("data", (chunk) => {
      stderr += String(chunk);
    });
    child.on("exit", (code) => {
      if (code === 0 || allowFailure) {
        resolveRun();
        return;
      }
      rejectRun(
        new Error(
          [stderr.trim(), stdout.trim(), `${args.join(" ")} failed with exit code ${code}`]
            .filter(Boolean)
            .join("\n"),
        ),
      );
    });
    child.on("error", rejectRun);
  });
}
