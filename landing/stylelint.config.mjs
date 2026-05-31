/**
 * Stylelint config for cila — the static source-layer token guard.
 *
 * This complements the rendered-layer token-conformance Playwright gate: that
 * one catches off-token colors that survive the *build*; this one catches raw
 * hex/rgb literals in *source* before they ever render. Belt and suspenders —
 * the static lint is cheap and runs first.
 *
 * Scope: hand-authored CSS (src/styles/**.css). The token file that *defines*
 * the palette legitimately contains hex/oklch literals, so it's ignored below.
 *
 * Tailwind arbitrary values (`bg-[#abc123]`, `p-[13px]`) live in className
 * strings inside .astro/.tsx files, NOT in CSS, so stylelint can't see them.
 * They are guarded two ways:
 *   1. The rendered token-conformance Playwright gate catches the *effect* of an
 *      arbitrary value (an off-token color that survives the build).
 *   2. Optionally add an ESLint rule (eslint-plugin-tailwindcss /
 *      `no-arbitrary-value`, or a `no-restricted-syntax` regex on `-[...]`)
 *      to fail arbitrary values at the markup layer. Wire it into `gate:tokens`
 *      if your repo uses heavy Tailwind utility authoring.
 */

/** @type {import('stylelint').Config} */
export default {
  extends: ['stylelint-config-standard'],
  plugins: ['stylelint-declaration-strict-value'],
  // The token-definition layer is where literals are allowed to exist.
  ignoreFiles: [
    '**/tokens.css',
    '**/theme.css',
    '**/*.generated.css',
    'node_modules/**',
  ],
  rules: {
    // ── Ban raw color literals in source: use a var(--token) instead. ────────
    // Covers legacy (rgb/hsl) AND modern wide-gamut color functions (oklch is
    // the cila default, plus color()/lab/lch) so a hand-authored
    // `color: oklch(0.6 0.15 150)` outside the token layer is caught. The
    // tokens.css/theme.css definition layer is exempt via ignoreFiles above.
    'color-no-hex': true,
    'color-named': 'never',
    'function-disallowed-list': [
      'rgb',
      'rgba',
      'hsl',
      'hsla',
      'oklch',
      'oklab',
      'color',
      'lab',
      'lch',
    ],

    // ── Force color/spacing props to be driven by custom properties. ─────────
    // `expression: '/^var\\(--.+\\)$/'` requires the value to be a token ref.
    // `auto-fix: false` — we report, never silently rewrite design intent.
    'scale-unlimited/declaration-strict-value': [
      [
        '/color$/',
        'fill',
        'stroke',
        'background',
        'background-color',
        'box-shadow',
      ],
      {
        ignoreValues: [
          'transparent',
          'currentColor',
          'inherit',
          'initial',
          'unset',
          'none',
        ],
        ignoreFunctions: false,
        disableFix: true,
        message:
          'Use a design token (var(--…)) instead of a literal color/shadow.',
      },
    ],

    // Tailwind v4 @theme / @apply / @layer etc. are valid at-rules.
    'at-rule-no-unknown': [
      true,
      {
        ignoreAtRules: [
          'theme',
          'apply',
          'layer',
          'variant',
          'custom-variant',
          'utility',
          'source',
          'config',
          'plugin',
          'reference',
          'tailwind',
        ],
      },
    ],
    // Tailwind's `theme()` / `--alpha()` helpers.
    'function-no-unknown': [true, { ignoreFunctions: ['theme', '--alpha', '--spacing'] }],

    // Keep it a guard, not a style nanny — these fight Tailwind/utility CSS.
    'custom-property-pattern': null,
    'selector-class-pattern': null,
    'no-descending-specificity': null,
  },
};
