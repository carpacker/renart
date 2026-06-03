import { copyFile, mkdir, stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const source = resolve("node_modules/@polyglot-sql/sdk/dist/polyglot_sql.wasm");
const destination = resolve("../internal/web/sqlformat/polyglot_sql.wasm");

await stat(source).catch((error) => {
  throw new Error(`Polyglot SQL WASM not found at ${source}. Run corepack pnpm install in web/.`, { cause: error });
});

await mkdir(dirname(destination), { recursive: true });
await copyFile(source, destination);
console.log(`Copied ${source} to ${destination}`);
