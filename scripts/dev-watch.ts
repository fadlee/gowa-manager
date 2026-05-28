/*
  Cross-platform integrated dev runner for Bun.
  - Starts the client in Vite watch mode and the server in Bun watch mode
  - Avoids shell operators, which break on Windows
  - Shuts both child processes down together on exit
*/

import treeKill from "tree-kill";

const SERVER_ENTRY = "src/index.ts";
const CLIENT_DIR = "client";
const CLIENT_WATCH_SCRIPT = "build:watch";

let server: Bun.Subprocess | null = null;
let client: Bun.Subprocess | null = null;
let shuttingDown = false;

function spawnProcess(cmd: string[], cwd?: string) {
  return Bun.spawn(cmd, {
    cwd,
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
    env: {
      ...process.env,
      NODE_ENV: process.env.NODE_ENV || "development",
    },
  });
}

function startServer() {
  if (server) return;
  server = spawnProcess(["bun", "--watch", "run", SERVER_ENTRY]);
  console.log(`[dev-watch] Server watcher started (pid=${server.pid}).`);
}

function startClient() {
  if (client) return;
  client = spawnProcess(["bun", "run", CLIENT_WATCH_SCRIPT], CLIENT_DIR);
  console.log(`[dev-watch] Client watcher started (pid=${client.pid}).`);
}

function stopProcess(processRef: Bun.Subprocess | null, name: string, signal: "SIGINT" | "SIGTERM" | "SIGKILL" = "SIGINT") {
  if (!processRef) return;

  console.log(`[dev-watch] Stopping ${name} (pid=${processRef.pid}) ...`);
  treeKill(processRef.pid, signal, (err) => {
    if (!err) return;
    if ((err as NodeJS.ErrnoException).code === "ESRCH") return;
    console.warn(`[dev-watch] Failed to tree-kill ${name}:`, err);
  });
}

function cleanupAndExit(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;

  stopProcess(client, "client watcher", "SIGINT");
  stopProcess(server, "server watcher", "SIGINT");
  client = null;
  server = null;

  setTimeout(() => process.exit(code), 100);
}

process.on("SIGINT", () => {
  console.log("\n[dev-watch] SIGINT received");
  cleanupAndExit(0);
});

process.on("SIGTERM", () => {
  console.log("\n[dev-watch] SIGTERM received");
  cleanupAndExit(0);
});

startClient();
startServer();
