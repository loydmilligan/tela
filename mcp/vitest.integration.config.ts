import { defineConfig } from "vitest/config";

// Live MCP <-> backend integration test (M16.C.2). Requires the Tela stack
// running on TELA_BASE_URL (default http://localhost:8780) with bootstrap
// admin credentials set via TELA_ADMIN_USERNAME / TELA_ADMIN_PASSWORD. Use
// `make test-mcp-integration` from the repo root — it brings the stack up,
// runs this config, and tears down.
export default defineConfig({
  test: {
    include: ["test/**/*.live.test.ts"],
    environment: "node",
    // 180s per test covers cold-build delay on the backend + the ~11 tool
    // calls. Default 5s would time out on a freshly-spun stack.
    testTimeout: 180_000,
    hookTimeout: 180_000,
    // Live tests share global state (the spawned MCP child, the seed) and
    // therefore must run serially in a single worker.
    pool: "forks",
    poolOptions: { forks: { singleFork: true } },
  },
});
