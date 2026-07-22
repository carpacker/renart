import { copyFile, mkdir, readFile, stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const source = resolve("node_modules/@polyglot-sql/sdk/dist/polyglot_sql.wasm");
const destination = resolve("../internal/sqlformat/polyglot_sql.wasm");

await stat(source).catch((error) => {
  throw new Error(`Polyglot SQL WASM not found at ${source}. Run corepack pnpm install in web/.`, {
    cause: error,
  });
});

await mkdir(dirname(destination), { recursive: true });
if (process.argv.includes("--check")) {
  const [sourceBytes, destinationBytes] = await Promise.all([
    readFile(source),
    readFile(destination),
  ]);
  if (!sourceBytes.equals(destinationBytes)) {
    throw new Error(
      `Embedded Polyglot SQL WASM does not match ${source}. Run corepack pnpm sync:polyglot-wasm in web/.`,
    );
  }
  console.log(`Verified ${destination} matches ${source}`);
  process.exit(0);
}
await copyFile(source, destination);
console.log(`Copied ${source} to ${destination}`);
