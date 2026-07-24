#!/usr/bin/env node

// Generate the terminal sessions embedded in the CLI docs from the current
// Renart binary. The casts are deterministic asciicast v2 files: commands are
// executed for real against a freshly scaffolded project, while playback timing
// is authored here so documentation diffs do not depend on machine speed.

import { execFile } from "node:child_process";
import { access, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const binaryPath = path.resolve(
  process.env.RENART_DOCS_BINARY ??
    path.join(repoRoot, ".tmp", "docs-cli-recordings", "renart"),
);
const outputDir = path.resolve(
  process.env.RENART_CLI_RECORDINGS_DIR ??
    path.join(repoRoot, "docs", "public", "cli-recordings"),
);
const fixedWindow = [
  "--start-date",
  "2026-07-22T00:00:00Z",
  "--end-date",
  "2026-07-23T00:00:00Z",
];
const shellPrompt = "\u001b[1;32m$\u001b[0m ";

const sessions = [
  {
    file: "workflow.cast",
    title: "Inspect and validate a Renart pipeline",
    width: 104,
    height: 16,
    commands: [
      {
        marker: "List assets",
        display: "renart ls assets",
        args: ["ls", "assets"],
      },
      {
        marker: "Type-check",
        display: "renart type-check retail",
        args: ["type-check", "retail"],
      },
      {
        marker: "Review the plan",
        display:
          "renart plan retail --all --start-date 2026-07-22T00:00:00Z --end-date 2026-07-23T00:00:00Z",
        args: [
          "plan",
          "retail",
          "--all",
          ...fixedWindow,
          "--execution-time",
          "2026-07-23T12:00:00Z",
        ],
      },
    ],
  },
  {
    file: "render.cast",
    title: "Preview a Renart asset",
    width: 104,
    height: 34,
    commands: [
      {
        marker: "Render an asset",
        display:
          "renart render analytics.daily_revenue --start-date 2026-07-22T00:00:00Z --end-date 2026-07-23T00:00:00Z",
        args: [
          "render",
          "analytics.daily_revenue",
          ...fixedWindow,
          "--execution-time",
          "2026-07-23T12:00:00Z",
        ],
      },
    ],
  },
];

await access(binaryPath).catch(() => {
  throw new Error(
    `Renart binary not found at ${binaryPath}. Run "make cli-recordings" or set RENART_DOCS_BINARY.`,
  );
});

const userSuffix =
  typeof process.getuid === "function" ? String(process.getuid()) : "user";
const workspaceRoot = path.join(
  tmpdir(),
  `renart-cli-docs-workspace-${userSuffix}`,
);
const runtimeRoot = path.join(
  tmpdir(),
  `renart-cli-docs-runtime-${userSuffix}`,
);
const recordingBaseEnv = { ...process.env };
delete recordingBaseEnv.NO_COLOR;
delete recordingBaseEnv.CLICOLOR;
delete recordingBaseEnv.CLICOLOR_FORCE;
const commandEnv = {
  ...recordingBaseEnv,
  CLICOLOR_FORCE: "1",
  COLUMNS: "104",
  GIT_AUTHOR_DATE: "2026-07-22T00:00:00Z",
  GIT_AUTHOR_EMAIL: "docs@getrenart.com",
  GIT_AUTHOR_NAME: "Renart Docs",
  GIT_COMMITTER_DATE: "2026-07-22T00:00:00Z",
  GIT_COMMITTER_EMAIL: "docs@getrenart.com",
  GIT_COMMITTER_NAME: "Renart Docs",
  LINES: "34",
  TERM: "xterm-256color",
  TZ: "UTC",
  XDG_CONFIG_HOME: path.join(runtimeRoot, "config"),
  RENART_PROJECTS_REGISTRY: path.join(runtimeRoot, "projects.json"),
};

try {
  await rm(workspaceRoot, { recursive: true, force: true });
  await rm(runtimeRoot, { recursive: true, force: true });
  await execute(["init", workspaceRoot, "--template", "retail"], repoRoot);
  await writeFile(
    path.join(workspaceRoot, ".renart", "project.yml"),
    "id: 00000000-0000-4000-8000-000000000001\nname: cli-recording-demo\n",
    "utf8",
  );
  await execFileAsync(
    "git",
    ["-C", workspaceRoot, "add", ".renart/project.yml"],
    {
      env: commandEnv,
    },
  );
  await execFileAsync(
    "git",
    ["-C", workspaceRoot, "commit", "--amend", "--no-edit"],
    {
      env: commandEnv,
    },
  );
  await mkdir(outputDir, { recursive: true });

  for (const session of sessions) {
    const events = [];
    let time = 0.2;
    let sawColoredCommandOutput = false;
    for (const command of session.commands) {
      events.push([time, "m", command.marker]);
      time = appendTypedCommand(events, time, command.display);

      const output = stableRecordingOutput(
        await execute(command.args, workspaceRoot),
      );
      assertPublishable(output, command.display);
      sawColoredCommandOutput ||= containsANSI(output);
      events.push([time, "o", terminalLines(output)]);
      time += Math.min(1.25, Math.max(0.55, output.split("\n").length * 0.025));
      events.push([time, "o", "\r\n"]);
      time += 0.35;
    }

    if (!sawColoredCommandOutput) {
      throw new Error(
        `Refusing to publish ${session.file}: command output did not contain ANSI color`,
      );
    }

    events.push([time, "o", shellPrompt]);
    time += 0.2;

    const header = {
      version: 2,
      width: session.width,
      height: session.height,
      duration: Number((time + 0.25).toFixed(3)),
      idle_time_limit: 1.5,
      title: session.title,
      env: { SHELL: "/bin/sh", TERM: "xterm-256color" },
    };
    const cast = [
      JSON.stringify(header),
      ...events.map(([eventTime, code, data]) =>
        JSON.stringify([Number(eventTime.toFixed(3)), code, data]),
      ),
      "",
    ].join("\n");
    await writeFile(path.join(outputDir, session.file), cast, "utf8");
  }

  console.log(`generated ${sessions.length} CLI recordings in ${outputDir}`);
} finally {
  await rm(workspaceRoot, { recursive: true, force: true });
  await rm(runtimeRoot, { recursive: true, force: true });
}

async function execute(args, cwd) {
  try {
    const result = await execFileAsync(binaryPath, args, {
      cwd,
      env: commandEnv,
      encoding: "utf8",
      maxBuffer: 8 * 1024 * 1024,
    });
    return `${result.stdout}${result.stderr}`;
  } catch (error) {
    const stdout = typeof error.stdout === "string" ? error.stdout : "";
    const stderr = typeof error.stderr === "string" ? error.stderr : "";
    throw new Error(
      `Command failed: renart ${args.join(" ")}\n${stdout}${stderr}`,
      { cause: error },
    );
  }
}

function assertPublishable(output, command) {
  if (/bruin/i.test(output)) {
    throw new Error(
      `Command output for "${command}" contains an internal engine name and cannot be published.`,
    );
  }
}

function stableRecordingOutput(output) {
  // The source identity includes the scaffold commit. Keep the truthful source
  // kind while replacing this run-specific short hash with an explicit label;
  // all command results and rendered operation bodies remain verbatim.
  return output.replace(
    /^(Source: working(?:_| )tree)([^\r\n]*?)([0-9a-f]{8})([^\r\n]*)$/gm,
    "$1$2<current>$4",
  );
}

function terminalLines(output) {
  return output
    .replaceAll("\r\n", "\n")
    .replaceAll("\r", "\n")
    .replace(/\n?$/, "\n")
    .replaceAll("\n", "\r\n");
}

function containsANSI(value) {
  return /\u001b\[[0-?]*[ -/]*[@-~]/.test(value);
}

function typingDelay(character, index, seed, command) {
  const jitterSeed = (Math.imul(seed ^ index, 1664525) + 1013904223) >>> 0;
  const jitter = 0.82 + (jitterSeed % 41) / 100;
  const kind = characterKind(character);
  const previousKind = index > 0 ? characterKind(command[index - 1]) : null;

  let base;
  if (kind === "space") {
    base = 0.19;
  } else if (kind === "number") {
    base = 0.1;
  } else if (kind === "uppercase") {
    base = 0.08;
  } else if (kind === "special") {
    base = 0.11;
  } else {
    base = 0.065;
  }

  // Humans type in bursts, but switching into or out of a number/symbol run
  // usually adds a small cognitive and hand-position pause.
  if (
    previousKind !== null &&
    previousKind !== kind &&
    (kind === "number" ||
      kind === "special" ||
      previousKind === "number" ||
      previousKind === "special")
  ) {
    base += 0.03;
  }

  return base * jitter;
}

function characterKind(character) {
  if (/\s/.test(character)) return "space";
  if (/[0-9]/.test(character)) return "number";
  if (/[A-Z]/.test(character)) return "uppercase";
  if (/[a-z]/.test(character)) return "letter";
  return "special";
}

function appendTypedCommand(events, startTime, command) {
  events.push([startTime, "o", shellPrompt]);

  const seed = [...command].reduce(
    (sum, character) => (sum * 31 + character.charCodeAt(0)) >>> 0,
    17,
  );
  let time = startTime + 0.14;
  for (const [index, character] of [...command].entries()) {
    events.push([time, "o", character]);
    time += typingDelay(character, index, seed, command);
  }
  events.push([time, "o", "\r\n"]);

  return time + 0.28;
}
