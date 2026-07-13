import { useEffect, useState } from 'react'
import { muesli } from '@/api'
import { EmptyState } from './EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import type { InsightsResponse, MeetingCountByDay, MeetingHoursByWeek, PersonWithMeetingCount, CompanyWithMeetingCount, FolderWithMeetingCount } from '../../shared/types'

const DAY_FORMATTER = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' })
const WEEK_FORMATTER = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' })

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

function formatCount(count: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(count)
}

function formatHours(hours: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 }).format(hours)
}

function formatDayLabel(day: string): string {
  return DAY_FORMATTER.format(new Date(day))
}

function formatWeekLabel(weekStart: string): string {
  return WEEK_FORMATTER.format(new Date(weekStart))
}

function entityLabel(entity: { display_name?: string; primary_email?: string; name?: string; domain?: string; id: string }): string {
  if ('display_name' in entity) return entity.display_name || entity.primary_email || 'Untitled person'
  if ('name' in entity) return entity.name || entity.domain || 'Untitled entity'
  return entity.domain || entity.name || 'Untitled folder'
}

function MeetingChart({
  data,
}: {
  data: MeetingCountByDay[]
}) {
  if (data.length === 0) {
    return (
      <figure className="rounded-[var(--radius)] border border-border bg-card p-4">
        <div className="mb-3 flex items-baseline justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">Meetings over time</h2>
            <p className="text-xs text-muted-foreground">Daily meetings for the selected range.</p>
          </div>
        </div>
        <p className="text-sm text-muted-foreground">No meetings in this range.</p>
      </figure>
    )
  }

  const width = 720
  const height = 220
  const padding = { top: 18, right: 18, bottom: 38, left: 18 }
  const chartWidth = width - padding.left - padding.right
  const chartHeight = height - padding.top - padding.bottom
  const maxCount = Math.max(1, ...data.map((point) => point.count))
  const barWidth = data.length > 0 ? chartWidth / data.length : chartWidth

  return (
    <figure className="rounded-[var(--radius)] border border-border bg-card p-4">
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Meetings over time</h2>
          <p className="text-xs text-muted-foreground">Daily meetings for the selected range.</p>
        </div>
        <p className="text-xs text-muted-foreground">{data.length} day{data.length === 1 ? '' : 's'}</p>
      </div>
      <svg
        role="img"
        aria-label="Meetings over time chart"
        viewBox={`0 0 ${width} ${height}`}
        className="h-56 w-full"
      >
        <title>Meetings over time</title>
        <desc>Vertical bars showing the number of meetings for each day in the range.</desc>
        <line
          x1={padding.left}
          y1={height - padding.bottom}
          x2={width - padding.right}
          y2={height - padding.bottom}
          stroke="currentColor"
          strokeOpacity="0.18"
        />
        {data.map((point, index) => {
          const ratio = point.count / maxCount
          const heightPx = Math.max(2, ratio * chartHeight)
          const x = padding.left + index * barWidth + barWidth * 0.18
          const y = height - padding.bottom - heightPx
          const label = `${formatDayLabel(point.day)}: ${formatCount(point.count)} meetings`
          return (
            <g key={point.day} aria-label={label}>
              <title>{label}</title>
              <rect
                x={x}
                y={y}
                width={Math.max(2, barWidth * 0.64)}
                height={heightPx}
                rx={4}
                fill="hsl(var(--primary))"
              />
              <text
                x={x + Math.max(2, barWidth * 0.32)}
                y={height - 12}
                textAnchor="middle"
                className="fill-muted-foreground"
                style={{ fontSize: 10 }}
              >
                {formatDayLabel(point.day)}
              </text>
              <text
                x={x + Math.max(2, barWidth * 0.32)}
                y={Math.max(16, y - 4)}
                textAnchor="middle"
                className="fill-foreground"
                style={{ fontSize: 10, fontWeight: 600 }}
              >
                {formatCount(point.count)}
              </text>
            </g>
          )
        })}
      </svg>
      <ul className="sr-only">
        {data.map((point) => (
          <li key={point.day}>{formatDayLabel(point.day)}: {formatCount(point.count)} meetings</li>
        ))}
      </ul>
    </figure>
  )
}

function WeeklyHoursChart({
  data,
}: {
  data: MeetingHoursByWeek[]
}) {
  if (data.length === 0) {
    return <p className="text-sm text-muted-foreground">No weekly hours in this range.</p>
  }

  const maxHours = Math.max(1, ...data.map((point) => point.hours))

  return (
    <ul className="flex flex-col gap-3">
      {data.map((point) => {
        const ratio = point.hours / maxHours
        const label = `${formatWeekLabel(point.week_start)}: ${formatHours(point.hours)} hours`
        return (
          <li key={point.week_start} aria-label={label} className="grid gap-1">
            <div className="flex items-baseline justify-between gap-3">
              <span className="truncate text-sm font-medium">{formatWeekLabel(point.week_start)}</span>
              <span className="shrink-0 text-sm text-muted-foreground">{formatHours(point.hours)}h</span>
            </div>
            <div className="h-2 rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary"
                style={{ width: `${Math.max(2, ratio * 100)}%` }}
                title={label}
              />
            </div>
          </li>
        )
      })}
    </ul>
  )
}

