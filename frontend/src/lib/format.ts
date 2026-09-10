/**
 * Lightroom sends capture times without a time zone ("2026-07-01T10:11:12").
 * They are shown as wall-clock time, so parse them as local time.
 */
export function parseTakenAt(value?: string): Date | null {
  if (!value) return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

const dateFormat = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' })

export function formatDate(value?: string): string {
  const d = parseTakenAt(value)
  return d ? dateFormat.format(d) : ''
}

/** "3 May 2026" or "1 May 2026 – 3 May 2026". */
export function formatDateRange(from?: string, to?: string): string {
  const a = formatDate(from)
  const b = formatDate(to)
  if (!a) return b
  if (!b || a === b) return a
  return `${a} – ${b}`
}

export function photoCount(n: number): string {
  return n === 1 ? '1 photo' : `${n} photos`
}
