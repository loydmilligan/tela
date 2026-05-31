/**
 * Gate: token conformance at the rendered layer (cila Tier-1).
 *
 * The design system is the test: we read the allow-set of colors from the
 * resolved CSS custom properties on `:root` (via getComputedStyle in-page),
 * then walk every visible element and assert its computed color /
 * background-color / border-color is a member of that set. This catches
 * hardcoded `#3b82f6`, Tailwind arbitrary values (`bg-[#abc123]`) and any
 * off-token color that survived the build.
 *
 * NOTE: Playwright's `expect(locator).toHaveCSS()` cannot read CSS custom
 * properties — it returns the raw `var(--x)` text or '' for `--prop` names.
 * Token allow-sets MUST be built with `page.evaluate` + `getComputedStyle`.
 * That's what `buildColorAllowSet` does.
 */

import { test, expect } from '@playwright/test';
import {
  ROUTES,
  buildColorAllowSet,
  canonicalizeColor,
  collectVisibleStyles,
  isOnTokenBase,
  settle,
} from './_helpers';

for (const route of ROUTES) {
  test.describe(`token conformance — ${route}`, () => {
    test(`colors are on-token @ ${route}`, async ({ page }) => {
      await page.goto(route);
      await settle(page);

      const allow = await buildColorAllowSet(page);
      expect(allow.length, 'no color tokens resolved off :root').toBeGreaterThan(0);

      const styles = await collectVisibleStyles(page);
      const offenders: string[] = [];

      const check = (selector: string, prop: string, raw: string) => {
        const c = canonicalizeColor(raw);
        // Skip empties, fully transparent, and non-resolvable keywords.
        if (!c || c === 'transparent' || c === 'currentcolor') return;
        // Membership is alpha-agnostic AND tolerant per channel: opacity
        // modifiers on a token (`bg-brand/35`, `text-fg-on-brand/80` → rgba /
        // color-mix-with-transparent) are permitted — only the token's base
        // r,g,b must match (within compositing/rounding tolerance), not alpha.
        if (!isOnTokenBase(c, allow)) offenders.push(`${selector} { ${prop}: ${raw} }`);
      };

      for (const el of styles) {
        check(el.selector, 'color', el.color);
        check(el.selector, 'background-color', el.backgroundColor);
        // Border colors only matter when a border is actually painted.
        if (el.hasVisibleBorder) {
          check(el.selector, 'border-top-color', el.borderTopColor);
          check(el.selector, 'border-right-color', el.borderRightColor);
          check(el.selector, 'border-bottom-color', el.borderBottomColor);
          check(el.selector, 'border-left-color', el.borderLeftColor);
        }
      }

      // De-dup for a readable failure message.
      const unique = [...new Set(offenders)];
      expect(
        unique,
        `${unique.length} off-token color value(s) found. Allowed (resolved from :root): ` +
          `${[...allow].sort().join(' | ')}`
      ).toEqual([]);
    });
  });
}
