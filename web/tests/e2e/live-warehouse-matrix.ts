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
const STARROCKS_IMAGE = "starrocks/allin1-ubuntu:3.5-latest";
const MINIO_IMAGE = "quay.io/minio/minio:RELEASE.2025-06-13T11-33-47Z";
const MINIO_CLIENT_IMAGE = "quay.io/minio/mc:RELEASE.2025-05-21T01-59-54Z";
const MINIO_ACCESS_KEY = "renart";
const MINIO_SECRET_KEY = "renart-secret";
const DUCKLAKE_BUCKET = "renart-ducklake";

export type LiveWarehouseMatrix = {
  postgresPort: number;
  clickhouseNativePort: number;
  clickhouseHTTPPort: number;
  trinoPort: number;
  starrocksMySQLPort: number;
  starrocksHTTPPort: number;
  starrocksStreamLoadPort: number;
  minioPort: number;
  dispose: () => Promise<void>;
};

export async function createLiveWarehouseMatrix(
  enabledWarehouses: Iterable<string> = [
    "duckdb",
    "ducklake",
    "postgres",
    "trino",
    "clickhouse",
    "starrocks",
  ],
): Promise<LiveWarehouseMatrix> {
  const enabled = new Set(enabledWarehouses);
  const suffix = randomUUID().slice(0, 8);
  const networkName = `renart-e2e-warehouse-${suffix}`;
  const postgresName = `renart-e2e-pg-${suffix}`;
  const clickhouseName = `renart-e2e-ch-${suffix}`;
  const trinoName = `renart-e2e-trino-${suffix}`;
  const starrocksName = `renart-e2e-starrocks-${suffix}`;
  const minioName = `renart-e2e-minio-${suffix}`;
  const [
    postgresPort,
    clickhouseNativePort,
    clickhouseHTTPPort,
    trinoPort,
    starrocksMySQLPort,
    starrocksHTTPPort,
    starrocksStreamLoadPort,
    minioPort,
  ] = await Promise.all(Array.from({ length: 8 }, () => getAvailablePort()));

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

    if (enabled.has("clickhouse")) {
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
    }

    if (enabled.has("trino")) {
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
    }

    if (enabled.has("starrocks")) {
      await runCommand([
        "docker",
        "run",
        "--rm",
        "-d",
        "--name",
        starrocksName,
        "--network",
        networkName,
        "--tmpfs",
        "/data/deploy/starrocks/be/storage:rw,noexec,nosuid,size=2g",
        "-p",
        `127.0.0.1:${starrocksMySQLPort}:9030`,
        "-p",
        `127.0.0.1:${starrocksHTTPPort}:8030`,
        "-p",
        `127.0.0.1:${starrocksStreamLoadPort}:8040`,
        STARROCKS_IMAGE,
      ]);
      containers.push(starrocksName);
      await waitForCommand([
        "docker",
        "exec",
        starrocksName,
        "bash",
        "-lc",
        `mysql -P 9030 -h 127.0.0.1 -u root -N -e "show backends" | grep -q $'\\ttrue\\t'`,
      ]);
      await runCommand([
        "docker",
        "exec",
        starrocksName,
        "mysql",
        "-P",
        "9030",
        "-h",
        "127.0.0.1",
        "-u",
        "root",
        "-e",
        "create database if not exists analytics",
      ]);
    }

    if (enabled.has("ducklake")) {
      await runCommand([
        "docker",
        "run",
        "--rm",
        "-d",
        "--name",
        minioName,
        "--network",
        networkName,
        "--network-alias",
        "minio",
        "-e",
        `MINIO_ROOT_USER=${MINIO_ACCESS_KEY}`,
        "-e",
        `MINIO_ROOT_PASSWORD=${MINIO_SECRET_KEY}`,
        "--tmpfs",
        "/data:rw,noexec,nosuid,size=512m",
        "-p",
        `127.0.0.1:${minioPort}:9000`,
        MINIO_IMAGE,
        "server",
        "/data",
      ]);
      containers.push(minioName);
      await waitForHTTP(`http://127.0.0.1:${minioPort}/minio/health/live`);
      await runCommand([
        "docker",
        "run",
        "--rm",
        "--network",
        networkName,
        "--entrypoint",
        "/bin/sh",
        MINIO_CLIENT_IMAGE,
        "-c",
        `mc alias set local http://minio:9000 ${MINIO_ACCESS_KEY} ${MINIO_SECRET_KEY} >/dev/null && mc mb --ignore-existing local/${DUCKLAKE_BUCKET}`,
      ]);
    }

    return {
      postgresPort,
      clickhouseNativePort,
      clickhouseHTTPPort,
      trinoPort,
      starrocksMySQLPort,
      starrocksHTTPPort,
      starrocksStreamLoadPort,
      minioPort,
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

async function waitForHTTP(url: string) {
  const deadline = Date.now() + 180_000;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
      lastError = new Error(`${url} returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 500));
  }
  throw lastError ?? new Error(`Timed out waiting for ${url}`);
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
