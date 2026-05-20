import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    exclude: [
      "test/**/*.smoke.test.ts",
      "test/**/*.live.test.ts",
      "node_modules",
      "dist",
    ],
    environment: "node",
  },
});
