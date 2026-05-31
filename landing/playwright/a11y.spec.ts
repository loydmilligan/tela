/**
 * Gate: accessibility (WCAG 2.2 AA) via axe-core (cila Tier-1).
 *
 * axe-core computes contrast and ARIA correctness against the *flattened,
 * rendered* DOM, so we run it post-render across the viewport matrix (a layout
 * that passes at 1440 can fail contrast/target rules at 360). Full results are
 * attached to the report as JSON for triage; the test hard-fails on any
 * violation.
 *
 * Automation only catches ~25-40% of WCAG issue *types* — keep manual
 * screen-reader spot-checks (NVDA / VoiceOver / TalkBack) per release. This
 * gate is the floor, not the ceiling.
 */

import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { ROUTES, VIEWPORTS, settle } from './_helpers';

const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

for (const vp of VIEWPORTS) {
  for (const route of ROUTES) {
    test.describe(`a11y @ ${vp.name} (${vp.width}) — ${route}`, () => {
      test.use({ viewport: { width: vp.width, height: vp.height } });

      test(`no WCAG 2.2 AA violations`, async ({ page }, testInfo) => {
        await page.goto(route);
        await settle(page);

        const results = await new AxeBuilder({ page }).withTags(TAGS).analyze();

        // Attach the full axe run to the HTML/JSON report for triage.
        await testInfo.attach('axe-results.json', {
          body: JSON.stringify(results, null, 2),
          contentType: 'application/json',
        });

        const summary = results.violations.map(
          (v) =>
            `[${v.impact ?? 'n/a'}] ${v.id}: ${v.help} (${v.nodes.length} node(s)) — ${v.helpUrl}`
        );
        expect(
          results.violations,
          summary.length ? `axe violations:\n${summary.join('\n')}` : ''
        ).toEqual([]);
      });
    });
  }
}
