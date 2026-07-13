const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function toDate(value: Date | string): Date {
  return value instanceof Date ? new Date(value.getTime()) : new Date(value)
}

function shortAbsoluteDate(date: Date): string {
  return `${date.getDate()} ${MONTHS[date.getMonth()]}`
}

export function relativeTime(date: Date | string, now: Date = new Date()): string {
  const created = toDate(date)
  if (Number.isNaN(created.getTime()) || Number.isNaN(now.getTime())) return ''

  const elapsed = Math.max(0, now.getTime() - created.getTime())
  const seconds = Math.floor(elapsed / 1000)
  if (seconds < 60) return 'just now'

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`

  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}d ago`

  return shortAbsoluteDate(created)
}
