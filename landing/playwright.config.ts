/**
 * Playwright config for cila production gates.
 *
 * - BASE_URL is parameterizable (env), defaulting to Astro's dev/preview port.
 *   For Next.js set BASE_URL=http://localhost:3000 (and adjust webServer below
 *   or just point at an already-running server).
 * - The viewport matrix lives in the specs themselves (via test.use) so each
 *   gate runs across 360/768/1024/1440; the single `chromium` project here sets
 *   shared, deterministic defaults.
 * - webServer auto-starts the site for local runs; on CI we reuse a server that
 *   the pipeline already started when PLAYWRIGHT_NO_WEBSERVER is set.
 */

import { defineConfig, devices } from '@playwright/test';

const BASE_URL = process.env.BASE_URL ?? 'http://localhost:4321';

/** Command to boot the site under test. Override per stack via env. */
const WEB_SERVER_COMMAND =
  process.env.CILA_WEBSERVER_CMD ?? 'npm run preview'; // Astro preview; Next: `npm run start`

export default defineConfig({
  testDir: './playwright',
  // Visual baselines + any future snapshots live next to the specs.
  snapshotDir: './playwright/__snapshots__',
  outputDir: './.gate-results',

  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  // Generous per-test timeout: full-page screenshots across the matrix + axe.
  timeout: 60_000,
  expect: {
    timeout: 10_000,
    toHaveScreenshot: {
      animations: 'disabled',
      maxDiffPixelRatio: 0.01,
    },
  },

  reporter: [
    ['list'],
    ['html', { outputFolder: './.gate-report', open: 'never' }],
    ['json', { outputFile: './.gate-results/results.json' }],
    ...(process.env.CI ? [['github'] as const] : []),
  ],

  use: {
    baseURL: BASE_URL,
    // Lock device pixel ratio + color scheme so computed styles & screenshots
    // are reproducible across machines.
    deviceScaleFactor: 1,
    colorScheme: 'light',
    // Assess the SETTLED state: scroll/entrance reveals (opacity transitions) are
    // transient — axe evaluates the flattened composited tree, so a frame caught
    // mid-reveal blends text with its bg and reports false low-contrast. Reduced
    // motion is a real, fully-supported mode where every reveal resolves to its
    // rest state (opacity:1), which is the accessible state we actually ship.
    reducedMotion: 'reduce',
    timezoneId: 'UTC',
    locale: 'en-US',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      // reducedMotion must be re-asserted here: spreading the device preset
      // resets `use`, so the top-level reducedMotion wouldn't survive otherwise.
      use: { ...devices['Desktop Chrome'], reducedMotion: 'reduce' },
    },
  ],

  // Start the site for local runs. On CI, set PLAYWRIGHT_NO_WEBSERVER=1 if the
  // pipeline boots the server itself.
  webServer: process.env.PLAYWRIGHT_NO_WEBSERVER
    ? undefined
    : {
        command: WEB_SERVER_COMMAND,
        url: BASE_URL,
        reuseExistingServer: !process.env.CI,
        timeout: 120_000,
        stdout: 'pipe',
        stderr: 'pipe',
      },
});