function TopBarList<T extends { count: number }>(
  {
    items,
    emptyLabel,
    getLabel,
  }: {
    items: T[]
    emptyLabel: string
    getLabel: (item: T) => string
  },
) {
  if (items.length === 0) {
    return <p className="text-sm text-muted-foreground">{emptyLabel}</p>
  }

  const maxCount = Math.max(1, ...items.map((item) => item.count))

  return (
    <ul className="flex flex-col gap-3">
      {items.map((item, index) => {
        const label = getLabel(item)
        const ratio = item.count / maxCount
        return (
          <li
            key={`${label}-${index}`}
            aria-label={`${label}: ${formatCount(item.count)} meetings`}
            className="grid gap-1"
          >
            <div className="flex items-baseline justify-between gap-3">
              <span className="truncate text-sm font-medium">{label}</span>
              <span className="shrink-0 text-sm text-muted-foreground">{formatCount(item.count)}</span>
            </div>
            <div className="h-2 rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary"
                style={{ width: `${Math.max(2, ratio * 100)}%` }}
                title={`${label}: ${formatCount(item.count)} meetings`}
              />
            </div>
          </li>
        )
      })}
    </ul>
  )
}

function SummaryCard({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint?: string
}) {
  return (
    <div className="rounded-[var(--radius)] border border-border bg-card p-4">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-2 text-2xl font-semibold">{value}</p>
      {hint ? <p className="mt-1 text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

function InsightsSkeleton() {
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <div>
        <div className="mb-2 h-8 w-40"><Skeleton className="h-full w-full" /></div>
        <div className="h-4 w-72"><Skeleton className="h-full w-full" /></div>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
      <Skeleton className="h-72 w-full" />
      <div className="grid gap-4 lg:grid-cols-3">
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-56 w-full" />
      </div>
    </div>
  )
}

export function InsightsScreen() {
  const [insights, setInsights] = useState<InsightsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    void muesli.getInsights()
      .then((data) => {
        if (cancelled) return
        setInsights(data)
        setError(null)
      })
      .catch((err) => {
        if (cancelled) return
        setInsights(null)
        setError(errorMessage(err))
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (insights === null && error === null) {
    return <InsightsSkeleton />
  }

  if (error) {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <h1 className="mb-1 font-serif text-xl font-semibold">Insights</h1>
        <p className="mb-4 text-sm text-muted-foreground">Meeting trends and top collaborators across the last 30 days.</p>
        <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          <p className="font-medium">Could not load insights</p>
          <p className="mt-1 break-words">{error}</p>
        </div>
      </div>
    )
  }

  const data = insights ?? {
    meetings_per_day: [],
    total_hours: 0,
    hours_per_week: [],
    top_people: [],
    top_companies: [],
    top_folders: [],
  }
  const totalMeetings = data.meetings_per_day.reduce((sum, point) => sum + point.count, 0)
  const hasNoData =
    data.meetings_per_day.length === 0 &&
    data.hours_per_week.length === 0 &&
    data.top_people.length === 0 &&
    data.top_companies.length === 0 &&
    data.top_folders.length === 0 &&
    data.total_hours === 0

  if (hasNoData) {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <h1 className="mb-1 font-serif text-xl font-semibold">Insights</h1>
        <p className="mb-4 text-sm text-muted-foreground">Meeting trends and top collaborators across the last 30 days.</p>
        <EmptyState
          title="No insights yet"
          hint="As meetings and linked notes accumulate, summary cards and charts will appear here."
        />
      </div>
    )
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <div>
        <h1 className="font-serif text-xl font-semibold">Insights</h1>
        <p className="text-sm text-muted-foreground">Meeting trends and top collaborators across the last 30 days.</p>
      </div>

      <section aria-label="Summary">
        <div className="grid gap-3 md:grid-cols-3">
          <SummaryCard label="Total meetings" value={formatCount(totalMeetings)} hint="Sum of meetings per day" />
          <SummaryCard label="Total hours" value={formatHours(data.total_hours)} hint="Across all meetings" />
          <SummaryCard label="Distinct people" value={formatCount(data.top_people.length)} hint="People seen in the range" />
        </div>
      </section>

      <section className="grid gap-6 lg:grid-cols-2" aria-label="Charts">
        <MeetingChart data={data.meetings_per_day} />
        <div className="rounded-[var(--radius)] border border-border bg-card p-4">
          <div className="mb-3 flex items-baseline justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold">Weekly hours</h2>
              <p className="text-xs text-muted-foreground">Total meeting hours by calendar week.</p>
            </div>
            <p className="text-xs text-muted-foreground">{formatHours(data.total_hours)}h total</p>
          </div>
          <WeeklyHoursChart data={data.hours_per_week} />
        </div>
      </section>

      <section className="grid gap-4 lg:grid-cols-3">
        <div className="rounded-[var(--radius)] border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-semibold">Top people</h2>
          <TopBarList<PersonWithMeetingCount>
            items={data.top_people}
            emptyLabel="No people in this range."
            getLabel={(person) => entityLabel(person)}
          />
        </div>
        <div className="rounded-[var(--radius)] border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-semibold">Top companies</h2>
          <TopBarList<CompanyWithMeetingCount>
            items={data.top_companies}
            emptyLabel="No companies in this range."
            getLabel={(company) => entityLabel(company)}
          />
        </div>
        <div className="rounded-[var(--radius)] border border-border bg-card p-4">
          <h2 className="mb-3 text-sm font-semibold">Top folders</h2>
          <TopBarList<FolderWithMeetingCount>
            items={data.top_folders}
            emptyLabel="No folders in this range."
            getLabel={(folder) => entityLabel(folder)}
          />
        </div>
      </section>
    </div>
  )
}
