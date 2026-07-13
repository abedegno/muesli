import { monogram, monogramColor, type MonogramTone } from '@/lib/monogram'

const TILE: Record<MonogramTone, string> = {
  teal: 'bg-primary/15 text-primary',
  blue: 'bg-blue-500/15 text-blue-600 dark:text-blue-300',
  violet: 'bg-violet-500/15 text-violet-600 dark:text-violet-300',
  amber: 'bg-amber-500/15 text-amber-600 dark:text-amber-300',
  rose: 'bg-rose-500/15 text-rose-600 dark:text-rose-300',
  slate: 'bg-slate-500/15 text-slate-600 dark:text-slate-300',
}

export function MonogramAvatar({
  id,
  label,
  className = 'h-12 w-12 text-base',
}: {
  id: string
  label: string
  className?: string
}) {
  const { initial, tone } = monogram({ id, label })
  const { bg, fg } = monogramColor(label)
  return (
    <div
      className={`flex shrink-0 items-center justify-center rounded-[var(--radius)] font-semibold ${TILE[tone]} ${className}`}
      style={{ backgroundColor: bg, color: fg }}
    >
      {initial}
    </div>
  )
}
