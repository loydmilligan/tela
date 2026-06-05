// BrandMark — the tela folded-paper "t" app mark. Indigo rounded tile + white
// sheet + a dark fold pocket. Colors come from theme tokens (--accent /
// --accent-fg) so it tracks the active theme; the fold is a fixed dark wash.
// Geometry is shared verbatim with the favicon / landing header (512 viewBox).
export function BrandMark({
  size = 24,
  className,
}: {
  size?: number
  className?: string
}) {
  return (
    <svg
      viewBox="0 0 512 512"
      width={size}
      height={size}
      fill="none"
      role="img"
      aria-label="tela"
      className={className}
    >
      <rect width="512" height="512" rx="112" fill="var(--accent)" />
      <path
        fill="var(--accent-fg)"
        d="M150 240 L196 188 Q205 178 218 178 H356 Q378 178 366 200 L332 240 Q325 250 312 250 H162 Q140 250 150 240 Z"
      />
      <path
        fill="var(--accent-fg)"
        d="M238 250 H296 Q300 250 300 268 V396 Q300 414 281 410 L245 402 Q226 398 226 380 V272 Q226 252 238 250 Z"
      />
      <path
        fill="#1e1b4b"
        fillOpacity="0.4"
        d="M250 250 H312 Q325 250 332 240 L304 270 Q297 278 297 290 V324 Z"
      />
    </svg>
  )
}
