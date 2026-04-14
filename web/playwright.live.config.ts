import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  testMatch: "**/*.live.spec.ts",
  outputDir: "/dev/shm/bruin-playwright/test-results",
  globalSetup: "./tests/e2e/live-global-setup.ts",
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  use: {
    trace: "on-first-retry",
    video: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium-live",
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "mobile-chrome-live",
      use: { ...devices["Pixel 7"] },
    },
  ],
});
