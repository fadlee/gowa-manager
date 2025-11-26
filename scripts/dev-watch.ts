/*
  Debounced dev server watcher for Bun.
  - Watches the `src/` directory for changes
  - Waits 3 seconds after the last change before restarting the server
  - Cleanly kills child process on restart and on exit
*/

import { watch, FSWatcher } from "fs";
import treeKill from "tree-kill";

const DEBOUNCE_MS = 3000;

let server: Bun.Subprocess | null = null;
let debounceTimer: ReturnType<typeof setTimeout> | undefined;
let watcher: FSWatcher | null = null;
let shuttingDown = false;

function startServer() {
  // Start the Bun server process
  server = Bun.spawn(["bun", "run", "src/index.ts"], {
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
    env: {
      ...process.env,
      NODE_ENV: process.env.NODE_ENV || "development",
    },
  });
  console.log(`[dev-watch] Server started (pid=${server.pid}).`);
}

function stopServer(signal: "SIGINT" | "SIGTERM" | "SIGKILL" = "SIGINT") {
  if (server !== null) {
    console.log(`[dev-watch] Stopping server (pid=${server.pid}) ...`);
    treeKill(server.pid, signal, (err) => {
      if (err) {
        console.warn(`[dev-watch] Failed to tree-kill server:`, err);
      }
    });
    server = null;
  }
}

function scheduleRestart(reason: string) {
  if (shuttingDown) return;
  if (debounceTimer) clearTimeout(debounceTimer);
  console.log(`[dev-watch] Change detected (${reason}). Waiting ${DEBOUNCE_MS}ms before restart...`);
  debounceTimer = setTimeout(() => {
    if (shuttingDown) return;
    stopServer();
    startServer();
  }, DEBOUNCE_MS);
}

function setupWatcher() {
  watcher = watch(
    "src",
    { recursive: true },
    (eventType, filename) => {
      const path = filename ? `src/${filename}` : "src";
      // Ignore editor temp files (common patterns)
      if (/\.(swp|swx|tmp|DS_Store)$/i.test(path)) return;
      scheduleRestart(`${eventType}: ${path}`);
    }
  );
  console.log(`[dev-watch] Watching ./src for changes (recursive, debounce ${DEBOUNCE_MS}ms).`);
}

function cleanupAndExit(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  if (debounceTimer) clearTimeout(debounceTimer);
  try {
    if (watcher) watcher.close();
  } catch {}
  stopServer("SIGINT");
  // Small delay to allow child process to terminate
  setTimeout(() => process.exit(code), 50);
}

process.on("SIGINT", () => {
  console.log("\n[dev-watch] SIGINT received");
  cleanupAndExit(0);
});
process.on("SIGTERM", () => {
  console.log("\n[dev-watch] SIGTERM received");
  cleanupAndExit(0);
});
process.on("exit", (code) => {
  if (!shuttingDown) cleanupAndExit(code ?? 0);
});

// Start
startServer();
setupWatcher();
