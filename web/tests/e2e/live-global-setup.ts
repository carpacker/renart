import { mkdirSync } from "node:fs";

export default function globalSetup() {
  const tempRoot = "/dev/shm/bruin-playwright";
  mkdirSync(tempRoot, { recursive: true });
  process.env.TMPDIR = tempRoot;
  process.env.TMP = tempRoot;
  process.env.TEMP = tempRoot;
}
