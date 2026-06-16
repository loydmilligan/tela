import { useId } from 'react'
import { cn } from '../../lib/utils'

export interface SparklineProps {
  values: number[]
  width?: number
  height?: number
  // Draw a soft area fill under the line. Stroke + fill both inherit
  // `currentColor`, so the caller sets the hue via a text-color token class.
  area?: boolean
  className?: string
  ariaLabel?: string
}

// A tiny dependency-free SVG sparkline. Color comes from `currentColor` (set a
// text-[var(--…)] token on the wrapper); geometry scales to the value range.
export function Sparkline({
  values,
  width = 120,
  height = 32,
  area = true,
  className,
  ariaLabel,
}: SparklineProps) {
  const gradId = useId()
  const n = values.length
  if (n === 0) return <svg width={width} height={height} className={className} />

  const max = Math.max(...values)
  const min = Math.min(...values)
  const span = max - min || 1
  const pad = 1.5
  const stepX = n > 1 ? (width - pad * 2) / (n - 1) : 0
  const y = (v: number) => height - pad - ((v - min) / span) * (height - pad * 2)
  const x = (i: number) => pad + i * stepX

  const line = values.map((v, i) => `${x(i).toFixed(2)},${y(v).toFixed(2)}`).join(' ')
  const areaPath = `M ${x(0).toFixed(2)},${height} L ${line.split(' ').join(' L ')} L ${x(n - 1).toFixed(2)},${height} Z`

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      role="img"
      aria-label={ariaLabel}
      className={cn('overflow-visible', className)}
    >
      {area ? (
        <>
          <defs>
            <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="currentColor" stopOpacity="0.22" />
              <stop offset="100%" stopColor="currentColor" stopOpacity="0" />
            </linearGradient>
          </defs>
          <path d={areaPath} fill={`url(#${gradId})`} stroke="none" />
        </>
      ) : null}
      <polyline
        points={line}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
