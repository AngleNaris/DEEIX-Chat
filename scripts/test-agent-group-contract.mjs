import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const backendRoot = path.join(repoRoot, "backend");
const frontendRoot = path.join(repoRoot, "frontend");
const goModuleCache = path.join(repoRoot, ".gomodcache", "agent-group-contract");
const goBuildCache = path.join(repoRoot, ".gocache", "agent-group-contract");

function run(command, args, cwd) {
  const result = spawnSync(command, args, {
    cwd,
    stdio: "inherit",
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

function commandAvailable(command, args = ["version"]) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    stdio: "ignore",
  });
  return !result.error && result.status === 0;
}

run(
  process.execPath,
  ["--test", "features/chat/model/conversation-request-model.test.mjs"],
  frontendRoot,
);

const goTestArgs = [
  "test",
  "./internal/domain/conversation",
  "./internal/transport/http/conversation",
  "-run",
  "AgentGroup.*Model",
  "-count=1",
];

if (commandAvailable("go")) {
  run("go", goTestArgs, backendRoot);
} else if (commandAvailable("docker", ["version"])) {
  mkdirSync(goModuleCache, { recursive: true });
  mkdirSync(goBuildCache, { recursive: true });
  run(
    "docker",
    [
      "run",
      "--rm",
      "--mount",
      `type=bind,src=${backendRoot},dst=/app`,
      "--mount",
      `type=bind,src=${goModuleCache},dst=/go/pkg/mod`,
      "--mount",
      `type=bind,src=${goBuildCache},dst=/root/.cache/go-build`,
      "-w",
      "/app",
      "golang:1.26.5-bookworm",
      "go",
      ...goTestArgs,
    ],
    repoRoot,
  );
} else {
  console.error("Agent Group contract tests require either Go or Docker.");
  process.exit(1);
}
