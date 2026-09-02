import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 45_000,
  use: {
    baseURL: process.env.RELAY_TEST_URL ?? "http://127.0.0.1:8080",
    headless: true,
    ...(process.env.RELAY_BROWSER_PATH
      ? { launchOptions: { executablePath: process.env.RELAY_BROWSER_PATH } }
      : {}),
  },
  reporter: "list",
});
