import { uiText, formatDate } from '@/i18n'

function toDate(value: Date | string): Date {
  return value instanceof Date ? new Date(value.getTime()) : new Date(value)
}

function shortAbsoluteDate(date: Date): string {
  return formatDate(date, { day: 'numeric', month: 'short' })
}

export function relativeTime(date: Date | string, now: Date = new Date()): string {
  const created = toDate(date)
  if (Number.isNaN(created.getTime()) || Number.isNaN(now.getTime())) return ''

  const elapsed = Math.max(0, now.getTime() - created.getTime())
  const seconds = Math.floor(elapsed / 1000)
  if (seconds < 60) return uiText('just now')

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return uiText('{count}m ago', { count: minutes })

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return uiText('{count}h ago', { count: hours })

  const days = Math.floor(hours / 24)
  if (days < 7) return uiText('{count}d ago', { count: days })

  return shortAbsoluteDate(created)
}
