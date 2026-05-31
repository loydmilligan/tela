/**
 * Gate: reduced-motion fallback (cila Tier-1 — HARD).
 *
 * Under `prefers-reduced-motion: reduce`, non-essential motion must be *guarded*:
 * a page that keeps a continuous animation loop (raw rAF/canvas churn, an R3F
 * `frameloop` still rendering, etc.) MUST also expose a documented static/instant
 * substitute so reduced-motion users get a still experience. We require, on each
 * route × viewport, ONE of:
 *
 *   (a) the page is effectively static under reduce — no continuous
 *       requestAnimationFrame loop driving the render; OR
 *   (b) a documented static fallback element `[data-reduced-fallback]` is present
 *       and visible (the author intentionally swapped the animated hero for a
 *       still image / poster / static gradient).
 *
 * This gate is intentionally narrow: it only checks that a reduced-motion guard
 * exists. It does NOT police *how* motion is implemented — animating width/
 * height/top/left/margin, `transition: all`, or "non-composited" properties is
 * a performance/implementation choice, not a reduced-motion accessibility fact,
 * and is out of scope here. An intentional, rich hero animation is fine; it just
 * has to either stop under reduce or ship a `[data-reduced-fallback]`.
 *
 * It measures real rAF activity in the page, so it can't be charmed by a
 * self-reported flag.
 */

import { test, expect, type Page } from '@playwright/test';
import { ROUTES, VIEWPORTS, settle } from './_helpers';

/**
 * Real-time window (ms) over which we sample rAF activity after the page has
 * settled. At 60fps a continuous loop fires ~ window/16.7 callbacks.
 */
const SAMPLE_MS = 1000;

/**
 * Max rAF callbacks allowed within SAMPLE_MS for a page to count as "static".
 * A continuous 60fps loop fires ~60; a settled page fires ~0. We allow a small
 * budget for one-shot reveals / late layout work that schedule a handful of
 * frames and then stop.
 */
const MAX_RAF_IN_WINDOW = 10;

/** Install the rAF counter before any page script executes. */
async function instrumentRaf(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const w = window as unknown as { __cilaRaf: number };
    w.__cilaRaf = 0;
    const orig = window.requestAnimationFrame.bind(window);
    window.requestAnimationFrame = (cb: FrameRequestCallback): number => {
      w.__cilaRaf++;
      return orig(cb);
    };
  });
}

async function rafCount(page: Page): Promise<number> {
  return page.evaluate(() => (window as unknown as { __cilaRaf: number }).__cilaRaf);
}

for (const vp of VIEWPORTS) {
  for (const route of ROUTES) {
    test.describe(`reduced-motion @ ${vp.name} (${vp.width}) — ${route}`, () => {
      test.use({
        viewport: { width: vp.width, height: vp.height },
        // The whole point of the gate: emulate a user who asked for less motion.
        reducedMotion: 'reduce',
      });

      test('non-essential motion is guarded by a reduced-motion fallback', async ({ page }) => {
        await instrumentRaf(page);
        await page.goto(route);
        await settle(page);

        // (b) Documented static fallback — author opted into an explicit
        // reduced-motion substitute. If it's present & visible, the page passes
        // regardless of any loop (e.g. an ambient compositor-only background).
        const fallback = page.locator('[data-reduced-fallback]');
        const fallbackVisible =
          (await fallback.count()) > 0 && (await fallback.first().isVisible());

        // (a) Measure real rAF activity across a fixed window. Reset the counter
        // first so we only count callbacks scheduled *after* settle().
        await page.evaluate(() => {
          (window as unknown as { __cilaRaf: number }).__cilaRaf = 0;
        });
        await page.waitForTimeout(SAMPLE_MS);
        const fired = await rafCount(page);

        const isStatic = fired <= MAX_RAF_IN_WINDOW;

        expect(
          fallbackVisible || isStatic,
          fallbackVisible
            ? ''
            : `Under prefers-reduced-motion:reduce, ${route} kept a continuous ` +
                `animation loop (${fired} requestAnimationFrame callbacks in ` +
                `${SAMPLE_MS}ms) with no reduced-motion fallback. Either stop the ` +
                `loop under reduce (e.g. R3F frameloop="never"/"demand", pause the ` +
                `rAF loop) or render an explicit static substitute marked ` +
                `[data-reduced-fallback]. (This gate checks only that the fallback ` +
                `exists — it does not police which CSS properties you animate.)`
        ).toBeTruthy();
      });
    });
  }
}
